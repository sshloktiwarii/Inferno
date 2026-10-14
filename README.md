# Inferno Telemetry Proxy 🚀

> **High-Throughput Inference Proxy with Dynamic Continuous Batching, 3-Tier Latency Instrumentation & Hardware Telemetry**

Inferno solves the **Compute Economics & Inference Bottleneck Problem**. Running modern LLMs is fundamentally memory-bandwidth bound: to generate each token, the GPU must stream billions of parameters from High-Bandwidth Memory (HBM) into SRAM. Under multi-user concurrency, KV cache bloat starves VRAM, leading to catastrophic Time-to-First-Token (TTFT > 4s), decode jitter, and silent Out-Of-Memory (OOM) crashes.

Inferno sits in front of inference servers (vLLM, Ollama, TensorRT-LLM, or custom engines) as an OpenAI-compatible reverse proxy with dynamic batching and real-time hardware telemetry.

---

## ⚡ Core Architecture

- **Dynamic Continuous Batching**: Queues and micro-batches concurrent prompts within configurable temporal windows ($T_{\text{window}}$) to amortize memory bandwidth across weights.
- **Granular 3-Tier Latency Decomposition**:
  - $T_{\text{queue}}$: Milliseconds queued before admission.
  - $T_{\text{prefill}}$ (TTFT): Prompt evaluation latency to the first generated token.
  - $T_{\text{decode}}$ (ITL): Token-by-token generation pace across the stream.
  - Computes running p50, p95, and p99 percentiles and token throughput.
- **Hardware Telemetry & Dynamic Backpressure**: Continuously monitors host/GPU memory pressure. When utilization crosses safety thresholds (>90%), admits requests selectively and sheds excess load with HTTP `429 Too Many Requests` (`Retry-After: 5`) to prevent unrecoverable kernel OOM panics.
- **OpenAI-Compatible & Streaming SSE**: Direct drop-in replacement for `/v1/chat/completions` with both non-streaming JSON and streaming Server-Sent Events (`text/event-stream`).
- **Prometheus Exporter**: Direct `/metrics` scraping endpoint for production Grafana dashboards.

---

## 🚀 Quick Start

### 1. Run Unit & Integration Tests
```bash
go test -v ./...
```

### 2. Start the Proxy
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

## 📊 3-Tier Latency Diagnostics Headers

Every request response returned by Inferno includes detailed execution metadata:

| Header | Description |
| :--- | :--- |
| `X-Inferno-Queue-Ms` | Time spent waiting in the dynamic micro-batch queue |
| `X-Inferno-Prefill-Ms` | Time to First Token (TTFT) / Prompt processing phase |
| `X-Inferno-Decode-Ms` | Inter-Token Latency (ITL) total decoding phase |
| `X-Inferno-Batch-Size` | Concurrency batch depth during dispatch |
| `X-Inferno-Tokens-Generated` | Total token count yielded |

---

## 📜 License
MIT License (c) 2026 Shlok Tiwari
