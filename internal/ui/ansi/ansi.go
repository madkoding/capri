// Package ansi contiene utilidades para pintar la pantalla directamente (sin Ink): estilos truecolor,
// medida visible y recorte.
// Supuesto: todos los glifos que usamos ocupan 1 celda (sin emojis ni caracteres anchos).
package ansi

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Reset devuelve el terminal a su estilo por defecto.
const Reset = "\x1b[0m"

var (
	rgbMu    sync.Mutex
	rgbCache = map[string]string{}
)

func rgb(hex string) string {
	rgbMu.Lock()
	defer rgbMu.Unlock()
	if v, ok := rgbCache[hex]; ok {
		return v
	}
	if len(hex) < 7 {
		return ""
	}
	r, _ := strconv.ParseInt(hex[1:3], 16, 32)
	g, _ := strconv.ParseInt(hex[3:5], 16, 32)
	b, _ := strconv.ParseInt(hex[5:7], 16, 32)
	v := strconv.FormatInt(r, 10) + ";" + strconv.FormatInt(g, 10) + ";" + strconv.FormatInt(b, 10)
	rgbCache[hex] = v
	return v
}

// Style es el estilo opcional de un texto.
type Style struct {
	Color   string
	Bold    bool
	Italic  bool
	Inverse bool
	Dim     bool
}

// Paint pinta texto con el estilo dado. Vacío → "".
func Paint(text string, st Style) string {
	if text == "" {
		return ""
	}
	var pre strings.Builder
	if st.Color != "" {
		pre.WriteString("\x1b[38;2;" + rgb(st.Color) + "m")
	}
	if st.Bold {
		pre.WriteString("\x1b[1m")
	}
	if st.Italic {
		pre.WriteString("\x1b[3m")
	}
	if st.Inverse {
		pre.WriteString("\x1b[7m")
	}
	if st.Dim {
		pre.WriteString("\x1b[2m")
	}
	if pre.Len() == 0 {
		return text
	}
	return pre.String() + text + Reset
}

// Strip quita las secuencias ANSI de una cadena.
func Strip(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					break
				}
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Vlen cuenta las celdas visibles de una cadena con ANSI.
func Vlen(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			i++
			for i < len(s) {
				k := s[i]
				if (k >= 'A' && k <= 'Z') || (k >= 'a' && k <= 'z') {
					break
				}
				i++
			}
		} else if c&0xC0 != 0x80 { // no contar bytes de continuación UTF-8
			n++
		}
	}
	return n
}

// PadEnd rellena a `w` celdas visibles.
func PadEnd(s string, w int) string {
	n := Vlen(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

// ClipAnsi recorta una cadena con ANSI a `w` celdas visibles (con «…» si se pasa).
func ClipAnsi(s string, w int) string {
	if Vlen(s) <= w {
		return s
	}
	var out strings.Builder
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					break
				}
				j++
			}
			if j < len(s) {
				j++
			}
			out.WriteString(s[i:j])
			i = j
			continue
		}
		if n < w-1 {
			out.WriteByte(s[i])
			n++
		} else {
			break
		}
		i++
	}
	return out.String() + Reset + "…"
}

// FitRow ajusta una fila a exactamente `w` celdas (recorta o rellena).
func FitRow(s string, w int) string {
	n := Vlen(s)
	if n == w {
		return s
	}
	if n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return PadEnd(ClipAnsi(s, w), w)
}

var (
	quantMu    sync.Mutex
	quantCache = map[string]string{}
)

func quant(c string) string {
	quantMu.Lock()
	defer quantMu.Unlock()
	if r, ok := quantCache[c]; ok {
		return r
	}
	if len(c) < 7 {
		return c
	}
	var n [3]int
	for i := 0; i < 3; i++ {
		v, _ := strconv.ParseInt(c[1+i*2:3+i*2], 16, 32)
		q := int((float64(v)/20.0)+0.5) * 20
		if q > 255 {
			q = 255
		}
		n[i] = q
	}
	r := fmt.Sprintf("#%02x%02x%02x", n[0], n[1], n[2])
	quantCache[c] = r
	return r
}

// Cell es una celda con carácter y estilo.
type Cell struct {
	Ch    string
	Color string
	Bold  bool
}

// Runs convierte celdas a cadena, fusionando las contiguas con el mismo estilo.
func Runs(cells []Cell) string {
	var out strings.Builder
	var curText, curColor string
	curBold := false
	has := false
	flush := func() {
		if has {
			out.WriteString(Paint(curText, Style{Color: curColor, Bold: curBold}))
		}
	}
	for _, c := range cells {
		color := ""
		if c.Color != "" {
			color = quant(c.Color)
		}
		if has && curColor == color && curBold == c.Bold {
			curText += c.Ch
		} else {
			flush()
			curText, curColor, curBold, has = c.Ch, color, c.Bold, true
		}
	}
	flush()
	return out.String()
}

// Column es una columna para HStack.
type Column struct {
	W    int
	Rows []string
}

// HStack une columnas lado a lado; las más bajas se rellenan.
func HStack(cols []Column) []string {
	h := 0
	for _, c := range cols {
		if len(c.Rows) > h {
			h = len(c.Rows)
		}
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		var b strings.Builder
		for _, c := range cols {
			row := ""
			if i < len(c.Rows) {
				row = c.Rows[i]
			}
			b.WriteString(PadEnd(row, c.W))
		}
		out[i] = b.String()
	}
	return out
}
