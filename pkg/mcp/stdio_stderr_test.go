package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestStdioClient_StderrIsLogged(t *testing.T) {
	var buf bytes.Buffer
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stdout).Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stderr).Write([]byte("fatal: refusing to continue\n")); err != nil {
		t.Fatal(err)
	}

	logBuffer := logging.NewLogBuffer(10)
	logger := slog.New(logging.NewBufferHandler(logBuffer, nil)).With("server", "crash")
	client := newTestStdioClient("crash", logger)
	pending := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[1] = pending
	client.responsesMu.Unlock()
	client.startStdioReaders(bytes.NewReader(buf.Bytes()))
	t.Cleanup(func() { _ = client.Close() })

	select {
	case resp := <-pending:
		if string(resp.Result) != `{"ok":true}` {
			t.Fatalf("result = %s", resp.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdout response did not arrive")
	}

	deadline := time.Now().Add(2 * time.Second)
	var entry logging.BufferedEntry
	for {
		found := false
		for _, candidate := range logBuffer.GetRecent(10) {
			if candidate.Message == "server stderr" && candidate.Level == "WARN" {
				entry = candidate
				found = true
			}
		}
		if found || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if entry.Message != "server stderr" || entry.Attrs["output"] != "fatal: refusing to continue" || entry.Attrs["server"] != "crash" {
		t.Fatalf("stderr log = %#v", logBuffer.GetRecent(10))
	}
}

func TestStdioClient_OversizedStderrDoesNotStallStdout(t *testing.T) {
	var buf bytes.Buffer
	line := strings.Repeat("x", 1024*1024+32)
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stderr).Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stderr).Write([]byte("\nfatal: after oversized\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := stdcopy.NewStdWriter(&buf, stdcopy.Stdout).Write([]byte(`{"jsonrpc":"2.0","id":7,"result":{}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	logBuffer := logging.NewLogBuffer(10)
	client := newTestStdioClient("crash", slog.New(logging.NewBufferHandler(logBuffer, nil)))
	pending := make(chan *jsonrpc.Response, 1)
	client.responsesMu.Lock()
	client.responses[7] = pending
	client.responsesMu.Unlock()
	client.startStdioReaders(bytes.NewReader(buf.Bytes()))
	t.Cleanup(func() { _ = client.Close() })

	select {
	case <-pending:
	case <-time.After(2 * time.Second):
		t.Fatal("oversized stderr stalled the following stdout frame")
	}
	deadline := time.Now().Add(2 * time.Second)
	var truncated, later bool
	for {
		for _, entry := range logBuffer.GetRecent(10) {
			if entry.Message != "server stderr" {
				continue
			}
			output, _ := entry.Attrs["output"].(string)
			if entry.Attrs["truncated"] == true && len(output) == maxStderrLine && strings.HasPrefix(output, "x") {
				truncated = true
			}
			if output == "fatal: after oversized" {
				later = true
			}
		}
		if (truncated && later) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !truncated || !later {
		t.Fatalf("truncated=%v later=%v logs=%#v", truncated, later, summarizeStderr(logBuffer.GetRecent(10)))
	}
}

func summarizeStderr(entries []logging.BufferedEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		output, _ := entry.Attrs["output"].(string)
		if len(output) > 40 {
			output = output[:40] + "..."
		}
		out = append(out, entry.Message+":"+output)
	}
	return out
}

type attachDocker struct {
	t       *testing.T
	attach  func() net.Conn
	running bool
}

func (d attachDocker) ContainerAttach(_ context.Context, _ string, opts container.AttachOptions) (types.HijackedResponse, error) {
	d.t.Helper()
	if opts.Logs {
		d.t.Fatal("attach requested log replay")
	}
	client := d.attach()
	resp := types.NewHijackedResponse(client, "application/vnd.docker.raw-stream")
	if resp.Reader == nil {
		resp.Reader = bufio.NewReader(client)
	}
	return resp, nil
}

func (attachDocker) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *v1.Platform, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (attachDocker) ContainerStart(context.Context, string, container.StartOptions) error {
	return nil
}
func (attachDocker) ContainerStop(context.Context, string, container.StopOptions) error {
	return nil
}
func (attachDocker) ContainerRestart(context.Context, string, container.StopOptions) error {
	return nil
}
func (attachDocker) ContainerRemove(context.Context, string, container.RemoveOptions) error {
	return nil
}
func (attachDocker) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return nil, nil
}
func (d attachDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	if !d.running {
		return container.InspectResponse{}, nil
	}
	return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Running: true, Status: "running"}}}, nil
}
func (attachDocker) ContainerLogs(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (attachDocker) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return nil, nil
}
func (attachDocker) NetworkCreate(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (attachDocker) NetworkRemove(context.Context, string) error { return nil }
func (attachDocker) ImageList(context.Context, image.ListOptions) ([]image.Summary, error) {
	return nil, nil
}
func (attachDocker) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (attachDocker) ImageBuild(context.Context, io.Reader, build.ImageBuildOptions) (build.ImageBuildResponse, error) {
	return build.ImageBuildResponse{}, nil
}
func (attachDocker) Ping(context.Context) (types.Ping, error) { return types.Ping{}, nil }
func (attachDocker) Close() error                             { return nil }

func newAttachClient(t *testing.T, serve func(net.Conn)) *StdioClient {
	t.Helper()
	client := NewStdioClient("crash", "container-1", attachDocker{
		t:       t,
		running: true,
		attach: func() net.Conn {
			left, right := net.Pipe()
			go serve(right)
			return left
		},
	})
	if err := client.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return client
}

func assertCloseThenReconnect(t *testing.T, client *StdioClient) {
	t.Helper()
	start := time.Now()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= processKillGracePeriod {
		t.Fatalf("close took %s, want well under %s", elapsed, processKillGracePeriod)
	}
	if err := client.Connect(t.Context()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStdioClient_CloseWithStderrInFlight(t *testing.T) {
	client := newAttachClient(t, func(conn net.Conn) {
		defer conn.Close()
		writer := stdcopy.NewStdWriter(conn, stdcopy.Stderr)
		for {
			if _, err := writer.Write([]byte("fatal: still writing\n")); err != nil {
				return
			}
		}
	})
	assertCloseThenReconnect(t, client)
}

func TestStdioClient_CloseWithSilentStream(t *testing.T) {
	client := newAttachClient(t, func(conn net.Conn) {
		<-t.Context().Done()
		_ = conn.Close()
	})
	assertCloseThenReconnect(t, client)
}

func TestStdioClient_InspectContainer(t *testing.T) {
	finished := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		state   *container.State
		nilBase bool
		wantErr bool
		wantNil bool
		code    int
		oom     bool
	}{
		{name: "nil state", wantErr: true},
		{name: "nil base", nilBase: true, wantErr: true},
		{name: "running", state: &container.State{Running: true, Status: "running"}, wantNil: true},
		{name: "exited", state: &container.State{ExitCode: 3, Status: "exited", FinishedAt: finished.Format(time.RFC3339Nano)}, code: 3},
		{name: "oom", state: &container.State{ExitCode: 137, OOMKilled: true, Status: "exited", FinishedAt: "0001-01-01T00:00:00Z"}, code: 137, oom: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var client *StdioClient
			if tc.nilBase {
				client = NewStdioClient("crash", "container-1", attachDocker{t: t})
			} else {
				client = NewStdioClient("crash", "container-1", inspectDocker{state: tc.state})
			}
			got, err := client.InspectContainer(t.Context())
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "container state unavailable") {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("running inspect = %#v", got)
				}
				return
			}
			if got == nil || got.Code != tc.code || got.OOMKilled != tc.oom || got.Status != "exited" {
				t.Fatalf("inspect = %#v", got)
			}
			if tc.oom && got.FinishedAt != nil {
				t.Fatal("zero finishedAt was recorded")
			}
			if !tc.oom && (got.FinishedAt == nil || !got.FinishedAt.Equal(finished)) {
				t.Fatalf("finishedAt = %v", got.FinishedAt)
			}
		})
	}
}

type inspectDocker struct {
	attachDocker
	state *container.State
}

func (d inspectDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{State: d.state}}, nil
}

type exitedDocker struct {
	attachDocker
	attaches *atomic.Int32
}

func (d *exitedDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Status: "exited", ExitCode: 3}}}, nil
}

func (d *exitedDocker) ContainerAttach(context.Context, string, container.AttachOptions) (types.HijackedResponse, error) {
	d.attaches.Add(1)
	return types.HijackedResponse{}, errors.New("attach called")
}

func TestStdioClient_ConnectRefusesStoppedContainer(t *testing.T) {
	var attaches atomic.Int32
	client := NewStdioClient("crash", "container-1", &exitedDocker{attaches: &attaches})
	err := client.Connect(t.Context())
	if err == nil || !strings.Contains(err.Error(), "container not running (status exited)") {
		t.Fatalf("err = %v", err)
	}
	if attaches.Load() != 0 {
		t.Fatalf("attach calls = %d", attaches.Load())
	}
}
