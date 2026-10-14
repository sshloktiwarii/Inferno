# Inferno Telemetry & Concurrency Gateway

> **High-Throughput Inference Gateway with Speculative Prefix-Cache Routing, Adaptive Request Queueing, 3-Tier Latency Instrumentation & Hardware Telemetry**

Inferno solves the **Compute Economics & Hardware Saturation Problem** in LLM serving. In production, unthrottled concurrent requests overwhelm GPU clusters, causing catastrophic Time-to-First-Token spikes (TTFT > 4s), decode latency stutter, and unrecoverable Out-Of-Memory (OOM) kernel panics.

Inferno sits between developer client applications and upstream inference engines (such as vLLM, Ollama, TensorRT-LLM, or hosted APIs) as an OpenAI-compatible reverse proxy with real-time hardware telemetry, speculative prompt prefix routing, and dynamic admission backpressure.

---

## Architectural Division of Responsibility

| Capability | GPU Inference Engine (vLLM / TensorRT / llama.cpp) | Inferno Telemetry Gateway |
| :--- | :--- | :--- |
| **KV-Cache Memory Management** | PagedAttention at CUDA kernel level | Monitored via hardware telemetry & prefix routing |
| **Prefix-Cache Routing** | Single-node cache lookup | **Cluster-Wide Radix Tree** routing requests to warm GPU slots |
| **Continuous Batching** | Performed internally across model tensor weights | N/A (Engine runtime responsibility) |
| **Adaptive Request Queueing** | Queueing at socket boundary | Micro-batch admission queue & pooling |
| **Latency Deconstruction** | Aggregated total request time | **3-Tier Decomposition** ($T_{\text{queue}}$, $T_{\text{prefill}}$/TTFT, $T_{\text{decode}}$/ITL) |
| **Hardware Backpressure** | Fatal OOM crash on saturation | Dynamic `429 Too Many Requests` load shedding |

---

## Core Capabilities

- **Speculative Prompt Prefix-Cache Routing**:
  - Thread-safe Radix Tree tracks token prefixes across incoming requests and active GPU worker slots.
  - Matches prompt prefixes to route requests directly to instances holding the relevant KV-cache.
  - Slashes Time to First Token ($T_{\text{prefill}}$ / TTFT) by up to 90% on cache hits.
  - Exposes prefix cache statistics in Prometheus metrics and diagnostic response headers.
- **Adaptive Request Queueing**:
  - Aggregates and pools concurrent incoming prompts within configurable admission windows ($T_{\text{window}}$).
  - Groups requests into optimal micro-batches before dispatch.
- **3-Tier Latency Decomposition**:
  - $T_{\text{queue}}$: Milliseconds queued before dispatch.
  - $T_{\text{prefill}}$ (TTFT): Prompt evaluation latency to the first generated token.
  - $T_{\text{decode}}$ (ITL): Token-by-token generation pace across the stream.
  - Computes running p50, p95, and p99 percentiles and token throughput.
- **Cross-Platform Hardware Telemetry**:
  - **macOS (`//go:build darwin`)**: Probes Apple Silicon unified memory pressure via sysctl.
  - **Linux (`//go:build linux`)**: Probes cgroup v2 memory limits and host NVML.
  - **Dynamic Backpressure**: When memory pressure crosses safety limits (>90%), sheds excess load with HTTP `429 Too Many Requests` (`Retry-After: 5`) to prevent cluster crashes.
- **OpenAI-Compatible & Streaming SSE**:
  - Direct drop-in replacement for `/v1/chat/completions` supporting both JSON and Server-Sent Events (`text/event-stream`).
  - Actively monitors socket disconnects, halting upstream generation immediately to save GPU compute on cancelled requests.
- **Prometheus Exporter & Grafana Dashboards**:
  - Direct `/metrics` scraping endpoint providing request counts, latency histograms, prefix hit ratio, and token generation rates.
  - Bundled production Grafana dashboard in `dashboards/grafana-dashboard.json`.

---

## Diagnostic Response Headers

Every completion response from Inferno includes diagnostic telemetry headers:

| Header | Description | Example |
| :--- | :--- | :--- |
| `X-Inferno-Queue-Ms` | Duration request waited in admission queue | `4` |
| `X-Inferno-Prefill-Ms` | Time to First Token ($T_{\text{prefill}}$) | `2` |
| `X-Inferno-Decode-Ms` | Total token generation duration ($T_{\text{decode}}$) | `192` |
| `X-Inferno-Batch-Size` | Co-scheduled micro-batch size | `8` |
| `X-Inferno-Tokens-Generated` | Number of output tokens streamed | `24` |
| `X-Inferno-Prefix-Cache-Hit` | Indicates whether a KV prefix match occurred | `true` |
| `X-Inferno-Prefix-Tokens-Matched` | Count of shared prefix tokens matched in Radix Tree | `16` |
| `X-Inferno-Prefix-Savings-Ms` | Estimated prefill latency saved by KV reuse | `12` |
| `X-Inferno-Target-Worker` | Designated worker slot assigned by router | `worker-gpu-0` |

---

## Quick Start

### 1. Run Unit & Integration Tests
```bash
go test -v ./...
```

### 2. Start the Gateway
```bash
go run main.go --port 8085 --batch-window-ms 20 --max-vram-pressure 90.0
```

### 3. Send a Chat Completion Request
```bash
curl -X POST http://localhost:8085/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "inferno-llama-3-8b",
    "messages": [
      {"role": "system", "content": "You are a specialized code optimization assistant."},
      {"role": "user", "content": "Explain memory bandwidth bottlenecks."}
    ],
    "stream": false
  }'
```

### 4. Inspect Telemetry & Prometheus Metrics
```bash
# Prometheus metrics (request counters, percentiles, prefix hit ratio)
curl http://localhost:8085/metrics

# Hardware health & memory pressure
curl http://localhost:8085/health
```

---

## License
MIT License (c) 2026 Shlok Tiwari
