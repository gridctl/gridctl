// Go plugin loader for skill.go-handler skills. The gateway-builder
// calls loadGoSkillPlugins after the registry store is initialized and
// before the registry server is added to the router — so a Go skill's
// MCP tool entry shows up on the first RefreshTools the router runs.
//
// Operational sharp edges live in the standard library plugin package
// and are not gridctl's to fix:
//
//   - Host (gridctl daemon) and plugin (skill.so) MUST build with
//     identical Go versions and identical dep-graph hashes. A stale
//     plugin produces an opaque plugin.Open error; the manifest
//     guardrails here turn that opaque failure into an actionable
//     slog.Warn ("rebuild with `gridctl agent build <name>`") before
//     the toolchain mismatch ever reaches plugin.Open.
//   - plugin.Open is one-way: plugins cannot be unloaded. The hot
//     reload path explicitly skips Go skill re-discovery for the same
//     reason. Daemon restart is the documented path.
//   - The plugin package is unavailable on Windows; the windows build
//     uses the stub in go_plugins_stub.go which logs and returns.
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
)

// goSkillManifest mirrors the subset of the agent-build manifest the
// loader needs at start time. Unknown fields are tolerated — the build
// path is free to add metadata without forcing a coordinated update.
type goSkillManifest struct {
	Name       string `json:"name"`
	Handler    string `json:"handler"`
	GoVersion  string `json:"go_version"`
	GoModHash  string `json:"go_mod_hash,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
}

// daemonGoModHash returns the sha256 of the daemon's resolved go.mod,
// or ("", false) when the daemon is running from an installed binary
// that has no on-disk go.mod parent. The walker first probes upward
// from os.Executable() and falls back to the working directory — the
// `make build` flow puts the binary at the repo root, while `go run`
// puts it under $TMPDIR. Either form resolves; an installed binary
// returns false, which the caller treats as "skip the check" rather
// than "skip every plugin" (Pitfall 9).
//
// Captured at the package level so the first lookup pays the I/O once
// per daemon process; subsequent loader calls (a daemon restart-only
// path today, but kept resilient against future repeated calls) reuse
// the cached hash.
var daemonGoModHash = func() (string, bool) {
	var found string
	if exe, err := os.Executable(); err == nil {
		if hash, ok := goModHashWalk(filepath.Dir(exe)); ok {
			found = hash
		}
	}
	if found == "" {
		if cwd, err := os.Getwd(); err == nil {
			if hash, ok := goModHashWalk(cwd); ok {
				found = hash
			}
		}
	}
	if found == "" {
		return "", false
	}
	return found, true
}

// goModHashWalk walks upward from dir to the filesystem root, hashing
// the first go.mod it finds. Mirrors the build-time walker in
// cmd/gridctl/agent.go so the comparison the loader performs at gateway
// start matches the field the build path wrote into manifest.json.
func goModHashWalk(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			data, err := os.ReadFile(candidate) // #nosec G304 -- candidate is filepath-joined from a trusted starting dir
			if err != nil {
				return "", false
			}
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:]), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// readGoSkillManifest reads and decodes manifest.json adjacent to a
// Go skill's compiled plugin. Returns ("", err) when the manifest is
// missing or undecodable; the caller turns either case into an
// actionable slog.Warn that names the skill and the rebuild command.
func readGoSkillManifest(path string) (*goSkillManifest, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is registry-walker derived
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", path, err)
	}
	var m goSkillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	return &m, nil
}

// checkGoSkillGuardrails compares the manifest's recorded toolchain
// fingerprint against the running daemon. Returns (true, "") when the
// plugin is safe to open. Returns (false, reason) when the loader
// must skip the plugin — reason is the operator-facing message that
// goes into the slog.Warn at the call site.
//
// Missing go_mod_hash in the manifest reads as "build path could not
// determine go.mod" and is treated as "skip the check", not "skip the
// plugin" — same shape as the build-side log at Pitfall 9. Missing
// go_mod_hash on the daemon side is symmetric: an installed binary
// without a go.mod parent loads plugins on the go_version check alone.
func checkGoSkillGuardrails(m *goSkillManifest, daemonGoVersion string, daemonModHash string, daemonModHashOK bool) (ok bool, reason string) {
	if m.GoVersion != "" && m.GoVersion != daemonGoVersion {
		return false, fmt.Sprintf("plugin built with %s, daemon running %s", m.GoVersion, daemonGoVersion)
	}
	if m.GoModHash != "" && daemonModHashOK && m.GoModHash != daemonModHash {
		return false, "go.mod hash mismatch between plugin and daemon"
	}
	return true, ""
}

// loadGoSkillPlugins walks the registry store for Go-handler skills
// and registers each one's plugin export against reg. Per-plugin
// failures (missing artifacts, manifest mismatch, plugin.Open error,
// missing RegisterSkill symbol) are logged at warn and the loop
// continues — one broken skill never blocks gateway start. After
// RegisterSkill populates the staging registry, the loader re-decorates
// each Definition with the on-disk SKILL.md body via
// skill.WithSkillBody so ctx.SkillBody() returns the same prose a
// non-plugin in-process registration would surface.
//
// Build-tag note: the unconditional helpers (manifest read, guardrail
// check, body re-decoration) live in this file because they compile
// on every platform. The plugin.Open + plugin.Lookup invocation lives
// in go_plugins_open.go behind `//go:build !windows`; the windows
// build replaces it with a no-op stub.
func loadGoSkillPlugins(store *registry.Store, reg *skill.Registry, logger *slog.Logger) {
	if store == nil || reg == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	daemonGoVersion := runtime.Version()
	daemonHash, daemonHashOK := daemonGoModHash()

	for _, sk := range store.ListSkills() {
		if sk == nil || sk.HandlerLanguage != "go" {
			continue
		}
		handlerPath, ok := store.HandlerPath(sk.Name)
		if !ok {
			logger.Warn("go skill: handler path missing; skipping plugin load",
				"skill", sk.Name)
			continue
		}
		distDir := filepath.Join(filepath.Dir(handlerPath), "dist")
		soPath := filepath.Join(distDir, "skill.so")
		manifestPath := filepath.Join(distDir, "manifest.json")

		if _, err := os.Stat(soPath); err != nil {
			logger.Warn("go skill: skill.so missing; rebuild with `gridctl agent build`",
				"skill", sk.Name, "path", soPath, "error", err)
			continue
		}

		manifest, err := readGoSkillManifest(manifestPath)
		if err != nil {
			logger.Warn("go skill: manifest read failed; rebuild with `gridctl agent build`",
				"skill", sk.Name, "path", manifestPath, "error", err)
			continue
		}

		if ok, reason := checkGoSkillGuardrails(manifest, daemonGoVersion, daemonHash, daemonHashOK); !ok {
			logger.Warn("go skill: guardrail mismatch; rebuild with `gridctl agent build`",
				"skill", sk.Name, "reason", reason, "manifest", manifestPath)
			continue
		}

		staging := skill.NewRegistry()
		if err := openAndRegisterGoSkill(soPath, staging); err != nil {
			logger.Warn("go skill: plugin load failed",
				"skill", sk.Name, "path", soPath, "error", err)
			continue
		}

		body := sk.Body
		for _, def := range staging.List() {
			final := def
			if body != "" {
				final = wrapDefinitionWithBody(def, body)
			}
			if err := reg.Register(final); err != nil {
				logger.Warn("go skill: register failed",
					"skill", sk.Name, "definition", def.Name, "error", err)
			}
		}
	}
}

// wrapDefinitionWithBody returns a copy of def whose Invoker layers
// body onto the call's context via skill.WithSkillBody before
// delegating. The original Definition's runContext was constructed
// with body="" at plugin-build time (the scaffold passes "" because
// the SKILL.md body is not available inside the plugin's package);
// the override lets the user's run() read ctx.SkillBody() and see the
// on-disk prose without the plugin needing to know how to wire it.
func wrapDefinitionWithBody(def *skill.Definition, body string) *skill.Definition {
	if def == nil {
		return nil
	}
	orig := def.Invoker
	wrapped := *def
	wrapped.Invoker = func(ctx context.Context, args map[string]any) (*mcp.ToolCallResult, error) {
		return orig(skill.WithSkillBody(ctx, body), args)
	}
	return &wrapped
}
