# Stack config files

This stack copies two files into an Alpine container at create time: an inline settings file, and `prometheus-extra.conf` from the same directory. `./dashboards` is a volume source relative to this stack file. The image is a digest-pinned Alpine stand-in so the example validates without a third-party server. The motivating case is a Prometheus MCP server that needs an `X-Scope-OrgID` header in an `--http.config` file. The companion file is not named `.yaml` because example reference checks validate every `.yaml` file as a stack.

```bash
gridctl validate examples/stack-configs/stack.yaml
gridctl apply examples/stack-configs/stack.yaml
gridctl destroy examples/stack-configs/stack.yaml
```

`gridctl validate` does not open `prometheus-extra.conf`. A missing file fails apply. `execution.mode: hardened` cannot use `configs`; seed an engine-local volume instead. See [Configs](../../docs/config-schema.md#configs).
