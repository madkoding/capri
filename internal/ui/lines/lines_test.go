package lines

import (
	"strings"
	"testing"
)

func plain(segs []Seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// regexp.Split de Go descarta lo que coincide: la negrita y el código desaparecían del texto.
func TestInlineConservaNegritaYCodigo(t *testing.T) {
	segs := inlineSegs("Hay **0** archivos y `ls` los lista", "")
	if got, want := plain(segs), "Hay 0 archivos y ls los lista"; got != want {
		t.Fatalf("texto=%q, quiero %q", got, want)
	}
	var bold, code bool
	for _, s := range segs {
		if s.Bold && s.Text == "0" {
			bold = true
		}
		if s.Text == "ls" && !s.Bold {
			code = true
		}
	}
	if !bold || !code {
		t.Errorf("faltan estilos: negrita=%v código=%v (%+v)", bold, code, segs)
	}
}

// Los modelos emiten marcadores sueltos o sin cerrar; antes provocaba slice bounds out of range.
func TestMarkdownRotoNoPanica(t *testing.T) {
	casos := []string{
		"", "**", "**x", "x**", "****", "***", "`", "``", "```", "`x", "x`", "**`", "`**", "**a**b**",
		"**negrita sin cerrar y `código` después", "texto ** con ** dos ** pares", "- **", "# ", "## `",
		"```\nsin cerrar", "```js\ncode\n```\n**x", "1. **", "a\n\n\n**\n`",
	}
	for _, c := range casos {
		for _, w := range []int{1, 5, 20, 80} {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("pánico con %q (ancho %d): %v", c, w, r)
					}
				}()
				_ = inlineSegs(c, "")
				_ = MarkdownLines(c)
				_ = ItemLines(Item{Kind: "assistant", Text: c, At: "10:00:00"}, w)
			}()
		}
	}
}

func FuzzRender(f *testing.F) {
	for _, s := range []string{"**x", "`", "- **a**", "```js\nx", "# t\n\n**a** `b` c", "1. **", "añ**ó`ñ`**"} {
		f.Add(s, 40)
	}
	f.Fuzz(func(t *testing.T, text string, w int) {
		if w < 1 || w > 300 {
			w = 40
		}
		for _, kind := range []string{"assistant", "user", "sys", "error", "note"} {
			_ = ItemLines(Item{Kind: kind, Text: text, At: "10:00:00", Meta: "1s"}, w)
		}
	})
}
