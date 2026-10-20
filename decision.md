# Architecture Decision Record (ADR): inferno-telemetry-proxy

## Status
Accepted & Implemented (Commit `1a8fe03`)

## Context
Static concurrency limits either starve upstream GPU throughput when models are idle or cause massive queuing delays and 429 timeouts when bursts of agent requests hit the gateway.

## Decisions

### 1. Dynamic AIMD Concurrency Sizing with 500ms Hysteresis
- **Decision:** Increase allowable in-flight requests linearly on 200 OK (`+1`) up to 256, and halve concurrency on 429 (`/2`) with a 500ms hysteresis timer.
- **Consequence:** Automatically finds the optimal operating throughput of the underlying LLM engine without manual tuning.

### 2. Continuous Batching over Discrete Queues
- **Decision:** Collect incoming requests into batches within a 10ms sliding window before invoking the worker.
- **Consequence:** Maximizes GPU compute parallelism while adding < 10ms of initial queue latency.
