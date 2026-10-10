package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/runtime"

	"github.com/docker/docker/api/types/container"
)

func TestDockerRuntime_Start_MaterializesConfigs(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)
	uid := 65534
	cfg := runtime.WorkloadConfig{
		Name:  "prometheus",
		Stack: "demo",
		Image: "alpine",
		Configs: []runtime.ConfigFile{
			{Target: "/tmp/b.txt", Content: "beta", Mode: "0640"},
			{Target: "/etc/gridctl/a.txt", Content: "alpha", Mode: "0440", UID: &uid, GID: &uid},
		},
		Labels: map[string]string{LabelMCPServer: "prometheus"},
	}
	if _, err := rt.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if mock.LastCopyPath != "/" {
		t.Fatalf("copy path = %q", mock.LastCopyPath)
	}
	if mock.LastCopyOptions != (container.CopyToContainerOptions{}) {
		t.Fatalf("copy options = %+v, want empty", mock.LastCopyOptions)
	}
	first := append([]byte(nil), mock.LastCopyArchive...)
	mock2 := &MockDockerClient{}
	if _, err := NewWithClient(mock2).Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, mock2.LastCopyArchive) {
		t.Fatal("archive bytes changed for the same inputs")
	}
	entries := untarConfigs(t, first)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 regular files", len(entries))
	}
	if entries[0].name != "etc/gridctl/a.txt" || entries[0].mode != 0o440 || entries[0].uid != 65534 || string(entries[0].body) != "alpha" {
		t.Fatalf("first entry = %+v", entries[0])
	}
	if entries[1].name != "tmp/b.txt" || entries[1].mode != 0o640 || string(entries[1].body) != "beta" {
		t.Fatalf("second entry = %+v", entries[1])
	}
	for _, entry := range entries {
		if entry.dir {
			t.Fatalf("directory entry %s", entry.name)
		}
	}
}

func TestDockerRuntime_Start_ConfigCopyFailureRemovesContainer(t *testing.T) {
	mock := &MockDockerClient{CopyToContainerError: errors.New("copy failed")}
	rt := NewWithClient(mock)
	_, err := rt.Start(context.Background(), runtime.WorkloadConfig{
		Name: "prometheus", Stack: "demo", Image: "alpine",
		Configs: []runtime.ConfigFile{{Target: "/etc/a.txt", Content: "a"}},
	})
	if err == nil || len(mock.RemovedContainers) != 1 || len(mock.StartedContainers) != 0 {
		t.Fatalf("err=%v removed=%v started=%v", err, mock.RemovedContainers, mock.StartedContainers)
	}
}

func TestDockerRuntime_Start_RefusesHardenedConfigs(t *testing.T) {
	mock := &MockDockerClient{}
	rt := NewWithClient(mock)
	_, err := rt.Start(context.Background(), runtime.WorkloadConfig{
		Name: "prometheus", Stack: "demo", Image: "alpine", Type: runtime.WorkloadTypeMCPServer,
		Execution: &execution.ExecutionContract{Mode: "hardened"},
		Configs:   []runtime.ConfigFile{{Target: "/etc/a.txt", Content: "a"}},
	})
	if err == nil || len(mock.Calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, mock.Calls)
	}
}

func TestDockerRuntime_Start_ConfigsRevisionMismatchRecreates(t *testing.T) {
	containerName := ContainerName("demo", "prometheus")
	mock := &MockDockerClient{
		Containers: []container.Summary{{ID: "old-id", Names: []string{"/" + containerName}}},
		ContainerDetails: map[string]container.InspectResponse{
			"old-id": {
				ContainerJSONBase: &container.ContainerJSONBase{State: &container.State{Status: "running"}},
				Config:            &container.Config{Labels: map[string]string{LabelConfigsRevision: "old"}},
			},
		},
	}
	rt := NewWithClient(mock)
	_, err := rt.Start(context.Background(), runtime.WorkloadConfig{
		Name: "prometheus", Stack: "demo", Image: "alpine",
		Configs: []runtime.ConfigFile{{Target: "/etc/a.txt", Content: "new"}},
		Labels:  map[string]string{LabelConfigsRevision: "new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.RemovedContainers) != 1 || mock.RemovedContainers[0] != "old-id" {
		t.Fatalf("removed = %v", mock.RemovedContainers)
	}
	if len(mock.CreatedContainers) != 1 {
		t.Fatalf("created = %v", mock.CreatedContainers)
	}
	if len(mock.LastCopyArchive) == 0 {
		t.Fatal("expected a new archive copy")
	}
}

func TestDockerRuntime_Start_ReuseStartSkipsConfigInspect(t *testing.T) {
	containerName := ContainerName("demo", "prometheus")
	mock := &MockDockerClient{
		Containers: []container.Summary{{ID: "old-id", Names: []string{"/" + containerName}}},
	}
	rt := NewWithClient(mock)
	_, err := rt.Start(context.Background(), runtime.WorkloadConfig{Name: "prometheus", Stack: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	sawStart := false
	for _, call := range mock.Calls {
		if call == "ContainerInspect" && !sawStart {
			t.Fatalf("inspected before start: %v", mock.Calls)
		}
		if call == "ContainerStart" {
			sawStart = true
		}
	}
	if !sawStart || len(mock.CreatedContainers) != 0 {
		t.Fatalf("calls=%v created=%v", mock.Calls, mock.CreatedContainers)
	}
}

type tarEntry struct {
	name string
	mode int64
	uid  int
	gid  int
	dir  bool
	body []byte
}

func untarConfigs(t *testing.T, archive []byte) []tarEntry {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(archive))
	var entries []tarEntry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, tarEntry{
			name: hdr.Name,
			mode: hdr.Mode,
			uid:  hdr.Uid,
			gid:  hdr.Gid,
			dir:  hdr.Typeflag == tar.TypeDir,
			body: body,
		})
	}
}
