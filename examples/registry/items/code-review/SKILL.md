---
name: code-review
description: Structured code review prompt with language and focus area
tags:
  - development
  - review
state: draft
---

# Code Review

Review the following {{language}} code. Focus on {{focus_area}}.

Provide feedback organized as:

1. **Issues found** (bugs, security, performance)
2. **Suggestions for improvement**
3. **What the code does well**

```
{{code}}
```
