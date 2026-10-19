package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/vault"
)

const (
	stdoutLimit = 1 << 20
	stderrLimit = 4 << 10
	waitDelay   = 2 * time.Second
)

// Provider resolves one secret path.
type Provider interface {
	Resolve(ctx context.Context, path string) (string, error)
	Name() string
	Type() string
}

type execProvider struct {
	name    string
	command []string
	timeout time.Duration
	env     []string
}

// NewExec builds an argv provider. {{path}} is replaced inside each element.
// The command is never passed to a shell.
func NewExec(cfg config.SecretProvider, env []string) Provider {
	timeout, err := cfg.ProviderTimeout()
	if err != nil {
		timeout = 10 * time.Second
	}
	return &execProvider{
		name:    cfg.Name,
		command: append([]string(nil), cfg.Command...),
		timeout: timeout,
		env:     filterInternalEnv(env),
	}
}

func (p *execProvider) Name() string { return p.name }
func (p *execProvider) Type() string { return "exec" }

func (p *execProvider) Resolve(ctx context.Context, path string) (string, error) {
	ref := Reference{Provider: p.name, Path: path}
	if len(p.command) == 0 {
		return "", secretError(ref, "empty value", nil)
	}
	argv := make([]string, len(p.command))
	for i, el := range p.command {
		argv[i] = strings.ReplaceAll(el, "{{path}}", path)
	}
	return runProvider(ctx, ref, p.timeout, argv, p.env, nil)
}

func filterInternalEnv(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || vault.IsInternalCredential(key) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func runProvider(ctx context.Context, ref Reference, timeout time.Duration, argv, env []string, extra func()) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", contextCategory(ref, timeout, err)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if extra != nil {
		extra()
	}
	// #nosec G204 -- stack-authored argv, never a shell; path is substituted inside elements
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = nil
	cmd.WaitDelay = waitDelay
	var stdout, stderr cappedBuffer
	stdout.limit = stdoutLimit
	stderr.limit = stderrLimit
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	if stdout.overflow {
		e := secretError(ref, "output too large", err)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
		return "", e
	}
	if err != nil {
		e := commandError(ref, timeout, argv[0], ctx, runCtx, err)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category, "stderr_bytes", stderr.n)
		return "", e
	}
	value := trimOneNewline(stdout.String())
	if value == "" {
		e := secretError(ref, "empty value", nil)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
		return "", e
	}
	slog.Debug("secret resolved", "provider", ref.Provider, "path", ref.Path, "duration", time.Since(start))
	return value, nil
}

func commandError(ref Reference, timeout time.Duration, binary string, parent, run context.Context, err error) *Error {
	if parent.Err() != nil {
		return contextCategory(ref, timeout, parent.Err())
	}
	if run.Err() != nil {
		return contextCategory(ref, timeout, run.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return secretError(ref, fmt.Sprintf("provider exited %d", exitErr.ExitCode()), err)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return secretError(ref, binary+" not found on PATH", err)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
		return secretError(ref, binary+" not found on PATH", execErr)
	}
	return secretError(ref, "provider exited 1", err)
}

func contextCategory(ref Reference, timeout time.Duration, err error) *Error {
	if errors.Is(err, context.DeadlineExceeded) {
		return secretError(ref, fmt.Sprintf("timeout after %s", timeout), context.DeadlineExceeded)
	}
	return secretError(ref, "canceled", context.Canceled)
}

func trimOneNewline(s string) string {
	if strings.HasSuffix(s, "\r\n") {
		return strings.TrimSuffix(s, "\r\n")
	}
	if strings.HasSuffix(s, "\n") {
		return strings.TrimSuffix(s, "\n")
	}
	return s
}

// cappedBuffer captures up to limit bytes and counts the rest.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	n        int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.n += len(p)
	if b.limit <= 0 {
		b.overflow = true
		return len(p), nil
	}
	remain := b.limit - b.buf.Len()
	if remain <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if len(p) > remain {
		b.overflow = true
		_, _ = b.buf.Write(p[:remain])
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *cappedBuffer) String() string { return b.buf.String() }
