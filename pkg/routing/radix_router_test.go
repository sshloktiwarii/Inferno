package routing

import (
	"fmt"
	"sync"
	"testing"
)

func TestRadixPrefixRouter_ExactAndPartialMatch(t *testing.T) {
	router := NewRadixPrefixRouter([]string{"worker-gpu-0", "worker-gpu-1"})

	systemPrompt := "You are an autonomous AI coding assistant specializing in distributed systems engineering and systems architecture."
	worker := "worker-gpu-0"

	// Pre-populate router with system prompt
	router.InsertPrefix(systemPrompt, worker)

	// Case 1: Exact match on system prompt
	resExact := router.MatchLongestPrefix(systemPrompt)
	if !resExact.CacheHit {
		t.Fatalf("Expected cache hit on exact prompt, got false")
	}
	if resExact.WorkerID != worker {
		t.Errorf("Expected worker %s, got %s", worker, resExact.WorkerID)
	}
	if resExact.MatchedTokens == 0 {
		t.Errorf("Expected non-zero matched tokens")
	}
	if resExact.EstimatedTTFTSavingsMs <= 0 {
		t.Errorf("Expected positive TTFT savings, got %d", resExact.EstimatedTTFTSavingsMs)
	}

	// Case 2: Partial prefix match (shared system prompt + novel user message)
	extendedPrompt := systemPrompt + " Please audit the following Rust memory safety invariants."
	resPartial := router.MatchLongestPrefix(extendedPrompt)
	if !resPartial.CacheHit {
		t.Fatalf("Expected cache hit on extended prompt prefix, got false")
	}
	if resPartial.WorkerID != worker {
		t.Errorf("Expected worker %s, got %s", worker, resPartial.WorkerID)
	}
	if resPartial.MatchedTokens < 10 {
		t.Errorf("Expected at least 10 matched tokens, got %d", resPartial.MatchedTokens)
	}

	// Case 3: Completely disjoint prompt (Cache miss)
	disjointPrompt := "Quantum computing simulation using Qiskit and superconducting qubits."
	resMiss := router.MatchLongestPrefix(disjointPrompt)
	if resMiss.CacheHit {
		t.Errorf("Expected cache miss on disjoint prompt, got hit")
	}
}

func TestRadixPrefixRouter_MultiWorkerAffinity(t *testing.T) {
	router := NewRadixPrefixRouter([]string{"worker-gpu-alpha", "worker-gpu-beta"})

	prefixAlpha := "System prompt for Python machine learning workflows and PyTorch tensors"
	prefixBeta := "System prompt for Go microservices and gRPC streaming proxies"

	router.InsertPrefix(prefixAlpha, "worker-gpu-alpha")
	router.InsertPrefix(prefixBeta, "worker-gpu-beta")

	resAlpha := router.MatchLongestPrefix(prefixAlpha + " Run training loop")
	if resAlpha.WorkerID != "worker-gpu-alpha" {
		t.Errorf("Expected worker-gpu-alpha, got %s", resAlpha.WorkerID)
	}

	resBeta := router.MatchLongestPrefix(prefixBeta + " Implement server interface")
	if resBeta.WorkerID != "worker-gpu-beta" {
		t.Errorf("Expected worker-gpu-beta, got %s", resBeta.WorkerID)
	}
}

func TestRadixPrefixRouter_ConcurrentLookups(t *testing.T) {
	router := NewRadixPrefixRouter([]string{"worker-0", "worker-1", "worker-2"})
	basePrompt := "Standard header token stream for distributed agent transactions and audit logging"
	router.InsertPrefix(basePrompt, "worker-1")

	var wg sync.WaitGroup
	workersCount := 50
	queriesPerWorker := 100

	for i := 0; i < workersCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < queriesPerWorker; j++ {
				query := fmt.Sprintf("%s request_%d_%d", basePrompt, id, j)
				res := router.MatchLongestPrefix(query)
				if !res.CacheHit {
					t.Errorf("Expected cache hit in concurrent lookup")
				}
			}
		}(i)
	}

	wg.Wait()

	stats := router.GetStats()
	if stats.TotalLookups < int64(workersCount*queriesPerWorker) {
		t.Errorf("Expected at least %d lookups, got %d", workersCount*queriesPerWorker, stats.TotalLookups)
	}
	if stats.CacheHits == 0 {
		t.Errorf("Expected non-zero cache hits")
	}
	if stats.HitRatio <= 0.0 {
		t.Errorf("Expected positive hit ratio, got %f", stats.HitRatio)
	}
}
