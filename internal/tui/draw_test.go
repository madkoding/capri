package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"capri/internal/ui/lines"
)

func benchApp(t *testing.T, n int) *app {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { devnull.Close() })
	a := &app{painter: &painter{out: devnull}, out: devnull, t0: time.Now(), nextID: 1, tick: time.Second / 60, cols: 160, rows: 45}
	a.finishBoot()
	for i := 0; i < n; i++ {
		switch i % 4 {
		case 0:
			a.push(lines.Item{Kind: "user", Text: fmt.Sprintf("pregunta %d sobre archivos y memoria", i), At: "12:00:00"})
		case 1:
			a.push(lines.Item{Kind: "assistant", Text: strings.Repeat("Una respuesta con **markdown**, `código` y texto largo. ", 6), At: "12:00:01", Meta: "3.2s"})
		case 2:
			a.push(lines.Item{Kind: "tool", Name: "fs_read", Args: map[string]string{"path": "a.txt"}, OK: true, Result: "1| hola\n2| mundo"})
		default:
			a.push(lines.Item{Kind: "note", Text: "💭 " + strings.Repeat("pienso ", 40)})
		}
	}
	return a
}

// A 60 fps cada fotograma dispone de ~16 ms: con el historial cacheado un
// repintado con cientos de elementos tiene que costar una fracción de eso.
func TestFrameFitsIn60FPSBudget(t *testing.T) {
	a := benchApp(t, 400)
	a.busy = &a.t0     // panel animado, como durante un turno
	a.draw(time.Now()) // primer fotograma: llena la caché
	const frames = 120
	t0 := time.Now()
	for i := 0; i < frames; i++ {
		a.draw(t0.Add(time.Duration(i) * a.tick))
	}
	per := time.Since(t0) / frames
	t.Logf("fotograma medio con 400 elementos: %v", per)
	if per > 12*time.Millisecond {
		t.Errorf("fotograma de %v: no cabe en 60 fps", per)
	}
	if len(a.lineCache) != len(a.items) {
		t.Errorf("caché = %d elementos, historial = %d", len(a.lineCache), len(a.items))
	}
	// cambiar de ancho invalida la caché
	a.cols = 120
	a.draw(time.Now())
	if a.cacheW != 117 || len(a.lineCache) != len(a.items) {
		t.Errorf("tras redimensionar: W=%d caché=%d", a.cacheW, len(a.lineCache))
	}
}

func TestTickInterval(t *testing.T) {
	t.Setenv("CAPRI_ANIM", "")
	t.Setenv("CAPRI_FPS", "")
	if got := tickInterval(); got != time.Second/60 {
		t.Errorf("por defecto 60 fps: %v", got)
	}
	t.Setenv("CAPRI_FPS", "30")
	if got := tickInterval(); got != time.Second/30 {
		t.Errorf("CAPRI_FPS=30: %v", got)
	}
	t.Setenv("CAPRI_ANIM", "0")
	if got := tickInterval(); got != time.Second {
		t.Errorf("CAPRI_ANIM=0: %v", got)
	}
}
