# Proposals

A proposal is a staged change to one governed document, held until a human reviews it. It carries a unified diff, the composite hash of the content its author read, a classification, prevalence counts, opaque evidence references, a reason, and a free-form source label. gridctl stores it, scans it, and writes nothing to the target until someone approves it.

The intended producer is an out-of-band curation worker that drafts changes to rules, skills, and hooks from what it observed. Anyone who wants a library edit reviewed with its evidence before it lands can use the same path. gridctl never generates proposals, reads transcripts, fetches evidence, or calls a model, and nothing is approved automatically.

Three kinds of target are supported:

| Kind | Target | Diff paths are relative to |
|---|---|---|
| `skill` | A registry skill under `~/.gridctl/registry/skills/<name>/` | The skill directory (`SKILL.md` and supporting files) |
| `context` | A rule fragment, or `AGENTS.md` when fragments mode is off | The fragment's directory; the only allowed path is the fragment's own file (`<name>.md` or `AGENTS.md`) |
| `hook` | An imported hook under `~/.gridctl/registry/hooks/<name>/` | The hook directory |

`kind: agent` is a recognized token that create refuses: agent pins do not exist yet, so there is nothing to bind an approval to.

## Storage and record

Each proposal is one JSON file at `~/.gridctl/proposals/<id>.json`, mode `0600`, written atomically under a file lock. Ids are `p-` followed by 12 lowercase hex characters. The store keeps no state between calls; every command and REST request reloads it. The daemon holds no proposal state either. Proposals are stackless: `--stack` on the CLI only selects the skill pin store.

| Field | Meaning |
|---|---|
| `id`, `version` | Proposal id and file version (`"1"`) |
| `kind`, `target` | `skill`, `context`, or `hook`, and the target name |
| `base_hash` | Composite hash of the content the proposer read |
| `base_current` | Whether `base_hash` matched the target at create. `show` and the REST get refresh it |
| `diff` | The unified diff, stored as given (at most 256 KiB) |
| `classification` | `knowledge` or `guarantee` |
| `prevalence` | `sessions` and `runs`, non-negative integers |
| `evidence` | Opaque references, stored as text and never fetched (at most 15) |
| `reason`, `source` | Why the change was proposed, and a free-form label for the producer (at most 256 bytes) |
| `created_at`, `created_by`, `created_by_source` | Creation time and attribution |
| `status` | `pending`, `approved`, or `rejected` |
| `path` | `apply` or `record`, decided at create and decided again at approve |
| `provenance` | `kind` (`local`, `local-import`, `git`, or `pack`), with `pack` or `source_dir` when known |
| `findings` | Advisory poisoning-scan findings over the post-apply content |
| `files` | The relative paths the diff touches, sorted |
| `verdict` | `null` while pending; otherwise `at`, `by`, `by_source`, `reason`, `evidence`, `applied`, `approved_hash`, `new_hash`, `stack` (skills), and `pin_error` when set |

`proposals show` and `GET /api/proposals/{id}` also return a computed `pin_status` (`pinned`, `drift`, `unpinned`, or `unknown` when the pin store is not available). It is never written to the file.

Evidence is capped at 15 references, one fewer than the approvals log allows, because an apply-path approval adds a synthetic `proposal:<id>` reference.

## Creating a proposal

`gridctl propose <file|->` reads a JSON create document from a file or stdin:

```json
{
  "kind": "skill",
  "target": "incident-triage",
  "base_hash": "4be1c0d5e2f3...",
  "diff": "--- a/SKILL.md\n+++ b/SKILL.md\n@@ -8,1 +8,1 @@\n-Page the on-call owner.\n+Page the on-call owner within five minutes.\n",
  "classification": "knowledge",
  "prevalence": {"sessions": 14, "runs": 6},
  "evidence": ["session:abc123", "https://example.test/pull/42"],
  "reason": "Seen in 14 sessions where the page went out late.",
  "source": "dream",
  "created_by": "curator"
}
```

```text
$ gridctl propose proposal.json
✓ Proposal p-3f9a1c2b7d4e staged
  target:    skill/incident-triage
  path:      apply (local)
  findings:  none
  base:      current
  review:    gridctl proposals show p-3f9a1c2b7d4e
```

`--format json` prints the stored record. `POST /api/proposals` accepts the same document.

### Getting the base hash

`base_hash` is the target's composite hash in the exact form the pin stores produce. Two commands print it:

```bash
gridctl proposals base skill incident-triage        # the hash alone
gridctl skill pins diff incident-triage --format json   # composite_hash, pinned skills only
gridctl ctx pins diff style --format json               # composite_hash for a fragment
```

`proposals base` is read-only and needs no pin record, so it works for a skill that has never been pinned, where `skill pins diff` exits 2. It accepts `skill`, `context`, and `hook`, and `--stack` for skills only.

Create does not require the base to match the current content, because a worker may be behind. A stale base is stored with `base_current: false`, and approve refuses it until the proposer re-bases.

### What create refuses

Every refusal happens before a file is written.

