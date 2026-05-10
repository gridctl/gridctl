package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/dev/scaffold"
	"github.com/gridctl/gridctl/pkg/agent/dev/watcher"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/registry"

	"github.com/spf13/cobra"
)

// goosProbe reports the current platform. Defaulted to runtime.GOOS;
// tests override it to exercise the Windows pre-flight error without
// running on Windows.
var goosProbe = func() string { return runtime.GOOS }

// sha256Hex returns the hex-encoded SHA-256 of b. Used by agent build
// to fingerprint handler source so reruns over identical input produce
// identical manifests.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// agentCmd is the parent for skill-authoring-time operations:
// editor canvas (`agent dev`), scaffolding (`agent init`),
// validation (`agent validate`), and the explicit skill compile
// step (`agent build`). Phase F ships dev + init; build + validate
// land in Phase G alongside the rest of the CLI surface.
var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Skill authoring tools — IDE, scaffolds, build, validate",
	Long: `The agent command groups skill-authoring operations.

  agent init        scaffold a runnable hello-world TS skill in cwd
  agent dev         start the IDE dev server (live canvas + trace overlay)
  agent validate    validate a skill's manifest and handler
  agent build       compile + emit a publishable manifest for a skill

The IDE is read-only with respect to source — code is canon. The
canvas is a derived view of the AST on disk; click any node to jump
to that file:line in $EDITOR.`,
}

var (
	agentDevPort        int
	agentDevRoot        string
	agentDevFormat      string
	agentInitName       string
	agentInitForce      bool
	agentInitDir        string
	agentInitFormat     string
	agentInitLang       string
	agentInitPromptOnly bool
	agentValidateFormat string
	agentBuildFormat    string
	agentBuildOutDir    string
)

var agentDevCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start the agent IDE dev server",
	Long: `Boots an HTTP server that serves the agent IDE at localhost:<port>.

The server walks the project root, parses every typed-skill source it
finds, and streams file-watcher events to the IDE so saves reflect on
the canvas in <300ms (TS) or after explicit 'agent build' (Go).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := agentDevRoot
		if root == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("agent dev: getwd: %w", err)
			}
			root = cwd
		}
		root, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("agent dev: abs %s: %w", root, err)
		}
		w, err := watcher.New(root)
		if err != nil {
			return fmt.Errorf("agent dev: watcher: %w", err)
		}
		srv, err := devserver.NewServer(root, w)
		if err != nil {
			return fmt.Errorf("agent dev: server: %w", err)
		}

		ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		// Run the watcher concurrently with the HTTP server.
		watchErrCh := make(chan error, 1)
		go func() { watchErrCh <- w.Run(ctx) }()

		addr := fmt.Sprintf("127.0.0.1:%d", agentDevPort)
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("agent dev: listen %s: %w", addr, err)
		}

		httpSrv := &http.Server{
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		serveErrCh := make(chan error, 1)
		go func() { serveErrCh <- httpSrv.Serve(listener) }()

		startup := struct {
			Status string `json:"status"`
			Addr   string `json:"addr"`
			Root   string `json:"root"`
		}{Status: "ready", Addr: "http://" + listener.Addr().String(), Root: root}

		if strings.EqualFold(agentDevFormat, "json") {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(startup)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "agent IDE ready at %s (root=%s)\n", startup.Addr, root)
		}

		select {
		case <-ctx.Done():
		case err := <-serveErrCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		case err := <-watchErrCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		return nil
	},
}

var agentInitCmd = &cobra.Command{
	Use:   "init [DIR]",
	Short: "Scaffold a runnable hello-world skill",
	Long: `Drops a starter skill into DIR.

Three flavors:
  agent init                    TS skill: SKILL.md + skill.ts + agent.json (default)
  agent init --lang go          Go skill: SKILL.md + skill.go + skill_test.go
  agent init --prompt-only      Prompt-only skill: SKILL.md only

DIR defaults to the current directory. Existing files are skipped
(re-running 'agent init' is idempotent unless --force is set).
--prompt-only is mutually exclusive with --lang.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentInit,
}

