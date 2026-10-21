# 🔭 Tracing

Distributed tracing for gateway tool calls, exported via OTLP. Traces always land in the in-memory ring buffer behind the web UI Traces workspace and `gridctl traces`; the `tracing:` block additionally exports them to an external backend.

## 📄 Examples

| File | Description |
|------|-------------|
| `otlp-jaeger.yaml` | Export traces to a local Jaeger instance via OTLP HTTP |
| `otel-collector.yaml` | Export traces and metrics to a local collector on `:4318` |
| `otel-collector-config.yaml` | Collector config: OTLP receiver and detailed debug exporter |

## ⚙️ Prerequisites

Start Jaeger with OTLP HTTP ingest on `:4318` and the UI on `:16686`:

```bash
docker run --rm -p 4318:4318 -p 16686:16686 jaegertracing/jaeger:latest
```

## 💻 Usage

```bash
gridctl apply examples/tracing/otlp-jaeger.yaml
open http://localhost:16686
```

For HTTPS backends (Honeycomb, Grafana Cloud, Grafana Tempo Cloud), set `endpoint` to an `https://` URL; TLS is enabled automatically by the scheme. A pathless URL is posted to `/v1/traces` or `/v1/metrics`. Any other path is used as-is. Most cloud backends require auth headers. Set `headers` with a `${var:OTEL_HEADER}` reference, or use an OTel Collector as a proxy. Do not embed a literal secret in stack YAML.

`gateway.tracing` and `gateway.metrics.otlp` are read at startup. A change requires a restart.

Collector with the debug exporter:

```bash
docker run --rm -p 4318:4318 \
  -v "$PWD/examples/tracing/otel-collector-config.yaml:/etc/otelcol/config.yaml:ro" \
  otel/opentelemetry-collector:0.163.1@sha256:f0e23036dce3db3f9cb80d907c75a0d034cd32e93511a1943482d46129b9ae9c \
  --config /etc/otelcol/config.yaml
```

See [Usage Observability](../../docs/usage-observability.md) for the full tracing reference.