- A kind other than `skill`, `context`, or `hook`; `agent` names the missing agent pins.
- A target name that fails its kind's name rule, or a target that does not exist.
- A missing or malformed `base_hash`. The message names `gridctl proposals base` and the kind's `pins diff --format json` command.
- A diff that is missing, larger than 256 KiB, or not a unified diff with at least one file section.
- A diff path that is absolute, contains `..`, has a dotfile component, ends in `.tmp`, names a symlink or a directory, falls under a nested `SKILL.md`, or is not UTF-8 text. These match the set the skill pin hash covers, so every file a proposal writes is pinned.
- File deletion, renames, binary patches, and mode changes. A diff may modify files and add new ones (`--- /dev/null`).
- A diff whose hunks do not apply to a current base. Such a proposal could never be approved. A diff against a stale base is stored, with files and findings computed from its added lines.
- A classification other than `knowledge` or `guarantee`, a negative prevalence count, or a source longer than 256 bytes.
- A credential-shaped evidence reference, reason, source, or `created_by`, using the same rules as pin approvals.

### Findings

Create applies the diff in memory and scans the result with the same poisoning heuristics skill pins use: the post-apply `SKILL.md` and each touched supporting file for a skill, the post-apply fragment for context, and the diff text for a hook. The hook scan is an approximation of the import scanner and is kept on the proposal only; it never reaches a hook pin. `gateway.security.schema_pinning.scan` and `scan_ignore` from the single deployed stack apply. Findings are advisory. A proposal with findings can be approved, but only with a reason.

## Review

```bash
gridctl proposals list                      # pending first, then newest first
gridctl proposals list --status pending --kind skill --format json
gridctl proposals show p-3f9a1c2b7d4e
```

`show` prints the target, status, classification, path, provenance, base status, pin status, and files, then the stored diff verbatim, the findings, numbered evidence, prevalence, reason, source, and the verdict when present. Untrusted text is display-escaped.

The Library workspace has a Proposals segment at `/library?kind=proposal`, with a pending count on the kind switch. It lists proposals by status (pending by default) and opens a detail panel with a summary (what approve will do, provenance, prevalence, base status, pin status), the stored diff with added and removed lines highlighted and non-printable characters escaped, findings, evidence, reason, source, and the verdict. Approve is disabled, with the reason shown, while the base is stale, while the target shows pin drift on the apply path, or while findings exist and the reason field is empty. The reason field starts with the proposal's reason. Reject asks for a reason. The UI has no create form; proposals come from `gridctl propose` or the REST route.

## Approve

`gridctl proposals approve <id>` is compare-and-swap on the base hash. It recomputes the target's composite hash and refuses when it differs from `base_hash`, leaving the proposal pending and the target unchanged. It refuses a proposal that is not pending. `--reason` replaces the proposal's reason for this approval; when findings exist and both are empty, approve refuses. Repeatable `--evidence` appends references to the proposal's own.

Approve then decides the path again, because a skill may have been imported since create.

### Apply path

A locally authored skill, or a local fragment (or `AGENTS.md`), takes the apply path. Before writing, approve reads the target's pin status and refuses when it is `drift`: the current content differs from the pinned content, and applying on top of it would put an unreviewed edit under the proposal's approval entry. Approve or reset that drift first (`gridctl skill pins diff <name>` or `gridctl ctx pins diff <name>`, or the Pins workspace). A target with no pin record proceeds, and the approval pins it, as every pin store already allows.

For a skill, approve applies the diff in memory and parses the result. A hunk that no longer matches, or a `SKILL.md` that fails to parse or validate, refuses before any write. It computes the post-apply hash from the rendered `SKILL.md` that `SaveSkill` will write, recomputes the current hash once more immediately before the first write, and then writes supporting files first and `SKILL.md` last. The prior bytes of every touched file are captured first. If a write fails, every file already written is restored to its prior bytes (an added file is removed), the proposal stays pending, and the error names what was restored:

```text
proposal p-... failed writing references/runbook.md: <error>. Restored SKILL.md and references/checklist.md to their prior content. The proposal is still pending.
```

If a restore also fails, the message names each file still changed. A multi-file change is several writes, not one atomic operation.

For a context fragment, approve parses the post-apply fragment and hashes its rendered form, because `SaveFragment` writes the render and not the caller's bytes. With fragments mode off, `AGENTS.md` is written through the canonical save and hashed directly.

After the writes, approve re-pins through the target's own pin store with the post-apply hash. That store appends the approvals log entry, with the effective reason and the evidence plus `proposal:<id>`. The proposal is marked `approved` with `verdict.applied: true`, `approved_hash` (the base), `new_hash`, and, for a skill, `stack`.

```text
✓ Approved proposal p-3f9a1c2b7d4e: applied to skill incident-triage and re-pinned
```

If the re-pin fails after the files were written, the proposal is still marked approved with `verdict.pin_error` set, the command exits 1, and the message names `gridctl skill pins approve <name>` (or `gridctl ctx pins approve <name>`) as the recovery. The target then shows ordinary pin drift.

A skill approve routes through `POST /api/proposals/{id}/approve` on the running daemon for the resolved stack, so the daemon's in-memory pin store sees the re-pin and its prompt view refreshes. Without a running daemon the CLI applies the change itself with that stack's pin store. Context and hook approves always run locally.