// runAgentInit is the agent init subcommand body, extracted so
// tests can drive it directly with a constructed cobra command in
// the same shape as runAgentValidate / runAgentBuild.
func runAgentInit(cmd *cobra.Command, args []string) error {
	flavor, err := resolveAgentInitFlavor(cmd)
	if err != nil {
		return err
	}
	dir := agentInitDir
	if len(args) == 1 {
		dir = args[0]
	}
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("agent init: getwd: %w", err)
		}
		dir = cwd
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("agent init: abs %s: %w", dir, err)
	}
	res, err := scaffold.Scaffold(dir, scaffold.Options{
		SkillName: agentInitName,
		Force:     agentInitForce,
		Language:  flavor,
	})
	if err != nil {
		return fmt.Errorf("agent init: %w", err)
	}
	if strings.EqualFold(agentInitFormat, "json") {
		_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"dir":     dir,
			"flavor":  flavor,
			"created": res.Created,
			"skipped": res.Skipped,
		})
		return nil
	}
	for _, f := range res.Created {
		fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", filepath.Join(dir, f))
	}
	for _, f := range res.Skipped {
		fmt.Fprintf(cmd.OutOrStdout(), "skipped %s (already exists)\n", filepath.Join(dir, f))
	}
	if flavor == "prompt" {
		fmt.Fprintf(cmd.OutOrStdout(), "\nrun: gridctl skill list --remote && gridctl run %s\n", agentInitName)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "\nrun: gridctl agent dev --root %q\n", dir)
	}
	return nil
}

// resolveAgentInitFlavor turns the --lang / --prompt-only flag pair
// into the scaffold.Options.Language value. Returns a clear error
// if the user combined --prompt-only with an explicit --lang or
// passed an unrecognised --lang value. Defaults match cobra's flag
// defaults (--lang ts).
func resolveAgentInitFlavor(cmd *cobra.Command) (string, error) {
	langChanged := cmd.Flags().Changed("lang")
	if agentInitPromptOnly && langChanged {
		return "", fmt.Errorf("agent init: --prompt-only is mutually exclusive with --lang %s", agentInitLang)
	}
	if agentInitPromptOnly {
		return "prompt", nil
	}
	switch agentInitLang {
	case "", "ts", "go":
		return agentInitLang, nil
	default:
		return "", fmt.Errorf("agent init: unsupported --lang %q (want ts or go)", agentInitLang)
	}
}

var agentValidateCmd = &cobra.Command{
	Use:   "validate <skill>",
	Short: "Validate a skill's manifest and handler",
	Long: `Validates a registered skill's SKILL.md (frontmatter + state) and,
when present, sanity-checks the handler:

  - skill.ts handlers transpile cleanly via esbuild
  - skill.go handlers parse cleanly and declare the plugin entry point
    'func RegisterSkill(*skill.Registry) error' (the symbol the gateway
    looks up after plugin.Open)

This is a static check — the skill is not invoked. The Go path does
not run 'go build'; use 'gridctl agent build <skill>' to compile.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentValidate(args[0])
	},
}

var agentBuildCmd = &cobra.Command{
	Use:   "build <skill>",
	Short: "Compile a skill and emit a publishable manifest",
	Long: `Compiles a typed skill and writes a manifest the registry can
publish.

For TS handlers the compile step runs esbuild and writes the
transpiled JS plus a manifest.json (name, description, input schema,
handler hash) to the output directory (defaults to <skill_dir>/dist/).

For Go handlers the compile step shells out to 'go build
-buildmode=plugin' and writes 'skill.so' alongside a manifest carrying
two extra guardrail fields: 'go_version' (the building toolchain) and
'go_mod_hash' (sha256 of the resolved go.mod). The gateway-builder
loader checks both at start time and skips plugins built against a
mismatched toolchain or dep graph. Go plugins are Linux/macOS only;
Windows returns a clear pre-flight error.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentBuild(args[0])
	},
}

