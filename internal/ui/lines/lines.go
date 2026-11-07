// Package lines es el motor de líneas del visor de chat: cada elemento del historial se convierte en líneas
// de segmentos con estilo ya ajustadas al ancho.
package lines

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"capri/internal/ui/theme"
)

// Seg es un segmento de texto con estilo.
type Seg struct {
	Text   string
	Color  string
	Bold   bool
	Italic bool
}

// SegN crea un segmento con color.
func SegN(text, color string) Seg { return Seg{Text: text, Color: color} }

// Logo es el logotipo ASCII de Capri.
var Logo = []string{
	" ██████╗ █████╗ ██████╗ ██████╗ ██╗",
	"██╔════╝██╔══██╗██╔══██╗██╔══██╗██║",
	"██║     ███████║██████╔╝██████╔╝██║",
	"██║     ██╔══██║██╔═══╝ ██╔══██╗██║",
	"╚██████╗██║  ██║██║     ██║  ██║██║",
	" ╚═════╝╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝╚═╝",
}

func plainLen(segs []Seg) int {
	n := 0
	for _, s := range segs {
		n += len([]rune(s.Text))
	}
	return n
}

// WrapSegs ajusta una línea de segmentos al ancho (corta por palabras; por caracteres si una palabra no cabe).
func WrapSegs(segs []Seg, width int) [][]Seg {
	w := width
	if w < 4 {
		w = 4
	}
	type ch struct {
		c  rune
		si int
	}
	var cs []ch
	for si, s := range segs {
		for _, c := range s.Text {
			cs = append(cs, ch{c, si})
		}
	}
	pack := func(arr []ch) []Seg {
		var line []Seg
		lastSi := -1
		for _, c := range arr {
			if len(line) > 0 && lastSi == c.si {
				line[len(line)-1].Text += string(c.c)
			} else {
				sg := segs[c.si]
				sg.Text = string(c.c)
				line = append(line, sg)
				lastSi = c.si
			}
		}
		return line
	}
	var out [][]Seg
	var cur []ch
	for _, c := range cs {
		if c.c == '\n' {
			out = append(out, pack(cur))
			cur = nil
			continue
		}
		cur = append(cur, c)
		if len(cur) > w {
			sp := -1
			for i := len(cur) - 1; i > 0; i-- {
				if cur[i].c == ' ' {
					sp = i
					break
				}
			}
			if sp > 0 {
				out = append(out, pack(cur[:sp]))
				cur = append([]ch{}, cur[sp+1:]...)
			} else {
				out = append(out, pack(cur[:w]))
				cur = append([]ch{}, cur[w:]...)
			}
		}
	}
	out = append(out, pack(cur))
	return out
}

// Clip recorta una línea a `width` con «…».
func Clip(segs []Seg, width int) []Seg {
	if plainLen(segs) <= width {
		return segs
	}
	var out []Seg
	left := width - 1
	if left < 1 {
		left = 1
	}
	for _, s := range segs {
		chars := []rune(s.Text)
		if len(chars) >= left {
			s.Text = string(chars[:left])
			out = append(out, s)
			break
		}
		out = append(out, s)
		left -= len(chars)
	}
	out = append(out, Seg{Text: "…", Color: theme.C["dim"]})
	return out
}

var inlineRe = regexp.MustCompile("(`[^`]+`|\\*\\*[^*]+\\*\\*)")

func inlineSegs(text, base string) []Seg {
	var out []Seg
	plain := func(t string) {
		if t != "" {
			out = append(out, Seg{Text: t, Color: base})
		}
	}
	last := 0
	// FindAllStringIndex y no Split: Split descarta lo que coincide (JS lo conservaba con el grupo de captura).
	for _, m := range inlineRe.FindAllStringIndex(text, -1) {
		plain(text[last:m[0]])
		tok := text[m[0]:m[1]]
		if strings.HasPrefix(tok, "`") {
			out = append(out, Seg{Text: tok[1 : len(tok)-1], Color: theme.C["yellow"]})
		} else {
			out = append(out, Seg{Text: tok[2 : len(tok)-2], Color: theme.C["primary"], Bold: true})
		}
		last = m[1]
	}
	plain(text[last:])
	return out
}

var (
	headingRe = regexp.MustCompile(`^#{1,3}\s`)
	bulletRe  = regexp.MustCompile(`^\s*[-*]\s`)
)

