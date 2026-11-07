// Package frame construye la pantalla completa como una lista de filas de EXACTAMENTE `cols` celdas.
// Es una función pura de (estado, t): se puede probar sin terminal y pintar con diferencias.
package frame

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"capri/internal/config"
	"capri/internal/llm"
	"capri/internal/ui/ansi"
	"capri/internal/ui/cpu"
	"capri/internal/ui/hud"
	"capri/internal/ui/lines"
	"capri/internal/ui/theme"
	"capri/internal/ui/widgets"
)

var (
	pipeOrder  = [][2]string{{"recall", "RECALL"}, {"route", "ROUTE"}, {"think", "THINK"}, {"act", "ACT"}, {"memory", "MEMORY"}}
	phaseLabel = map[string]string{"recall": "CONSULTANDO MEMORIA", "route": "ENRUTANDO", "think": "PENSANDO", "act": "EJECUTANDO", "web": "BUSCANDO EN INTERNET", "memory": "MEMORIZANDO"}
	phaseColor = map[string]string{"recall": theme.C["violet"], "route": theme.C["primary"], "think": theme.C["secondary"], "act": theme.C["yellow"], "web": theme.C["yellow"], "memory": theme.C["violet"]}
)

// TIPS son los consejos rotatorios del pie.
var TIPS = []string{
	"/undo deshace el último turno que modificó algo",
	"/mem explora y olvida recuerdos",
	"Esc cancela el turno en curso",
	"RePág / AvPág desplazan el chat",
	"/compact compacta el contexto · /context lo muestra",
	"/dedupe funde notas repetidas",
	"/bad marca un turno mal resuelto",
	"/export guarda la conversación en un archivo",
	"/auto ejecuta sin preguntar (sobrescribir siempre pregunta)",
	"↑ ↓ recorre tu historial de órdenes",
	"dime un dato tuyo y lo recordaré solo",
}

func d(t string) string   { return ansi.Paint(t, ansi.Style{Color: theme.C["dim"]}) }
func m(t string) string   { return ansi.Paint(t, ansi.Style{Color: theme.C["mute"]}) }
func ink(t string) string { return ansi.Paint(t, ansi.Style{Color: theme.C["ink"]}) }