func init() {
	agentDevCmd.Flags().IntVar(&agentDevPort, "port", 8181, "Port to bind the dev server on")
	agentDevCmd.Flags().StringVar(&agentDevRoot, "root", "", "Project root to watch (defaults to cwd)")
	agentDevCmd.Flags().StringVar(&agentDevFormat, "format", "text", "Output format: text or json")

	agentInitCmd.Flags().StringVar(&agentInitName, "name", "hello-ts", "Skill name to scaffold")
	agentInitCmd.Flags().StringVar(&agentInitDir, "dir", "", "Directory to scaffold into (defaults to cwd or first arg)")
	agentInitCmd.Flags().BoolVar(&agentInitForce, "force", false, "Overwrite existing files")
	agentInitCmd.Flags().StringVar(&agentInitFormat, "format", "text", "Output format: text or json")
	agentInitCmd.Flags().StringVar(&agentInitLang, "lang", "ts", "Skill language: ts or go")
	agentInitCmd.Flags().BoolVar(&agentInitPromptOnly, "prompt-only", false, "Scaffold a prompt-only skill (SKILL.md only; mutually exclusive with --lang)")

	agentValidateCmd.Flags().StringVar(&agentValidateFormat, "format", "text", "Output format: text or json")

	agentBuildCmd.Flags().StringVar(&agentBuildFormat, "format", "text", "Output format: text or json")
	agentBuildCmd.Flags().StringVar(&agentBuildOutDir, "out", "", "Output directory for the manifest (defaults to <skill_dir>/dist/)")

	agentCmd.AddCommand(agentDevCmd)
	agentCmd.AddCommand(agentInitCmd)
	agentCmd.AddCommand(agentValidateCmd)
	agentCmd.AddCommand(agentBuildCmd)
	rootCmd.AddCommand(agentCmd)
}

// agentValidateReport is the structured shape returned by
// `gridctl agent validate <skill> --format json`. Mirrors what the
// pretty path prints so JSON consumers can re-render losslessly.
type agentValidateReport struct {
	Skill    string   `json:"skill"`
	Handler  string   `json:"handler,omitempty"` // "go", "ts", ""
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Valid    bool     `json:"valid"`
}

// agentBuildReport is the structured shape returned by
// `gridctl agent build <skill> --format json`. Manifest is written to
// disk; this is the index-of-what-was-produced.
type agentBuildReport struct {
	Skill        string   `json:"skill"`
	Handler      string   `json:"handler"`
	OutDir       string   `json:"out_dir"`
	ManifestPath string   `json:"manifest_path,omitempty"`
	Artifacts    []string `json:"artifacts,omitempty"`
	Status       string   `json:"status"` // "ok" or "deferred"
	Notes        []string `json:"notes,omitempty"`
}

