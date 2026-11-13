package hooksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"text/template"

	"github.com/gridctl/gridctl/pkg/skills"
)

// openCodePluginTemplateVersion versions openCodePluginTemplate. Any
// template change that changes output bytes bumps it, so a re-rendered
// plugin reads as stale rather than as a hand edit.
const openCodePluginTemplateVersion = 1

// openCodeDefaultTimeoutMs is the deadline for a handler with no timeout:
// 600 seconds, the default the Claude Code hooks reference documents for
// command hooks (https://code.claude.com/docs/en/hooks). Every rule the
// plugin carries has a finite deadline.
const openCodeDefaultTimeoutMs = 600_000

// openCodeMaxTimeoutMs caps a handler deadline at the largest delay
// setTimeout honors. A larger delay fires at once in Node and Bun.
const openCodeMaxTimeoutMs = math.MaxInt32

// openCodeTools maps Claude Code tool names to OpenCode tool IDs. A
// matcher token outside this table is unexpressible for OpenCode.
var openCodeTools = map[string]string{
	"Bash":      "shell",
	"Edit":      "edit",
	"Glob":      "glob",
	"Grep":      "grep",
	"Read":      "read",
	"WebFetch":  "webfetch",
	"WebSearch": "websearch",
	"Write":     "write",
}

// openCodeInputKeys maps an OpenCode tool's argument key to the Claude
// key a script reads, per tool. The adapter adds the Claude key beside the
// original and passes every other key through unchanged.
var openCodeInputKeys = map[string]map[string]string{
	"edit":  {"path": "file_path"},
	"read":  {"path": "file_path"},
	"write": {"path": "file_path"},
}

type openCodeRule struct {
	Event    string            `json:"event"`
	Tools    []string          `json:"tools"`
	Handlers []openCodeHandler `json:"handlers"`
}

type openCodeHandler struct {
	Script    string   `json:"script"`
	Args      []string `json:"args"`
	TimeoutMs int64    `json:"timeoutMs"`
}

