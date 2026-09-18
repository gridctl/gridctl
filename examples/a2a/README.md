# Outbound A2A

`stack.yaml` exposes a remote A2A agent as MCP tools. This experimental,
Unreleased adapter is off by default; the example enables `experimental.a2a`.
It supports A2A 1.0 and 0.3 JSON-RPC without Docker or Podman.

## Setup

Replace the placeholder `a2a.card` URL with your agent's HTTPS Agent Card URL.
Supply its bearer token through the variable store's interactive prompt:

```bash
gridctl var set A2A_TOKEN
gridctl validate examples/a2a/stack.yaml
gridctl apply examples/a2a/stack.yaml
gridctl call agent --help --stack a2a-example
```

For an unauthenticated compatible agent, remove `a2a.auth`. For Bedrock AgentCore,
set `profile: bedrock` and use the URL-encoded runtime ARN in
`/runtimes/{escaped-arn}/invocations/.well-known/agent-card.json`. An advertised
RPC URL on a different origin requires an explicit `a2a.endpoint`. No SigV4 or
automatic OAuth token acquisition is provided. Validation checks the declaration;
apply performs discovery and persists first-use card trust before exposing tools.

## Calls and cleanup

```bash
gridctl call agent__send '{"message":"Hello","return_immediately":true}' --stack a2a-example --format json
gridctl call agent__task_get @private-args.json --stack a2a-example --format json
gridctl call agent__task_cancel @private-args.json --stack a2a-example --format json
gridctl destroy examples/a2a/stack.yaml
```

The send result is JSON inside `result.content[].text`. If it returns a task
handle, create a mode-`0600` `private-args.json` containing a JSON object with
that value in `task_handle`. Keep this file outside version control, protect
stdout and transcripts, and use a confidential channel for remote gateway access.
The get and cancel commands above use only that task handle. A direct Message
response may have no task to poll or cancel.

Only a returned `canceled` state confirms remote cancellation. Local timeout or
destroy does not cancel remote work. Handles cannot be recovered from `--as`,
labels, or run history; replacement, card drift, and restart invalidate them.
Card approval does not restore old handles. Capabilities do not isolate an
agent's shared memory, and fixture coverage does not establish hosted compatibility.

See [A2A configuration](../../docs/config-schema.md#a2a) for skill selection,
hash-bound card approval, result bounds, and continuation rules.
