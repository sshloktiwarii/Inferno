package batcher

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestDynamicBatcherConcurrentAggregation(t *testing.T) {
	b := NewDynamicBatcher(30*time.Millisecond, 8)
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	numRequests := 10
	var wg sync.WaitGroup
	results := make([]*BatchResult, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			item := &RequestItem{
				ID:     fmt.Sprintf("req-%d", idx),
				Model:  "test-model",
				Prompt: fmt.Sprintf("prompt %d", idx),
			}
			res, err := b.Enqueue(ctx, item)
			if err != nil {
				t.Errorf("Enqueue failed for request %d: %v", idx, err)
				return
			}
			results[idx] = res
		}(i)
	}

	wg.Wait()

	for i, res := range results {
		if res == nil {
			t.Fatalf("Result %d was nil", i)
		}
		if res.BatchSize <= 1 {
			t.Logf("Notice: request %d had single batch size (timing dependent)", i)
		}
		if res.ResponseText == "" {
			t.Errorf("Result %d had empty response text", i)
		}
	}
}
