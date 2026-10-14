package telemetry

import (
	"sort"
	"sync"
	"time"
)

// RequestMetrics captures the 3-tier latency breakdown for an inference request.
type RequestMetrics struct {
	RequestID       string        `json:"request_id"`
	Model           string        `json:"model"`
	QueueLatency    time.Duration `json:"queue_latency_ms"`
	PrefillLatency  time.Duration `json:"prefill_latency_ms"` // Time to First Token (TTFT)
	DecodeLatency   time.Duration `json:"decode_latency_ms"`  // Inter-Token Latency (ITL) total
	TotalLatency    time.Duration `json:"total_latency_ms"`
	TokensGenerated int           `json:"tokens_generated"`
	BatchSize       int           `json:"batch_size"`
	Timestamp       time.Time     `json:"timestamp"`
}

// PercentileSummary contains p50, p95, p99 latency stats.
type PercentileSummary struct {
	Count             int     `json:"count"`
	CancelledCount    int     `json:"cancelled_count"`
	TotalTokens       int     `json:"total_tokens"`
	P50QueueMs        float64 `json:"p50_queue_ms"`
	P95QueueMs        float64 `json:"p95_queue_ms"`
	P99QueueMs        float64 `json:"p99_queue_ms"`
	P50PrefillMs      float64 `json:"p50_prefill_ms"` // P50 TTFT
	P95PrefillMs      float64 `json:"p95_prefill_ms"` // P95 TTFT
	P99PrefillMs      float64 `json:"p99_prefill_ms"` // P99 TTFT
	P50DecodeMs       float64 `json:"p50_decode_ms"`
	P95DecodeMs       float64 `json:"p95_decode_ms"`
	P99DecodeMs       float64 `json:"p99_decode_ms"`
	TokensPerSecond   float64 `json:"tokens_per_second"`
}

// LatencyTracker records request metrics and calculates percentiles.
type LatencyTracker struct {
	mu        sync.RWMutex
	records   []RequestMetrics
	cancelled int
}

func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		records: make([]RequestMetrics, 0, 1024),
	}
}

func (lt *LatencyTracker) Record(m RequestMetrics) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.records = append(lt.records, m)
}

func (lt *LatencyTracker) RecordCancellation() {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.cancelled++
}

func (lt *LatencyTracker) GetCancelledCount() int {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	return lt.cancelled
}

func (lt *LatencyTracker) GetSummary() PercentileSummary {
	lt.mu.RLock()
	defer lt.mu.RUnlock()

	n := len(lt.records)
	if n == 0 {
		return PercentileSummary{}
	}

	queueTimes := make([]float64, n)
	prefillTimes := make([]float64, n)
	decodeTimes := make([]float64, n)
	totalTokens := 0
	var totalDecodeDuration time.Duration

	for i, r := range lt.records {
		queueTimes[i] = float64(r.QueueLatency.Milliseconds())
		prefillTimes[i] = float64(r.PrefillLatency.Milliseconds())
		decodeTimes[i] = float64(r.DecodeLatency.Milliseconds())
		totalTokens += r.TokensGenerated
		totalDecodeDuration += r.DecodeLatency
	}

	sort.Float64s(queueTimes)
	sort.Float64s(prefillTimes)
	sort.Float64s(decodeTimes)

	p50Idx := int(float64(n) * 0.50)
	p95Idx := int(float64(n) * 0.95)
	p99Idx := int(float64(n) * 0.99)

	if p95Idx >= n {
		p95Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}

	tokensPerSec := 0.0
	if totalDecodeDuration.Seconds() > 0 {
		tokensPerSec = float64(totalTokens) / totalDecodeDuration.Seconds()
	}

	return PercentileSummary{
		Count:           n,
		CancelledCount:  lt.cancelled,
		TotalTokens:     totalTokens,
		P50QueueMs:      queueTimes[p50Idx],
		P95QueueMs:      queueTimes[p95Idx],
		P99QueueMs:      queueTimes[p99Idx],
		P50PrefillMs:    prefillTimes[p50Idx],
		P95PrefillMs:    prefillTimes[p95Idx],
		P99PrefillMs:    prefillTimes[p99Idx],
		P50DecodeMs:     decodeTimes[p50Idx],
		P95DecodeMs:     decodeTimes[p95Idx],
		P99DecodeMs:     decodeTimes[p99Idx],
		TokensPerSecond: tokensPerSec,
	}
}
