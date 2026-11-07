//go:build linux

package cpu

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func rawModel() string {
	b, _ := os.ReadFile("/proc/cpuinfo")
	if raw := ParseCpuModel(string(b)); raw != "" {
		return raw
	}
	for _, line := range strings.Split(string(b), "\n") {
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "hardware") || strings.HasPrefix(low, "processor") {
			if i := strings.Index(line, ":"); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func readPackageTemp() *float64 {
	entries, err := os.ReadDir("/sys/class/thermal")
	if err != nil {
		return nil
	}
	var zones []Zone
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "thermal_zone") {
			continue
		}
		dir := filepath.Join("/sys/class/thermal", e.Name())
		t, _ := os.ReadFile(filepath.Join(dir, "type"))
		b, err := os.ReadFile(filepath.Join(dir, "temp"))
		z := Zone{Type: strings.TrimSpace(string(t))}
		if err == nil {
			if n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
				z.Temp = n
				z.OK = true
			}
		}
		zones = append(zones, z)
	}
	return PickPackageTemp(zones)
}
