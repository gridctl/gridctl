// Package scaffold renders the starter files `gridctl agent init`
// drops into a fresh project: SKILL.md, hello.ts, agent.json. The
// resulting directory is runnable through `gridctl agent dev` with
// no further setup — the IDE opens to a working canvas, not an
// empty one.
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
// existing files are left in place.
type Options struct {
	SkillName string
	Force     bool
}

// Scaffold writes hello.ts + agent.json + SKILL.md into root. The
// root directory is created if missing. Returns ErrNotEmpty if
// Force=false and a target file already exists with a different
// content; pre-existing files with identical bytes are silently
// skipped so re-running `agent init` is idempotent.
func Scaffold(root string, opts Options) (Result, error) {
	if root == "" {
		return Result{}, errors.New("scaffold: root is required")
	}
	if opts.SkillName == "" {
		opts.SkillName = "hello-ts"
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Result{}, fmt.Errorf("scaffold: mkdir %s: %w", root, err)
	}
	files := starterFiles(opts)
	res := Result{}
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
// reviewed.
func starterFiles(opts Options) []starterFile {
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

// helloSkillMD renders a minimal SKILL.md that satisfies the
// agentskills.io frontmatter schema and lands an active state so
// `gridctl agent dev` shows it immediately.
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