func runAgentValidate(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}
	sk, err := store.GetSkill(name)
	if err != nil {
		return err
	}

	result := registry.ValidateSkillFull(sk)
	report := agentValidateReport{
		Skill:    name,
		Handler:  sk.HandlerLanguage,
		Errors:   append([]string(nil), result.Errors...),
		Warnings: append([]string(nil), result.Warnings...),
		Valid:    result.Valid(),
	}

	if sk.HandlerLanguage == "ts" {
		path, ok := store.HandlerPath(name)
		if !ok {
			report.Errors = append(report.Errors, "handler path missing in registry")
			report.Valid = false
		} else {
			source, readErr := os.ReadFile(path) // #nosec G304 -- registry-walker derived path
			if readErr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("reading handler %s: %v", path, readErr))
				report.Valid = false
			} else if _, terr := sandbox.TranspileTS(string(source)); terr != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("transpile failed: %v", terr))
				report.Valid = false
			}
		}
	}

	if sk.HandlerLanguage == "go" {
		path, ok := store.HandlerPath(name)
		if !ok {
			report.Errors = append(report.Errors, "handler path missing in registry")
			report.Valid = false
		} else if symErr := validateGoSkillSymbol(path); symErr != nil {
			report.Errors = append(report.Errors, symErr.Error())
			report.Valid = false
		}
	}

	if strings.EqualFold(agentValidateFormat, "json") {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	for _, e := range report.Errors {
		fmt.Printf("  ✗ %s: %s\n", name, e)
	}
	for _, w := range report.Warnings {
		fmt.Printf("  ⚠ %s: %s\n", name, w)
	}
	if report.Valid {
		fmt.Printf("✓ %s is valid (handler=%s)\n", name, fallback(report.Handler, "none"))
	} else {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

func runAgentBuild(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}
	sk, err := store.GetSkill(name)
	if err != nil {
		return err
	}

	switch sk.HandlerLanguage {
	case "ts":
		return runAgentBuildTS(store, sk)
	case "go":
		return runAgentBuildGo(store, sk)
	case "":
		return fmt.Errorf("skill %q is markdown-only — nothing to build", name)
	default:
		return fmt.Errorf("skill %q has unsupported handler language %q", name, sk.HandlerLanguage)
	}
}

// runAgentBuildTS transpiles the TS handler, writes the JS output
// alongside a manifest.json the registry publish path can read. The
// fingerprint is sha256 of the source bytes so reruns over identical
// input produce identical manifests.
func runAgentBuildTS(store *registry.Store, sk *registry.AgentSkill) error {
	handlerPath, ok := store.HandlerPath(sk.Name)
	if !ok {
		return fmt.Errorf("skill %q: handler path missing in registry", sk.Name)
	}
	source, err := os.ReadFile(handlerPath) // #nosec G304 -- registry-walker derived path
	if err != nil {
		return fmt.Errorf("reading handler %s: %w", handlerPath, err)
	}
	transpiled, err := sandbox.TranspileTS(string(source))
	if err != nil {
		return fmt.Errorf("transpile %s: %w", handlerPath, err)
	}

	outDir := agentBuildOutDir
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(handlerPath), "dist")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // skill artifacts are user-readable by design
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	jsPath := filepath.Join(outDir, "skill.js")
	if err := os.WriteFile(jsPath, []byte(transpiled), 0o644); err != nil { //nolint:gosec // user-readable artifact
		return fmt.Errorf("writing %s: %w", jsPath, err)
	}

	hash := sha256Hex(source)
	manifest := map[string]any{
		"name":         sk.Name,
		"description":  sk.Description,
		"handler":      "ts",
		"handler_path": "skill.js",
		"source_hash":  hash,
		"input_schema": map[string]any{"type": "object"},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	manifestPath := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil { //nolint:gosec // user-readable artifact
		return fmt.Errorf("writing %s: %w", manifestPath, err)
	}

	report := agentBuildReport{
		Skill:        sk.Name,
		Handler:      "ts",
		OutDir:       outDir,
		ManifestPath: manifestPath,
		Artifacts:    []string{jsPath, manifestPath},
		Status:       "ok",
	}
	if strings.EqualFold(agentBuildFormat, "json") {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("✓ built %s -> %s\n", sk.Name, manifestPath)
	for _, a := range report.Artifacts {
		fmt.Printf("  artifact: %s\n", a)
	}
	return nil
}

// runAgentBuildGo compiles a Go-handler skill as a Go plugin
// (`go build -buildmode=plugin`) and writes a manifest.json the
// gateway-builder loader keys against at start time. Two guardrail
// fields — `go_version` and `go_mod_hash` — let the loader detect a
// stale plugin (operator upgraded the daemon's toolchain or dep graph
// without rebuilding the plugin) and skip it with an actionable warn
// rather than letting the opaque plugin.Open error confuse the
// operator. Plugins are Linux/macOS only; on Windows we return a
// clear pre-flight error before invoking the toolchain.
func runAgentBuildGo(store *registry.Store, sk *registry.AgentSkill) error {
	if goosProbe() == "windows" {
		return errors.New("go skill build requires Linux or macOS — Go plugins are not available on Windows")
	}

	handlerPath, ok := store.HandlerPath(sk.Name)
	if !ok {
		return fmt.Errorf("skill %q: handler path missing in registry", sk.Name)
	}
	source, err := os.ReadFile(handlerPath) // #nosec G304 -- registry-walker derived path
	if err != nil {
		return fmt.Errorf("reading handler %s: %w", handlerPath, err)
	}

	outDir := agentBuildOutDir
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(handlerPath), "dist")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // skill artifacts are user-readable by design
		return fmt.Errorf("creating %s: %w", outDir, err)
	}

	soPath := filepath.Join(outDir, "skill.so")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", soPath, handlerPath) // #nosec G204 -- registry-walker derived path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("go build %s: %w\n%s", handlerPath, err, msg)
		}
		return fmt.Errorf("go build %s: %w", handlerPath, err)
	}

	hash := sha256Hex(source)
	manifest := map[string]any{
		"name":         sk.Name,
		"description":  sk.Description,
		"handler":      "go",
		"handler_path": "skill.so",
		"source_hash":  hash,
		"input_schema": map[string]any{"type": "object"},
		"go_version":   runtime.Version(),
	}
	if modHash, ok := goModHashFor(handlerPath); ok {
		manifest["go_mod_hash"] = modHash
	} else {
		slog.Warn("agent build: go.mod not found walking up from handler — omitting go_mod_hash from manifest",
			"skill", sk.Name, "handler", handlerPath)
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	manifestPath := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil { //nolint:gosec // user-readable artifact
		return fmt.Errorf("writing %s: %w", manifestPath, err)
	}

	report := agentBuildReport{
		Skill:        sk.Name,
		Handler:      "go",
		OutDir:       outDir,
		ManifestPath: manifestPath,
		Artifacts:    []string{soPath, manifestPath},
		Status:       "ok",
	}
	if strings.EqualFold(agentBuildFormat, "json") {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Printf("✓ built %s -> %s\n", sk.Name, manifestPath)
	for _, a := range report.Artifacts {
		fmt.Printf("  artifact: %s\n", a)
	}
	return nil
}

// goModHashFor walks upward from handlerPath until it finds a go.mod
// and returns the sha256 of its bytes. If no go.mod is found before
// the filesystem root, returns ("", false) so the caller can decide
// whether to omit the field or fail. Standalone skill scaffolds
// without a go.mod parent are unusual but valid — the caller logs at
// warn and continues per Pitfall #9 in the implementation brief.
func goModHashFor(handlerPath string) (string, bool) {
	dir := filepath.Dir(handlerPath)
	for {
		candidate := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			data, err := os.ReadFile(candidate) // #nosec G304 -- candidate is filepath-joined from registry-derived path
			if err != nil {
				return "", false
			}
			return sha256Hex(data), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// validateGoSkillSymbol parses a Go-handler skill's source file via
// go/parser (no toolchain invocation) and reports a clear error when
// the plugin entry point — `func RegisterSkill(*skill.Registry) error`
// — is missing or has the wrong shape. Catches the most common
// copy-paste mistake at validate time so the operator does not waste
// a `go build -buildmode=plugin` round-trip discovering it.
//
// The check is deliberately forgiving: the parameter type's package
// qualifier is not pinned to "skill" (an alias is fine), only the
// pointer-to-Registry shape is. If the symbol is present but the
// daemon still cannot resolve it at plugin.Lookup time, the gateway
// loader's warn path catches the residual cases.
func validateGoSkillSymbol(path string) error {
	src, err := os.ReadFile(path) // #nosec G304 -- registry-walker derived path
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "RegisterSkill" {
			continue
		}
		if fn.Recv != nil {
			return errors.New("RegisterSkill must be a top-level function, not a method")
		}
		if !registerSkillSignatureOK(fn.Type) {
			return errors.New("RegisterSkill must have signature 'func(*skill.Registry) error'")
		}
		return nil
	}
	return errors.New("missing plugin entry point: declare 'func RegisterSkill(*skill.Registry) error' in skill.go")
}

// registerSkillSignatureOK reports whether ft is a "func(*X.Registry)
// error" shape. Helper for validateGoSkillSymbol.
func registerSkillSignatureOK(ft *ast.FuncType) bool {
	if ft == nil || ft.Params == nil || ft.Results == nil {
		return false
	}
	if countFields(ft.Params) != 1 || countFields(ft.Results) != 1 {
		return false
	}
	param := ft.Params.List[0].Type
	star, ok := param.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "Registry" {
		return false
	}
	result := ft.Results.List[0].Type
	ident, ok := result.(*ast.Ident)
	if !ok || ident.Name != "error" {
		return false
	}
	return true
}

// countFields sums the per-field-list name counts. A bare type with
// no name in the field list still counts as one field; e.g.
// `func(*skill.Registry) error` has one nameless param field.
func countFields(fl *ast.FieldList) int {
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
			continue
		}
		n += len(f.Names)
	}
	return n
}
