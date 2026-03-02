---
name: workflow-conditional
description: Conditional workflow — retry on failure and skip dependent steps
tags:
  - workflow
  - error-handling
  - demo
allowed-tools: local-tools__add, local-tools__echo, local-tools__get_time
state: draft

inputs:
  a:
    type: number
    description: First number
    required: true
  b:
    type: number
    description: Second number
    required: true

workflow:
  - id: compute
    tool: local-tools__add
    args:
      a: "{{ inputs.a }}"
      b: "{{ inputs.b }}"
    retry:
      max_attempts: 2
      backoff: "1s"
    on_error: skip

  - id: format-result
    tool: local-tools__echo
    args:
      message: "Computed: {{ steps.compute.result }}"
    depends_on: compute

  - id: timestamp
    tool: local-tools__get_time
    on_error: continue

output:
  format: merged
---

# Conditional Workflow

Demonstrates error handling policies and retry behavior.

## Error Handling

- **compute**: Retries up to 2 attempts with 1s backoff. On final failure,
  skips itself and `format-result` (its dependent)
- **format-result**: Only runs if `compute` succeeds
- **timestamp**: Runs independently. Uses `on_error: continue` so its failure
  doesn't stop the workflow

## Usage

Requires the `registry-basic` stack (`local-tools` server).

```bash
cp -r examples/registry/items/workflow-conditional ~/.gridctl/registry/skills/
curl -X POST http://localhost:8180/api/registry/skills/workflow-conditional/activate
curl -X POST http://localhost:8180/api/registry/skills/workflow-conditional/execute \
  -H 'Content-Type: application/json' \
  -d '{"arguments": {"a": 7, "b": 3}}'
```