// MarkdownLines convierte Markdown mínimo en líneas lógicas de segmentos (sin ajustar).
func MarkdownLines(text string) [][]Seg {
	code := false
	var out [][]Seg
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			code = !code
			label := strings.TrimSpace(strings.TrimSpace(l)[3:])
			if label == "" {
				label = "código"
			}
			body := "└──────────"
			if code {
				body = "┌─ " + label + " ─────"
			}
			out = append(out, []Seg{{Text: body, Color: theme.C["dim"]}})
			continue
		}
		if code {
			out = append(out, []Seg{{Text: "│ " + l, Color: theme.C["green"]}})
			continue
		}
		if headingRe.MatchString(l) {
			out = append(out, []Seg{{Text: "▌" + headingRe.ReplaceAllString(l, ""), Color: theme.C["secondary"], Bold: true}})
			continue
		}
		if bulletRe.MatchString(l) {
			rest := bulletRe.ReplaceAllString(l, "")
			out = append(out, append([]Seg{{Text: "  ▸ ", Color: theme.C["primary"]}}, inlineSegs(rest, theme.C["ink"])...))
			continue
		}
		out = append(out, inlineSegs(l, theme.C["ink"]))
	}
	return out
}

func gradientSegs(text string, stops []string, bold bool) []Seg {
	a := []rune(text)
	out := make([]Seg, len(a))
	for i, ch := range a {
		x := 0.0
		if len(a) > 1 {
			x = float64(i) / float64(len(a)-1)
		}
		out[i] = Seg{Text: string(ch), Color: theme.GradientAt(stops, x), Bold: bold}
	}
	return out
}

func barred(logical [][]Seg, color string, width int) [][]Seg {
	var rows [][]Seg
	for _, segs := range logical {
		for _, l := range WrapSegs(segs, width-4) {
			rows = append(rows, l)
		}
	}
	out := make([][]Seg, len(rows))
	for i, l := range rows {
		prefix := "  ┃ "
		if i == len(rows)-1 {
			prefix = "  ┗ "
		}
		out[i] = append([]Seg{{Text: prefix, Color: color}}, l...)
	}
	return out
}

const act = "      "

func k(n int) string {
	if n >= 1000 {
		if n >= 10000 {
			return strconv.Itoa(int(roundHalf(float64(n)/1000))) + "k"
		}
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	}
	return strconv.Itoa(int(roundHalf(float64(n))))
}

func roundHalf(f float64) float64 {
	if f < 0 {
		return -roundHalf(-f)
	}
	return float64(int(f + 0.5))
}

var wsRe = regexp.MustCompile(`\s+`)

func flatResult(txt string) string {
	t := strings.TrimSpace(wsRe.ReplaceAllString(txt, " "))
	r := []rune(t)
	if len(r) > 200 {
		return string(r[:170]) + "…" + string(r[len(r)-25:])
	}
	return t
}

