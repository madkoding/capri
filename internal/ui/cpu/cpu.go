// Package cpu lee telemetría de CPU para el HUD: modelo, hilos y temperatura del paquete.
package cpu

import (
	"math"
	"regexp"
	"runtime"
	"strings"
)

// Info es la telemetría de CPU.
type Info struct {
	Model   string   `json:"model"`
	Threads int      `json:"threads"`
	Temp    *float64 `json:"temp"`
}

var (
	brandRe = regexp.MustCompile(`(?i)\(R\)|\(TM\)|\(tm\)|\(r\)`)
	atRe    = regexp.MustCompile(`\s+@.*$`)
	cpuRe   = regexp.MustCompile(`(?i)\s+CPU\b`)
	wsRe    = regexp.MustCompile(`\s+`)
	modelRe = regexp.MustCompile(`(?m)^model name\s*:\s*(.+)$`)
)

// ShortModel acorta "Intel(R) Core(TM) i3-6100 CPU @ 3.70GHz" → "Intel Core i3-6100".
func ShortModel(raw string) string {
	s := brandRe.ReplaceAllString(raw, "")
	s = atRe.ReplaceAllString(s, "")
	s = cpuRe.ReplaceAllString(s, "")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ParseCpuModel extrae la primera línea "model name" de /proc/cpuinfo.
func ParseCpuModel(cpuinfo string) string {
	if m := modelRe.FindStringSubmatch(cpuinfo); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// Zone es una zona térmica.
type Zone struct {
	Type string
	Temp float64
	OK   bool
}

// PickPackageTemp devuelve la temperatura en °C de la zona x86_pkg_temp.
func PickPackageTemp(zones []Zone) *float64 {
	for _, z := range zones {
		if z.Type == "x86_pkg_temp" && z.OK && !math.IsNaN(z.Temp) {
			v := z.Temp / 1000
			return &v
		}
	}
	return nil
}

// ReadCpu devuelve modelo corto, hilos lógicos y °C del paquete (o nil).
func ReadCpu() *Info {
	raw := rawModel()
	out := &Info{Threads: runtime.NumCPU(), Temp: readPackageTemp()}
	if raw != "" {
		out.Model = ShortModel(raw)
	}
	return out
}
