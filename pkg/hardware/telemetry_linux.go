//go:build linux

package hardware

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// platformProbe samples Linux cgroup/sysfs memory metrics.
func platformProbe() (hostPressurePct float64, totalMemBytes uint64, usedMemBytes uint64) {
	// Attempt reading cgroup v2 memory stats
	currentBytes, err1 := os.ReadFile("/sys/fs/cgroup/memory.current")
	maxBytes, err2 := os.ReadFile("/sys/fs/cgroup/memory.max")

	if err1 == nil && err2 == nil {
		currentStr := strings.TrimSpace(string(currentBytes))
		maxStr := strings.TrimSpace(string(maxBytes))

		current, errC := strconv.ParseUint(currentStr, 10, 64)
		max, errM := strconv.ParseUint(maxStr, 10, 64)

		if errC == nil && errM == nil && max > 0 {
			usedMemBytes = current
			totalMemBytes = max
			hostPressurePct = (float64(usedMemBytes) / float64(totalMemBytes)) * 100.0
			return hostPressurePct, totalMemBytes, usedMemBytes
		}
	}

	// Fallback to runtime memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	totalMemBytes = 16 * 1024 * 1024 * 1024
	usedMemBytes = m.Alloc
	hostPressurePct = (float64(m.Alloc) / float64(m.Sys)) * 100.0

	return hostPressurePct, totalMemBytes, usedMemBytes
}
