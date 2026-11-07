//go:build darwin

package cpu

import (
	"os/exec"
	"strings"
)

func rawModel() string {
	for _, key := range []string{"machdep.cpu.brand_string", "hw.model"} {
		out, err := exec.Command("sysctl", "-n", key).Output()
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	return ""
}

// readPackageTemp devuelve nil: macOS no expone la temperatura sin privilegios.
func readPackageTemp() *float64 { return nil }
