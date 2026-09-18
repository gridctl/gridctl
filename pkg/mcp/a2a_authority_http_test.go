package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// These are real HTTP requests around the store's dispatch/commit boundary. No
// protocol adapter is needed to verify that the two local slots don't serialize
// network I/O or let a retired callback publish authority.
func TestCapabilityAuthority_HTTPOverlappingCancel(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "send-first", true: "cancel-first"}[cancelFirst], func(t *testing.T) {
			sendEntered := make(chan struct{})
			cancelEntered := make(chan struct{})
			releaseSend := make(chan struct{})
			releaseCancel := make(chan struct{})
			var releaseSendOnce, releaseCancelOnce sync.Once
			unblockSend := func() { releaseSendOnce.Do(func() { close(releaseSend) }) }
			unblockCancel := func() { releaseCancelOnce.Do(func() { close(releaseCancel) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				entered, release := sendEntered, releaseSend
				if r.URL.Path == "/cancel" {
					entered, release = cancelEntered, releaseCancel
				}
				close(entered)
				select {
				case <-release:
					w.WriteHeader(http.StatusOK)
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			defer unblockSend()
			defer unblockCancel()
			g := authorityGeneration(t, NewCapabilityStore(), "agent", "bedrock")
			d := authoritySeed(t, g)
			run := func(op *capabilityOperation, path, state string, done chan<- error) {
				if _, err := op.dispatch(); err != nil {
					op.fail(false)
					done <- err
					return
				}
				req, err := http.NewRequestWithContext(op.ctx, http.MethodPost, server.URL+path, nil)
				if err != nil {
					op.fail(false)
					done <- err
					return
				}
				resp, err := server.Client().Do(req)
				if err != nil {
					op.fail(true)
					done <- err
					return
				}
				_, err = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					op.fail(true)
					done <- err
					return
				}
				_, err = op.commit(capabilityResponse{contextID: "context", taskID: "task", state: state})
				done <- err
			}
			ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelCtx()
			send, err := g.admit(ctx, capabilitySend, d.contextHandle, d.taskHandle)
			if err != nil {
				t.Fatal(err)
			}
			sendDone, cancelDone := make(chan error, 1), make(chan error, 1)
			go run(send, "/send", "working", sendDone)
			select {
			case <-sendEntered:
			case <-ctx.Done():
				t.Fatal("send never reached HTTP")
			}
			control, err := g.admit(ctx, capabilityCancel, "", d.taskHandle)
			if err != nil {
				t.Fatal(err)
			}
			go run(control, "/cancel", "canceled", cancelDone)
			select {
			case <-cancelEntered:
			case <-ctx.Done():
				t.Fatal("cancel queued behind send")
			}
			if cancelFirst {
				unblockCancel()
				if err := <-cancelDone; err != nil {
					t.Fatal(err)
				}
				unblockSend()
				if err := <-sendDone; !errors.Is(err, errCapabilitySuperseded) {
					t.Fatal(err)
				}
			} else {
				unblockSend()
				if err := <-sendDone; !errors.Is(err, errCapabilitySuperseded) {
					t.Fatal(err)
				}
				unblockCancel()
				if err := <-cancelDone; err != nil {
					t.Fatal(err)
				}
			}
			if control.task.state != "canceled" || !control.task.uncertain {
				t.Fatal("late result changed cancel state or reconciled uncertainty")
			}
		})
	}
}

func TestCapabilityAuthority_ConcurrentRootSlots(t *testing.T) {
	g := authorityGeneration(t, NewCapabilityStore(), "agent", "")
	d := authoritySeed(t, g)
	send := authoritySend(t, g, d.contextHandle, d.taskHandle)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var cancels []*capabilityOperation
	for range 20 {
		wg.Go(func() {
			op, err := g.admit(context.Background(), capabilityCancel, "", d.taskHandle)
			if err == nil {
				mu.Lock()
				cancels = append(cancels, op)
				mu.Unlock()
			} else if !errors.Is(err, errCapabilityInProgress) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(cancels) != 1 {
		t.Fatalf("admitted %d cancels", len(cancels))
	}
	// Retirement and callback drain can happen in either order without returning
	// capacity twice or allowing an old result to overwrite a replacement.
	wg.Go(func() { g.retire() })
	wg.Go(func() { send.fail(true) })
	wg.Go(func() { cancels[0].fail(true) })
	wg.Wait()
	if g.store.used != (capabilityCounts{}) {
		t.Fatal(g.store.used)
	}
}
