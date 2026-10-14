package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// MatchResult encapsulates the prefix-cache routing outcome.
type MatchResult struct {
	WorkerID               string  `json:"worker_id"`
	MatchedTokens          int     `json:"matched_tokens"`
	TotalTokens            int     `json:"total_tokens"`
	MatchRatio             float64 `json:"match_ratio"`
	CacheHit               bool    `json:"cache_hit"`
	EstimatedTTFTSavingsMs int64   `json:"estimated_ttft_savings_ms"`
}

// PrefixNode represents a node in the Radix Tree.
type PrefixNode struct {
	TokenHash   string
	TokenChunk  string
	Children    map[string]*PrefixNode
	WorkerSet   map[string]int64 // workerID -> last accessed unix nano
	HitCount    int64
	LastAccess  time.Time
}

func newPrefixNode(hash, chunk string) *PrefixNode {
	return &PrefixNode{
		TokenHash:  hash,
		TokenChunk: chunk,
		Children:   make(map[string]*PrefixNode),
		WorkerSet:  make(map[string]int64),
		LastAccess: time.Now(),
	}
}

// RouterStats captures telemetry counters for the prefix router.
type RouterStats struct {
	TotalLookups    int64   `json:"total_lookups"`
	CacheHits       int64   `json:"cache_hits"`
	CacheMisses     int64   `json:"cache_misses"`
	HitRatio        float64 `json:"hit_ratio"`
	TotalTokensHit  int64   `json:"total_tokens_hit"`
	TotalTokensSavedMs int64 `json:"total_tokens_saved_ms"`
}

// RadixPrefixRouter indexes prompt token prefixes to route inference requests
// to the GPU worker holding the relevant KV-cache.
type RadixPrefixRouter struct {
	mu           sync.RWMutex
	root         *PrefixNode
	workers      map[string]bool
	workerList   []string
	chunkSize    int
	minHitTokens int
	msPerToken   float64

	// Telemetry metrics
	totalLookups   int64
	cacheHits      int64
	totalTokensHit int64
}

// NewRadixPrefixRouter initializes a prefix cache router with default workers.
func NewRadixPrefixRouter(workers []string) *RadixPrefixRouter {
	if len(workers) == 0 {
		workers = []string{"worker-gpu-0", "worker-gpu-1"}
	}
	workerMap := make(map[string]bool)
	for _, w := range workers {
		workerMap[w] = true
	}

	return &RadixPrefixRouter{
		root:         newPrefixNode("ROOT", ""),
		workers:      workerMap,
		workerList:   workers,
		chunkSize:    16, // Chunks of characters representing token units
		minHitTokens: 4,  // Minimum token match count to qualify as a cache hit
		msPerToken:   0.8, // Estimated prefill compute time per token saved
	}
}

// hashChunk produces a SHA256 hex string for a given text segment.
func hashChunk(chunk string) string {
	h := sha256.Sum256([]byte(chunk))
	return hex.EncodeToString(h[:8]) // 16 hex chars is sufficient for low collision
}

// Tokenize splits a prompt into token chunks.
func (r *RadixPrefixRouter) Tokenize(prompt string) []string {
	words := strings.Fields(prompt)
	if len(words) == 0 {
		return nil
	}
	return words
}

// InsertPrefix indexes an executed prompt into the Radix Tree and associates it with a worker.
func (r *RadixPrefixRouter) InsertPrefix(prompt string, workerID string) {
	tokens := r.Tokenize(prompt)
	if len(tokens) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	curr := r.root
	now := time.Now().UnixNano()

	for _, token := range tokens {
		h := hashChunk(token)
		child, exists := curr.Children[h]
		if !exists {
			child = newPrefixNode(h, token)
			curr.Children[h] = child
		}
		child.WorkerSet[workerID] = now
		child.LastAccess = time.Now()
		curr = child
	}
}

// MatchLongestPrefix traverses the Radix Tree to find the GPU worker with the longest KV prefix match.
func (r *RadixPrefixRouter) MatchLongestPrefix(prompt string) *MatchResult {
	tokens := r.Tokenize(prompt)
	totalTokens := len(tokens)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.totalLookups++

	if totalTokens == 0 {
		defaultWorker := r.selectFallbackWorker()
		return &MatchResult{
			WorkerID:               defaultWorker,
			MatchedTokens:          0,
			TotalTokens:            0,
			MatchRatio:             0.0,
			CacheHit:               false,
			EstimatedTTFTSavingsMs: 0,
		}
	}

	curr := r.root
	matchedTokens := 0
	matchedWorkerCounts := make(map[string]int)

	for _, token := range tokens {
		h := hashChunk(token)
		child, exists := curr.Children[h]
		if !exists {
			break
		}
		matchedTokens++
		child.HitCount++
		child.LastAccess = time.Now()
		for w := range child.WorkerSet {
			matchedWorkerCounts[w]++
		}
		curr = child
	}

	// Determine best worker holding the longest prefix
	bestWorker := ""
	maxDepth := -1
	for w, count := range matchedWorkerCounts {
		if count > maxDepth {
			maxDepth = count
			bestWorker = w
		}
	}

	if bestWorker == "" {
		bestWorker = r.selectFallbackWorker()
	}

	isHit := matchedTokens >= r.minHitTokens
	savingsMs := int64(0)
	if isHit {
		r.cacheHits++
		r.totalTokensHit += int64(matchedTokens)
		savingsMs = int64(float64(matchedTokens) * r.msPerToken)
		if savingsMs < 1 {
			savingsMs = 1
		}
	}

	// Always index/refresh this path for the designated worker (speculative warm-up)
	go r.InsertPrefix(prompt, bestWorker)

	return &MatchResult{
		WorkerID:               bestWorker,
		MatchedTokens:          matchedTokens,
		TotalTokens:            totalTokens,
		MatchRatio:             float64(matchedTokens) / float64(totalTokens),
		CacheHit:               isHit,
		EstimatedTTFTSavingsMs: savingsMs,
	}
}

// selectFallbackWorker provides round-robin or first available worker selection.
func (r *RadixPrefixRouter) selectFallbackWorker() string {
	if len(r.workerList) == 0 {
		return "worker-gpu-0"
	}
	idx := int(r.totalLookups) % len(r.workerList)
	return r.workerList[idx]
}

// GetStats returns router telemetry metrics.
func (r *RadixPrefixRouter) GetStats() RouterStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ratio := 0.0
	if r.totalLookups > 0 {
		ratio = float64(r.cacheHits) / float64(r.totalLookups)
	}

	return RouterStats{
		TotalLookups:       r.totalLookups,
		CacheHits:          r.cacheHits,
		CacheMisses:        r.totalLookups - r.cacheHits,
		HitRatio:           ratio,
		TotalTokensHit:     r.totalTokensHit,
		TotalTokensSavedMs: int64(float64(r.totalTokensHit) * r.msPerToken),
	}
}