func argsText(a map[string]string) string {
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var parts []string
	for _, k := range keys {
		v := strings.ReplaceAll(wsRe.ReplaceAllString(a[k], " "), "\n", " ")
		vr := []rune(v)
		if len(vr) > 48 {
			vr = vr[:48]
		}
		parts = append(parts, k+"="+strconv.Quote(string(vr)))
	}
	return strings.Join(parts, " ")
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// Item es un elemento del historial de chat.
type Item struct {
	ID     int64
	Kind   string
	Text   string
	At     string
	Meta   string
	Name   string
	Args   map[string]string
	OK     bool
	Result string

	// compactación
	Scope   string
	Reason  string
	Before  int
	After   int
	Turns   int
	Dropped bool
	Summary string
	RagID   *int64
	LLM     *bool
}

// ItemLines convierte un elemento del historial en líneas ajustadas a W columnas.
func ItemLines(it Item, W int) [][]Seg {
	switch it.Kind {
	case "banner":
		return [][]Seg{
			{{Text: "  // ", Color: theme.C["secondary"], Bold: true}, {Text: "NEURAL BRAIN", Color: theme.C["primary"]}, {Text: "  ░▒▓  RAG local · llama.cpp · embeddings · sqlite", Color: theme.C["dim"]}},
			{{Text: "  /help para comandos · el contexto vive en el RAG, no en el prompt", Color: theme.C["dim"]}},
			{},
		}
	case "user":
		out := [][]Seg{{}, {{Text: "  ┏ ", Color: theme.C["primary"]}, {Text: "TÚ", Color: theme.C["primary"], Bold: true}, {Text: "  " + it.At, Color: theme.C["dim"]}}}
		out = append(out, barred([][]Seg{{{Text: it.Text, Color: theme.C["ink"]}}}, theme.C["primary"], W)...)
		out = append(out, []Seg{})
		return out
	case "assistant":
		head := []Seg{{Text: "  ┏ ", Color: theme.C["secondary"]}}
		head = append(head, gradientSegs("CAPRI", []string{theme.C["secondary"], theme.C["violet"]}, true)...)
		meta := "  " + it.At
		if it.Meta != "" {
			meta += " · " + it.Meta
		}
		head = append(head, Seg{Text: meta, Color: theme.C["dim"]})
		out := [][]Seg{{}, head}
		out = append(out, barred(MarkdownLines(it.Text), theme.C["secondary"], W)...)
		out = append(out, []Seg{})
		return out
	case "tool":
		head := Clip([]Seg{
			{Text: act + "⚙ ", Color: theme.C["yellow"]},
			{Text: it.Name, Color: theme.C["primary"]},
			{Text: " " + argsText(it.Args), Color: theme.C["dim"]},
		}, W)
		color := theme.C["green"]
		mark := "✔"
		if !it.OK {
			color = theme.C["red"]
			mark = "✗"
		}
		var out [][]Seg
		out = append(out, head)
		rows := WrapSegs([]Seg{{Text: mark + " " + flatResult(it.Result), Color: color}}, W-len(act)-4)
		for i, row := range rows {
			prefix := act + "  ↳ "
			if i > 0 {
				prefix = act + "    "
			}
			line := []Seg{{Text: prefix, Color: theme.C["dim"]}}
			line = append(line, row...)
			out = append(out, line)
		}
		return out
	case "compact":
		pct := 0
		if it.Before > 0 {
			pct = int(roundHalf((1 - float64(it.After)/float64(it.Before)) * 100))
		}
		what := ""
		if it.Scope == "task" {
			plural := "s"
			if it.Turns == 1 {
				plural = ""
			}
			extra := ""
			if it.Dropped {
				extra = " + ejemplos"
			}
			what = fmt.Sprintf("%d paso%s%s → resumen", it.Turns, plural, extra)
		} else {
			plural := "s"
			if it.Turns == 1 {
				plural = ""
			}
			what = fmt.Sprintf("%d turno%s → resumen", it.Turns, plural)
		}
		scope := "sesión"
		if it.Scope == "task" {
			scope = "tarea"
		}
		summary := wsRe.ReplaceAllString(it.Summary, " ")
		sr := []rune(summary)
		if len(sr) > 220 {
			summary = string(sr[:220]) + "…"
		}
		rag := ""
		if it.RagID != nil {
			rag = fmt.Sprintf("  ·  guardado en memoria #%d", *it.RagID)
		}
		llmTxt := ""
		if it.LLM != nil && !*it.LLM {
			llmTxt = "  ·  resumen simple (el LLM falló)"
		}
		info := []Seg{
			{Text: fmt.Sprintf("    %s → %s tokens", k(it.Before), k(it.After)), Color: theme.C["ink"]},
			{Text: fmt.Sprintf(" (−%d%%)", pct), Color: theme.C["green"]},
			{Text: "  ·  " + what, Color: theme.C["mute"]},
			{Text: rag, Color: theme.C["violet"]},
			{Text: llmTxt, Color: theme.C["orange"]},
		}
		out := [][]Seg{
			{},
			{{Text: "  ⇣ CONTEXTO COMPACTADO", Color: theme.C["primary"], Bold: true}, {Text: "  " + scope + " · " + it.Reason, Color: theme.C["dim"]}},
			Clip(info, W),
		}
		out = append(out, WrapSegs([]Seg{{Text: "    «" + summary + "»", Color: theme.C["dim"], Italic: true}}, W)...)
		return out
	case "note":
		return WrapSegs([]Seg{{Text: act + it.Text, Color: theme.C["mute"], Italic: true}}, W)
	case "error":
		out := [][]Seg{{}}
		return append(out, WrapSegs([]Seg{{Text: "  ✗ " + it.Text, Color: theme.C["red"], Bold: true}}, W)...)
	default:
		var out [][]Seg
		for _, l := range strings.Split(it.Text, "\n") {
			out = append(out, WrapSegs([]Seg{{Text: " " + l, Color: theme.C["mute"]}}, W)...)
		}
		return out
	}
}

// LineToPlain une los segmentos de una línea en texto plano.
func LineToPlain(segs []Seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}
