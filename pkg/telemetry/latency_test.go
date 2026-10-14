package telemetry

import (
	"testing"
	"time"
)

func TestLatencyTrackerPercentiles(t *testing.T) {
	tracker := NewLatencyTracker()

	// Seed 20 test records with known distributions
	for i := 1; i <= 20; i++ {
		tracker.Record(RequestMetrics{
			RequestID:       "req-id",
			Model:           "test-model",
			QueueLatency:    time.Duration(i*5) * time.Millisecond,
			PrefillLatency:  time.Duration(i*10) * time.Millisecond,
			DecodeLatency:   time.Duration(i*20) * time.Millisecond,
			TotalLatency:    time.Duration(i*35) * time.Millisecond,
			TokensGenerated: 20,
			BatchSize:       4,
			Timestamp:       time.Now(),
		})
	}

	summary := tracker.GetSummary()
	if summary.Count != 20 {
		t.Fatalf("Expected 20 records, got %d", summary.Count)
	}

	if summary.P50QueueMs <= 0 || summary.P95QueueMs <= summary.P50QueueMs {
		t.Errorf("Invalid queue latency percentiles: p50=%.2f, p95=%.2f", summary.P50QueueMs, summary.P95QueueMs)
	}

	if summary.P50PrefillMs <= 0 || summary.P95PrefillMs <= summary.P50PrefillMs {
		t.Errorf("Invalid prefill (TTFT) percentiles: p50=%.2f, p95=%.2f", summary.P50PrefillMs, summary.P95PrefillMs)
	}

	if summary.TokensPerSecond <= 0 {
		t.Errorf("Expected positive tokens per second throughput, got %.2f", summary.TokensPerSecond)
	}
}
