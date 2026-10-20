# Project Breakdown: inferno-telemetry-proxy

## 1. Why It Exists (The Problem)
When multiple agents or users burst requests to an LLM server simultaneously, the backend GPU runs out of VRAM or HTTP connections saturate. The server starts failing with HTTP 429 Too Many Requests, connections drop abruptly, and users experience massive latency spikes.

---

## 2. What It Does (The Solution)
Inferno is a high-throughput concurrency firewall and continuous batching proxy:
- **Dynamic Continuous Batching:** Aggregates individual inference requests arriving within a configurable time window (e.g. 10ms) into unified batched forward passes.
- **Dynamic AIMD Rate Limiting:** Dynamically expands concurrency when requests succeed and multiplicatively halves concurrency upon receiving HTTP 429 signals, protected by a 500ms hysteresis cooldown.
- **Prefix-Cache Routing:** Accelerates Time-To-First-Token (TTFT) by predicting KV-cache hits on upstream inference engines.
- **Comprehensive Burst Benchmarks:** Verified under 1,000 concurrent simulated requests with zero deadlocks.

---

## 3. Targeted Niche & Use Cases
Backend engineers scaling local or cloud LLM clusters who require automated admission control, continuous request batching, and detailed 3-tier latency diagnostics.