### Record path

Any imported target takes the record path: a skill from a pack, a git source, a local directory, or a client location, a fragment a pack installed, and every hook. `gridctl skill update` and `gridctl pack add` overwrite local edits to those targets, so an applied change would not last.

A record-path approve is a verdict, not a content approval. It writes nothing to the target, calls no pin store, and appends nothing to any approvals log. The proposal is marked `approved` with `verdict.applied: false`, `approved_hash`, and the effective reason and evidence. The change itself has to arrive through the target's source, and the pin drift it then causes is approved the usual way, citing the proposal:

```text
✓ Approved proposal p-91ab...: verdict recorded for skill triage (pack portable-pack); nothing was written and no pin changed. The change arrives through 'gridctl pack add'; cite proposal:p-91ab... when approving that drift.
```

The arrival is `gridctl pack add` for a pack and for every hook, `gridctl skill update` for a git import, and editing the source directory then running `gridctl skill update` for a local import.

### Known limitation: the write window

The proposals lock serializes proposal mutations. It does not serialize the registry editor, `gridctl ctx edit`, or other writers of the same files, and each pin store hashes before taking its own lock. Approve narrows the window by checking the hash immediately before the first write, but an edit that lands during the write itself can be overwritten and lost. An edit that lands after the writes and before the re-pin fails the re-pin's hash check instead, which leaves the proposal approved with `pin_error` and the target in pin drift for review.

## Reject

```bash
gridctl proposals reject p-3f9a1c2b7d4e --reason "duplicate of p-91ab..."
```

`--reason` is required and checked like an approval reason. Reject records the verdict on the proposal, keeps the file for audit, and writes nothing to the target or any approvals log. A proposal cannot be edited in place; reject it and propose again. gridctl does not delete proposals. `gridctl reset --purge` removes the directory with the rest of the home.

## Exit codes

| Command | `0` | `1` | `2` |
|---|---|---|---|
| `propose` | Staged | Validation refusal, unsupported kind or diff, missing target, credential-shaped input, a diff that does not apply | Home unresolvable, unreadable document, unreadable or newer store file |
| `proposals list` | Listed | | Invalid filter or infrastructure error |
| `proposals show` | Shown | | Unknown id, newer or corrupt file, infrastructure error |
| `proposals approve` | Approved | Stale base, pin drift, reason required, not pending, apply failed, credential-shaped input, missing target, re-pin error after the write | Unknown id or infrastructure error, including no resolvable stack for a skill |
| `proposals reject` | Rejected | Not pending, missing or refused reason | Unknown id or infrastructure error |
| `proposals base` | Printed | `agent` or another unsupported kind, invalid name | Target not found or infrastructure error |

Every command accepts `--format json` (or `--json`). `list` returns `{"schema_version": 1, "proposals": [...], "skipped": [{"path", "category"}], "truncated": false}`. A file that cannot be loaded appears in `skipped` as `corrupt`, `newer_version`, `permission`, or `unreadable`, never with its contents. At most 10,000 files are read.

## REST

All routes are under the authenticated `/api/` namespace. See [API Reference](api-reference.md#proposals) for bodies and responses.

| Route | Success | Refusals |
|---|---|---|
| `POST /api/proposals` | `201` with the record | `400` validation, `404` missing target, `422` diff does not apply |
| `GET /api/proposals?status=&kind=` | `200` with the list document | `400` invalid filter |
| `GET /api/proposals/{id}` | `200` with `base_current` refreshed and `pin_status` | `404` |
| `POST /api/proposals/{id}/approve` | `200` with the record plus `approved_by`, `approved_by_source`, `evidence_count`, and `applied` | `400` input or reason required, `404`, `409` stale base, drift, or not pending, `422` apply failed, `500` re-pin error (body carries the record) or failed restore, `503` store unavailable |
| `POST /api/proposals/{id}/reject` | `200` with the record and attribution | `400`, `404`, `409` not pending |

A verified OAuth principal wins over a declared `created_by` or `approved_by`.

## Security report

`gridctl doctor --security` and `GET /api/security-report` include a `proposal.pending` check on the gateway subject. It passes with `proposals_pending` and the `pending_proposals` fact when proposals are waiting, passes with `no_pending_proposals` and a count of `0` otherwise, and is unknown (`store_unavailable`) when the store cannot be listed. It is not a fail predicate, so pending proposals never change the exit code. `--source file:` reports it unknown. See [Security Evidence](security-evidence.md).

## Upgrade and rollback

`gridctl info --json` advertises `schemas.proposals` (`"1"`), and the text `Schemas:` line includes `proposals=1`. A proposal file whose `version` is newer than this binary reads is reported as `newer_version` by `list`, refused by `show` and `approve`, and never rewritten.

`gridctl upgrade --rollback` reads the `version` of every proposal file once and refuses when one is newer than the target release reads, naming the file: `Delete or move the proposal file before rolling back.` `--force` turns the refusal into a warning. A target that does not advertise a `proposals` version prints one warning and is not refused. See [Troubleshooting](troubleshooting.md).
