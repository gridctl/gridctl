---
name: workflow-parallel
description: Fan-out computation — parallel add operations merged into a summary
tags:
  - workflow
  - parallel
  - demo
allowed-tools: local-tools__add, local-tools__echo, local-tools__get_time
state: draft

inputs:
  x:
    type: number
    description: Base number
    required: true
    default: 10

workflow:
  - id: add-five
    tool: local-tools__add
    args:
      a: "{{ inputs.x }}"
      b: 5

  - id: add-ten
    tool: local-tools__add
    args:
      a: "{{ inputs.x }}"
      b: 10

  - id: timestamp
    tool: local-tools__get_time

  - id: summary
    tool: local-tools__echo
    args:
      message: "x+5={{ steps.add-five.result }}, x+10={{ steps.add-ten.result }} (at {{ steps.timestamp.result }})"
    depends_on: [add-five, add-ten, timestamp]

output:
  format: last
---

# Parallel Workflow

Demonstrates fan-out parallelism and fan-in merge. The first three steps have
no dependencies on each other, so they execute concurrently in DAG Level 0.
The summary step waits for all three before running.

```
Level 0:  add-five  |  add-ten  |  timestamp   (concurrent)
Level 1:  summary                               (after all)
```

## Usage

Requires the `registry-basic` stack (`local-tools` server).

```bash
cp -r examples/registry/items/workflow-parallel ~/.gridctl/registry/skills/
curl -X POST http://localhost:8180/api/registry/skills/workflow-parallel/activate
curl -X POST http://localhost:8180/api/registry/skills/workflow-parallel/execute \
  -H 'Content-Type: application/json' \
  -d '{"arguments": {"x": 42}}'
```
