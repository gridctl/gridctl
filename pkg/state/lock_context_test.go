package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithLockContext_CancellationAndSerialization(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- WithLockContext(context.Background(), "context-test", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := WithLockContext(ctx, "context-test", func(context.Context) error {
		t.Error("entered contended lock")
		return nil
	})
	close(release)
	if got := <-done; got != nil {
		t.Fatal(got)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation: %v", err)
	}
	want := errors.New("callback")
	if err := WithLockContext(context.Background(), "context-test", func(context.Context) error { return want }); err != want {
		t.Fatalf("callback error: %v", err)
	}
	if err := WithLockContext(ctx, "context-test", func(context.Context) error { t.Error("canceled callback"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("already canceled: %v", err)
	}
}
