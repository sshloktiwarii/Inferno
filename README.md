# Inferno Telemetry & Concurrency Gateway

> **High-Throughput Inference Gateway with Adaptive Request Queueing, 3-Tier Latency Instrumentation & Hardware Telemetry**
>
> **New to this project?** Read the [Plain-English Guide for Dummies](file:///Users/shloktiwari/Documents/untitled%20folder/inferno-telemetry-proxy/README_FOR_DUMMIES.md) for a zero-jargon overview, everyday analogies, and a 60-second test walk-through.

Inferno solves the **Compute Economics & Hardware Saturation Problem** in LLM serving. In production, unthrottled concurrent requests overwhelm GPU clusters, causing catastrophic Time-to-First-Token spikes (TTFT > 4s), decode latency stutter, and unrecoverable Out-Of-Memory (OOM) kernel panics.

Inferno sits between developer client applications and upstream inference engines (such as vLLM, Ollama, TensorRT-LLM, or hosted APIs) as an OpenAI-compatible reverse proxy with real-time hardware telemetry and dynamic admission backpressure.

---

## Architectural Division of Responsibility

| Capability | GPU Inference Engine (vLLM / TensorRT / llama.cpp) | Inferno Telemetry Gateway |
| :--- | :--- | :--- |
| **KV-Cache Memory Management** | PagedAttention at CUDA kernel level | Monitored via hardware telemetry probes |
| **Continuous Batching** | Performed internally across model tensor weights | N/A (Engine runtime responsibility) |
| **Adaptive Request Queueing** | Queueing at socket boundary | Micro-batch admission queue & pooling |
| **Latency Deconstruction** | Aggregated total request time | **3-Tier Decomposition** ($T_{\text{queue}}$, $T_{\text{prefill}}$/TTFT, $T_{\text{decode}}$/ITL) |
| **Hardware Backpressure** | Fatal OOM crash on saturation | Dynamic `429 Too Many Requests` load shedding |

---

## Core Capabilities

- **Adaptive Request Queueing**: Aggregates and pools concurrent incoming prompts within configurable admission windows ($T_{\text{window}}$).
- **3-Tier Latency Decomposition**:
  - $T_{\text{queue}}$: Milliseconds queued before dispatch.
  - $T_{\text{prefill}}$ (TTFT): Prompt evaluation latency to the first generated token.
  - $T_{\text{decode}}$ (ITL): Token-by-token generation pace across the stream.
  - Computes running p50, p95, and p99 percentiles and token throughput.
- **Cross-Platform Hardware Telemetry**:
  - **macOS (`//go:build darwin`)**: Probes Apple Silicon unified memory pressure via sysctl.
  - **Linux (`//go:build linux`)**: Probes cgroup v2 memory limits and host NVML.
  - **Dynamic Backpressure**: When memory pressure crosses safety limits (>90%), sheds excess load with HTTP `429 Too Many Requests` (`Retry-After: 5`) to prevent cluster crashes.
- **OpenAI-Compatible & Streaming SSE**: Direct drop-in replacement for `/v1/chat/completions` with both non-streaming JSON and streaming Server-Sent Events (`text/event-stream`).
- **Prometheus Exporter**: Direct `/metrics` scraping endpoint for production Grafana dashboards.

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
    "messages": [{"role": "user", "content": "Explain memory bandwidth bottlenecks."}],
    "stream": false
  }'
```

### 4. Inspect Telemetry & Prometheus Metrics
```bash
# Prometheus metrics
curl http://localhost:8085/metrics

# Hardware health & percentiles
curl http://localhost:8085/health
```

---

## License
MIT License (c) 2026 Shlok Tiwari
