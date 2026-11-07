// Package summary resume los resultados observados y las acciones de un turno.
package summary

import (
	"regexp"
	"strings"

	"capri/internal/text"
)

// Obs es un paso ejecutado y su resultado.
type Obs struct {
	Tool     string            `json:"tool"`
	Args     map[string]string `json:"args"`
	OK       bool              `json:"ok"`
	Rejected bool              `json:"rejected,omitempty"`
	Text     string            `json:"text,omitempty"`
	Raw      string            `json:"raw,omitempty"`
	Line     string            `json:"line,omitempty"`
}

// OBSERVING son las herramientas cuyo resultado se muestra al usuario.
var OBSERVING = map[string]bool{"fs_read": true, "fs_list": true, "shell": true, "web_search": true}

// Label describe un paso en una línea.
func Label(o Obs) string {
	v := o.Args["path"]
	if v == "" {
		v = o.Args["command"]
	}
	if v == "" {
		v = o.Args["query"]
	}
	return strings.TrimSpace(o.Tool + " " + v)
}

// Observed devuelve los resultados observados, compactos, para redactar la respuesta final.
func Observed(obs []Obs) string {
	var parts []string
	for _, o := range obs {
		if o.OK && OBSERVING[o.Tool] {
			raw := o.Raw
			if raw == "" {
				raw = o.Text
			}
			parts = append(parts, "["+Label(o)+"]\n"+text.Trim(strings.TrimSpace(raw), 900))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Resultados:\n" + strings.Join(parts, "\n") + "\n\n"
}

// Summarize construye el informe final: acciones hechas, lo observado y por qué se detuvo.
func Summarize(obs []Obs, halted string) string {
	var lines []string
	for _, o := range obs {
		if !o.OK {
			lines = append(lines, "✗ "+Label(o)+": "+o.Text)
		} else {
			switch o.Tool {
			case "fs_write":
				lines = append(lines, "✔ escribí `"+o.Args["path"]+"`")
			case "fs_edit":
				lines = append(lines, "✔ edité `"+o.Args["path"]+"`")
			case "fs_delete":
				lines = append(lines, "✔ eliminé `"+o.Args["path"]+"`")
			}
		}
	}
	var observed []Obs
	for _, o := range obs {
		if o.OK && OBSERVING[o.Tool] {
			observed = append(observed, o)
		}
	}
	if len(observed) > 2 {
		observed = observed[len(observed)-2:]
	}
	for _, o := range observed {
		lines = append(lines, text.Block(o.Text))
	}
	if halted != "" {
		lines = append(lines, "Me detuve: "+halted)
	}
	if len(lines) == 0 {
		return "(sin resultado)"
	}
	return strings.Join(lines, "\n")
}

var actionRe = regexp.MustCompile(`^[✔✗]`)

// ActionLines deja solo las líneas deterministas (acciones hechas/fallidas).
func ActionLines(txt string) string {
	var out []string
	for _, l := range strings.Split(txt, "\n") {
		if actionRe.MatchString(l) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
