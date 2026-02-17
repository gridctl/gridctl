---
name: add-and-echo
description: Add two numbers and echo the result
tags:
  - chaining
  - demo
allowed-tools: local-tools__add, local-tools__echo
state: draft
---

# Add and Echo

A two-step workflow demonstrating output piping between tools.

## Steps

1. Call `local-tools__add` with inputs `a` and `b`
2. Call `local-tools__echo` with the result from step 1

## Usage

Provide two numbers (`a` and `b`). The skill adds them and echoes the sum.
