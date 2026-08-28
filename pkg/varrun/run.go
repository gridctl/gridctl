// Package varrun delivers an explicit subset of stored variables to a direct
// child process without invoking a shell.
package varrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strings"
	"sync"

	"github.com/gridctl/gridctl/pkg/vault"
)

// Options controls one child process invocation.
type Options struct {
	Command     []string
	Variables   []vault.Variable
	Environment []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	StdoutTTY   bool
	StderrTTY   bool
	NoRedact    bool
	ForceRedact bool
}

// Result describes a child that was successfully started and waited for.
type Result struct{ ExitCode int }

// Run starts and waits for a direct child process.
func Run(ctx context.Context, opts Options) (Result, error) {
	if len(opts.Command) == 0 {
		return Result{}, fmt.Errorf("command is required after --")
	}
	if opts.ForceRedact && (opts.StdoutTTY || opts.StderrTTY) {
		return Result{}, fmt.Errorf("--redact cannot be used with TTY output")
	}
	cmd := exec.Command(opts.Command[0], opts.Command[1:]...) // #nosec G204 -- explicit user-requested command
	cmd.Stdin = opts.Stdin
	cmd.Env = buildEnvironment(opts.Environment, opts.Variables)
	group := !opts.StdoutTTY && !opts.StderrTTY
	setupProcess(cmd, group)

	secrets := make([]string, 0, len(opts.Variables))
	for _, v := range opts.Variables {
		if v.IsSecret && v.Value != "" {
			secrets = append(secrets, v.Value)
		}
	}
	stdout, closeOut := outputWriter(opts.Stdout, opts.NoRedact || (opts.StdoutTTY && !opts.ForceRedact), secrets)
	stderr, closeErr := outputWriter(opts.Stderr, opts.NoRedact || (opts.StderrTTY && !opts.ForceRedact), secrets)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting command: %w", err)
	}

	sigs := processSignals()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, sigs...)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	forwardDone := make(chan struct{})
	var forwardErr error
	var mu sync.Mutex
	go func() {
		defer close(forwardDone)
		ctxDone := ctx.Done()
		for {
			select {
			case sig := <-sigCh:
				recordForwardError(&mu, &forwardErr, signalProcess(cmd.Process, sig, group))
			case <-ctxDone:
				recordForwardError(&mu, &forwardErr, terminateProcess(cmd.Process, group))
				ctxDone = nil
			case <-done:
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	close(done)
	<-forwardDone
	outErr, errErr := closeOut(), closeErr()
	mu.Lock()
	fwdErr := forwardErr
	mu.Unlock()
	if outErr != nil {
		return Result{}, fmt.Errorf("writing stdout: %w", outErr)
	}
	if errErr != nil {
		return Result{}, fmt.Errorf("writing stderr: %w", errErr)
	}
	if fwdErr != nil && !errors.Is(fwdErr, os.ErrProcessDone) {
		return Result{}, fmt.Errorf("forwarding signal: %w", fwdErr)
	}
	code, ok := decodeExit(waitErr)
	if !ok {
		return Result{}, fmt.Errorf("waiting for command: %w", waitErr)
	}
	return Result{ExitCode: code}, nil
}

func recordForwardError(mu *sync.Mutex, dst *error, err error) {
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return
	}
	mu.Lock()
	if *dst == nil {
		*dst = err
	}
	mu.Unlock()
}

func outputWriter(dst io.Writer, raw bool, secrets []string) (io.Writer, func() error) {
	if dst == nil {
		dst = io.Discard
	}
	if raw || len(secrets) == 0 {
		return dst, func() error { return nil }
	}
	r := NewRedactor(dst, secrets)
	return r, r.Close
}

func buildEnvironment(ambient []string, vars []vault.Variable) []string {
	values := make(map[string]string, len(ambient)+len(vars))
	for _, item := range ambient {
		key, value, ok := strings.Cut(item, "=")
		if ok && !vault.IsInternalCredential(key) {
			values[key] = value
		}
	}
	for _, v := range vars {
		if !vault.IsInternalCredential(v.Key) {
			values[v.Key] = v.Value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}
