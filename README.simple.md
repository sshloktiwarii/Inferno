# Inferno Telemetry & Concurrency Gateway — Plain-English Guide

## What is Inferno?

Inferno is a traffic controller for AI model servers.

When you run large language models (LLMs) on GPUs, too many requests at once will crash the server or make everything unbearably slow. Inferno sits between your apps and the AI server, managing the flow of requests so the GPU never gets overwhelmed.

## What problem does it solve?

Without Inferno, if 100 users send requests to your AI server simultaneously:
- The GPU runs out of memory and crashes (OOM panic)
- Everyone's first response takes 4+ seconds instead of milliseconds
- You have no idea which requests are slow or why

With Inferno:
- Requests are queued and admitted at a rate the GPU can handle
- If the GPU is full, users get a "try again in a moment" response instead of a crash
- You get detailed breakdowns of exactly where time is spent

## How does it work?

1. **Smart routing:** Inferno remembers what prompts each GPU has seen recently. If you send a similar prompt, it routes you to the GPU that already has relevant data cached — making the response up to 90% faster.

2. **Admission control:** When the GPU is under heavy load, Inferno holds back new requests in a queue instead of flooding the server. Think of it like a bouncer at a club.

3. **Latency breakdown:** Instead of one big "response time" number, Inferno splits it into three:
   - Queue time (waiting in line)
   - Prefill time (AI reading your prompt)
   - Decode time (AI writing its response)

4. **Hardware monitoring:** Inferno watches GPU memory, temperature, and utilization in real time.

## What's it built with?

- **Go** — Fast, concurrent server language
- **Prometheus** — Metrics collection
- **Grafana** — Dashboard visualization

## Who is it for?

Anyone serving LLMs in production (via vLLM, Ollama, TensorRT-LLM) who needs to prevent crashes and understand performance.
