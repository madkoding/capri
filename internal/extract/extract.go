// Package extract extrae argumentos de herramientas con gramática GBNF: la salida SIEMPRE es válida,
// y el modelo solo tiene que copiar/ajustar datos de la petición.
package extract

import (
	"context"
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"

	"capri/internal/llm"
)

type field struct {
	Name string
	Kind string
}

type spec struct {
	Fields []field
	Shots  [][2]any // [texto, []string de valores]
}

var specs = map[string]spec{
	"fs_list": {
		Fields: []field{{"path", "line"}},
		Shots:  [][2]any{{"qué hay en la carpeta src", []string{"src"}}, {"lista los archivos", []string{"."}}},
	},
	"fs_read": {
		Fields: []field{{"path", "line"}},
		Shots:  [][2]any{{"muestra el contenido de notas.txt", []string{"notas.txt"}}, {"lee src/app.js", []string{"src/app.js"}}},
	},
	"fs_delete": {
		Fields: []field{{"path", "line"}},
		Shots:  [][2]any{{"borra el archivo viejo.txt", []string{"viejo.txt"}}, {"elimina la carpeta tmp", []string{"tmp"}}},
	},
	"fs_write": {
		Fields: []field{{"path", "line"}, {"content", "text"}},
		Shots: [][2]any{
			{"crea hola.txt que diga hola mundo", []string{"hola.txt", "hola mundo"}},
			{"escribe en lista.md: comprar pan y leche", []string{"lista.md", "comprar pan y leche"}},
			{"crea run.sh que imprima la fecha", []string{"run.sh", "#!/bin/bash\ndate"}},
		},
	},
	"fs_edit": {
		Fields: []field{{"path", "line"}, {"find", "line"}, {"replace", "line"}},
		Shots:  [][2]any{{"en config.json cambia el puerto 80 por 8080", []string{"config.json", "80", "8080"}}, {"reemplaza foo por bar en main.js", []string{"main.js", "foo", "bar"}}},
	},
	"shell": {
		Fields: []field{{"command", "line"}},
		Shots: [][2]any{
			{"ejecuta el comando uname -a", []string{"uname -a"}},
			{"qué hora es", []string{"date"}},
			{"crea una carpeta llamada fotos", []string{"mkdir fotos"}},
			{"corre npm test", []string{"npm test"}},
			{"cuál es mi ip", []string{"hostname -I"}},
			{"cuánto pesa la carpeta src", []string{"du -sh src"}},
		},
	},
}

// NEEDS_ARGS son las herramientas que necesitan extracción de argumentos.
var NEEDS_ARGS = map[string]bool{}

func init() {
	for k := range specs {
		NEEDS_ARGS[k] = true
	}
}

func fmtVals(fields []field, vals []string) string {
	var b strings.Builder
	for i, f := range fields {
		v := ""
		if i < len(vals) {
			v = vals[i]
		}
		b.WriteString("<" + f.Name + ">" + v + "</" + f.Name + ">")
	}
	return b.String()
}

func grammar(fields []field) string {
	var parts []string
	for _, f := range fields {
		parts = append(parts, `"<`+f.Name+`>" `+f.Kind+` "</`+f.Name+`>"`)
	}
	return "root ::= " + strings.Join(parts, " ") + "\nline ::= [^<\\n]+\ntext ::= [^<]+"
}

var (
	fileRe = regexp2.MustCompile(`(?:^|[\s"'\x60(])((?:[\w.-]+/)*[\w-]+(?:\.[\w-]{1,8})+)(?=$|[\s"'\x60),.:;!?])`, 0)
	cmdRe  = regexp.MustCompile(`(?i)\b(?:comando|ejecuta(?:r)?|corre|correr|lanza|run|exec(?:ute)?)\s+(?:el\s+|the\s+)?(?:comando\s+|command\s+)?(.+?)\s*$`)
	artRe  = regexp.MustCompile(`(?i)^(un|una|el|la|los|las)\s`)
	currRe = regexp.MustCompile(`(?i)\b(actual|aqu[ií]|here|current|esta carpeta|proyecto)\b`)
	edgeRe = regexp.MustCompile(`^[` + "`" + `"']|[` + "`" + `"'.]$`)
)

// Heuristics aplica reglas deterministas que corrigen/ganan al modelo cuando la petición es inequívoca.
func Heuristics(tool, txt string) map[string]string {
	h := map[string]string{}
	file := ""
	if m, err := fileRe.FindStringMatch(txt); err == nil && m != nil && len(m.Groups()) > 1 {
		file = m.Groups()[1].String()
	}
	if tool == "shell" {
		if m := cmdRe.FindStringSubmatch(txt); m != nil && !artRe.MatchString(m[1]) {
			h["command"] = edgeRe.ReplaceAllString(m[1], "")
		}
	}
	if tool == "fs_list" && currRe.MatchString(txt) {
		h["path"] = "."
	}
	switch tool {
	case "fs_read", "fs_write", "fs_edit", "fs_delete":
		if file != "" {
			h["path"] = file
		}
	}
	return h
}

// ExtractArgs obtiene los argumentos de una herramienta a partir del texto del usuario.
func ExtractArgs(ctx context.Context, tool, userText string) (map[string]string, error) {
	sp := specs[tool]
	h := Heuristics(tool, userText)
	all := true
	for _, f := range sp.Fields {
		if _, ok := h[f.Name]; !ok {
			all = false
			break
		}
	}
	if all {
		return h, nil
	}
	msgs := []llm.Message{{
		Role:    "system",
		Content: `Extract the arguments of the "` + tool + `" tool from the user's request. Paths are relative. Commands must be valid POSIX/Linux shell (no Windows commands). Copy names and text exactly. Output only the tags.`,
	}}
	for _, shot := range sp.Shots {
		text := shot[0].(string)
		vals := shot[1].([]string)
		msgs = append(msgs,
			llm.Message{Role: "user", Content: text},
			llm.Message{Role: "assistant", Content: fmtVals(sp.Fields, vals)})
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: userText})
	out, err := llm.Structured(ctx, msgs, grammar(sp.Fields), 400, 0.1)
	if err != nil {
		return nil, err
	}
	args := map[string]string{}
	for _, f := range sp.Fields {
		re := regexp.MustCompile(`(?s)<` + f.Name + `>([\s\S]*?)</` + f.Name + `>`)
		v := ""
		if m := re.FindStringSubmatch(out); m != nil {
			v = strings.TrimSpace(m[1])
		}
		args[f.Name] = v
	}
	for k, v := range h {
		args[k] = v
	}
	return args, nil
}
