# Concepts

Gridctl keeps an agent setup in files you can commit, then runs the gateway and applies the rest to the clients you select. This page defines the words the other guides use. It does not rename them.

A committed stack declares servers. A pack selects skills, agents, rules, and wiring, and may carry a stack. [`gridctl link`](cli-reference.md#llm-clients) writes the gateway endpoint into a client config and does not install skills, agents, or rules. Those move by [projection](#projection), and only onto clients that can read that resource ([skill channels](skills.md#how-skills-reach-the-model), [context coverage](global-context.md#write-strategies)). Secret values and lockfiles stay on the host. A tool call is not a cross-host reproduction.

## Stack

A stack is the YAML file (`stack.yaml`, or any path you pass to `gridctl apply`) that declares MCP servers, the gateway, and optional controls. The `policy:` key is the per-tool call policy. The `skills:` key is an exposure policy, not skill bodies. `enabled: false` parks a server or resource without removing it, and an optional override file can change that flag and a short allowlist of fields without editing a shared base ([Override files](config-schema.md#override-files)). `gridctl apply` starts the workloads and the gateway, and `gridctl plan` previews deployment changes, not agent work ([Stack](config-schema.md#stack), [Skills (exposure policy)](config-schema.md#skills-exposure-policy)).

## Pack

A pack is a git repository with a `gridctl-pack.yaml` manifest: a versioned selection of skills, agents, rule fragments, gateway wiring, and an optional stack file, pinned on add at `~/.gridctl/packs/<name>/<commit>/`. A booster pack extends another pack's stack with a fragment; `pack apply` composes them into one gateway on the base pack's port. `gridctl-pack.lock.yaml`, written by `gridctl pack lock`, pins each external source commit so two machines import the same revisions. `gridctl pack add` imports that selection, and `gridctl pack apply` starts a carried stack from the CLI only, then projects the selected resources; REST and the web UI project resources but do not start, stop, or reload the daemon ([Packs](packs.md), [Carrying a stack](packs.md#carrying-a-stack), [Lock file](packs.md#lock-file)).

## Skill

A skill is Agent Skills prose at `~/.gridctl/registry/skills/<name>/SKILL.md`, written in an editor or the Library workspace. `gridctl skill` imports, lists, validates, pins, updates, and removes skills, and `gridctl activate` promotes a draft once a gateway is running ([What a skill looks like](skills.md#what-a-skill-looks-like)).

## Agent

An agent is an imported definition at `~/.gridctl/registry/agents/<name>/AGENT.md`, copied from `agents/*.md`, and it is not the removed Agent IDE. `gridctl skill list --kind agent` and `gridctl skill project sync --kind agent` act on that kind ([Agent definitions](skills.md#agent-definitions)).

## Rule fragment

A rule fragment is one markdown file in the opt-in library at `~/.gridctl/context/fragments/*.md`, and a pack ships the same kind of file from `rules/*.md` or `fragments/*.md`. `gridctl ctx add`, `ctx list`, and `ctx rm` manage the local library ([Rule fragments](global-context.md#rule-fragments-opt-in)).

## Global context

Global context is the user-level instruction file at `~/.gridctl/context/AGENTS.md`, in the [agents.md](https://agents.md) convention; fragments mode replaces that file with the library above. `gridctl ctx` projects it into client locations that have a writable global file, and per-project `AGENTS.md` files stay in their repositories ([The canonical file](global-context.md#the-canonical-file)).

## Wiring

Wiring is the gateway entry `gridctl link` writes into a client MCP config, with ownership (path, name, and a value hash) recorded in `~/.gridctl/project.lock.yaml`. `gridctl project sync --kind wiring` reapplies recorded links, and `gridctl unlink` removes one ([Wiring ownership](cli-reference.md#wiring-ownership-project)).

## Registry

The registry is the local store at `~/.gridctl/registry/` for skills and agents, and it is not declared in stack YAML. Skills enter through `gridctl skill add` (a git repo, or a local directory), `gridctl skill import`, `gridctl import <client> --kind all`, a pack, the Library workspace, or the REST API; local-directory import, client-home import, and `--kind` are included in v1.0.0-rc.2. `~/.gridctl/skills.yaml` is a display list only, and `gridctl skill update` does not read it ([Skills](skills.md), [Skill sources](config-schema.md#skill-sources)).

## Projection

Projection copies skills, agents, global context, and wiring onto client disks and records each file in `~/.gridctl/project.lock.yaml`. Named skills use `gridctl skill project`, all imported agents can use `gridctl skill project sync --kind agent`, global context uses `gridctl ctx sync`, and wiring uses `gridctl project sync --kind wiring`; linking runs none of these commands ([Projecting skills into clients](skills.md#projecting-skills-into-clients)).

## Group

A group is a named subset of tools under `groups:` in the stack file, served at `/groups/{name}/mcp`. `gridctl groups` lists them, and `gridctl link <client> --group <name>` points a client at that endpoint ([Groups](config-schema.md#groups-tool-bundles)).

## Client profile

A client profile is an allow-list under `clients.profiles` in the stack file that limits which servers and tools a connecting client can see. `gridctl link --client-id <id>` embeds that identifier on the gateway URL, and with no `clients:` block a connecting client sees every tool ([Clients](config-schema.md#clients-per-client-access-scoping)).

## Call policy

A call policy is the optional top-level `policy:` block. It allows or blocks a listed tool by canonical name and does not remove the tool from listings. It is not the offline `gridctl validate --policy` check, and it is not the `skills:` exposure policy ([Policy](config-schema.md#policy-per-tool-call-policy)).

## Link

Link writes the gateway URL into one installed client's MCP config, with `gridctl link` or a `link:` block that `gridctl apply` reconciles, and it does not install skills, agents, or rules. An explicit `gridctl link <client>` fails when that client is not installed; a declared `link:` entry warns and skips ([LLM clients](cli-reference.md#llm-clients), [Link (declared clients)](config-schema.md#link-declared-clients)).

## Service

A service is a user-scoped supervisor unit written by `gridctl service install`, recorded in `~/.gridctl/project.lock.yaml` as kind `service` before the file is written. It runs `gridctl apply --foreground` and is not created by `gridctl apply` itself. Linux uses a systemd user unit. macOS uses a launch agent that starts at login, not at boot. There is no start or stop API for it. `POST /api/reset` removes installed units with the rest of a reset ([Run as a service](installation.md#run-as-a-service)).

## How they compose

`gridctl apply` recreates the gateway and its workloads from the stack file. It does not recreate instruction files or variable values, and it does not install a user service. A `link:` block links installed clients. Projection is separate. See [Stack lifecycle](cli-reference.md#stack-lifecycle).

`gridctl pack add` imports the manifest's selection and pins a carried stack checkout under `~/.gridctl/packs/<name>/<commit>/`. `gridctl pack apply` starts that stack from the CLI and projects the selected skills, agents, rules, and wiring. A booster composes onto that gateway instead of starting its own. REST and the web UI can project those resources. They do not start, stop, or reload the daemon, and they do not write the pack lock. See [Carrying a stack](packs.md#carrying-a-stack) and [Extending another pack's stack](packs.md#extending-another-packs-stack).

What stays on the host: variable values in `~/.gridctl/vault/` (`gridctl var`), tool pins in `~/.gridctl/pins/{stackName}.json` and skill pins in `~/.gridctl/pins/skills/` (`gridctl pins`, `gridctl skill pins`), and lockfiles `~/.gridctl/project.lock.yaml` and `~/.gridctl/skills.lock.yaml`. Commit the stack, the pack manifest, `gridctl-pack.lock.yaml` when you run `gridctl pack lock`, and the instruction files. Do not commit those stores. See [Variables](cli-reference.md#variables), [schema pinning](config-schema.md#gateway), and [skill pins](skills.md#skill-pins-and-exposure-policy).

Repeatable setup is those pinned inputs on a host that can run them, not a hermetic rebuild or identical agent behavior. Stability labels are in [Project status](project-status.md).
