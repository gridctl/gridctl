package secrets

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
)

var opEnvAllow = map[string]bool{
	"PATH":                     true,
	"HOME":                     true,
	"XDG_CONFIG_HOME":          true,
	"OP_SERVICE_ACCOUNT_TOKEN": true,
	"OP_CONNECT_HOST":          true,
	"OP_CONNECT_TOKEN":         true,
	"OP_ACCOUNT":               true,
}

type onePasswordProvider struct {
	name      string
	timeout   time.Duration
	env       []string
	tokenFile string
	bin       string
	binErr    error
}

// NewOnePassword builds an `op read` provider. The binary is looked up once
// at construction; a missing binary is reported at resolve time.
func NewOnePassword(cfg config.SecretProvider, env []string) Provider {
	timeout, err := cfg.ProviderTimeout()
	if err != nil {
		timeout = 10 * time.Second
	}
	bin, binErr := exec.LookPath("op")
	return &onePasswordProvider{
		name:      cfg.Name,
		timeout:   timeout,
		env:       filterOPEnv(env),
		tokenFile: cfg.TokenFile,
		bin:       bin,
		binErr:    binErr,
	}
}

func (p *onePasswordProvider) Name() string { return p.name }
func (p *onePasswordProvider) Type() string { return "onepassword" }

func (p *onePasswordProvider) Resolve(ctx context.Context, path string) (string, error) {
	ref := Reference{Provider: p.name, Path: path}
	if err := validateOPPath(path); err != nil {
		e := secretError(ref, "invalid path", err)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
		return "", e
	}
	if p.binErr != nil || p.bin == "" {
		e := secretError(ref, "op not found on PATH", p.binErr)
		slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
		return "", e
	}
	env := append([]string(nil), p.env...)
	if p.tokenFile != "" {
		token, err := readTokenFile(p.tokenFile)
		if err != nil {
			e := secretError(ref, err.Error(), err)
			slog.Warn("secret resolution failed", "provider", ref.Provider, "path", ref.Path, "category", e.Category)
			return "", e
		}
		logging.RegisterSensitiveValues(token)
		env = replaceEnv(env, "OP_SERVICE_ACCOUNT_TOKEN", token)
	}
	argv := []string{p.bin, "read", "--no-newline", "op://" + path}
	return runProvider(ctx, ref, p.timeout, argv, env, nil)
}

func validateOPPath(path string) error {
	if strings.HasPrefix(path, "op://") {
		return fmt.Errorf("op:// prefix")
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 && len(parts) != 4 {
		return fmt.Errorf("segment count")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("empty segment")
		}
	}
	return nil
}

func filterOPEnv(env []string) []string {
	var out []string
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !opEnvAllow[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	next := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		next = append(next, entry)
	}
	return append(next, prefix+value)
}

func readTokenFile(path string) (string, error) {
	expanded, err := expandTokenFile(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(expanded)
	if err != nil {
		return "", fmt.Errorf("token file unreadable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("token file symlink")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("token file not regular")
	}
	if runtime.GOOS != "windows" {
		if err := checkTokenOwnerMode(info); err != nil {
			return "", err
		}
	}
	f, err := os.OpenFile(expanded, os.O_RDONLY|tokenOpenFlag(), 0)
	if err != nil {
		if isSymlinkOpen(err) {
			return "", fmt.Errorf("token file symlink")
		}
		return "", fmt.Errorf("token file unreadable")
	}
	defer f.Close()
	limited := io.LimitReader(f, 1<<16+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("token file unreadable")
	}
	if len(buf) > 1<<16 {
		return "", fmt.Errorf("token file too large")
	}
	token := trimOneNewline(string(buf))
	if token == "" {
		return "", fmt.Errorf("token file empty")
	}
	return token, nil
}

func expandTokenFile(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("token file home")
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func isSymlinkOpen(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "symlink") || strings.Contains(msg, "too many levels of symbolic links")
}
