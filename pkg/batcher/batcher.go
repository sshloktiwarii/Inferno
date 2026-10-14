package batcher

import (
	"context"
	"sync"
	"time"
)

// RequestItem represents an incoming prompt request queued for continuous batching.
type RequestItem struct {
	ID        string
	Model     string
	Prompt    string
	EnqueuedAt time.Time
	ResultCh  chan BatchResult
}

// BatchResult returns the result of the batched execution.
type BatchResult struct {
	QueueLatency time.Duration
	ResponseText string
	BatchSize    int
	Error        error
}

// Config controls continuous batching thresholds.
type Config struct {
	MaxBatchSize time.Duration
	BatchWindow  time.Duration
	BatchLimit   int
}

// DynamicBatcher aggregates concurrent requests into micro-batches.
type DynamicBatcher struct {
	batchWindow time.Duration
	batchLimit  int
	queue       chan *RequestItem
	closeCh     chan struct{}
	wg          sync.WaitGroup
}

func NewDynamicBatcher(batchWindow time.Duration, batchLimit int) *DynamicBatcher {
	if batchLimit <= 0 {
		batchLimit = 16
	}
	if batchWindow <= 0 {
		batchWindow = 20 * time.Millisecond
	}

	b := &DynamicBatcher{
		batchWindow: batchWindow,
		batchLimit:  batchLimit,
		queue:       make(chan *RequestItem, 1024),
		closeCh:     make(chan struct{}),
	}

	b.wg.Add(1)
	go b.worker()

	return b
}

func (b *DynamicBatcher) Enqueue(ctx context.Context, item *RequestItem) (*BatchResult, error) {
	item.EnqueuedAt = time.Now()
	item.ResultCh = make(chan BatchResult, 1)

	select {
	case b.queue <- item:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case res := <-item.ResultCh:
		return &res, res.Error
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *DynamicBatcher) worker() {
	defer b.wg.Done()

	for {
		var batch []*RequestItem

		select {
		case <-b.closeCh:
			return
		case first, ok := <-b.queue:
			if !ok {
				return
			}
			batch = append(batch, first)
		}

		// Collect additional items within the batch window up to batchLimit
		timer := time.NewTimer(b.batchWindow)
	collectLoop:
		for len(batch) < b.batchLimit {
			select {
			case next := <-b.queue:
				batch = append(batch, next)
			case <-timer.C:
				break collectLoop
			case <-b.closeCh:
				timer.Stop()
				return
			}
		}
		timer.Stop()

		// Process the micro-batch
		b.processBatch(batch)
	}
}

func (b *DynamicBatcher) processBatch(batch []*RequestItem) {
	now := time.Now()
	batchSize := len(batch)

	for _, item := range batch {
		queueLatency := now.Sub(item.EnqueuedAt)
		item.ResultCh <- BatchResult{
			QueueLatency: queueLatency,
			ResponseText: "Batched inference completion for: " + item.Prompt,
			BatchSize:    batchSize,
			Error:        nil,
		}
	}
}

func (b *DynamicBatcher) Close() {
	close(b.closeCh)
	b.wg.Wait()
}
