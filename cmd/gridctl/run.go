package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/registry"

	"github.com/spf13/cobra"
)

// runSandboxTimeout caps a single in-process skill invocation. Skills
// authored against the gateway-wired runtime can run longer; the
// standalone CLI path keeps the bound short so a runaway TS skill
// surrenders the terminal in a reasonable wall-clock window.
const runSandboxTimeout = 60 * time.Second

var (
	runInput   string
	runFormat  string
	runRunID   string
	runQuiet   bool
)

var runCmd = &cobra.Command{
	Use:   "run <skill> [flags]",
	Short: "Execute a registered typed skill",
	Long: `Run a typed skill end-to-end and stream its event timeline.

Resolves <skill> from the local registry (~/.gridctl/registry/skills),
records the run as JSONL at ~/.gridctl/runs/<run_id>.jsonl, and streams
typed events to stdout. The default output is human-readable; --format
json emits one JSON object per event (NDJSON) followed by a final
summary line.

Input may be supplied three ways:
    --input '<json>'          inline JSON object
    --input @path/to/file     contents of a file
    --input -                 read from stdin

The standalone CLI path runs TS skills in-process via the gridctl
sandbox. Skills that need tool() / llm() / approval() bindings should
be invoked through a running daemon (gridctl apply <stack.yaml>) so
the gateway, vault, and approval registry are wired. Go-handler skills
require gridctl agent build registration, which lands in Phase H.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRun(cmd.Context(), args[0])
	},
}

func init() {
	runCmd.Flags().StringVar(&runInput, "input", "", "Skill input: inline JSON, @file, or - for stdin")
	runCmd.Flags().StringVar(&runFormat, "format", "text", "Output format: text or json")
	runCmd.Flags().StringVar(&runRunID, "run-id", "", "Override the generated run ID (advanced; use sparingly)")
	runCmd.Flags().BoolVar(&runQuiet, "quiet", false, "Suppress event stream; print only the final result")

	rootCmd.AddCommand(runCmd)
}

// runRun is the testable entry point. The Cobra wrapper above calls
// it with the cobra context so signal handling propagates naturally.
func runRun(ctx context.Context, skillName string) error {
	input, inputRaw, err := resolveRunInput(runInput, os.Stdin)
	if err != nil {
		return err
	}

	store, err := loadRegistry()
	if err != nil {
		return err
	}
	sk, err := store.GetSkill(skillName)
	if err != nil {
		return fmt.Errorf("resolving skill: %w", err)
	}

	switch sk.HandlerLanguage {
	case "ts":
		return runTSSkill(ctx, store, sk, input, inputRaw)
	case "go":
		return fmt.Errorf("skill %q has a Go handler; standalone CLI execution requires `gridctl agent build` registration (lands in Phase H — see https://github.com/gridctl/gridctl/issues for tracking)", skillName)
	case "":
		return fmt.Errorf("skill %q is markdown-only (no skill.ts or skill.go handler) — nothing to run", skillName)
	default:
		return fmt.Errorf("skill %q has unsupported handler language %q", skillName, sk.HandlerLanguage)
	}
}

// runTSSkill drives the in-process TS sandbox path. handoff() is wired
// against the loaded registry so a TS skill can call other registered
// TS skills locally; tool() and llm() remain unbound and surface as JS
// errors at call time when the skill reaches for them — the explicit
// failure beats faking a result.
func runTSSkill(ctx context.Context, store *registry.Store, sk *registry.AgentSkill, input map[string]any, inputRaw json.RawMessage) error {
	handlerPath, ok := store.HandlerPath(sk.Name)
	if !ok {
		return fmt.Errorf("skill %q: handler path missing — registry walker did not record skill.ts", sk.Name)
	}
	source, err := os.ReadFile(handlerPath) // #nosec G304 -- path resolved through the registry walker
	if err != nil {
		return fmt.Errorf("reading skill source %s: %w", handlerPath, err)
	}

	runID := runRunID
	if runID == "" {
		runID = persist.NewRunID()
	}

	persistStore := runsStore()
	rec, err := persistStore.OpenWriter(runID)
	if err != nil {
		return fmt.Errorf("opening run ledger: %w", err)
	}
	defer func() {
		if cerr := rec.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warn: closing run ledger: %v\n", cerr)
		}
	}()

	// Build a registry-backed dispatcher so handoff() resolves to other
	// TS skills the local registry knows about. tool() and llm() are
	// intentionally nil — see the function comment.
	skillRegistry := buildLocalSkillRegistry(store)

	emit := func(eventType persist.EventType, payload any) {
		ev, err := rec.Record(eventType, payload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: recording %s: %v\n", eventType, err)
			return
		}
		if !runQuiet {
			emitEventLine(ev)
		}
	}

	emit(persist.EventRunStarted, persist.RunStartedPayload{
		Skill:  sk.Name,
		Flavor: "ts",
		Input:  inputRaw,
	})

	nodeID := sk.Name
	emit(persist.EventNodeEnter, persist.NodeEnterPayload{
		NodeID:   nodeID,
		NodeName: sk.Name,
	})

	sb := sandbox.New(runSandboxTimeout)
	bindings := sandbox.Bindings{SkillCaller: skillRegistry}
	start := time.Now()
	result, runErr := sb.Execute(ctx, string(source), input, bindings)
	durMicros := time.Since(start).Microseconds()

	emit(persist.EventNodeExit, persist.NodeExitPayload{
		NodeID:         nodeID,
		DurationMicros: durMicros,
		Success:        runErr == nil,
	})

	if runErr != nil {
		emit(persist.EventError, persist.ErrorPayload{
			Message: runErr.Error(),
			NodeID:  nodeID,
		})
		emit(persist.EventRunCompleted, persist.RunCompletedPayload{
			Status: "error",
			Error:  runErr.Error(),
		})
		printRunFooter(runID, "error", "")
		return runErr
	}

	output := json.RawMessage("null")
	if result != nil && result.Value != "" {
		// Validate before wrapping: a non-JSON return from the sandbox
		// would silently corrupt the ledger row otherwise.
		var probe any
		if jerr := json.Unmarshal([]byte(result.Value), &probe); jerr == nil {
			output = json.RawMessage(result.Value)
		} else {
			fmt.Fprintf(os.Stderr, "warn: skill returned non-JSON value, recording null: %v\n", jerr)
		}
	}
	emit(persist.EventRunCompleted, persist.RunCompletedPayload{
		Status: "ok",
		Output: output,
	})
	printRunFooter(runID, "ok", string(output))
	return nil
}

// buildLocalSkillRegistry walks the registry store and registers every
// TS-handler skill into a fresh skill.Registry so handoff() resolves
// against a real dispatcher in the standalone CLI path. Skills that
// fail to register (duplicates, malformed schemas) are skipped with a
// stderr warning rather than aborting the run — handoff() to a missing
// skill surfaces a clear JS error if the running skill needs it.
func buildLocalSkillRegistry(store *registry.Store) *skill.Registry {
	reg := skill.NewRegistry()
	sb := sandbox.New(runSandboxTimeout)
	for _, sk := range store.ListSkills() {
		if sk.HandlerLanguage != "ts" {
			continue
		}
		path, ok := store.HandlerPath(sk.Name)
		if !ok {
			continue
		}
		loader := func(p string) sandbox.SourceLoader {
			return func(string) (string, error) {
				data, err := os.ReadFile(p) // #nosec G304 -- registry-walker derived path
				if err != nil {
					return "", err
				}
				return string(data), nil
			}
		}(path)
		invoker := sb.NewInvoker(sk.Name, loader, func(ctx context.Context) sandbox.Bindings {
			return sandbox.Bindings{}
		})
		def := &skill.Definition{
			Name:        sk.Name,
			Description: sk.Description,
			Invoker:     invoker,
		}
		if err := reg.Register(def); err != nil {
			fmt.Fprintf(os.Stderr, "warn: registering %q for handoff: %v\n", sk.Name, err)
		}
	}
	return reg
}

// resolveRunInput returns both the decoded map (for the sandbox) and
// the original JSON bytes (for the persist.RunStartedPayload). The
// raw form is preserved verbatim so resume can replay the same input
// without re-encoding through Go's map iteration order.
func resolveRunInput(flag string, stdin io.Reader) (map[string]any, json.RawMessage, error) {
	raw, err := readRunInputBytes(flag, stdin)
	if err != nil {
		return nil, nil, err
	}
	if len(raw) == 0 {
		return map[string]any{}, json.RawMessage("{}"), nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, fmt.Errorf("parsing --input as JSON object: %w", err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}
	canonical, _ := json.Marshal(decoded)
	return decoded, canonical, nil
}

// readRunInputBytes resolves the --input flag's three forms (inline,
// file via @prefix, stdin via -) into the raw input bytes. An empty
// flag means no input — the sandbox sees {}.
func readRunInputBytes(flag string, stdin io.Reader) ([]byte, error) {
	flag = strings.TrimSpace(flag)
	switch {
	case flag == "":
		return nil, nil
	case flag == "-":
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading --input from stdin: %w", err)
		}
		return data, nil
	case strings.HasPrefix(flag, "@"):
		path := flag[1:]
		data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- user-supplied input path
		if err != nil {
			return nil, fmt.Errorf("reading --input file %s: %w", path, err)
		}
		return data, nil
	default:
		return []byte(flag), nil
	}
}

// emitEventLine prints one event in the format runFormat dictates.
// Pretty path reuses the inspect renderer (printEventLine) so a
// streaming run reads the same as an after-the-fact `runs inspect`.
// JSON path emits NDJSON so downstream tools can `jq` line-by-line.
func emitEventLine(ev persist.Event) {
	if strings.EqualFold(runFormat, "json") {
		raw, err := json.Marshal(ev)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: marshaling event %s: %v\n", ev.Type, err)
			return
		}
		fmt.Println(string(raw))
		return
	}
	printEventLine(ev)
}

// printRunFooter writes the post-run summary line. JSON output is
// shape-stable across quiet / verbose modes — both paths emit one
// final object so a streaming consumer reads the entire transcript
// as well-formed NDJSON. Text output drops to bare value(s) under
// --quiet so it composes with shell pipelines.
func printRunFooter(runID, status, output string) {
	if strings.EqualFold(runFormat, "json") {
		payload := map[string]any{
			"run_id": runID,
			"status": status,
		}
		if output != "" {
			payload["output"] = json.RawMessage(output)
		}
		_ = json.NewEncoder(os.Stdout).Encode(payload)
		return
	}
	if runQuiet {
		if output != "" {
			fmt.Println(output)
		} else {
			fmt.Println(status)
		}
		return
	}
	fmt.Printf("\nrun %s -> %s\n", runID, status)
	if output != "" && status == "ok" {
		fmt.Printf("output: %s\n", output)
	}
	fmt.Printf("inspect: gridctl runs inspect %s\n", runID)
}

// resetRunFlagsForTest restores the package-level flag vars to their
// default state. Cobra binds these directly so values leak across
// in-process test invocations without an explicit reset.
func resetRunFlagsForTest() {
	runInput = ""
	runFormat = "text"
	runRunID = ""
	runQuiet = false
}