func k(n int) string {
	if n >= 1000 {
		if n >= 10000 {
			return fmt.Sprintf("%.0fk", float64(n)/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.0f", math.Round(float64(n)))
}

func kw(n int) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dk", n/1024)
	}
	return k(n)
}

// Hit es un recuerdo mostrado en el panel de memoria.
type Hit struct {
	ID    int64
	Sim   float64
	Title string
}

// Stats son los contadores del cerebro.
type Stats struct {
	Note    int
	Chat    int
	Summary int
}

// Health es el estado de los servicios.
type Health struct {
	LLM *bool
	Emb *bool
}

// HudState es la telemetría mostrada.
type HudState struct {
	Gpu   *hud.GPU
	Cpu   *cpu.Info
	Model string
	Nctx  int
}

// Pipe es el estado del enlace neuronal.
type Pipe struct {
	Active string
	Done   []string
	Step   int
	Total  int
}

// Pending es una confirmación pendiente.
type Pending struct {
	Name string
	What string
}

// BrowserRow es una fila del explorador de memoria.
type BrowserRow struct {
	ID      int64
	Kind    string
	Title   string
	Content string
	Updated int64
}

// Browser es el explorador de memoria.
type Browser struct {
	Rows    []BrowserRow
	Sel     int
	Confirm int64
}

// Boot es el estado del arranque.
type Boot struct {
	T float64
}

// State es todo lo que necesita un fotograma.
type State struct {
	Cols int
	Rows int
	T    float64

	Boot   *Boot
	Lines  [][]lines.Seg
	Offset int

	Busy        bool
	Elapsed     float64
	Pipe        Pipe
	Memorizing  bool
	Typing      bool
	Compacting  bool
	Compactions int

	Pending *Pending
	Browser *Browser
	Health  Health
	Hud     HudState
	Stats   Stats
	Hits    []Hit

	Auto   bool
	Value  string
	Cur    int
	Model  string
	Count  int
	Locked bool
}

// Frame es el resultado de construir la pantalla.
type Frame struct {
	Rows  []string
	H     int
	Total int
	Off   int
}

func winOf(h HudState) int {
	cfg := config.Get()
	if h.Nctx > 0 {
		if h.Nctx < cfg.CtxWindow {
			return h.Nctx
		}
	}
	return cfg.CtxWindow
}

func ctxColor(v float64) string {
	cfg := config.Get()
	switch {
	case v < 0.5:
		return theme.C["primary"]
	case v < cfg.CtxThreshold:
		return theme.C["yellow"]
	case v < 0.95:
		return theme.C["orange"]
	default:
		return theme.C["red"]
	}
}

func speedColor(v float64) string {
	if v >= 40 {
		return theme.C["green"]
	}
	if v >= 20 {
		return theme.C["yellow"]
	}
	return theme.C["orange"]
}

func clock() string { return time.Now().Format("15:04:05") }

func ledOf(v *bool) string {
	if v == nil {
		return "busy"
	}
	if *v {
		return "on"
	}
	return "alert"
}

// ---------- paneles del dock ----------

func systemPanel(s State, w int) []string {
	st := llm.Snapshot()
	g := s.Hud.Gpu
	c := s.Hud.Cpu
	iw := w - 4
	name := strings.TrimRight(widgets.Fit(s.Model, max(6, iw-6)), " ")
	tps := st.Gen
	nctx := winOf(s.Hud)
	cv := math.Min(1, float64(st.Ctx)/float64(nctx))
	rows := []string{
		widgets.Led(ledOf(s.Health.LLM), theme.C["green"], s.T) + m(" LLM  ") + ink(name),
		widgets.Led(ledOf(s.Health.Emb), theme.C["green"], s.T) + m(" EMB  ") + ink(embedLabel()) + func() string {
			if tps > 0 {
				return d("  ") + ansi.Paint(fmt.Sprintf("↯%.0f t/s", tps), ansi.Style{Color: speedColor(tps)})
			}
			return ""
		}(),
	}
	if c != nil {
		temp := 0.0
		hasTemp := c.Temp != nil
		if hasTemp {
			temp = *c.Temp
		}
		state := "on"
		if hasTemp && temp >= 90 {
			state = "alert"
		}
		row := widgets.Led(state, theme.Heat(temp/100), s.T) + m(" CPU  ") + ink(strings.TrimRight(widgets.Fit(c.Model, 16), " ")) + d(fmt.Sprintf(" %dT", c.Threads))
		if hasTemp {
			row += "  " + ansi.Paint(fmt.Sprintf("%.0f°C", temp), ansi.Style{Color: theme.Heat(temp / 100), Bold: true}) + " " + widgets.Meter(temp/100, 6, widgets.MeterOptions{Heated: true})
		} else {
			row += d("  temp n/d")
		}
		rows = append(rows, row)
	}
	if g != nil {
		throttle := d(fmt.Sprintf("%.0fMHz", g.Sm))
		if g.Thermal {
			col := theme.C["orange"]
			if int(math.Floor(s.T/300))%2 == 1 {
				col = theme.C["red"]
			}
			throttle = ansi.Paint("THROTTLE", ansi.Style{Color: col, Bold: true})
		}
		rows = append(rows,
			widgets.Led(map[bool]string{true: "alert", false: "on"}[g.Thermal], theme.Heat(g.Temp/100), s.T)+m(" TEMP ")+
				ansi.Paint(fmt.Sprintf("%.0f°C", g.Temp), ansi.Style{Color: theme.Heat(g.Temp / 100), Bold: true})+" "+
				widgets.Meter(g.Temp/100, 6, widgets.MeterOptions{Heated: true})+" "+throttle,
			m("  VRAM ")+widgets.Meter(g.MemUsed/g.MemTotal, 8, widgets.MeterOptions{Color: theme.C["blue"]})+
				d(fmt.Sprintf(" %.1f/%.1fG %.0f%%", g.MemUsed/1024, g.MemTotal/1024, g.Util)),
		)
	} else {
		rows = append(rows, widgets.Led("off", "", s.T)+d(" TEMP n/d desde aquí"), m("  MEM  ")+d(fmt.Sprintf("%d registros", s.Count)))
	}
	rows = append(rows, m("  CTX  ")+widgets.Meter(cv, 8, widgets.MeterOptions{Color: ctxColor(cv)})+
		ansi.Paint(fmt.Sprintf(" %s/%s", k(st.Ctx), kw(nctx)), ansi.Style{Color: ctxColor(cv)})+
		d(fmt.Sprintf(" %.0f%%", cv*100)))
	return widgets.Panel(widgets.PanelOptions{Title: "SISTEMA", W: w, Color: theme.C["primary"], Rows: rows, T: s.T, Live: true})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func neuralPanel(s State, w int) []string {
	iw := w - 4
	showing := s.Busy || s.Memorizing || s.Typing
	nodeState := func(id string) string {
		if id == "memory" && s.Memorizing {
			return "active"
		}
		if s.Busy && s.Pipe.Active == id {
			return "active"
		}
		if showing && contains(s.Pipe.Done, id) {
			return "done"
		}
		return "idle"
	}
	conn := int(math.Max(1, math.Min(8, math.Floor(float64(iw-35)/4))))
	current := ""
	if s.Busy {
		current = s.Pipe.Active
	} else if s.Memorizing {
		current = "memory"
	}
	col := theme.C["dim"]
	if current != "" {
		if c, ok := phaseColor[current]; ok {
			col = c
		}
	}
	energy := 0.14
	switch {
	case s.Compacting:
		energy = 0.8
	case s.Typing:
		energy = 0.55
	case s.Busy:
		switch s.Pipe.Active {
		case "think":
			energy = 1
		case "act":
			energy = 0.65
		default:
			energy = 0.5
		}
	case s.Memorizing:
		energy = 0.4
	}
	head := int(math.Floor(s.T/110)) % (conn + 1)

	var nodes strings.Builder
	for i, p := range pipeOrder {
		id, label := p[0], p[1]
		stt := nodeState(id)
		c := theme.C["dim"]
		switch stt {
		case "active":
			c = phaseColor[id]
		case "done":
			c = theme.C["green"]
		}
		glyph := "○"
		switch stt {
		case "active":
			if int(math.Floor(s.T/150))%2 == 1 {
				glyph = "◉"
			} else {
				glyph = "●"
			}
		case "done":
			glyph = "◉"
		}
		color := c
		if stt == "active" {
			color = theme.Mix(theme.C["dark"], c, 0.55+0.45*theme.Sin01(s.T, 500, 0))
		}
		nodes.WriteString(ansi.Paint(glyph+" "+label, ansi.Style{Color: color, Bold: stt == "active"}))
		if i < len(pipeOrder)-1 {
			next := nodeState(pipeOrder[i+1][0])
			lit := stt == "done" || stt == "active"
			flowing := lit && (next == "active" || stt == "active") && showing
			cc := theme.C["dim"]
			if lit {
				cc = theme.C["green"]
				if flowing {
					cc = theme.C["ink"]
				}
			}
			var b strings.Builder
			for j := 0; j < conn; j++ {
				if flowing && j == head%conn {
					b.WriteString("▸")
				} else if lit {
					b.WriteString("━")
				} else {
					b.WriteString("·")
				}
			}
			nodes.WriteString(ansi.Paint(b.String(), ansi.Style{Color: cc}))
		}
	}

	var status string
	switch {
	case s.Compacting:
		status = widgets.Glitch("⟫ COMPACTANDO CONTEXTO", s.T, 0.12, []string{theme.C["primary"], theme.C["ink"], theme.C["primary"]}, true)
	case current != "":
		status = widgets.Glitch("⟫ "+phaseLabel[current], s.T, 0.1, []string{col, theme.C["ink"], col}, true)
	case s.Typing:
		status = ansi.Paint("⟫ TRANSMITIENDO", ansi.Style{Color: theme.C["secondary"], Bold: true})
	default:
		status = d("⟫ EN ESPERA — escribe una orden")
	}
	meta := ""
	if s.Busy || s.Compacting {
		step := ""
		if s.Pipe.Step > 0 {
			step = fmt.Sprintf("paso %d/%d · ", s.Pipe.Step, s.Pipe.Total)
		}
		meta = d(fmt.Sprintf("%s%.0fs", step, s.Elapsed))
	}
	statusRow := ansi.ClipAnsi(status, max(4, iw-ansi.Vlen(meta)-1))
	cfg := config.Get()
	rows := []string{
		nodes.String(),
		widgets.Equalizer(s.T, iw, energy, nil),
		widgets.Scan(s.T, iw, widgets.ScanOptions{Speed: map[bool]float64{true: 0.06, false: 0.016}[s.Busy], Color: map[bool]string{true: col, false: theme.C["primary"]}[s.Busy]}),
		ansi.PadEnd(statusRow, iw-ansi.Vlen(meta)) + meta,
		d("⌁ temp ") + ansi.Paint(fmt.Sprint(cfg.Temperature), ansi.Style{Color: theme.C["primary"]}) +
			d(" · top-k ") + ansi.Paint(fmt.Sprint(cfg.TopK), ansi.Style{Color: theme.C["primary"]}) +
			d(" · rep ") + ansi.Paint(fmt.Sprintf("%.1f", cfg.RepeatPenalty), ansi.Style{Color: theme.C["primary"]}) +
			func() string {
				if s.Compactions > 0 {
					return ansi.Paint(fmt.Sprintf(" · ⇣%d", s.Compactions), ansi.Style{Color: theme.C["primary"]})
				}
				return ""
			}(),
	}
	panelColor := theme.C["violet"]
	if s.Busy {
		panelColor = col
	}
	return widgets.Panel(widgets.PanelOptions{Title: "ENLACE NEURONAL", W: w, Color: panelColor, Rows: rows, T: s.T, Live: true})
}

func memoryPanel(s State, w int) []string {
	iw := w - 4
	summary := ""
	if s.Stats.Summary > 0 {
		summary = ansi.Paint(fmt.Sprintf("  ⇣ %d", s.Stats.Summary), ansi.Style{Color: theme.C["primary"], Bold: true})
	}
	rows := []string{
		ansi.Paint(fmt.Sprintf("◈ %d", s.Stats.Note), ansi.Style{Color: theme.C["violet"], Bold: true}) +
			d(" notas  ") + ansi.Paint(fmt.Sprintf("◇ %d", s.Stats.Chat), ansi.Style{Color: theme.C["mute"], Bold: true}) +
			d(" turnos") + summary,
	}
	for i := 0; i < 4; i++ {
		if i >= len(s.Hits) {
			txt := ""
			if i == 0 {
				txt = "  sin recuerdos activos"
			}
			rows = append(rows, d(txt))
			continue
		}
		h := s.Hits[i]
		sim := h.Sim
		if sim == 0 {
			sim = 0.4
		}
		n := int(math.Max(1, math.Round(sim*5)))
		lead := strings.Repeat("▰", n) + strings.Repeat("▱", 5-n)
		simTxt := " · "
		if h.Sim != 0 {
			simTxt = fmt.Sprintf(" %3.0f%% ", h.Sim*100)
		}
		title := ansi.ClipAnsi(fmt.Sprintf("#%d %s", h.ID, h.Title), max(4, iw-len([]rune(lead))-len([]rune(simTxt))))
		rows = append(rows, ansi.Paint(lead, ansi.Style{Color: theme.Mix(theme.C["dark"], theme.C["violet"], 0.55+0.45*theme.Sin01(s.T, 1400, float64(i)))})+
			d(simTxt)+ink(title))
	}
	return widgets.Panel(widgets.PanelOptions{Title: "MEMORIA", W: w, Color: theme.C["violet"], Rows: rows, T: s.T, Live: true})
}

func compactDock(s State) []string {
	st := llm.Snapshot()
	cur := "EN ESPERA"
	if s.Busy {
		if l, ok := phaseLabel[s.Pipe.Active]; ok {
			cur = l
		} else {
			cur = "…"
		}
	}
	nctx := winOf(s.Hud)
	g := s.Hud.Gpu
	row1 := " " + widgets.Led(map[bool]string{true: "on", false: "alert"}[s.Health.LLM != nil && *s.Health.LLM], theme.C["green"], s.T) +
		m(" LLM ") + widgets.Led(map[bool]string{true: "on", false: "alert"}[s.Health.Emb != nil && *s.Health.Emb], theme.C["green"], s.T) + m(" EMB ")
	if g != nil {
		row1 += widgets.Led(map[bool]string{true: "alert", false: "on"}[g.Thermal], theme.Heat(g.Temp/100), s.T) +
			ansi.Paint(fmt.Sprintf(" %.0f°C", g.Temp), ansi.Style{Color: theme.Heat(g.Temp / 100)})
	}
	row1 += ansi.Paint(fmt.Sprintf("  ◈ %d", s.Stats.Note), ansi.Style{Color: theme.C["violet"]}) +
		ansi.Paint(fmt.Sprintf("  CTX %s/%s", k(st.Ctx), kw(nctx)), ansi.Style{Color: ctxColor(float64(st.Ctx) / float64(nctx))})
	state := cur
	if s.Compacting {
		state = "COMPACTANDO"
	}
	el := ""
	if s.Busy {
		el = fmt.Sprintf(" %.0fs", s.Elapsed)
	}
	row1 += d("  " + state + el)
	energy := 0.14
	if s.Busy {
		energy = 0.9
	}
	return []string{row1, " " + widgets.Equalizer(s.T, max(10, s.Cols-2), energy, nil)}
}

// ---------- barra de título, entrada y pie ----------

func titleBar(s State) string {
	st := llm.Snapshot()
	left := "░▒▓ C A P R I ▓▒░"
	sub := ""
	if s.Cols >= 100 {
		sub = " // NEURAL BRAIN"
	}
	g := s.Hud.Gpu
	nctx := winOf(s.Hud)
	cv := math.Min(1, float64(st.Ctx)/float64(nctx))
	long := s.Cols >= 90
	gtxt := ""
	if g != nil {
		gtxt = fmt.Sprintf("%.0f°C", g.Temp)
		if g.Thermal {
			gtxt += "!"
		}
	}
	sep := " "
	if long {
		sep = " · "
	}
	gcol := theme.C["mute"]
	if g != nil {
		if g.Thermal {
			if int(math.Floor(s.T/300))%2 == 1 {
				gcol = theme.C["red"]
			} else {
				gcol = theme.C["orange"]
			}
		} else {
			gcol = theme.Heat(g.Temp / 100)
		}
	}
	right := ""
	if g != nil {
		label := ""
		if long {
			label = m("GPU ")
		}
		right += label + ansi.Paint(gtxt, ansi.Style{Color: gcol, Bold: true}) + d(sep)
	}
	ctxLabel := ""
	if long {
		ctxLabel = m("CTX ")
	}
	right += ctxLabel + ansi.Paint(fmt.Sprintf("%s/%s", k(st.Ctx), kw(nctx)), ansi.Style{Color: ctxColor(cv), Bold: true}) + d(sep) + m(clock())
	scanW := s.Cols - len([]rune(left)) - len([]rune(sub)) - ansi.Vlen(right) - 4
	mid := " "
	if scanW >= 4 {
		mid = " " + widgets.Scan(s.T, scanW, widgets.ScanOptions{Speed: map[bool]float64{true: 0.05, false: 0.014}[s.Busy], Color: theme.C["secondary"]}) + " "
	}
	level := 0.035
	if s.Busy {
		level = 0.1
	}
	return ansi.FitRow(" "+widgets.Glitch(left, s.T, level, theme.NEON, true)+d(sub)+mid+right+" ", s.Cols)
}

func inputBox(s State) []string {
	w := s.Cols
	iw := w - 4
	flash := ""
	if s.Pending != nil {
		if int(math.Floor(s.T/350))%2 == 1 {
			flash = theme.C["red"]
		} else {
			flash = theme.C["orange"]
		}
	}
	var content string
	switch {
	case s.Pending != nil:
		content = ansi.Paint(" ⚠ esperando tu autorización…", ansi.Style{Color: theme.C["orange"], Bold: true})
	case s.Busy:
		n := 1 + (int(math.Floor(s.T/160)) % 4)
		content = ansi.Paint(" "+ansi.PadEnd(strings.Repeat("▸", n), 4), ansi.Style{Color: theme.C["secondary"], Bold: true}) +
			d(fmt.Sprintf(" procesando%s  ·  Esc cancela", func() string {
				if s.Pipe.Step > 0 {
					return fmt.Sprintf(" · paso %d", s.Pipe.Step)
				}
				return ""
			}()))
	case s.Locked:
		content = d(" …")
	default:
		avail := max(4, iw-3)
		start := max(0, s.Cur-(avail-1))
		val := []rune(s.Value)
		if start > len(val) {
			start = len(val)
		}
		end := start + avail
		if end > len(val) {
			end = len(val)
		}
		view := val[start:end]
		c := s.Cur - start
		blink := int(math.Floor(s.T/530))%2 == 0
		pre := ansi.Paint(" ❯ ", ansi.Style{Color: theme.Cycle([]string{theme.C["primary"], theme.C["secondary"]}, 0, s.T, 0.0004), Bold: true})
		if len(val) == 0 {
			content = pre + ansi.Paint(" ", ansi.Style{Inverse: blink}) + d("escribe una orden o un dato…  /help")
		} else {
			if c > len(view) {
				c = len(view)
			}
			leftPart := string(view[:c])
			curCh := " "
			if c < len(view) {
				curCh = string(view[c])
			}
			rightPart := ""
			if c+1 < len(view) {
				rightPart = string(view[c+1:])
			}
			content = pre + ink(leftPart) + ansi.Paint(curCh, ansi.Style{Inverse: blink, Color: theme.C["primary"]}) + ink(rightPart)
		}
	}
	stops := theme.NEON
	speed := 0.02
	trail := 16
	if s.Busy {
		stops = []string{theme.C["secondary"], theme.C["orange"], theme.C["yellow"]}
		speed = 0.06
		trail = 26
	}
	return widgets.NeonBox(widgets.NeonBoxOptions{W: w, Rows: []string{content}, T: s.T, Speed: speed, Trail: trail, Stops: stops, Flash: flash})
}

func footer(s State, off int) string {
	cfg := config.Get()
	iso := cfg.Isolated
	tip := TIPS[int(math.Floor(s.T/7000))%len(TIPS)]
	right := ""
	if off > 0 {
		right = ansi.Paint(fmt.Sprintf(" ▲ %d líneas arriba · AvPág", off), ansi.Style{Color: theme.C["yellow"], Bold: true})
	}
	isoState := "alert"
	isoColor := theme.C["orange"]
	isoTxt := "HOST"
	if iso {
		isoState = "on"
		isoColor = theme.C["green"]
		isoTxt = "SANDBOX"
	}
	autoTxt := "◆ ASK"
	autoColor := theme.C["primary"]
	if s.Auto {
		autoTxt = "↯ AUTO"
		autoColor = theme.C["yellow"]
	}
	left := " " + widgets.Led(isoState, isoColor, s.T) + ansi.Paint(" "+isoTxt, ansi.Style{Color: isoColor, Bold: true}) +
		d("  ·  ") + ansi.Paint(autoTxt, ansi.Style{Color: autoColor, Bold: true}) + d("   "+tip)
	width := s.Cols - ansi.Vlen(right) - 1
	return ansi.FitRow(ansi.PadEnd(ansi.ClipAnsi(left, width), width)+right+" ", s.Cols)
}

// ---------- overlays ----------

var dangerRe = regexp.MustCompile(`SOBRESCRIBIR|OLVIDAR|ELIMINAR|anterior falló`)

func confirmRows(s State) []string {
	w := s.Cols
	danger := dangerRe.MatchString(s.Pending.What)
	base := theme.C["orange"]
	if danger {
		base = theme.C["red"]
	}
	edge := base
	if int(math.Floor(s.T/400))%2 == 1 {
		edge = theme.Mix(theme.C["dark"], base, 0.55)
	}
	wall := ansi.Paint("║", ansi.Style{Color: edge})
	line := func(txt string) string {
		return wall + " " + ansi.PadEnd(ansi.ClipAnsi(txt, w-4), w-4) + " " + wall
	}
	tail := d("(esta siempre pregunta)")
	if !danger {
		tail = widgets.Cap("A", theme.C["yellow"]) + m(" sí a todo (modo auto)")
	}
	return []string{
		ansi.Paint("╔"+strings.Repeat("═", max(0, w-2))+"╗", ansi.Style{Color: edge}),
		line(widgets.Glitch("⚠ AUTORIZACIÓN REQUERIDA  ░▒▓  "+s.Pending.Name, s.T, 0.08, []string{base, theme.C["yellow"], base}, true)),
		line(ink(s.Pending.What)),
		line(widgets.Cap("Y", theme.C["green"]) + m(" sí   ") + widgets.Cap("N", theme.C["red"]) + m(" no   ") + tail),
		ansi.Paint("╚"+strings.Repeat("═", max(0, w-2))+"╝", ansi.Style{Color: edge}),
	}
}

func browserRows(s State, vis int) []string {
	w := s.Cols
	b := s.Browser
	start := int(math.Max(0, math.Min(float64(len(b.Rows)-vis), float64(b.Sel-vis/2))))
	sel := BrowserRow{}
	if b.Sel >= 0 && b.Sel < len(b.Rows) {
		sel = b.Rows[b.Sel]
	}
	iw := w - 4
	rows := []string{d(fmt.Sprintf("↑↓ mover  ·  d olvidar  ·  Esc cerrar  ·  %d registros", len(b.Rows)))}
	for i := 0; i < vis; i++ {
		idx := start + i
		if idx >= len(b.Rows) {
			rows = append(rows, "")
			continue
		}
		r := b.Rows[idx]
		on := idx == b.Sel
		icon := "◇"
		switch r.Kind {
		case "note":
			icon = "◈"
		case "summary":
			icon = "⇣"
		}
		cursor := " "
		if on {
			cursor = "▸"
		}
		date := time.UnixMilli(r.Updated).UTC().Format("2006-01-02")
		txt := fmt.Sprintf("%s %s #%d  %s  %s", cursor, icon, r.ID, date, r.Title)
		color := theme.C["mute"]
		if on {
			color = theme.C["primary"]
		}
		rows = append(rows, ansi.Paint(ansi.ClipAnsi(txt, iw), ansi.Style{Color: color, Bold: on}))
	}
	content := "  el cerebro está vacío"
	if sel.ID != 0 {
		content = "  " + strings.TrimSpace(wsRe.ReplaceAllString(sel.Content, " "))
	}
	rows = append(rows, d(content))
	if b.Confirm != 0 {
		rows = append(rows, ansi.Paint(fmt.Sprintf("¿olvidar #%d?  ", b.Confirm), ansi.Style{Color: theme.C["red"], Bold: true})+
			widgets.Cap("Y", theme.C["green"])+" sí  "+widgets.Cap("N", theme.C["red"])+" no  "+d("(se puede deshacer con /undo)"))
	}
	return widgets.Panel(widgets.PanelOptions{Title: "EXPLORADOR DE MEMORIA", W: w, Color: theme.C["violet"], Rows: rows, T: s.T, Live: true})
}

// ---------- arranque ----------

// BootMs es la duración del arranque.
const BootMs = 3000

var trailingDots = regexp.MustCompile(`(·+)$`)

func bootRows(s State, R int) []string {
	t := s.Boot.T
	g := s.Hud.Gpu
	lvl := math.Max(0, 0.55-t/2400)
	type val struct{ txt, c string }
	st := func(v *bool) val {
		if v == nil {
			return val{"…", theme.C["dim"]}
		}
		if *v {
			return val{"ONLINE", theme.C["green"]}
		}
		return val{"OFFLINE", theme.C["red"]}
	}
	gpuVal := val{"N/D", theme.C["dim"]}
	if g != nil {
		if g.Thermal {
			gpuVal = val{"THROTTLE", theme.C["red"]}
		} else {
			gpuVal = val{"OK", theme.C["green"]}
		}
	}
	envVal := val{"HOST", theme.C["orange"]}
	if config.Get().Isolated {
		envVal = val{"SANDBOX", theme.C["green"]}
	}
	model := s.Model
	if model == "" {
		model = "…"
	}
	gpuTxt := "gpu"
	if g != nil {
		gpuTxt = fmt.Sprintf("gpu %.0f°C %.0f/%.0fMHz", g.Temp, g.Sm, g.SmMax)
	}
	linesList := [][2]val{
		{{"enlace neuronal", theme.C["ink"]}, {"OK", theme.C["green"]}},
		{{"modelo " + model, theme.C["ink"]}, st(s.Health.LLM)},
		{{"embeddings " + embedLabel(), theme.C["ink"]}, st(s.Health.Emb)},
		{{fmt.Sprintf("memoria ◈ %d notas · ◇ %d turnos", s.Stats.Note, s.Stats.Chat), theme.C["ink"]}, {"OK", theme.C["green"]}},
		{{gpuTxt, theme.C["ink"]}, gpuVal},
		{{"entorno", theme.C["ink"]}, envVal},
	}
	var block []string
	for r, l := range lines.Logo {
		if t < float64(r)*150 {
			block = append(block, "")
		} else {
			block = append(block, widgets.Glitch(l, t, lvl, nil, false))
		}
	}
	block = append(block, "")
	for i, pair := range linesList {
		if t < 600+float64(i)*330 {
			block = append(block, "")
			continue
		}
		label := widgets.Fit(pair[0].txt, 44)
		label = strings.ReplaceAll(label, " ", "·")
		label = trailingDots.ReplaceAllString(label, " $1")
		block = append(block, ansi.Paint("▸ ", ansi.Style{Color: theme.C["primary"]})+ink(label)+" "+
			ansi.Paint(pair[1].txt, ansi.Style{Color: pair[1].c, Bold: true}))
	}
	block = append(block, "")
	pct := math.Min(100, math.Round((t/BootMs)*100))
	block = append(block, ansi.Paint("INICIANDO ", ansi.Style{Color: theme.C["secondary"], Bold: true})+
		widgets.Meter(math.Min(1, t/BootMs), 30, widgets.MeterOptions{Color: theme.C["secondary"]})+
		d(fmt.Sprintf(" %.0f%%", pct)))
	bw := 0
	for _, v := range block {
		if n := ansi.Vlen(v); n > bw {
			bw = n
		}
	}
	left := int(math.Max(0, math.Floor(float64(s.Cols-bw)/2)))
	top := int(math.Max(0, math.Floor(float64(R-len(block))/2)))
	out := make([]string, 0, R)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, r := range block {
		out = append(out, strings.Repeat(" ", left)+r)
	}
	for len(out) < R {
		out = append(out, "")
	}
	if len(out) > R {
		out = out[:R]
	}
	for i := range out {
		out[i] = ansi.FitRow(out[i], s.Cols)
	}
	return out
}

// ---------- visor de chat ----------

var wsRe = regexp.MustCompile(`\s+`)

func viewportRows(ln [][]lines.Seg, H, cols, offset int) ([]string, int, int) {
	total := len(ln)
	off := int(math.Min(float64(offset), math.Max(0, float64(total-H))))
	start := int(math.Max(0, float64(total-H-off)))
	vis := [][]lines.Seg{}
	if start < total {
		end := start + H
		if end > total {
			end = total
		}
		vis = ln[start:end]
	}
	thumb := 0
	if total > H {
		thumb = int(math.Max(1, math.Round(float64(H*H)/float64(total))))
	}
	top := 0
	if total > H {
		top = int(math.Round((float64(total-H-off) / float64(total-H)) * float64(H-thumb)))
	}
	rows := make([]string, H)
	for i := 0; i < H; i++ {
		var line []lines.Seg
		if i < len(vis) {
			line = vis[i]
		}
		inThumb := thumb > 0 && i >= top && i < top+thumb
		var body strings.Builder
		for _, sg := range line {
			body.WriteString(ansi.Paint(sg.Text, ansi.Style{Color: sg.Color, Bold: sg.Bold, Italic: sg.Italic}))
		}
		bar := " "
		if total > H {
			if inThumb {
				bar = ansi.Paint("█", ansi.Style{Color: theme.C["primary"]})
			} else {
				bar = ansi.Paint("│", ansi.Style{Color: theme.C["dim"]})
			}
		}
		rows[i] = ansi.PadEnd(ansi.ClipAnsi(body.String(), cols-1), cols-1) + bar
	}
	return rows, off, total
}

// DOCKH son las alturas fijas del dock (filas) según el modo.
var DOCKH = map[string]int{"full": 7, "compact": 2, "none": 0}

// DockModeOf decide el modo del dock según el tamaño.
func DockModeOf(cols, R int) string {
	if cols >= 100 && R >= 30 {
		return "full"
	}
	if R >= 20 {
		return "compact"
	}
	return "none"
}

// BuildFrame convierte el estado en un fotograma exacto.
func BuildFrame(s State) Frame {
	cols := s.Cols
	R := max(10, s.Rows-1)
	if s.Boot != nil {
		return Frame{Rows: bootRows(s, R), H: 0, Total: 0, Off: 0}
	}
	mode := DockModeOf(cols, R)
	dockH := DOCKH[mode]
	confirmH := 0
	if s.Pending != nil {
		confirmH = 5
	}
	maxVis := max(3, R-1-dockH-4-8)
	vis := 0
	if s.Browser != nil {
		vis = int(math.Min(math.Min(math.Max(float64(len(s.Browser.Rows)), 1), 12), float64(maxVis)))
	}
	browserH := 0
	if s.Browser != nil {
		browserH = vis + 4
		if s.Browser.Confirm != 0 {
			browserH++
		}
	}
	H := max(3, R-(1+dockH+3+1)-confirmH-browserH)

	chatRows, off, total := viewportRows(s.Lines, H, cols, s.Offset)
	var dock []string
	switch mode {
	case "full":
		if cols >= 118 {
			dock = ansi.HStack([]ansi.Column{
				{W: 32, Rows: systemPanel(s, 32)},
				{W: cols - 66, Rows: neuralPanel(s, cols-66)},
				{W: 34, Rows: memoryPanel(s, 34)},
			})
		} else {
			dock = ansi.HStack([]ansi.Column{
				{W: 34, Rows: systemPanel(s, 34)},
				{W: cols - 34, Rows: neuralPanel(s, cols-34)},
			})
		}
	case "compact":
		dock = compactDock(s)
	}

	rows := []string{titleBar(s)}
	rows = append(rows, chatRows...)
	if s.Browser != nil {
		rows = append(rows, browserRows(s, vis)...)
	}
	if s.Pending != nil {
		rows = append(rows, confirmRows(s)...)
	}
	rows = append(rows, dock...)
	rows = append(rows, inputBox(s)...)
	rows = append(rows, footer(s, off))
	for len(rows) < R {
		rows = append(rows, "")
	}
	if len(rows) > R {
		rows = rows[:R]
	}
	for i := range rows {
		rows[i] = ansi.FitRow(rows[i], cols)
	}
	return Frame{Rows: rows, H: H, Total: total, Off: off}
}

// embedLabel es el nombre corto del modelo de embeddings configurado.
func embedLabel() string {
	n := config.Get().EmbedModel
	if r := []rune(n); len(r) > 22 {
		n = string(r[:22])
	}
	return n
}
