---
name: chained-calculation
description: Cross-server chain — add numbers, echo the result, then get the time
tags:
  - cross-server
  - chaining
allowed-tools: math-server__add, text-server__echo, math-server__get_time
state: draft
---

# Chained Calculation

A three-step workflow spanning two MCP servers (`math-server` and `text-server`).

## Steps

1. Call `math-server__add` with inputs `a` and `b`
2. Call `text-server__echo` with the sum from step 1
3. Call `math-server__get_time` to return the current timestamp

## Usage

Provide two numbers (`a` and `b`). The skill adds them on one server, echoes the result on another, and appends the current time.

> **Note:** Requires the `registry-advanced` stack which registers both `math-server` and `text-server`.
