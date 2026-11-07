// Package react implementa el bucle ReAct de un paso por decisión.
package react

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"capri/internal/config"
	"capri/internal/llm"
	"capri/internal/planner"
	"capri/internal/skills"
	"capri/internal/text"
)

// Grammar fija el espacio de salida: una llamada o DONE.
const Grammar = `
root ::= "DONE" | call
call ::= "fs_list: " one | "fs_read: " one | "fs_delete: " one | "fs_write: " one " | " rest | "fs_edit: " one " | " one " | " one | "shell: " rest | "web_search: " rest
one ::= [^\n|\\]+
rest ::= [^\n]+
`

// grammarNoWeb es la gramática sin web_search: con la web apagada el modelo no
// debe poder elegirla (un modelo pequeño la usaría para «la fecha de hoy»).
const grammarNoWeb = `
root ::= "DONE" | call
call ::= "fs_list: " one | "fs_read: " one | "fs_delete: " one | "fs_write: " one " | " rest | "fs_edit: " one " | " one " | " one | "shell: " rest
one ::= [^\n|\\]+
rest ::= [^\n]+
`

// System es el prompt del bucle.
const System = `Eres un agente que trabaja paso a paso. Respondes con UNA sola línea: el siguiente paso, o DONE cuando el objetivo ya está cumplido.
Pasos (herramienta: argumentos):
fs_list: carpeta
fs_read: archivo
fs_write: archivo | contenido   (usa \n para saltos de línea; crea las carpetas que falten)
fs_edit: archivo | texto a buscar | texto nuevo
fs_delete: archivo
shell: comando de Linux   (la hora y la fecha salen de aquí: date)
web_search: consulta   (busca en internet y guarda lo que encuentre en tu memoria)
Tras cada paso recibes "Resultado:" o "Error:" reales; úsalos para decidir el siguiente (copia los datos que veas, no inventes). Usa EXACTAMENTE los nombres de archivo del usuario. Los argumentos son solo datos: nada de explicaciones ni razonamientos en la línea. Si un paso falla, corrige o prueba otro camino; si no se puede, responde DONE. Responde DONE en cuanto esté cumplido; no hagas pasos de más.`

// GrammarFor devuelve la gramática según la web esté activa o no.
func GrammarFor(web bool) string {
	if web {
		return Grammar
	}
	return grammarNoWeb
}

// SystemFor devuelve el prompt del bucle; sin web, sin la línea de web_search.
func SystemFor(web bool) string {
	if web {
		return System
	}
	return strings.Replace(System, "\nweb_search: consulta   (busca en internet y guarda lo que encuentre en tu memoria)", "", 1)
}

// Conversation es la conversación viva de una tarea.
type Conversation struct {
	Messages []llm.Message
	GoalIdx  int
	Shots    int
}

// StartConversation crea la conversación con sistema, ejemplos y objetivo.
func StartConversation(goal, env string, sk []skills.Skill) *Conversation {
	var shots []llm.Message
	for _, s := range sk {
		shots = append(shots, s.Messages()...)
	}
	msgs := make([]llm.Message, 0, 2+len(shots))
	msgs = append(msgs, llm.Message{Role: "system", Content: SystemFor(config.Get().Web)})
	msgs = append(msgs, shots...)
	user := "Objetivo: " + goal
	if env != "" {
		user = env + "\n" + user
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: user})
	return &Conversation{Messages: msgs, GoalIdx: len(msgs) - 1, Shots: len(shots)}
}

// CompactResult describe una compactación de tarea.
type CompactResult struct {
	Summary string
	Removed int
	Dropped bool
}

// CompactConversation reduce los pasos viejos a un resumen añadido al objetivo.
func CompactConversation(c *Conversation, keepPairs int, dropShots bool) *CompactResult {
	if keepPairs == 0 {
		keepPairs = 2
	}
	pairs := (len(c.Messages) - c.GoalIdx - 1) / 2
	summary := ""
	removed := 0
	if pairs > keepPairs {
		n := (pairs - keepPairs) * 2
		old := make([]llm.Message, n)
		copy(old, c.Messages[c.GoalIdx+1:c.GoalIdx+1+n])
		c.Messages = append(c.Messages[:c.GoalIdx+1], c.Messages[c.GoalIdx+1+n:]...)
		var lines []string
		for i := 0; i+1 < len(old); i += 2 {
			body := text.Collapse(old[i+1].Content)
			body = text.Trim(body, 110)
			lines = append(lines, "- "+old[i].Content+" → "+body)
		}
		summary = strings.Join(lines, "\n")
		removed = len(lines)
		c.Messages[c.GoalIdx].Content += "\n\nPasos ya hechos (contexto compactado):\n" + summary
	}
	dropped := false
	if dropShots && c.Shots > 0 {
		c.Messages = append(c.Messages[:1], c.Messages[1+c.Shots:]...)
		c.GoalIdx -= c.Shots
		c.Shots = 0
		dropped = true
	}
	if removed > 0 || dropped {
		return &CompactResult{Summary: summary, Removed: removed, Dropped: dropped}
	}
	return nil
}

