# Python source containers

Gridctl can generate a container image for an exact public PyPI release or a
packaged Python project in a git or local source. Docker or Podman performs the
build; host Python and uv are not required.

## PyPI example

`pypi.yaml` builds `mcp-server-fetch==0.6.0` from the official public PyPI
index. The package exposes one console script, so the server-level `command` can
be omitted. Generated Python servers default to stdio and do not publish a
port.

```bash
gridctl validate examples/python-sources/pypi.yaml
gridctl apply examples/python-sources/pypi.yaml
gridctl logs --server fetch
gridctl destroy examples/python-sources/pypi.yaml
```

The first apply resolves package metadata and builds a content-addressed image.
Later unchanged applies reuse the image when its build-input label matches.

## Packaged git and local projects

For a packaged project in git, set `runtime: python` and use `path` for a clean
subdirectory below the checkout:

```yaml
source:
  type: git
  url: https://github.com/example/mcp-monorepo.git
  ref: 0123456789abcdef0123456789abcdef01234567
  path: services/weather
  runtime: python
```

For a local source, `path` remains the source root and `project_path` selects a
project below it:

```yaml
source:
  type: local
  path: ./mcp-monorepo
  project_path: services/weather
  runtime: python
```

Both forms require static package metadata in `pyproject.toml` or `setup.py`.
Omit `dockerfile` to generate the image. Setting a non-empty `dockerfile`
explicitly selects that custom Dockerfile instead.

See the [Source schema](../../docs/config-schema.md#source) for Python version,
extras, additional dependencies, OS packages, command selection, and path
validation.
