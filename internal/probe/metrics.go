package probe

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ContainerMetrics is deliberately a local CLI, never an HTTP endpoint. Both
// baseline containers execute the same small collector. Its transient memory
// contributes to cgroup measurements; PID 1 RSS excludes the collector.
func ContainerMetrics() (map[string]any, error) {
	status, err := os.ReadFile("/proc/1/status")
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err != nil {
		return nil, fmt.Errorf("requires Linux cgroup v2: %w", err)
	}
	stat, err := os.ReadFile("/sys/fs/cgroup/memory.stat")
	if err != nil {
		return nil, err
	}
	result := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano)}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "VmRSS:" || fields[0] == "VmHWM:") {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return nil, err
			}
			key := "rssBytes"
			if fields[0] == "VmHWM:" {
				key = "rssHighWaterBytes"
			}
			result[key] = n * 1024
		}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(current)), 10, 64)
	if err != nil {
		return nil, err
	}
	result["cgroupBytes"] = n
	for _, line := range strings.Split(string(stat), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[0] == "anon" || fields[0] == "file") {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return nil, err
			}
			result["cgroup"+strings.ToUpper(fields[0][:1])+fields[0][1:]+"Bytes"] = n
		}
	}
	if peak, err := os.ReadFile("/sys/fs/cgroup/memory.peak"); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(string(peak)), 10, 64); err == nil {
			result["cgroupPeakBytes"] = n
		}
	}
	return result, nil
}
