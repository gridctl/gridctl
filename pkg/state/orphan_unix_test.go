//go:build !windows

package state

import (
	"errors"
	"os"
	"testing"
)

// stubProbeHealth installs a fake probeHealthFn and restores the real
// implementation on test cleanup. Mirrors stubFindOrphan in
// cmd/gridctl/stop_test.go.
func stubProbeHealth(t *testing.T, fn func(int) bool) {
	t.Helper()
	orig := probeHealthFn
	probeHealthFn = fn
	t.Cleanup(func() { probeHealthFn = orig })
}

func stubListenerForPort(t *testing.T, fn func(int) ([]int, error)) {
	t.Helper()
	orig := listenerForPort
	listenerForPort = fn
	t.Cleanup(func() { listenerForPort = orig })
}

func stubExecutableForPID(t *testing.T, fn func(int) (string, error)) {
	t.Helper()
	orig := executableForPID
	executableForPID = fn
	t.Cleanup(func() { executableForPID = orig })
}

func TestFindOrphan_ForegroundProcess(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return []int{4242}, nil })
	stubExecutableForPID(t, func(int) (string, error) { return "gridctl", nil })

	pid, ok, err := FindOrphan(8180)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || pid != 4242 {
		t.Errorf("expected (4242, true, nil); got (%d, %v, %v)", pid, ok, err)
	}
}

func TestFindOrphan_NonGridctlListener(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return []int{4242}, nil })
	stubExecutableForPID(t, func(int) (string, error) { return "nginx", nil })

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
}

func TestFindOrphan_ExcludesSelf(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	self := os.Getpid()
	stubListenerForPort(t, func(int) ([]int, error) { return []int{self}, nil })
	exeCalled := false
	stubExecutableForPID(t, func(int) (string, error) {
		exeCalled = true
		return "gridctl", nil
	})

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
	if exeCalled {
		t.Error("expected executable lookup to be skipped when only candidate is self")
	}
}

func TestFindOrphan_NoListener(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return nil, nil })
	stubExecutableForPID(t, func(int) (string, error) {
		t.Fatal("executable lookup should not run when no listeners")
		return "", nil
	})

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
}

func TestFindOrphan_MultipleListeners(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return []int{4242, 5151}, nil })
	stubExecutableForPID(t, func(int) (string, error) {
		t.Fatal("executable lookup should not run when listeners are ambiguous")
		return "", nil
	})

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
}

func TestFindOrphan_HealthDown(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return false })
	stubListenerForPort(t, func(int) ([]int, error) {
		t.Fatal("listener lookup should not run when health probe fails")
		return nil, nil
	})
	stubExecutableForPID(t, func(int) (string, error) {
		t.Fatal("executable lookup should not run when health probe fails")
		return "", nil
	})

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
}

func TestFindOrphan_ListenerLookupError(t *testing.T) {
	wantErr := errors.New("boom")
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return nil, wantErr })
	stubExecutableForPID(t, func(int) (string, error) {
		t.Fatal("executable lookup should not run on listener lookup error")
		return "", nil
	})

	pid, ok, err := FindOrphan(8180)
	if !errors.Is(err, wantErr) || ok || pid != 0 {
		t.Errorf("expected (0, false, %v); got (%d, %v, %v)", wantErr, pid, ok, err)
	}
}

func TestFindOrphan_ExecutableLookupError(t *testing.T) {
	stubProbeHealth(t, func(int) bool { return true })
	stubListenerForPort(t, func(int) ([]int, error) { return []int{4242}, nil })
	stubExecutableForPID(t, func(int) (string, error) { return "", errors.New("nope") })

	pid, ok, err := FindOrphan(8180)
	if err != nil || ok || pid != 0 {
		t.Errorf("expected (0, false, nil); got (%d, %v, %v)", pid, ok, err)
	}
}
