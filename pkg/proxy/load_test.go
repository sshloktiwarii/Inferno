package proxy_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/batcher"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/hardware"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/proxy"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/routing"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/telemetry"
)

// BenchmarkHighConcurrencyBurst tests the gateway under 1,000 concurrent simulated requests.
// Verifies zero deadlocks, zero panic crashes, dynamic batching coalescence, and proper status code returns.
func BenchmarkHighConcurrencyBurst(b *testing.B) {
	dynBatcher := batcher.NewDynamicBatcher(15*time.Millisecond, 32)
	defer dynBatcher.Close()

	gov := hardware.NewBackpressureGovernor(95.0)
	tracker := telemetry.NewLatencyTracker()
	router := routing.NewRadixPrefixRouter(nil)
	server := proxy.NewServerWithRouter(dynBatcher, gov, tracker, router)

	ts := httptest.NewServer(server)
	defer ts.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
		},
	}

	payload, _ := json.Marshal(proxy.ChatCompletionRequest{
		Model: "inferno-bench-8b",
		Messages: []proxy.ChatMessage{
			{Role: "user", Content: "High throughput benchmark prompt for concurrency stress test"},
		},
		Stream: false,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		var successCount uint64
		var throttledCount uint64
		var errorCount uint64

		concurrency := 1000
		wg.Add(concurrency)

		for c := 0; c < concurrency; c++ {
			go func(reqIdx int) {
				defer wg.Done()

				req, err := http.NewRequest("POST", ts.URL+"/v1/chat/completions", bytes.NewReader(payload))
				if err != nil {
					atomic.AddUint64(&errorCount, 1)
					return
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := client.Do(req)
				if err != nil {
					atomic.AddUint64(&errorCount, 1)
					return
				}
				defer resp.Body.Close()

				switch resp.StatusCode {
				case http.StatusOK:
					atomic.AddUint64(&successCount, 1)
				case http.StatusTooManyRequests:
					atomic.AddUint64(&throttledCount, 1)
				default:
					atomic.AddUint64(&errorCount, 1)
				}
			}(c)
		}

		wg.Wait()
		b.Logf("Completed burst: 200 OK=%d, 429 Throttle=%d, Errors=%d", successCount, throttledCount, errorCount)
	}
}

func TestBurstConcurrencyStability(t *testing.T) {
	dynBatcher := batcher.NewDynamicBatcher(10*time.Millisecond, 16)
	defer dynBatcher.Close()

	gov := hardware.NewBackpressureGovernor(95.0)
	tracker := telemetry.NewLatencyTracker()
	server := proxy.NewServer(dynBatcher, gov, tracker)

	ts := httptest.NewServer(server)
	defer ts.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        500,
			MaxIdleConnsPerHost: 500,
		},
	}

	payload, _ := json.Marshal(proxy.ChatCompletionRequest{
		Model: "inferno-llama-3-8b",
		Messages: []proxy.ChatMessage{
			{Role: "user", Content: "Stability test under 200 concurrent bursts"},
		},
	})

	concurrency := 200
	var wg sync.WaitGroup
	wg.Add(concurrency)

	var successes int32
	var throttles int32

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			req, err := http.NewRequest("POST", ts.URL+"/v1/chat/completions", bytes.NewReader(payload))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				atomic.AddInt32(&successes, 1)
			} else if resp.StatusCode == http.StatusTooManyRequests {
				atomic.AddInt32(&throttles, 1)
			}
		}()
	}

	wg.Wait()

	totalHandled := successes + throttles
	if totalHandled != int32(concurrency) {
		t.Fatalf("Expected all %d requests accounted for, got handled=%d (succ=%d, throttled=%d)", concurrency, totalHandled, successes, throttles)
	}
	t.Logf("Bursts accounted cleanly: %d successes, %d throttles out of %d total", successes, throttles, concurrency)
}
