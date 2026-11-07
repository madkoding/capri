// Package widgets contiene widgets como funciones puras de (estado, t) → cadenas ANSI.
package widgets

import (
	"math"
	"strings"

	"capri/internal/ui/ansi"
	"capri/internal/ui/theme"
)

func rlen(s string) int { return len([]rune(s)) }

// Fit recorta o rellena a `w` runas (con «…» si se pasa).
func Fit(s string, w int) string {
	a := []rune(s)
	if len(a) > w {
		if w <= 1 {
			return "…"
		}
		return string(a[:w-1]) + "…"
	}
	return string(a) + strings.Repeat(" ", w-len(a))
}

// Neon pinta texto con un degradado neón que fluye.
func Neon(text string, t float64, stops []string, speed float64, bold bool) string {
	if stops == nil {
		stops = theme.NEON
	}
	a := []rune(text)
	cells := make([]ansi.Cell, len(a))
	for i, ch := range a {
		x := 0.0
		if len(a) > 1 {
			x = float64(i) / float64(len(a)-1)
		}
		cells[i] = ansi.Cell{Ch: string(ch), Bold: bold, Color: theme.Cycle(stops, x, t, speed)}
	}
	return ansi.Runs(cells)
}

// Glitch pinta texto con corrupción ocasional.
func Glitch(text string, t float64, level float64, stops []string, bold bool) string {
	if stops == nil {
		stops = theme.NEON
	}
	frame := int64(math.Floor(t / 90))
	a := []rune(text)
	gl := []rune(theme.GLITCH)
	cells := make([]ansi.Cell, len(a))
	for i, ch := range a {
		bad := ch != ' ' && theme.Rnd(frame, int64(i*7+3)) < level
		out := ch
		color := theme.Cycle(stops, ratio(i, len(a)), t, 0.00018)
		if bad {
			idx := int(math.Floor(theme.Rnd(frame+1, int64(i)) * float64(len(gl))))
			if idx >= len(gl) {
				idx = len(gl) - 1
			}
			out = gl[idx]
			if theme.Rnd(frame, int64(i)) < 0.5 {
				color = theme.C["primary"]
			} else {
				color = theme.C["secondary"]
			}
		}
		cells[i] = ansi.Cell{Ch: string(out), Bold: bold, Color: color}
	}
	return ansi.Runs(cells)
}

func ratio(i, n int) float64 {
	if n > 1 {
		return float64(i) / float64(n-1)
	}
	return 0
}

// Led dibuja un LED: on = respira, busy = parpadea rápido, alert = parpadea rojo, off = apagado.
func Led(state string, color string, t float64) string {
	if color == "" {
		color = theme.C["green"]
	}
	switch state {
	case "", "on":
		return ansi.Paint("●", ansi.Style{Color: theme.Glow(color, t, 2200, 0.45)})
	case "off":
		return ansi.Paint("○", ansi.Style{Color: theme.C["dim"]})
	case "busy":
		on := int(math.Floor(t/130))%2 == 1
		ch := "◉"
		c := theme.Mix(theme.C["dark"], color, 0.35)
		if on {
			ch = "●"
			c = color
		}
		return ansi.Paint(ch, ansi.Style{Color: c})
	case "alert":
		on := int(math.Floor(t/280))%2 == 1
		if on {
			return ansi.Paint("●", ansi.Style{Color: theme.C["red"], Bold: true})
		}
		return ansi.Paint("○", ansi.Style{Color: theme.C["orange"], Bold: true})
	}
	return ansi.Paint("●", ansi.Style{Color: theme.Glow(color, t, 2200, 0.45)})
}

// MeterOptions controla el medidor.
type MeterOptions struct {
	Heated bool
	Color  string
}

