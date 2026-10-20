package hardware

import (
	"sync"
	"time"
)

// DynamicAimdController implements TCP-Vegas/AIMD dynamic concurrency window sizing.
// It increases concurrency linearly on successful requests (+1) and shrinks multiplicatively
// (/2) upon receiving upstream 429 Too Many Requests or severe backpressure.
// It features a 500ms hysteresis cooldown to prevent oscillation stampedes.
type DynamicAimdController struct {
	mu            sync.RWMutex
	currentWindow int
	minWindow     int
	maxWindow     int
	inFlight      int
	lastThrottle  time.Time
	cooldown      time.Duration
}

// NewDynamicAimdController initializes the AIMD controller with default bounds.
func NewDynamicAimdController(initialWindow, minWindow, maxWindow int, cooldown time.Duration) *DynamicAimdController {
	if minWindow <= 0 {
		minWindow = 1
	}
	if maxWindow < minWindow {
		maxWindow = 256
	}
	if initialWindow < minWindow {
		initialWindow = minWindow
	}
	if initialWindow > maxWindow {
		initialWindow = maxWindow
	}
	if cooldown <= 0 {
		cooldown = 500 * time.Millisecond
	}

	return &DynamicAimdController{
		currentWindow: initialWindow,
		minWindow:     minWindow,
		maxWindow:     maxWindow,
		inFlight:      0,
		cooldown:      cooldown,
	}
}

// Acquire attempts to acquire a concurrency slot under the current dynamic window.
// Returns true if admitted, false if rejected due to concurrency limit.
func (c *DynamicAimdController) Acquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inFlight >= c.currentWindow {
		return false
	}
	c.inFlight++
	return true
}

// Release marks an in-flight request as completed.
func (c *DynamicAimdController) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.inFlight > 0 {
		c.inFlight--
	}
}

// RecordSuccess performs Additive Increase (+1) up to maxWindow.
func (c *DynamicAimdController) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.currentWindow < c.maxWindow {
		c.currentWindow++
	}
}

// RecordThrottle performs Multiplicative Decrease (/2) down to minWindow,
// protected by the hysteresis cooldown duration.
func (c *DynamicAimdController) RecordThrottle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if now.Sub(c.lastThrottle) < c.cooldown {
		// Suppress oscillation within cooldown window
		return false
	}

	c.lastThrottle = now
	c.currentWindow = c.currentWindow / 2
	if c.currentWindow < c.minWindow {
		c.currentWindow = c.minWindow
	}
	return true
}

// WindowStats returns the instantaneous window, inFlight, minWindow, and maxWindow.
func (c *DynamicAimdController) WindowStats() (window, inFlight, min, max int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentWindow, c.inFlight, c.minWindow, c.maxWindow
}