// Decision es el siguiente paso o el fin de la tarea.
type Decision struct {
	Done bool
	Step planner.Step
	Line string
}

// NextStep pide el siguiente paso al modelo.
func NextStep(ctx context.Context, messages []llm.Message) (Decision, error) {
	out, err := llm.Structured(ctx, messages, GrammarFor(config.Get().Web), 250, 0)
	if err != nil {
		return Decision{}, err
	}
	out = strings.TrimSpace(out)
	if os.Getenv("CAPRI_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[react] %s\n", text.Trim(out, 300))
	}
	if i := strings.Index(out, " → "); i >= 0 {
		out = out[:i]
	}
	if out == "DONE" || out == "" {
		return Decision{Done: true, Line: "DONE"}, nil
	}
	steps := planner.ParsePlan(out)
	if len(steps) == 0 {
		return Decision{Done: true, Line: "DONE"}, nil
	}
	return Decision{Step: steps[0], Line: out}, nil
}

var (
	pipeTail = regexp.MustCompile(`(?i)\s*\|\s*(?:fs_\w+|answer|DONE)\b.*$`)
	arrow    = regexp.MustCompile(`\s+→.*$`)
	// prosa que un modelo pequeño pega tras el comando: «date +%F para obtener la fecha, luego…»
	proseRe = regexp.MustCompile(`(?i)^\s+(?:para|luego|despu[eé]s|pero|entonces|y (?:luego|despu[eé]s|as[ií])|porque|ya que|que (?:me|nos|te) |-\s)`)
)

// CleanCommand deja solo el comando: corta lo que el modelo pega de más.
func CleanCommand(cmd string) string {
	cmd = strings.Split(cmd, `\n`)[0]
	cmd = pipeTail.ReplaceAllString(cmd, "")
	cmd = arrow.ReplaceAllString(cmd, "")
	cmd = cutProse(cmd)
	return strings.TrimSpace(cmd)
}

// cutProse corta el comando donde empieza una explicación (fuera de comillas).
func cutProse(cmd string) string {
	var quote rune
	for i, r := range cmd {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case r == ' ' || r == '\t':
			if proseRe.MatchString(cmd[i:]) {
				return cmd[:i]
			}
		}
	}
	return cmd
}

// ErrorHint da pistas accionables junto al error.
func ErrorHint(tool, txt string) string {
	has := func(p string) bool { return regexp.MustCompile(p).MatchString(txt) }
	if has(`ENOENT`) && tool == "fs_edit" {
		return " El archivo no existe: para crearlo usa fs_write."
	}
	if has(`ENOENT`) && (tool == "fs_read" || tool == "fs_delete") {
		return " El archivo no existe. Mira los que hay con fs_list; si debías crearlo usa fs_write."
	}
	if has(`ENOTDIR`) {
		return " Eso es un archivo, no una carpeta: para ver su contenido usa fs_read; para contar líneas usa shell: wc -l < archivo."
	}
	if has(`aparece \d+ veces`) {
		return " Para reemplazar todas las apariciones usa shell: sed -i 's/viejo/nuevo/g' archivo."
	}
	if has(`comodines`) {
		return " Usa shell: ls, rm -f o find con el patrón."
	}
	if has(`comando no encontrado`) {
		return " Prueba otro comando (por ejemplo python3 en vez de python)."
	}
	if has(`no se encontr|no aparece`) && tool == "fs_edit" {
		return " Lee el archivo con fs_read para ver el texto exacto."
	}
	return ""
}

// Observation resume el resultado real de un paso para el modelo.
func Observation(ok bool, txt string, max int, tool string) string {
	if max == 0 {
		max = 600
	}
	t := strings.TrimSpace(txt)
	r := []rune(t)
	if len(r) > max {
		t = string(r[:max]) + "…"
	}
	prefix := "Resultado: "
	if !ok {
		prefix = "Error: "
	}
	out := prefix + t
	if !ok {
		out += ErrorHint(tool, t)
	}
	return out
}
