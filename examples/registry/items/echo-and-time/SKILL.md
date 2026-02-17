---
name: echo-and-time
description: Echo a message and return the current time
tags:
  - basic
  - demo
allowed-tools: local-tools__echo, local-tools__get_time
state: draft
---

# Echo and Time

A two-step workflow using the `local-tools` MCP server.

## Steps

1. Call `local-tools__echo` with the user's message
2. Call `local-tools__get_time` to return the current timestamp

## Usage

Provide a `message` input. The skill echoes it back and appends the current server time.