// Meter dibuja un medidor segmentado ▰▰▰▱▱.
func Meter(v float64, w int, o MeterOptions) string {
	if o.Color == "" {
		o.Color = theme.C["primary"]
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	n := int(math.Round(v * float64(w)))
	cells := make([]ansi.Cell, w)
	for i := 0; i < w; i++ {
		if i < n {
			color := o.Color
			if o.Heated {
				color = theme.Heat(float64(i) / math.Max(1, float64(w-1)))
			}
			cells[i] = ansi.Cell{Ch: "▰", Color: color}
		} else {
			cells[i] = ansi.Cell{Ch: "▱", Color: theme.C["dim"]}
		}
	}
	return ansi.Runs(cells)
}

// Equalizer dibuja un ecualizador animado.
func Equalizer(t float64, w int, energy float64, stops []string) string {
	if stops == nil {
		stops = theme.NEON
	}
	levels := theme.LEVELS
	cells := make([]ansi.Cell, w)
	for i := 0; i < w; i++ {
		wave := 0.55*theme.Sin01(t, float64(420+(i%7)*63), float64(i)*1.3) +
			0.45*theme.Sin01(t, float64(970+(i%4)*110), float64(i)*0.7)
		extra := 0.0
		if energy >= 0.3 {
			extra = 0.1
		}
		lvl := int(math.Round((0.08 + 0.92*wave*energy + extra) * 7))
		if lvl < 0 {
			lvl = 0
		}
		if lvl > 7 {
			lvl = 7
		}
		cells[i] = ansi.Cell{Ch: levels[lvl], Color: theme.Cycle(stops, float64(i)/math.Max(1, float64(w-1)), t, 0.00025)}
	}
	return ansi.Runs(cells)
}

// ScanOptions controla la línea con cometa.
type ScanOptions struct {
	Speed float64
	Color string
	Base  string
	Ch    string
}

// Scan dibuja una línea con un cometa que va y viene (efecto Knight Rider).
func Scan(t float64, w int, o ScanOptions) string {
	if o.Speed == 0 {
		o.Speed = 0.03
	}
	if o.Color == "" {
		o.Color = theme.C["primary"]
	}
	if o.Base == "" {
		o.Base = theme.C["dim"]
	}
	if o.Ch == "" {
		o.Ch = "─"
	}
	span := w*2 - 2
	if span < 1 {
		span = 1
	}
	p := int(math.Floor(t*o.Speed)) % span
	head := p
	dir := 1
	if p >= w {
		head = span - p
		dir = -1
	}
	trail := []string{"▓", "▒", "░", "░"}
	cells := make([]ansi.Cell, w)
	for i := 0; i < w; i++ {
		d := (head - i) * dir
		switch {
		case d == 0:
			cells[i] = ansi.Cell{Ch: "█", Color: theme.C["ink"]}
		case d > 0 && d < 5:
			cells[i] = ansi.Cell{Ch: trail[d-1], Color: theme.Mix(o.Base, o.Color, 1-float64(d)/5)}
		default:
			cells[i] = ansi.Cell{Ch: o.Ch, Color: o.Base}
		}
	}
	return ansi.Runs(cells)
}

// Cap dibuja una tecla "keycap": [ Y ].
func Cap(k, color string) string {
	if color == "" {
		color = theme.C["primary"]
	}
	return ansi.Paint("[", ansi.Style{Color: color, Bold: true}) +
		ansi.Paint(" "+k+" ", ansi.Style{Color: theme.C["ink"]}) +
		ansi.Paint("]", ansi.Style{Color: color, Bold: true})
}

// PanelOptions describe un panel con título.
type PanelOptions struct {
	Title string
	W     int
	Color string
	Rows  []string
	T     float64
	Live  bool
}

// Panel dibuja un panel con título dentro del borde superior.
func Panel(o PanelOptions) []string {
	if o.Color == "" {
		o.Color = theme.C["violet"]
	}
	edge := o.Color
	if o.Live {
		edge = theme.Glow(o.Color, o.T, 2600, 0.55)
	}
	label := " " + o.Title + " "
	iw := o.W - 4
	top := ansi.Paint("╭─", ansi.Style{Color: edge}) +
		ansi.Paint(label, ansi.Style{Bold: true, Color: theme.C["ink"]}) +
		ansi.Paint(strings.Repeat("─", max(0, o.W-3-rlen(label)))+"╮", ansi.Style{Color: edge})
	out := []string{top}
	for _, r := range o.Rows {
		out = append(out, ansi.Paint("│ ", ansi.Style{Color: edge})+ansi.FitRow(r, iw)+ansi.Paint(" │", ansi.Style{Color: edge}))
	}
	out = append(out, ansi.Paint("╰"+strings.Repeat("─", max(0, o.W-2))+"╯", ansi.Style{Color: edge}))
	return out
}

// NeonBoxOptions describe una caja neón.
type NeonBoxOptions struct {
	W     int
	Rows  []string
	T     float64
	Speed float64
	Trail int
	Stops []string
	Base  string
	Flash string
}

// NeonBox dibuja una caja neón: una luz recorre el perímetro.
func NeonBox(o NeonBoxOptions) []string {
	if o.Speed == 0 {
		o.Speed = 0.022
	}
	if o.Trail == 0 {
		o.Trail = 16
	}
	if o.Stops == nil {
		o.Stops = theme.NEON
	}
	if o.Base == "" {
		o.Base = theme.C["dim"]
	}
	h := len(o.Rows) + 2
	p := 2*(o.W-1) + 2*(h-1)
	head := 0
	if p > 0 {
		head = int(math.Floor(o.T*o.Speed)) % p
	}
	col := func(pos int) string {
		if o.Flash != "" {
			return o.Flash
		}
		d := (head - pos + p) % p
		if d < 0 {
			d += p
		}
		if d >= o.Trail {
			return o.Base
		}
		return theme.Mix(o.Base, theme.Cycle(o.Stops, 1-float64(d)/float64(o.Trail), o.T, 0.0001), 1-(float64(d)/float64(o.Trail))*0.55)
	}
	topPos := func(i int) int { return i }
	rightPos := func(r int) int { return o.W - 1 + r }
	botPos := func(i int) int { return o.W - 1 + (h - 1) + (o.W - 1 - i) }
	leftPos := func(r int) int { return 2*(o.W-1) + (h - 1) + (h - 1 - r) }
	edge := func(l, mid, r string, pos func(int) int) string {
		cells := make([]ansi.Cell, 0, o.W)
		cells = append(cells, ansi.Cell{Ch: l, Color: col(pos(0))})
		for i := 0; i < o.W-2; i++ {
			cells = append(cells, ansi.Cell{Ch: mid, Color: col(pos(i + 1))})
		}
		cells = append(cells, ansi.Cell{Ch: r, Color: col(pos(o.W - 1))})
		return ansi.Runs(cells)
	}
	out := []string{edge("╭", "─", "╮", topPos)}
	for r, row := range o.Rows {
		out = append(out, ansi.Paint("│", ansi.Style{Color: col(leftPos(r + 1))})+
			ansi.FitRow(row, o.W-2)+
			ansi.Paint("│", ansi.Style{Color: col(rightPos(r + 1))}))
	}
	out = append(out, edge("╰", "─", "╯", botPos))
	return out
}
