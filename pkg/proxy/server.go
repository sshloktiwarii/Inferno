package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/batcher"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/hardware"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/routing"
	"github.com/sshloktiwarii/inferno-telemetry-proxy/pkg/telemetry"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   map[string]int         `json:"usage"`
}

type Server struct {
	batcher   *batcher.DynamicBatcher
	governor  *hardware.BackpressureGovernor
	tracker   *telemetry.LatencyTracker
	router    *routing.RadixPrefixRouter
	mux       *http.ServeMux
}

func NewServer(
	b *batcher.DynamicBatcher,
	gov *hardware.BackpressureGovernor,
	t *telemetry.LatencyTracker,
) *Server {
	return NewServerWithRouter(b, gov, t, routing.NewRadixPrefixRouter(nil))
}

func NewServerWithRouter(
	b *batcher.DynamicBatcher,
	gov *hardware.BackpressureGovernor,
	t *telemetry.LatencyTracker,
	r *routing.RadixPrefixRouter,
) *Server {
	if r == nil {
		r = routing.NewRadixPrefixRouter(nil)
	}
	s := &Server{
		batcher:  b,
		governor: gov,
		tracker:  t,
		router:   r,
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// 1. Hardware Backpressure & AIMD Concurrency Check
	if throttle, reason := s.governor.ShouldThrottle(); throttle {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "rate_limit_exceeded",
			"message": reason,
		})
		return
	}
	defer s.governor.Aimd().Release()

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	prompt := ""
	for _, m := range req.Messages {
		prompt += m.Role + ": " + m.Content + "\n"
	}

	reqID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())

	// 2. Queue into Dynamic Continuous Batcher
	item := &batcher.RequestItem{
		ID:     reqID,
		Model:  req.Model,
		Prompt: strings.TrimSpace(prompt),
	}

	batchRes, err := s.batcher.Enqueue(r.Context(), item)
	if err != nil {
		http.Error(w, "Batch queue error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Prefix-Cache Routing & TTFT Acceleration
	matchRes := s.router.MatchLongestPrefix(prompt)
	prefillDuration := 18 * time.Millisecond // Base Time to First Token (TTFT)
	if matchRes.CacheHit {
		savings := time.Duration(matchRes.EstimatedTTFTSavingsMs) * time.Millisecond
		if savings >= prefillDuration-2*time.Millisecond {
			prefillDuration = 2 * time.Millisecond
		} else {
			prefillDuration -= savings
		}
	}
	tokenCount := 24
	decodeDuration := time.Duration(tokenCount) * 8 * time.Millisecond // Inter-token latency

	totalLatency := batchRes.QueueLatency + prefillDuration + decodeDuration

	// 4. Record 3-Tier Latency Telemetry and Additive Increase for AIMD
	metrics := telemetry.RequestMetrics{
		RequestID:       reqID,
		Model:           req.Model,
		QueueLatency:    batchRes.QueueLatency,
		PrefillLatency:  prefillDuration,
		DecodeLatency:   decodeDuration,
		TotalLatency:    totalLatency,
		TokensGenerated: tokenCount,
		BatchSize:       batchRes.BatchSize,
		Timestamp:       time.Now(),
	}
	s.tracker.Record(metrics)
	s.governor.Aimd().RecordSuccess()

	curWin, inFlight, _, _ := s.governor.Aimd().WindowStats()

	// Set Telemetry Diagnostics Response Headers
	w.Header().Set("X-Inferno-Queue-Ms", fmt.Sprintf("%d", batchRes.QueueLatency.Milliseconds()))
	w.Header().Set("X-Inferno-Prefill-Ms", fmt.Sprintf("%d", prefillDuration.Milliseconds()))
	w.Header().Set("X-Inferno-Decode-Ms", fmt.Sprintf("%d", decodeDuration.Milliseconds()))
	w.Header().Set("X-Inferno-Batch-Size", fmt.Sprintf("%d", batchRes.BatchSize))
	w.Header().Set("X-Inferno-Tokens-Generated", fmt.Sprintf("%d", tokenCount))
	w.Header().Set("X-Inferno-Prefix-Cache-Hit", fmt.Sprintf("%t", matchRes.CacheHit))
	w.Header().Set("X-Inferno-Prefix-Tokens-Matched", fmt.Sprintf("%d", matchRes.MatchedTokens))
	w.Header().Set("X-Inferno-Prefix-Savings-Ms", fmt.Sprintf("%d", matchRes.EstimatedTTFTSavingsMs))
	w.Header().Set("X-Inferno-Target-Worker", matchRes.WorkerID)
	w.Header().Set("X-Inferno-Aimd-Window", fmt.Sprintf("%d", curWin))
	w.Header().Set("X-Inferno-Aimd-Inflight", fmt.Sprintf("%d", inFlight))

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		words := strings.Fields(batchRes.ResponseText)
		for i, word := range words {
			select {
			case <-ctx.Done():
				// Client disconnected or cancelled request: abort streaming immediately
				s.tracker.RecordCancellation()
				return
			default:
			}

			chunk := map[string]interface{}{
				"id":      reqID,
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]string{
							"content": word + " ",
						},
						"finish_reason": nil,
					},
				},
			}
			data, _ := json.Marshal(chunk)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				// Socket write failed (client disconnected)
				s.tracker.RecordCancellation()
				return
			}
			flusher.Flush()

			if i < len(words)-1 {
				select {
				case <-ctx.Done():
					s.tracker.RecordCancellation()
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
		}

		select {
		case <-ctx.Done():
			s.tracker.RecordCancellation()
			return
		default:
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
		return
	}

	// Non-streaming response
	w.Header().Set("Content-Type", "application/json")
	resp := ChatCompletionResponse{
		ID:      reqID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: batchRes.ResponseText,
				},
				FinishReason: "stop",
			},
		},
		Usage: map[string]int{
			"prompt_tokens":     len(req.Messages) * 8,
			"completion_tokens": tokenCount,
			"total_tokens":      len(req.Messages)*8 + tokenCount,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	summary := s.tracker.GetSummary()
	snap := s.governor.GetLatestSnapshot()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP inferno_requests_total Total completed inference requests\n")
	fmt.Fprintf(w, "# TYPE inferno_requests_total counter\n")
	fmt.Fprintf(w, "inferno_requests_total %d\n\n", summary.Count)

	fmt.Fprintf(w, "# HELP inferno_requests_cancelled_total Total client-cancelled inference streams\n")
	fmt.Fprintf(w, "# TYPE inferno_requests_cancelled_total counter\n")
	fmt.Fprintf(w, "inferno_requests_cancelled_total %d\n\n", summary.CancelledCount)

	fmt.Fprintf(w, "# HELP inferno_tokens_total Total tokens generated\n")
	fmt.Fprintf(w, "# TYPE inferno_tokens_total counter\n")
	fmt.Fprintf(w, "inferno_tokens_total %d\n\n", summary.TotalTokens)

	fmt.Fprintf(w, "# HELP inferno_queue_latency_ms Queue waiting time percentiles\n")
	fmt.Fprintf(w, "# TYPE inferno_queue_latency_ms gauge\n")
	fmt.Fprintf(w, "inferno_queue_latency_ms{quantile=\"0.50\"} %.2f\n", summary.P50QueueMs)
	fmt.Fprintf(w, "inferno_queue_latency_ms{quantile=\"0.95\"} %.2f\n", summary.P95QueueMs)
	fmt.Fprintf(w, "inferno_queue_latency_ms{quantile=\"0.99\"} %.2f\n\n", summary.P99QueueMs)

	fmt.Fprintf(w, "# HELP inferno_ttft_ms Time to First Token (prefill) percentiles\n")
	fmt.Fprintf(w, "# TYPE inferno_ttft_ms gauge\n")
	fmt.Fprintf(w, "inferno_ttft_ms{quantile=\"0.50\"} %.2f\n", summary.P50PrefillMs)
	fmt.Fprintf(w, "inferno_ttft_ms{quantile=\"0.95\"} %.2f\n", summary.P95PrefillMs)
	fmt.Fprintf(w, "inferno_ttft_ms{quantile=\"0.99\"} %.2f\n\n", summary.P99PrefillMs)

	fmt.Fprintf(w, "# HELP inferno_itl_ms Inter-Token Latency (decode) percentiles\n")
	fmt.Fprintf(w, "# TYPE inferno_itl_ms gauge\n")
	fmt.Fprintf(w, "inferno_itl_ms{quantile=\"0.50\"} %.2f\n", summary.P50DecodeMs)
	fmt.Fprintf(w, "inferno_itl_ms{quantile=\"0.95\"} %.2f\n", summary.P95DecodeMs)
	fmt.Fprintf(w, "inferno_itl_ms{quantile=\"0.99\"} %.2f\n\n", summary.P99DecodeMs)

	fmt.Fprintf(w, "# HELP inferno_vram_pressure_pct GPU VRAM / Unified memory pressure\n")
	fmt.Fprintf(w, "# TYPE inferno_vram_pressure_pct gauge\n")
	fmt.Fprintf(w, "inferno_vram_pressure_pct %.2f\n\n", snap.GpuVramPressurePct)

	routerStats := s.router.GetStats()
	fmt.Fprintf(w, "# HELP inferno_prefix_lookups_total Total prompt prefix routing lookups\n")
	fmt.Fprintf(w, "# TYPE inferno_prefix_lookups_total counter\n")
	fmt.Fprintf(w, "inferno_prefix_lookups_total %d\n\n", routerStats.TotalLookups)

	fmt.Fprintf(w, "# HELP inferno_prefix_hits_total Total prefix cache hits\n")
	fmt.Fprintf(w, "# TYPE inferno_prefix_hits_total counter\n")
	fmt.Fprintf(w, "inferno_prefix_hits_total %d\n\n", routerStats.CacheHits)

	fmt.Fprintf(w, "# HELP inferno_prefix_hit_ratio Ratio of prefix cache hits to lookups\n")
	fmt.Fprintf(w, "# TYPE inferno_prefix_hit_ratio gauge\n")
	fmt.Fprintf(w, "inferno_prefix_hit_ratio %.4f\n\n", routerStats.HitRatio)

	fmt.Fprintf(w, "# HELP inferno_prefix_tokens_saved_total Total prompt tokens reused from KV cache\n")
	fmt.Fprintf(w, "# TYPE inferno_prefix_tokens_saved_total counter\n")
	fmt.Fprintf(w, "inferno_prefix_tokens_saved_total %d\n", routerStats.TotalTokensHit)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	snap := s.governor.Sample()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "healthy",
		"telemetry":     snap,
		"metrics":       s.tracker.GetSummary(),
		"prefix_router": s.router.GetStats(),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{"id": "inferno-llama-3-8b", "object": "model", "owned_by": "inferno"},
			{"id": "inferno-qwen-2.5-7b", "object": "model", "owned_by": "inferno"},
		},
	})
}
