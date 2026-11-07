package frame_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"capri/internal/ui/ansi"
	"capri/internal/ui/cpu"
	"capri/internal/ui/frame"
	"capri/internal/ui/hud"
	"capri/internal/ui/lines"
	"capri/internal/ui/widgets"
)

var items = []lines.Item{
	{ID: 0, Kind: "banner"},
	{ID: 1, Kind: "user", Text: "lista los archivos y luego dime cuántos hay, por favor, con mucho detalle y bien explicado", At: "10:00:00"},
	{ID: 3, Kind: "tool", Name: "shell", Args: map[string]string{"command": "ls . | wc -l"}, OK: true, Result: "0 [exit 0]"},
	{ID: 4, Kind: "assistant", Text: "Hay **0** archivos. Aquí va un `bloque` largo para forzar el ajuste de línea en pantallas estrechas, con mucho texto.\n```js\nconsole.log(\"hola\")\n```\n- punto uno\n- punto dos", At: "10:00:02", Meta: "1.2s · 34 t/s"},
	{ID: 5, Kind: "compact", Scope: "session", Reason: "límite", Before: 6400, After: 900, Turns: 8, RagID: i64(3), Summary: "El usuario se llama Matías y trabaja en Valdivia. Se creó notas.txt."},
	{ID: 6, Kind: "error", Text: "algo falló"},
	{ID: 7, Kind: "sys", Text: "línea uno\nlínea dos"},
}

func i64(n int64) *int64 { return &n }

func base(cols, rows int) frame.State {
	var ls [][]lines.Seg
	for _, it := range items {
		ls = append(ls, lines.ItemLines(it, 60)...)
	}
	llmOK, embOK := true, true
	return frame.State{
		Cols:   cols,
		Rows:   rows,
		T:      123456,
		Lines:  ls,
		Pipe:   frame.Pipe{},
		Health: frame.Health{LLM: &llmOK, Emb: &embOK},
		Hud: frame.HudState{
			Gpu:   &hud.GPU{Temp: 72, Sm: 885, SmMax: 1531, Util: 40, MemUsed: 4600, MemTotal: 7680, Thermal: true},
			Cpu:   &cpu.Info{Model: "Intel Core i3-6100", Threads: 4},
			Model: "qwen2.5-7b-q2_k",
			Nctx:  16384,
		},
		Stats: frame.Stats{Note: 3, Chat: 12, Summary: 2},
		Hits:  []frame.Hit{{ID: 1, Sim: 0.7, Title: "me llamo Matías"}},
		Auto:  false,
		Value: "hola",
		Cur:   2,
		Model: "qwen2.5-7b-q2_k",
		Count: 17,
	}
}

type mut func(*frame.State)

var sizes = [][2]int{{160, 60}, {130, 46}, {118, 40}, {100, 34}, {100, 28}, {70, 24}, {60, 20}, {45, 14}, {40, 11}}

