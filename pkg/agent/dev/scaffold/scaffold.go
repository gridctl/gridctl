// Package scaffold renders the starter files `gridctl agent init`
// drops into a fresh project. The TS flavor (default) writes
// SKILL.md + skill.ts + agent.json — runnable through `gridctl
// agent dev` with no further setup. The prompt-only flavor writes
// just SKILL.md so authors can hand-edit a body that surfaces to
// upstream MCP clients as a prompt or tool description. The Go
// flavor writes SKILL.md + skill.go + skill_test.go — a typed Go
// skill the operator builds with `gridctl agent build` (Phase 4)
// and the gateway loads as a plugin at start (Phase 5).
package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Result describes what Scaffold wrote to disk. Useful for tests
// and the CLI output.
type Result struct {
	// Created lists files that did not exist before scaffolding.
	Created []string

	// Skipped lists files that already existed and were left
	// untouched. Scaffold never overwrites — the operator is
	// expected to re-run `agent init` only in a clean directory.
	Skipped []string
}

// Options controls the scaffolded files. SkillName seeds the
// SKILL.md frontmatter and the hello-world's identifier; an empty
// value defaults to "hello-ts". Force=false (the default) means
// existing files are left in place. Language picks the flavor:
// "" or "ts" → TypeScript skill (existing behavior, default);
// "prompt" → SKILL.md only (no handler sibling); "go" → typed Go
// skill (SKILL.md + skill.go + skill_test.go). Any other value is
// rejected.
type Options struct {
	SkillName string
	Force     bool
	Language  string
}

