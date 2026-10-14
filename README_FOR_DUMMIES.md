# Inferno telemetry proxy for dummies

## What is this?
Inferno is a traffic cop and stopwatch for your AI servers.

When you run local or hosted AI models (using tools like vLLM, Ollama, or OpenAI-compatible backends), sending too many requests at the same time can run your GPU out of video memory (VRAM). When that happens, the server crashes with an Out-Of-Memory (OOM) error.

Inferno sits between your applications and your AI model. It monitors server memory, prevents crashes by throttling traffic when memory gets dangerously high, and measures exact response times down to the millisecond.

## The problem in plain English
Imagine a busy restaurant kitchen with one chef (the GPU).

If 40 customers order complicated meals at the exact same second:
1. **Without Inferno**: The chef tries to cook all 40 meals simultaneously, runs out of counter space, panics, and the kitchen shuts down (server crash / OOM).
2. **With Inferno**: Inferno acts as the front-of-house host. If the kitchen reaches 90% capacity, Inferno politely tells new arrivals: "The kitchen is full, please wait 5 seconds" (HTTP 429). The chef finishes the existing meals safely without burning down the kitchen.

In addition, when a customer complains that their order took 10 seconds, standard setups cannot tell you why. Inferno times each stage separately:
- How long did the ticket sit on the counter? (Queue time)
- How long did the chef spend reading the recipe? (Time to first token / TTFT)
- How fast did the chef prepare each plate? (Inter-token latency / ITL)

## How it works

### 1. Admission backpressure
Inferno continuously checks the host machine's memory usage:
- On macOS, it reads Apple Silicon memory pressure.
- On Linux, it inspects cgroups and GPU memory.

If memory usage crosses 90%, Inferno returns an HTTP 429 `Too Many Requests` response with a `Retry-After: 5` header. This protects your server from fatal crashes.

### 2. Three-stage latency stopwatch
Inferno breaks every AI response into three distinct measurements:
- **Queue time ($T_{\text{queue}}$)**: The time a request spent waiting in Inferno's admission buffer.
- **Time to first token ($T_{\text{prefill}}$ / TTFT)**: The time it took the model to process your prompt and generate the very first word.
- **Inter-token latency ($T_{\text{decode}}$ / ITL)**: The average time taken between each streamed word.

Inferno computes running p50, p95, and p99 percentiles across all requests.

### 3. Client disconnect cancellation
If a user closes their browser tab or an agent cancels a task, the network connection drops. Inferno detects the closed socket immediately and cancels the upstream request. This prevents the GPU from wasting electricity finishing a response that nobody will read.

### 4. OpenAI-compatible reverse proxy
Inferno accepts standard OpenAI `/v1/chat/completions` requests. You can point your existing LangChain, LlamaIndex, or custom scripts to `http://localhost:8085/v1` without changing your application code.

### 5. Prometheus and Grafana metrics
Inferno exposes a `/metrics` endpoint that Prometheus can scrape every second. You can load our pre-built Grafana dashboard (`dashboards/grafana-dashboard.json`) to view real-time latency graphs and server health.

## Quick start

### 1. Run the test suite
Ensure you have Go installed, then run:
```bash
go test -v ./...
```
**Expected output:**
```text
PASS
ok  	inferno/proxy	0.125s
```

### 2. Start the proxy
Start Inferno listening on port 8085:
```bash
go run main.go --port 8085 --batch-window-ms 20 --max-vram-pressure 90.0
```
Inferno will start and log:
```text
[INFO] Starting Inferno Telemetry Gateway on 127.0.0.1:8085
[INFO] Forwarding requests to upstream: http://localhost:8000
```

### 3. Check health and memory status
Open a new terminal and inspect system health:
```bash
curl http://127.0.0.1:8085/health
```
**Output:**
```json
{
  "status": "healthy",
  "memory_pressure_pct": 42.5,
  "queue_depth": 0
}
```

### 4. Inspect Prometheus metrics
View real-time telemetry metrics:
```bash
curl http://127.0.0.1:8085/metrics
```
You will see Prometheus counters including `inferno_requests_total`, `inferno_ttft_ms`, `inferno_itl_ms`, and `inferno_requests_cancelled_total`.

## Summary of what it does
- Prevents GPU server crashes by shedding load when memory exceeds 90%.
- Times every millisecond of prompt processing and word generation.
- Cancels dead requests the moment a user disconnects.
- Plugs directly into your existing OpenAI code without rewriting APIs.
- Feeds live data into Grafana dashboards.
