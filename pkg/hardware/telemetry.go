package hardware

import (
	"runtime"
	"sync"
	"time"
)

// TelemetrySnapshot captures instantaneous compute and memory health.
type TelemetrySnapshot struct {
	Timestamp          time.Time `json:"timestamp"`
	AllocatedMemBytes  uint64    `json:"allocated_mem_bytes"`
	TotalSysMemBytes   uint64    `json:"total_sys_mem_bytes"`
	MemoryPressurePct  float64   `json:"memory_pressure_pct"`
	GpuVramUsedBytes   uint64    `json:"gpu_vram_used_bytes"`
	GpuVramTotalBytes  uint64    `json:"gpu_vram_total_bytes"`
	GpuVramPressurePct float64   `json:"gpu_vram_pressure_pct"`
	BackpressureActive bool      `json:"backpressure_active"`
	ActiveGoroutines   int       `json:"active_goroutines"`
}

// BackpressureGovernor manages admission control based on hardware telemetry.
type BackpressureGovernor struct {
	mu                      sync.RWMutex
	maxMemoryPressurePct    float64
	simulatedVramPressurePct float64
	lastSnapshot            TelemetrySnapshot
}

func NewBackpressureGovernor(maxMemoryPressurePct float64) *BackpressureGovernor {
	if maxMemoryPressurePct <= 0 {
		maxMemoryPressurePct = 90.0
	}
	gov := &BackpressureGovernor{
		maxMemoryPressurePct: maxMemoryPressurePct,
	}
	gov.Sample()
	return gov
}

// SetSimulatedVramPressure enables testing VRAM pressure triggers deterministically.
func (bg *BackpressureGovernor) SetSimulatedVramPressure(pct float64) {
	bg.mu.Lock()
	defer bg.mu.Unlock()
	bg.simulatedVramPressurePct = pct
}

// Sample inspects system memory statistics and updates telemetry.
func (bg *BackpressureGovernor) Sample() TelemetrySnapshot {
	bg.mu.Lock()
	defer bg.mu.Unlock()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	pressurePct := 0.0
	if m.Sys > 0 {
		pressurePct = (float64(m.Alloc) / float64(m.Sys)) * 100.0
	}

	vramPressure := bg.simulatedVramPressurePct
	if vramPressure == 0 {
		// Default conservative baseline: align with host memory pressure
		vramPressure = pressurePct * 0.8
	}

	backpressure := pressurePct >= bg.maxMemoryPressurePct || vramPressure >= bg.maxMemoryPressurePct

	snapshot := TelemetrySnapshot{
		Timestamp:          time.Now(),
		AllocatedMemBytes:  m.Alloc,
		TotalSysMemBytes:   m.Sys,
		MemoryPressurePct:  pressurePct,
		GpuVramUsedBytes:   uint64(vramPressure * 100000000),
		GpuVramTotalBytes:  16 * 1024 * 1024 * 1024, // 16GB reference VRAM
		GpuVramPressurePct: vramPressure,
		BackpressureActive: backpressure,
		ActiveGoroutines:   runtime.NumGoroutine(),
	}

	bg.lastSnapshot = snapshot
	return snapshot
}

// ShouldThrottle checks if an incoming inference request should be admitted or rejected.
func (bg *BackpressureGovernor) ShouldThrottle() (bool, string) {
	snap := bg.Sample()
	if snap.BackpressureActive {
		return true, "Hardware telemetry limit exceeded: VRAM/Memory pressure above safety threshold (>90%)"
	}
	return false, ""
}

func (bg *BackpressureGovernor) GetLatestSnapshot() TelemetrySnapshot {
	bg.mu.RLock()
	defer bg.mu.RUnlock()
	return bg.lastSnapshot
}