var states = map[string]mut{
	"reposo": func(*frame.State) {},
	"ocupado": func(s *frame.State) {
		s.Busy = true
		s.Elapsed = 3
		s.Pipe = frame.Pipe{Active: "think", Done: []string{"recall", "route"}, Step: 3, Total: 8}
	},
	"memorizando": func(s *frame.State) { s.Memorizing = true; s.Typing = true },
	"compactando": func(s *frame.State) { s.Compacting = true; s.Busy = true },
	"confirmacion": func(s *frame.State) {
		s.Pending = &frame.Pending{Name: "fs_write", What: "⚠ SOBRESCRIBIR hora.txt (ya existe, 5 bytes) con un texto bastante largo para comprobar el recorte"}
	},
	"explorador": func(s *frame.State) {
		b := &frame.Browser{}
		kinds := []string{"note", "chat", "summary"}
		for i := 0; i < 30; i++ {
			b.Rows = append(b.Rows, frame.BrowserRow{ID: int64(i + 1), Kind: kinds[i%3], Title: "registro número " + itoa(i+1) + " con un título largo", Content: strings.Repeat("contenido ", 30), Updated: time.Now().UnixMilli()})
		}
		b.Sel = 7
		s.Browser = b
	},
	"exploradorConfirm": func(s *frame.State) {
		s.Browser = &frame.Browser{Rows: []frame.BrowserRow{{ID: 1, Kind: "note", Title: "a", Content: "b", Updated: time.Now().UnixMilli()}}, Confirm: 1}
	},
	"scrolled": func(s *frame.State) { s.Offset = 5 },
	"sinGpu": func(s *frame.State) {
		no := false
		s.Hud = frame.HudState{}
		s.Health = frame.Health{LLM: &no}
	},
	"largo": func(s *frame.State) { s.Value = strings.Repeat("x", 400); s.Cur = 400 },
	"boot":  func(s *frame.State) { s.Boot = &frame.Boot{T: 1500}; s.Lines = nil },
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestExactFrameSizes(t *testing.T) {
	for name, mk := range states {
		for _, sz := range sizes {
			st := base(sz[0], sz[1])
			mk(&st)
			f := frame.BuildFrame(st)
			R := max(10, sz[1]-1)
			if len(f.Rows) != R {
				t.Fatalf("%s %dx%d: filas %d != %d", name, sz[0], sz[1], len(f.Rows), R)
			}
			for i, r := range f.Rows {
				if got := ansi.Vlen(r); got != sz[0] {
					t.Fatalf("%s %dx%d fila %d: %d celdas != %d\n%s", name, sz[0], sz[1], i, got, sz[0], ansi.Strip(r))
				}
			}
		}
	}
}

func TestChatGetsRemainingHeight(t *testing.T) {
	tall := frame.BuildFrame(base(130, 60))
	small := frame.BuildFrame(base(130, 40))
	if tall.H-small.H != 20 {
		t.Errorf("H %d - %d = %d", tall.H, small.H, tall.H-small.H)
	}
	if tall.H <= 30 {
		t.Errorf("H = %d", tall.H)
	}
}

func TestScrollClampedAndVisible(t *testing.T) {
	a := frame.BuildFrame(base(100, 34))
	st := base(100, 34)
	st.Offset = 6
	b := frame.BuildFrame(st)
	if b.Off != 6 {
		t.Errorf("Off = %d", b.Off)
	}
	if sameRows(a.Rows[1:6], b.Rows[1:6]) {
		t.Error("el scroll no cambió lo visible")
	}
	st.Offset = 9999
	c := frame.BuildFrame(st)
	want := max(0, a.Total-a.H)
	if c.Off != want {
		t.Errorf("Off = %d, quería %d", c.Off, want)
	}
}

func TestContextMeterUsesWorkWindow(t *testing.T) {
	f := stripJoin(frame.BuildFrame(base(130, 46)))
	if !regexp.MustCompile(`CTX\s+[▰▱]+\s+\S+/8k`).MatchString(f) {
		t.Errorf("no se encontró el medidor CTX/8k:\n%s", f)
	}
	if regexp.MustCompile(`/16k`).MatchString(f) {
		t.Error("no debe usar la ventana del servidor")
	}
}

func TestCompactionIndicators(t *testing.T) {
	st := base(130, 46)
	st.Compactions = 3
	f := stripJoin(frame.BuildFrame(st))
	if !regexp.MustCompile(`⇣3`).MatchString(f) {
		t.Error("falta ⇣3")
	}
	if !regexp.MustCompile(`⇣ 2`).MatchString(f) {
		t.Error("falta ⇣ 2")
	}
}

func TestCompactItemText(t *testing.T) {
	var out []string
	for _, l := range lines.ItemLines(lines.Item{Kind: "compact", Scope: "session", Reason: "límite", Before: 6400, After: 900, Turns: 8, RagID: i64(42), Summary: "resumen de prueba"}, 100) {
		out = append(out, lines.LineToPlain(l))
	}
	txt := strings.Join(out, "\n")
	for _, re := range []string{`CONTEXTO COMPACTADO`, `6\.4k → 900 tokens \(−86%\)`, `8 turnos → resumen`, `guardado en memoria #42`} {
		if !regexp.MustCompile(re).MatchString(txt) {
			t.Errorf("falta %q en:\n%s", re, txt)
		}
	}
}

func TestAnsiUtilities(t *testing.T) {
	if got := ansi.Vlen(ansi.Paint("hola", ansi.Style{Color: "#ff0000", Bold: true})); got != 4 {
		t.Errorf("Vlen = %d", got)
	}
	clipped := ansi.ClipAnsi(ansi.Paint("abcdefghij", ansi.Style{Color: "#00ff00"}), 5)
	if got := ansi.Vlen(clipped); got != 5 {
		t.Errorf("Vlen clip = %d", got)
	}
	if got := ansi.Strip(clipped); got != "abcd…" {
		t.Errorf("Strip clip = %q", got)
	}
	if got := ansi.Vlen(ansi.PadEnd(ansi.Paint("ab", ansi.Style{Color: "#fff000"}), 6)); got != 6 {
		t.Errorf("Vlen pad = %d", got)
	}
	if got := ansi.Vlen(ansi.FitRow(strings.Repeat("x", 50), 10)); got != 10 {
		t.Errorf("Vlen fit = %d", got)
	}
}

func TestWidgetWidths(t *testing.T) {
	for _, r := range widgets.Panel(widgets.PanelOptions{Title: "T", W: 30, Rows: []string{"a", "b"}}) {
		if ansi.Vlen(r) != 30 {
			t.Errorf("panel = %d: %q", ansi.Vlen(r), r)
		}
	}
	for _, r := range widgets.NeonBox(widgets.NeonBoxOptions{W: 40, Rows: []string{"hola"}}) {
		if ansi.Vlen(r) != 40 {
			t.Errorf("neonBox = %d: %q", ansi.Vlen(r), r)
		}
	}
	if got := ansi.Vlen(widgets.Scan(999, 33, widgets.ScanOptions{})); got != 33 {
		t.Errorf("scan = %d", got)
	}
	if got := ansi.Vlen(widgets.Equalizer(999, 21, 0.9, nil)); got != 21 {
		t.Errorf("equalizer = %d", got)
	}
	if got := ansi.Vlen(widgets.Meter(0.5, 8, widgets.MeterOptions{Heated: true})); got != 8 {
		t.Errorf("meter = %d", got)
	}
	if got := ansi.Vlen(widgets.Glitch("░▒▓ S P O T ▓▒░", 5000, 0.5, nil, false)); got != 15 {
		t.Errorf("glitch = %d", got)
	}
}

func TestItemIDNotClobbered(t *testing.T) {
	var xs []lines.Item
	next := int64(1)
	push := func(it lines.Item) {
		it.ID = next
		next++
		xs = append(xs, it)
	}
	push(lines.Item{Kind: "user", Text: "a", At: "1"})
	push(lines.Item{Kind: "compact", RagID: i64(1), Scope: "session", Reason: "límite", Before: 10, After: 5, Turns: 1, Summary: "s"})
	if xs[0].ID != 1 || xs[1].ID != 2 {
		t.Errorf("ids = %d, %d", xs[0].ID, xs[1].ID)
	}
	if xs[1].RagID == nil || *xs[1].RagID != 1 {
		t.Errorf("ragId = %v", xs[1].RagID)
	}
}

func stripJoin(f frame.Frame) string {
	var out []string
	for _, r := range f.Rows {
		out = append(out, ansi.Strip(r))
	}
	return strings.Join(out, "\n")
}

func sameRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// El motor de animación mide el tiempo en milisegundos (State.T y Boot.T). Si
// alguien pasa segundos, todo se mueve 1000 veces más lento y parece parado.
// Este test exige que la pantalla cambie dentro de un segundo de reloj.
func TestAnimates(t *testing.T) {
	cases := map[string]mut{
		"reposo":   func(s *frame.State) {},
		"ocupado":  func(s *frame.State) { s.Busy = true; s.Elapsed = 2 },
		"arranque": func(s *frame.State) { s.Boot = &frame.Boot{T: 500}; s.Lines = nil },
	}
	for name, mk := range cases {
		changed := false
		first := base(130, 46)
		mk(&first)
		a := frame.BuildFrame(first)
		for step := 1; step <= 10 && !changed; step++ { // 10 fotogramas de 100 ms = 1 s
			next := base(130, 46)
			mk(&next)
			next.T += float64(step * 100)
			if next.Boot != nil {
				next.Boot = &frame.Boot{T: next.Boot.T + float64(step*100)}
			}
			b := frame.BuildFrame(next)
			for i := range a.Rows {
				if a.Rows[i] != b.Rows[i] {
					changed = true
					break
				}
			}
		}
		if !changed {
			t.Errorf("%s: la pantalla no cambia en 1 s de animación (¿T en segundos en vez de ms?)", name)
		}
	}
}
