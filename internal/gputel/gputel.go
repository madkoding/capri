// Package gputel publica la telemetría de GPU del host para la TUI aislada.
package gputel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"capri/internal/ui/hud"
)

// Run escribe telemetry/gpu.json cada `every` ms hasta que se interrumpa.
func Run(dir string, every time.Duration) error {
	if dir == "" {
		dir = os.Getenv("GPU_TELEMETRY_DIR")
	}
	if dir == "" {
		dir = "telemetry"
	}
	if every <= 0 {
		if v := os.Getenv("GPU_TELEMETRY_MS"); v != "" {
			if n, err := time.ParseDuration(v + "ms"); err == nil {
				every = n
			}
		}
	}
	if every <= 0 {
		every = 2 * time.Second
	}
	abs, _ := filepath.Abs(dir)
	file := filepath.Join(abs, "gpu.json")
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	tick := func() error {
		gpu := hud.QueryGpu()
		b, err := json.Marshal(map[string]any{"ts": time.Now().UnixMilli(), "gpu": gpu})
		if err != nil {
			return err
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, file)
	}
	fmt.Printf("gpu-telemetry: escribiendo %s cada %d ms (Ctrl+C para parar)\n", file, every.Milliseconds())
	if err := tick(); err != nil {
		return err
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		if err := tick(); err != nil {
			fmt.Fprintf(os.Stderr, "gpu-telemetry: %v\n", err)
		}
	}
	return nil
}
