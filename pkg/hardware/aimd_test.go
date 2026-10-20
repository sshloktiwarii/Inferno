package hardware

import (
	"testing"
	"time"
)

func TestDynamicAimdController_AdditiveIncrease(t *testing.T) {
	aimd := NewDynamicAimdController(4, 2, 8, 100*time.Millisecond)

	w, inflight, min, max := aimd.WindowStats()
	if w != 4 || inflight != 0 || min != 2 || max != 8 {
		t.Fatalf("Unexpected initial stats: w=%d, in=%d, min=%d, max=%d", w, inflight, min, max)
	}

	// Record success -> window should increment
	aimd.RecordSuccess()
	w, _, _, _ = aimd.WindowStats()
	if w != 5 {
		t.Fatalf("Expected window=5, got %d", w)
	}

	// Saturate window
	for i := 0; i < 10; i++ {
		aimd.RecordSuccess()
	}
	w, _, _, _ = aimd.WindowStats()
	if w != 8 {
		t.Fatalf("Expected capped max window=8, got %d", w)
	}
}

func TestDynamicAimdController_MultiplicativeDecreaseAndHysteresis(t *testing.T) {
	aimd := NewDynamicAimdController(8, 2, 16, 200*time.Millisecond)

	// First throttle -> 8 / 2 = 4
	if !aimd.RecordThrottle() {
		t.Fatalf("First throttle should be accepted")
	}
	w, _, _, _ := aimd.WindowStats()
	if w != 4 {
		t.Fatalf("Expected window=4, got %d", w)
	}

	// Immediate second throttle within 200ms hysteresis cooldown should be suppressed
	if aimd.RecordThrottle() {
		t.Fatalf("Rapid second throttle should be suppressed by cooldown")
	}
	w, _, _, _ = aimd.WindowStats()
	if w != 4 {
		t.Fatalf("Window should remain 4 due to cooldown suppression, got %d", w)
	}

	// After cooldown, throttle again -> 4 / 2 = 2 (minWindow)
	time.Sleep(210 * time.Millisecond)
	if !aimd.RecordThrottle() {
		t.Fatalf("Throttle after cooldown should be accepted")
	}
	w, _, _, _ = aimd.WindowStats()
	if w != 2 {
		t.Fatalf("Expected window=2, got %d", w)
	}
}

func TestDynamicAimdController_AcquireReleaseSaturation(t *testing.T) {
	aimd := NewDynamicAimdController(2, 1, 4, 100*time.Millisecond)

	if !aimd.Acquire() {
		t.Fatalf("Failed to acquire first slot")
	}
	if !aimd.Acquire() {
		t.Fatalf("Failed to acquire second slot")
	}
	// Window is 2, third should fail
	if aimd.Acquire() {
		t.Fatalf("Third acquire should fail when window is 2")
	}

	aimd.Release()
	if !aimd.Acquire() {
		t.Fatalf("Should succeed acquire after release")
	}
	aimd.Release()
	aimd.Release()
}
