# SEP-2127 Server Card schema

Copyright 2025–2026 The MCP Server Card Authors.

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this schema except in compliance with the License. You may obtain a copy
at <https://www.apache.org/licenses/LICENSE-2.0>. Unless required by
applicable law or agreed to in writing, software distributed under the
License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS
OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.

`schema.json` is vendored from
[modelcontextprotocol/ext-server-card](https://github.com/modelcontextprotocol/ext-server-card)
at commit `92999cb3f5d82e648385cb92ce41ed9aa1386eda` (2026-06-27,
`docs: drop references to the MCP initialization handshake (SEP-2575)`).
That file is the contract for documents this package builds and accepts.
SEP-2127 is Final. `docs/discovery.md` in that repository and the AI Catalog
specification are drafts and are not this contract.

Gridctl does not fetch the schema over the network at runtime or in tests.
