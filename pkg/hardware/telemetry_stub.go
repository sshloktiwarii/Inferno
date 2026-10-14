//go:build !darwin && !linux

package hardware

import "runtime"

// platformProbe fallback for other operating systems.
func platformProbe() (hostPressurePct float64, totalMemBytes uint64, usedMemBytes uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	totalMemBytes = 16 * 1024 * 1024 * 1024
	usedMemBytes = m.Alloc
	hostPressurePct = (float64(m.Alloc) / float64(m.Sys)) * 100.0
	return hostPressurePct, totalMemBytes, usedMemBytes
}