// Scaffold writes the starter files for the requested flavor into
// root. The root directory is created if missing. Pre-existing
// files with identical bytes are silently skipped so re-running
// `agent init` is idempotent; non-identical files are skipped
// unless Force=true. Returns an error for unsupported Language
// values before any file is written.
func Scaffold(root string, opts Options) (Result, error) {
	if root == "" {
		return Result{}, errors.New("scaffold: root is required")
	}
	if opts.SkillName == "" {
		opts.SkillName = "hello-ts"
	}
	switch opts.Language {
	case "", "ts", "prompt", "go":
		// supported flavors
	default:
		return Result{}, fmt.Errorf("scaffold: unsupported language %q (want ts, go, or prompt)", opts.Language)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Result{}, fmt.Errorf("scaffold: mkdir %s: %w", root, err)
	}
	files := starterFiles(opts)
	res := Result{Created: []string{}, Skipped: []string{}}
	for _, f := range files {
		dst := filepath.Join(root, f.path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return res, err
		}
		existing, err := os.ReadFile(dst)
		if err == nil {
			if string(existing) == f.body || !opts.Force {
				res.Skipped = append(res.Skipped, f.path)
				continue
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		if err := os.WriteFile(dst, []byte(f.body), 0o644); err != nil {
			return res, fmt.Errorf("scaffold: write %s: %w", dst, err)
		}
		res.Created = append(res.Created, f.path)
	}
	return res, nil
}

// starterFile is one path/body pair the scaffolder writes.
type starterFile struct {
	path string
	body string
}

// starterFiles returns the contents to write. Kept inline so the
// scaffold has no embed dependency and stays read-trivially when
// reviewed. The flavor branches here, not in Scaffold, so the
// write loop stays single-shape regardless of language.
func starterFiles(opts Options) []starterFile {
	switch opts.Language {
	case "prompt":
		return []starterFile{
			{
				path: "SKILL.md",
				body: promptSkillMD(opts.SkillName),
			},
		}
	case "go":
		return []starterFile{
			{
				path: "SKILL.md",
				body: helloSkillMD(opts.SkillName),
			},
			{
				path: "skill.go",
				body: helloSkillGo(opts.SkillName),
			},
			{
				path: "skill_test.go",
				body: helloSkillGoTest(opts.SkillName),
			},
		}
	}
	return []starterFile{
		{
			path: "SKILL.md",
			body: helloSkillMD(opts.SkillName),
		},
		{
			path: "skill.ts",
			body: helloSkillTS(opts.SkillName),
		},
		{
			path: "agent.json",
			body: helloAgentJSON(opts.SkillName),
		},
	}
}

// helloSkillMD renders a minimal SKILL.md for the TS flavor that
// satisfies the agentskills.io frontmatter schema and lands an
// active state so `gridctl agent dev` shows it immediately.
func helloSkillMD(name string) string {
	return fmt.Sprintf(`---
name: %s
description: Greet the caller via one tool call and one LLM completion.
state: active
---

# %s

A starter typed skill — one tool call, one LLM completion. Edit ` + "`skill.ts`" + ` and the
canvas reflects the change in <300ms.

> The fallacy of the graph applies — code is canon.
`, name, name)
}

// promptSkillMD renders a prompt-only SKILL.md — frontmatter +
// markdown body, no handler sibling. Upstream MCP clients consume
// the body verbatim as a prompt or tool description; gridctl does
// not template-expand `{{double_brace}}` placeholders, so any
// substitution is the upstream client's responsibility.
func promptSkillMD(name string) string {
	return fmt.Sprintf(`---
name: %s
description: Starter prompt-only skill — body becomes the surfaced prompt.
state: active
---

# %s

A starter prompt-only skill. The body of this file is what upstream MCP
clients see — there is no handler sibling. Edit the body to shape the
prompt; rerun `+"`gridctl skill list --remote`"+` to confirm it surfaces.

Replace this body with the actual prompt content. Variables inside
`+"`{{double_braces}}`"+` are convention-only — gridctl does not
template-expand them; the upstream client is responsible for
substitution.

> The fallacy of the graph applies — code is canon. For prompt-only
> skills, the markdown body is the canon.
`, name, name)
}

// HelloSkillTS returns the body of the scaffolded skill.ts as bytes
// the regression suite can run through the sandbox verbatim. Exporting
// the helper means a future change to the scaffold immediately re-tests
// runtime compatibility — there's no parallel copy of the source to
// drift out of sync.
func HelloSkillTS(name string) string { return helloSkillTS(name) }

// helloSkillTS renders a runnable TS skill exercising the recognised
// primitives. Authors edit this file, the watcher fires, the IDE
// re-renders.
func helloSkillTS(name string) string {
	_ = name
	return `// Hello-world typed skill — exercise the gridctl agent runtime
// primitives. Edit this file and the IDE re-renders the canvas.
//
// The graph: tool() → llm() → return.
import { tool, llm } from "@gridctl/agent";

export interface HelloInput {
  name: string;
}

export interface HelloOutput {
  greeting: string;
}

export default async function run(input: HelloInput): Promise<HelloOutput> {
  // 1. Resolve the caller's preferred greeting style via an MCP tool.
  const style = await tool("gridctl__greeting_style", {
    audience: input.name,
  });

  // 2. Ask the model to phrase the greeting.
  const reply = await llm({
    model: "claude-sonnet-4-6",
    system: "You are a polite agent assistant.",
    messages: [
      {
        role: "user",
        content: ` + "`Greet ${input.name} in a ${style} tone.`" + `,
      },
    ],
  });

  return { greeting: reply.content };
}
`
}

// helloAgentJSON renders the project config the dev server reads.
// The defaults wire to no upstream gateway; the IDE renders the
// graph from source regardless of whether a gateway is reachable.
func helloAgentJSON(name string) string {
	return fmt.Sprintf(`{
  "skill": "%s",
  "default_model": "claude-sonnet-4-6",
  "mcp_servers": []
}
`, name)
}

// HelloSkillGo returns the body of the scaffolded skill.go as bytes
// the regression suite can run through `go build` verbatim. Same
// parity guarantee HelloSkillTS provides for the TS path: a future
// change to the scaffold immediately re-tests compile compatibility,
// no parallel copy of the source to drift out of sync.
func HelloSkillGo(name string) string { return helloSkillGo(name) }

// HelloSkillGoTest returns the body of the scaffolded skill_test.go
// for the same regression-channel reason as HelloSkillGo. The test
// is part of the scaffold output, so the compile-check pass needs
// it on disk alongside skill.go.
func HelloSkillGoTest(name string) string { return helloSkillGoTest(name) }

// helloSkillGo renders a runnable typed Go skill. Authors edit this
// file, then run `gridctl agent build <name>` to compile it as a Go
// plugin (`go build -buildmode=plugin`); the gateway loads the
// resulting skill.so on start and registers the skill against the
// shared registry. Plugins are package main without a runnable
// main() entry point, but `go build` (non-plugin) requires one — so
// the scaffold ships an empty main() stub the plugin build ignores.
func helloSkillGo(name string) string {
	return fmt.Sprintf(`// Hello-world typed Go skill — exercise the gridctl skill SDK
// primitives. Built with 'gridctl agent build %s' as a Go plugin
// (-buildmode=plugin); the gateway opens the resulting skill.so at
// start and calls RegisterSkill against the shared *skill.Registry.
//
// The graph: tool() -> llm() -> return. The first argument is
// skill.RunContext — it embeds context.Context and surfaces the
// SKILL.md body and the registered skill name so a Go skill can
// drive the hybrid pattern (feed body straight into llm.Generate's
// System slot).
//
// The fallacy of the graph applies — code is canon.
package main

import (
	"fmt"

	"github.com/gridctl/gridctl/pkg/agent/llm"
	"github.com/gridctl/gridctl/pkg/agent/skill"
)

// HelloInput is the typed input shape. The jsonschema tag drives
// the schema the gateway hands to upstream MCP clients; the json
// tag is the wire form on the way in.
type HelloInput struct {
	Name string %sjson:"name" jsonschema:"required,description=Name to greet"%s
}

// HelloOutput is the typed output shape. The skill SDK marshals it
// back to MCP content as a single JSON text block.
type HelloOutput struct {
	Greeting string %sjson:"greeting"%s
}

// greetingStyleTool is the unprefixed MCP tool the live skill
// dispatches the style lookup to. Kept as a constant so the
// scaffold reads as data-driven, not magic-string.
const greetingStyleTool = "gridctl__greeting_style"

// providerSlot is reserved for the llm.Provider the runtime plumbs
// in. Keeping the type referenced here pins the import and signals
// where the live wire-up plugs in.
var providerSlot llm.Provider

// run executes the skill. ctx is skill.RunContext: it embeds
// context.Context so cancellation flows through, and exposes
// ctx.SkillBody() / ctx.SkillName() for the hybrid pattern.
func run(ctx skill.RunContext, in HelloInput) (HelloOutput, error) {
	// 1. Resolve the caller's preferred greeting style via an MCP tool.
	//    The runtime hands the typed runner a tool dispatcher through
	//    RunContext; the const above names the tool the live call hits.
	style := "casual"
	_ = greetingStyleTool
	_ = ctx.SkillName()

	// 2. Ask the model to phrase the greeting. The runtime hands the
	//    typed runner an llm.Provider through RunContext; the example
	//    skills under examples/registry/items/ wire the live call.
	//    Hybrid pattern: ctx.SkillBody() returns the SKILL.md body,
	//    suitable for use as llm.Request.System.
	_ = providerSlot
	_ = ctx.SkillBody()
	greeting := fmt.Sprintf("hello %%s (%%s)", in.Name, style)

	return HelloOutput{Greeting: greeting}, nil
}

// New constructs the typed Definition the registry server lifts
// into an MCP tool envelope. Skills called via 'gridctl run' or
// via an upstream MCP client land in run() above. The body argument
// is what ctx.SkillBody() returns inside run; pass "" when the
// SKILL.md body is irrelevant to the handler.
func New() *skill.Definition {
	return skill.MustDefine[HelloInput, HelloOutput](
		%q,
		"Greet the caller via one tool call and one LLM completion.",
		"",
		run,
	)
}

// RegisterSkill is the plugin entry point the gateway-builder loader
// looks up after plugin.Open. The loader hands the plugin the shared
// *skill.Registry; the plugin registers each skill it owns. The
// symbol name and signature are the contract — renaming or
// re-shaping breaks the loader's plugin.Lookup.
func RegisterSkill(reg *skill.Registry) error {
	return reg.Register(New())
}

// main is unused — this package is built as a Go plugin via
// 'go build -buildmode=plugin' and has no executable entry point.
// The empty main() lets plain 'go build' compile-check the source
// without complaining about a missing main function.
func main() {}
`, name, "`", "`", "`", "`", name)
}

// helloSkillGoTest renders the table test that exercises the
// scaffolded run() and New() so authors land with a passing
// compile + test out of the box. The regression suite re-uses this
// body as part of the compile-check: scaffold output must build
// cleanly with the test alongside it.
func helloSkillGoTest(name string) string {
	_ = name
	return `package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeRunContext satisfies skill.RunContext for unit tests. The
// production runtime constructs RunContext through skill.Define;
// here we just need a value that embeds a context and answers the
// two skill-scoped accessors with fixtures.
type fakeRunContext struct {
	context.Context
	body string
	name string
}

func (f fakeRunContext) SkillBody() string { return f.body }
func (f fakeRunContext) SkillName() string { return f.name }

func TestRun_GreetsTheCaller(t *testing.T) {
	rc := fakeRunContext{Context: context.Background(), name: "hello-go"}
	out, err := run(rc, HelloInput{Name: "world"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.Greeting, "world") {
		t.Errorf("greeting = %q, want it to contain 'world'", out.Greeting)
	}
}

func TestNew_ProducesDispatchableDefinition(t *testing.T) {
	def := New()
	if def == nil {
		t.Fatal("New: definition is nil")
	}
	if def.Name == "" {
		t.Error("definition name is empty")
	}
	res, err := def.Invoker(context.Background(), map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("Invoker: %v", err)
	}
	if res == nil || len(res.Content) != 1 {
		t.Fatalf("expected one content item, got %+v", res)
	}
	var out HelloOutput
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.Greeting, "world") {
		t.Errorf("greeting = %q, want it to contain 'world'", out.Greeting)
	}
}
`
}
