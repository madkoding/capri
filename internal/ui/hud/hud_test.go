package hud_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"capri/internal/ui/hud"
)

func TestParseGpuLine(t *testing.T) {
	g := hud.ParseGpuLine("65, 885, 1531, 0, 25.57, 60.00, 4871, 7680, 0x0000000000000000\n")
	if g == nil {
		t.Fatal("ParseGpuLine devolvió nil")
	}
	if g.Temp != 65 || g.SmMax != 1531 || g.MemTotal != 7680 {
		t.Errorf("GPU = %+v", g)
	}
	if g.Thermal {
		t.Error("thermal no debía estar activo")
	}
}

func TestParseGpuThermalBit(t *testing.T) {
	g := hud.ParseGpuLine("88, 600, 1531, 90, 58, 60, 100, 7680, 0x0000000000000020")
	if g == nil || !g.Thermal {
		t.Fatalf("GPU = %+v", g)
	}
}

func TestParseGpuInvalid(t *testing.T) {
	for _, in := range []string{"", "65, 885"} {
		if g := hud.ParseGpuLine(in); g != nil {
			t.Errorf("ParseGpuLine(%q) = %+v", in, g)
		}
	}
}

func TestOldGpuFileHidden(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "gpu.json")
	gpu := hud.ParseGpuLine("65, 885, 1531, 0, 25.57, 60.00, 4871, 7680, 0x0")
	writeJSON(t, file, map[string]any{"ts": time.Now().UnixMilli() - 60000, "gpu": gpu})
	if g := hud.ReadGpuFile(file); g != nil {
		t.Errorf("GPU vieja no debe mostrarse: %+v", g)
	}
	writeJSON(t, file, map[string]any{"ts": time.Now().UnixMilli(), "gpu": gpu})
	if g := hud.ReadGpuFile(file); g == nil || g.Temp != 65 {
		t.Errorf("GPU nueva = %+v", g)
	}
}

func writeJSON(t *testing.T, file string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