// openCodePluginTemplate targets the OpenCode V2 plugin API. Its inputs
// are the hook name (validated against the hook name charset and used
// only in the header comment), JSON-encoded constants, and the template
// version. No user string is interpolated into code: every value reaches
// the plugin through encoding/json. The template uses double-quoted
// string literals only.
const openCodePluginTemplate = `// gridctl hook plugin v{{.Version}} for hook "{{.Name}}". Generated from the pinned hook.json; do not edit, gridctl rewrites this file on sync.
import { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"

type Handler = { script: string; args: string[]; timeoutMs: number }
type Rule = { event: string; tools: string[] | null; handlers: Handler[] }
type Outcome = { status: number | null; stdout: string; stderr: string; timedOut: boolean }

const HOOK: string = {{.NameJSON}}
const TEMPLATE_VERSION: number = {{.Version}}
const MAX_OUTPUT_BYTES = 1048576
const CLAUDE_TOOL_NAMES: Record<string, string> = {{.ToolNames}}
const CLAUDE_INPUT_KEYS: Record<string, Record<string, string>> = {{.InputKeys}}
const RULES: Rule[] = {{.Rules}}

function claudeInput(tool: string, input: unknown): unknown {
  const keys = CLAUDE_INPUT_KEYS[tool]
  if (!keys || input === null || typeof input !== "object" || Array.isArray(input)) return input
  const out: Record<string, unknown> = { ...(input as Record<string, unknown>) }
  for (const [from, to] of Object.entries(keys)) {
    if (from in out && !(to in out)) out[to] = out[from]
  }
  return out
}

function payloadFor(event: string, input: any, cwd: string): Record<string, unknown> {
  const payload: Record<string, unknown> = {
    hook_event_name: event,
    session_id: input.sessionID ?? "",
    cwd: cwd,
    tool_name: CLAUDE_TOOL_NAMES[input.tool] ?? input.tool,
    tool_input: claudeInput(input.tool, input.input),
  }
  if (event === "PostToolUse") {
    payload.tool_response = input.result !== undefined ? input.result : input.error
  }
  return payload
}

function collector() {
  const chunks: Buffer[] = []
  let size = 0
  return {
    add(chunk: Buffer) {
      if (size >= MAX_OUTPUT_BYTES) return
      const part = chunk.subarray(0, MAX_OUTPUT_BYTES - size)
      chunks.push(part)
      size += part.length
    },
    text(): string {
      return Buffer.concat(chunks).toString("utf8")
    },
  }
}

function runHandler(handler: Handler, payload: Record<string, unknown>): Promise<Outcome> {
  return new Promise((resolve, reject) => {
    const child = spawn(handler.script, handler.args, { shell: false, stdio: ["pipe", "pipe", "pipe"] })
    const stdout = collector()
    const stderr = collector()
    let settled = false
    const timer = setTimeout(() => {
      if (settled) return
      settled = true
      child.kill("SIGKILL")
      resolve({ status: null, stdout: stdout.text(), stderr: stderr.text(), timedOut: true })
    }, handler.timeoutMs)
    child.stdout.on("data", (chunk: Buffer) => stdout.add(chunk))
    child.stderr.on("data", (chunk: Buffer) => stderr.add(chunk))
    child.on("error", (err: Error) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      reject(err)
    })
    child.on("close", (status: number | null) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      resolve({ status: status, stdout: stdout.text(), stderr: stderr.text(), timedOut: false })
    })
    child.stdin.on("error", () => {})
    child.stdin.end(JSON.stringify(payload))
  })
}

function firstText(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value === "string" && value.trim() !== "") return value.trim()
  }
  return undefined
}

function decide(outcome: Outcome): { deny: boolean; reason: string; unparsable: boolean } {
  const text = outcome.stdout.trim()
  let parsed: any = undefined
  let unparsable = false
  if (text !== "") {
    try {
      parsed = JSON.parse(text)
    } catch {
      unparsable = true
    }
  }
  const top: any = parsed !== null && typeof parsed === "object" ? parsed : {}
  const specific: any = top.hookSpecificOutput !== null && typeof top.hookSpecificOutput === "object" ? top.hookSpecificOutput : {}
  const deny = outcome.status === 2 || specific.permissionDecision === "deny" || top.decision === "deny" || top.decision === "block"
  const reason = firstText(specific.permissionDecisionReason, top.reason, outcome.stderr) ?? "blocked by hook"
  return { deny: deny, reason: reason, unparsable: unparsable }
}

function warn(handler: Handler, outcome: string): void {
  console.warn("gridctl hook " + HOOK + " (plugin v" + TEMPLATE_VERSION + "): " + handler.script + ": " + outcome + "; continuing")
}

async function runRules(event: string, input: any, cwd: string): Promise<void> {
  for (const rule of RULES) {
    if (rule.event !== event) continue
    if (rule.tools !== null && !rule.tools.includes(input.tool)) continue
    const payload = payloadFor(event, input, cwd)
    for (const handler of rule.handlers) {
      let outcome: Outcome
      try {
        outcome = await runHandler(handler, payload)
      } catch (err) {
        warn(handler, "spawn failed: " + String(err))
        continue
      }
      if (outcome.timedOut) {
        warn(handler, "timed out after " + handler.timeoutMs + " ms")
        continue
      }
      const result = decide(outcome)
      if (event === "PreToolUse" && result.deny) {
        throw new Error("gridctl hook " + HOOK + " denied " + input.tool + ": " + result.reason)
      }
      if (outcome.status !== 0 && outcome.status !== 2) {
        warn(handler, "exited with status " + String(outcome.status))
      } else if (result.unparsable) {
        warn(handler, "stdout is not JSON")
      }
    }
  }
}

export default Plugin.define({
  id: "gridctl.hook." + HOOK,
  async setup(ctx: any) {
    const cwd: string = ctx.location?.directory ?? ""
    if (RULES.some((rule) => rule.event === "PreToolUse")) {
      await ctx.tool.hook("execute.before", async (event: any) => {
        await runRules("PreToolUse", event, cwd)
      })
    }
    if (RULES.some((rule) => rule.event === "PostToolUse")) {
      await ctx.tool.hook("execute.after", async (event: any) => {
        await runRules("PostToolUse", event, cwd)
      })
    }
  },
})
`

type openCodePluginInput struct {
	Version   int
	Name      string
	NameJSON  string
	ToolNames string
	InputKeys string
	Rules     string
}

