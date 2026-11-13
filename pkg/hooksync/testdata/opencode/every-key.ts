// gridctl hook plugin v1 for hook "every-key". Generated from the pinned hook.json; do not edit, gridctl rewrites this file on sync.
import { Plugin } from "@opencode/plugin"
import { spawn } from "node:child_process"

type Handler = { script: string; args: string[]; timeoutMs: number }
type Rule = { event: string; tools: string[] | null; handlers: Handler[] }
type Outcome = { status: number | null; stdout: string; stderr: string; timedOut: boolean }

const HOOK: string = "every-key"
const TEMPLATE_VERSION: number = 1
const MAX_OUTPUT_BYTES = 1048576
const CLAUDE_TOOL_NAMES: Record<string, string> = {"edit":"Edit","glob":"Glob","grep":"Grep","read":"Read","shell":"Bash","webfetch":"WebFetch","websearch":"WebSearch","write":"Write"}
const CLAUDE_INPUT_KEYS: Record<string, Record<string, string>> = {"edit":{"path":"file_path"},"read":{"path":"file_path"},"write":{"path":"file_path"}}
const RULES: Rule[] = [
  {
    "event": "PostToolUse",
    "tools": [
      "write",
      "edit"
    ],
    "handlers": [
      {
        "script": "/home/u/.gridctl/hooks/block-force-push@opencode/scripts/fmt.sh",
        "args": [
          "--check",
          "a b"
        ],
        "timeoutMs": 30000
      }
    ]
  },
  {
    "event": "PreToolUse",
    "tools": null,
    "handlers": [
      {
        "script": "/home/u/.gridctl/hooks/block-force-push@opencode/scripts/a.sh",
        "args": [],
        "timeoutMs": 600000
      },
      {
        "script": "/home/u/.gridctl/hooks/block-force-push@opencode/scripts/b.sh",
        "args": [],
        "timeoutMs": 1500
      }
    ]
  }
]

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
