# 🔭 Tracing

Distributed tracing for gateway tool calls, exported via OTLP. Traces always land in the in-memory ring buffer behind the web UI Traces workspace and `gridctl traces`; the `tracing:` block additionally exports them to an external backend.

## 📄 Examples

| File | Description |
|------|-------------|
| `otlp-jaeger.yaml` | Export traces to a local Jaeger instance via OTLP HTTP |

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

For HTTPS backends (Honeycomb, Grafana Cloud, Grafana Tempo Cloud), set `endpoint` to an `https://` URL; TLS is enabled automatically by the scheme. Most cloud backends require auth headers, so use an OTel Collector as a proxy to inject them rather than embedding credentials in stack YAML.

See [Usage Observability](../../docs/usage-observability.md) for the full tracing reference.
