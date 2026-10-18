package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// InfraError is an exit-2 condition: the supervisor binary is missing or
// its output cannot be read.
type InfraError struct {
	Msg string
}

func (e *InfraError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

// ErrUnitNotLoaded means the user manager does not have the unit. Destroy
// treats that as unsupervised: there is no Restart=on-failure to trip.
var ErrUnitNotLoaded = errors.New("unit not loaded")

// AttentionError is an exit-1 condition: the user has to choose or finish
// a step. The unit and lock entry are left as the message describes.
type AttentionError struct {
	Msg string
}

func (e *AttentionError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func isInfra(err error) bool {
	var infra *InfraError
	return errors.As(err, &infra)
}

func runSupervisor(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, &InfraError{Msg: fmt.Sprintf("%s not found", name)}
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...) // #nosec G204 -- fixed supervisor verbs over validated paths
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return out, fmt.Errorf("%s timed out after %s", name, timeout)
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return out, fmt.Errorf("%s: %w", name, err)
		}
		return out, fmt.Errorf("%s: %s", name, msg)
	}
	return out, nil
}

// runSupervisorInherit executes name with the caller's stdio so `journalctl
// -f` stays attached to the terminal. ctx cancellation (SIGINT) stops it.
func runSupervisorInherit(ctx context.Context, stdout, stderr ioWriter, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return &InfraError{Msg: fmt.Sprintf("%s not found", name)}
	}
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed supervisor verbs over validated paths
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

// ioWriter is the subset of io.Writer the inherit helper needs. Declared
// here so exec.go does not import io only for one parameter in a way that
// drifts from LogOptions.
type ioWriter interface {
	Write(p []byte) (int, error)
}
