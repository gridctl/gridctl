# Python MCP runtime base

`ghcr.io/gridctl/mcp-runtime-python` is a downstream Python 3.12 runtime
foundation under the `gridctl` GHCR organization. It is not the Gridctl
gateway, not a ready-to-run MCP server, and not a catalog of preinstalled
tools. Node and universal toolbox images are out of scope.

Generated Python 3.10-3.13 builds and custom Dockerfiles stay as they are.
This base is not substituted automatically. Adopting it is a manual
custom-Dockerfile or `image:` deployment.

## Contract

| Item | Value |
|------|-------|
| Python | 3.12.11 on Debian Bookworm slim (glibc) |
| Architectures | Linux amd64 and arm64 after native application tests |
| Identity | UID/GID 10001 (numeric; a username is not evidence) |
| Init | `tini` as `ENTRYPOINT`; Gridctl `command` replaces `CMD` only |
| HOME | `/tmp` (the hardened profile's default scratch) |
| Cache | `/tmp/cache` |
| Data | `/data` (mount a volume when you need persistence) |
| Bytecode | `PYTHONDONTWRITEBYTECODE=1` |
| Ownership | `/usr`, `/app`, and installed dependencies stay root-owned and readable |

The image does not declare `VOLUME`. Under a read-only root, writable image
paths still need explicit runtime mounts. Bind-mounting over `/app` or the
virtualenv hides the installed environment.

## Manual derivatives

Use the existing local or Git custom-Dockerfile source, or deploy a built
image by digest. Two complete recipes live in
`images/mcp-runtime-python/fixtures/echo-server/`:

- `Dockerfile.locked` installs from `uv.lock`
- `Dockerfile.hashed` builds a wheel and installs it with `--require-hashes`

Both install during the build, copy a matching `/app/.venv` onto the runtime
base, and execute the `mcp-echo-fixture` console script. Compilers, uv, and
credentials stay out of the final image.

```bash
docker build -t gridctl-mcp-runtime-python:local images/mcp-runtime-python
gridctl apply examples/python-runtime/locked.yaml
```

A published digest is required before treating a GHCR tag as runnable. Do
not copy an unverified digest into a stack. A moved mutable tag or a
top-level package version is not a frozen dependency closure. Changing the
literal `FROM` digest in a Dockerfile changes Gridctl's build-input identity
and rebuilds; that is content identity, not publisher verification.

## Writable state and errors

Hardened execution keeps code read-only. If a process cannot write, add
declared scratch or data mounts. Do not switch to privileged mode, run
`chmod 777`, recursively chown application files, or open the network to
make a write succeed.

Permission failures, missing runtime evidence, application errors, and
registry pull failures are different diagnostics. There is no blanket
secure badge.

## Evidence layers

| Layer | What it covers | What it does not cover |
|-------|----------------|------------------------|
| Base provenance and SBOM | OS and language components of this image, bound to the index or a platform manifest | Application packages you install later |
| Derived image evidence | Your project's lock or hashed requirements | Runtime confinement |
| Execution reports | Instance-bound kernel and engine checks for the requested profile | Image signatures |

Index and platform manifests are different subjects. Attesting an index is
not a substitute for platform-specific SBOMs.

## Publication and tags

The dedicated workflow builds and tests on native `ubuntu-24.04` (amd64) and
`ubuntu-24.04-arm` (arm64). Candidate publication is an explicit dispatch
after those tests. New GHCR packages default private; anonymous pulls and
public evidence need an explicit visibility change.

| Tag | Mutability |
|-----|------------|
| `sha-<40-char-source>` | Immutable revision; never overwritten |
| `candidate-sha-<40-char-source>` | Immutable candidate; never overwritten |
| `3.12`, `3.12-bookworm` | Convenience aliases, promoted only after required evidence |
| `latest` | Not used as a supported alias |

A private or partially tested package is not a supported public release.
Production pushes and visibility changes are operator actions.

## Maintenance

Owner: repository maintainers. Support window: Python 3.12 on Debian
Bookworm until upstream Python 3.12 or Debian Bookworm security support
ends. Scan weekly; rebuild on Critical base findings and at least monthly.
Vulnerability exceptions need an owner, reason, and expiry. Retirement
means stop promoting aliases, document the last revision, and leave
immutable tags in place.

Base patches produce a new revision. Consumers rebuild and redeploy
derivatives; existing derived images are not modified in place.

Mixed-license notices live in `images/mcp-runtime-python/NOTICE`. An SBOM
or source-license label does not discharge Debian, PSF, or application
license obligations.

## Related

- [Python source containers](../examples/python-sources/)
- [Runtime examples](../examples/python-runtime/)
- [Execution controls](execution.md)
- [Source schema](config-schema.md#source)
