---
name: workflow-basic
description: Add two numbers and echo the result — basic sequential workflow
tags:
  - workflow
  - demo
allowed-tools: local-tools__add, local-tools__echo
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
  - id: add-numbers
    tool: local-tools__add
    args:
      a: "{{ inputs.a }}"
      b: "{{ inputs.b }}"

  - id: echo-result
    tool: local-tools__echo
    args:
      message: "The sum is: {{ steps.add-numbers.result }}"
    depends_on: add-numbers

output:
  format: last
---

# Basic Workflow

A two-step sequential workflow demonstrating inputs, step dependencies, and
template expressions.

## What It Does

1. Adds two numbers using `local-tools__add`
2. Echoes the result using `local-tools__echo`

## Usage

Requires the `registry-basic` stack (`local-tools` server).

```bash
# Copy into registry
cp -r examples/registry/items/workflow-basic ~/.gridctl/registry/skills/

# Activate
curl -X POST http://localhost:8180/api/registry/skills/workflow-basic/activate

# Execute
curl -X POST http://localhost:8180/api/registry/skills/workflow-basic/execute \
  -H 'Content-Type: application/json' \
  -d '{"arguments": {"a": 5, "b": 3}}'
```
