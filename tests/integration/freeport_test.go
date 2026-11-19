//go:build integration && linux

package integration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

// ipLocalPortRange is the Linux IP_LOCAL_PORT_RANGE socket option (SOL_IP,
// kernel 6.3+). It restricts the ports an unbound socket may be assigned.
const ipLocalPortRange = 51

// TestFreePort_RefusesPortHeldOnAnotherAddress confines the probe to a single
// port held only on [::1] (IPv6-only). A loopback-only probe accepts that
// port, while the mock server's wildcard bind of it fails, so the probe must
// refuse it.
func TestFreePort_RefusesPortHeldOnAnotherAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hold, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("host has no IPv6 loopback: %v", err)
	}
	defer hold.Close()
	p := hold.Addr().(*net.TCPAddr).Port

	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var setErr error
			if err := c.Control(func(fd uintptr) {
				setErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, ipLocalPortRange, p|p<<16) //nolint:gosec // fd fits in int
			}); err != nil {
				return err
			}
			return setErr
		},
	}

	got, err := pickFreePort(ctx, &lc)
	if errors.Is(err, syscall.ENOPROTOOPT) {
		t.Skipf("IP_LOCAL_PORT_RANGE unsupported (requires Linux 6.3 or newer): %v", err)
	}
	if err == nil {
		t.Fatalf("pickFreePort returned port %d while %d is held on [::1]; want EADDRINUSE", got, p)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("pickFreePort error = %v; want EADDRINUSE", err)
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err == nil {
		l.Close()
		t.Fatalf("wildcard bind of port %d held on [::1] succeeded; want EADDRINUSE", p)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("wildcard bind error = %v; want EADDRINUSE", err)
	}

	if err := hold.Close(); err != nil {
		t.Fatalf("close held listener: %v", err)
	}
	got, err = pickFreePort(ctx, &lc)
	if err != nil {
		t.Fatalf("pickFreePort after release: %v", err)
	}
	if got != p {
		t.Fatalf("pickFreePort after release = %d; want %d", got, p)
	}
}
