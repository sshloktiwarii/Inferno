package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/batcher"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/hardware"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/telemetry"
)

func TestChatCompletionsEndpoint(t *testing.T) {
	dynBatcher := batcher.NewDynamicBatcher(10*time.Millisecond, 4)
	defer dynBatcher.Close()

	gov := hardware.NewBackpressureGovernor(90.0)
	tracker := telemetry.NewLatencyTracker()
	server := NewServer(dynBatcher, gov, tracker)

	reqPayload := ChatCompletionRequest{
		Model: "inferno-llama-3-8b",
		Messages: []ChatMessage{
			{Role: "user", Content: "Explain memory bandwidth in LLMs"},
		},
		Stream: false,
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Verify headers
	if w.Header().Get("X-Inferno-Prefill-Ms") == "" {
		t.Errorf("Missing X-Inferno-Prefill-Ms header")
	}

	var resp ChatCompletionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(resp.Choices) == 0 {
		t.Fatalf("Expected at least one completion choice")
	}
}

func TestHardwareBackpressureThrottle(t *testing.T) {
	dynBatcher := batcher.NewDynamicBatcher(10*time.Millisecond, 4)
	defer dynBatcher.Close()

	gov := hardware.NewBackpressureGovernor(90.0)
	// Simulate dangerous VRAM pressure (95%)
	gov.SetSimulatedVramPressure(95.0)

	tracker := telemetry.NewLatencyTracker()
	server := NewServer(dynBatcher, gov, tracker)

	reqPayload := ChatCompletionRequest{
		Model: "inferno-llama-3-8b",
		Messages: []ChatMessage{
			{Role: "user", Content: "Heavy prompt requiring lots of VRAM"},
		},
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("Expected 429 Too Many Requests under high VRAM pressure, got %d", w.Code)
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Errorf("Expected Retry-After header on 429 response")
	}
}

func TestStreamClientDisconnectCancellation(t *testing.T) {
	dynBatcher := batcher.NewDynamicBatcher(5*time.Millisecond, 4)
	defer dynBatcher.Close()

	gov := hardware.NewBackpressureGovernor(90.0)
	tracker := telemetry.NewLatencyTracker()
	server := NewServer(dynBatcher, gov, tracker)

	reqPayload := ChatCompletionRequest{
		Model: "inferno-llama-3-8b",
		Messages: []ChatMessage{
			{Role: "user", Content: "Write a long essay about distributed consensus"},
		},
		Stream: true,
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))

	// Create context that cancels during streaming
	ctx, cancel := context.WithCancel(req.Context())
	// Cancel after 10ms to simulate client disconnecting mid-stream
	time.AfterFunc(10*time.Millisecond, cancel)

	w := httptest.NewRecorder()
	server.ServeHTTP(w, req.WithContext(ctx))

	// Verify cancellation was captured
	if tracker.GetCancelledCount() == 0 {
		t.Errorf("Expected cancellation to be tracked, got 0")
	}

	// Verify Prometheus metrics export
	metricsReq := httptest.NewRequest("GET", "/metrics", nil)
	metricsW := httptest.NewRecorder()
	server.ServeHTTP(metricsW, metricsReq)

	metricsOutput := metricsW.Body.String()
	if !strings.Contains(metricsOutput, "inferno_requests_cancelled_total 1") {
		t.Errorf("Metrics missing cancelled total: %s", metricsOutput)
	}
	if !strings.Contains(metricsOutput, "inferno_itl_ms") {
		t.Errorf("Metrics missing inferno_itl_ms: %s", metricsOutput)
	}
}