// renderOpenCode renders the OpenCode plugin for one hook. PreToolUse and
// PostToolUse groups whose matcher maps through openCodeTools become rules;
// every other group is unexpressible and named. The output is a pure
// function of the definition, the name, the script directory, and the
// template version.
func renderOpenCode(spec *hookSpec, name, scriptDir string) (rendered, error) {
	if err := skills.ValidateHookName(name); err != nil {
		return rendered{}, err
	}
	out := rendered{client: "opencode"}
	var rules []openCodeRule
	for _, g := range spec.Groups {
		if g.Event != "PreToolUse" && g.Event != "PostToolUse" {
			out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("event %q has no opencode mapping", g.Event))
			out.DroppedGroups = appendUnique(out.DroppedGroups, g.Event)
			continue
		}
		tokens, all, ok := matcherTokens(g)
		if !ok {
			pattern := fmt.Sprint(g.Matcher)
			out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("matcher %q is not a plain tool alternation", pattern))
			out.DroppedGroups = appendUnique(out.DroppedGroups, fmt.Sprintf("matcher %q", pattern))
			continue
		}
		var tools []string
		if !all {
			missing := ""
			for _, token := range tokens {
				id, known := openCodeTools[token]
				if !known {
					missing = token
					break
				}
				tools = appendUnique(tools, id)
			}
			if missing != "" {
				out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("matcher token %q has no opencode tool", missing))
				out.DroppedGroups = appendUnique(out.DroppedGroups, fmt.Sprintf("matcher %q", missing))
				continue
			}
		}
		rule := openCodeRule{Event: g.Event, Tools: tools, Handlers: []openCodeHandler{}}
		for _, h := range g.Handlers {
			rule.Handlers = append(rule.Handlers, openCodeHandler{
				Script:    h.scriptPath(scriptDir),
				Args:      h.args(),
				TimeoutMs: openCodeTimeoutMs(h),
			})
			if cond, ok := h.Raw["if"].(string); ok {
				out.DroppedIf = appendUnique(out.DroppedIf, cond)
			}
			for _, key := range h.cosmeticKeys() {
				out.DroppedKeys = appendUnique(out.DroppedKeys, key)
			}
		}
		rules = append(rules, rule)
	}
	sortStrings(out.DroppedKeys)
	if len(rules) == 0 {
		if len(out.Unexpressible) == 0 {
			out.Unexpressible = []string{"hook has no matcher groups"}
		}
		return out, nil
	}
	data, err := renderOpenCodePlugin(name, rules)
	if err != nil {
		return rendered{}, err
	}
	out.Bytes = data
	return out, nil
}

// openCodeTimeoutMs converts the handler's timeout (seconds) to
// milliseconds, or returns the default deadline when it has none.
func openCodeTimeoutMs(h specHandler) int64 {
	sec, ok := h.timeoutSeconds()
	if !ok {
		return openCodeDefaultTimeoutMs
	}
	ms := math.Ceil(sec * 1000)
	if ms > openCodeMaxTimeoutMs {
		return openCodeMaxTimeoutMs
	}
	return int64(ms)
}

func renderOpenCodePlugin(name string, rules []openCodeRule) ([]byte, error) {
	nameJSON, err := json.Marshal(name)
	if err != nil {
		return nil, fmt.Errorf("encoding hook name: %w", err)
	}
	claudeNames := map[string]string{}
	for claude, id := range openCodeTools {
		claudeNames[id] = claude
	}
	toolNames, err := json.Marshal(claudeNames)
	if err != nil {
		return nil, fmt.Errorf("encoding tool table: %w", err)
	}
	inputKeys, err := json.Marshal(openCodeInputKeys)
	if err != nil {
		return nil, fmt.Errorf("encoding input key table: %w", err)
	}
	ruleJSON, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding rule table: %w", err)
	}
	tmpl, err := template.New("opencode-plugin").Parse(openCodePluginTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing plugin template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, openCodePluginInput{
		Version:   openCodePluginTemplateVersion,
		Name:      name,
		NameJSON:  string(nameJSON),
		ToolNames: string(toolNames),
		InputKeys: string(inputKeys),
		Rules:     string(ruleJSON),
	}); err != nil {
		return nil, fmt.Errorf("rendering plugin: %w", err)
	}
	return buf.Bytes(), nil
}
