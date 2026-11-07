// Package gate decide si un mensaje es charla, acción o una pregunta que falta completar.
package gate

import (
	"context"
	"regexp"
	"strings"

	"capri/internal/config"
	"capri/internal/intent"
	"capri/internal/llm"
	"capri/internal/text"
)

// Grammar garantiza una salida válida.
const Grammar = `root ::= "CHARLA" | "ACCION" | "PREGUNTA: " [^\n]{4,140}`

const system = `Decides cómo tratar el mensaje del usuario antes de actuar. Respondes con UNA sola línea:
CHARLA → conversación, saludo, opinión, o una pregunta sobre ti, tus capacidades, tus herramientas o sobre conocimiento general. No hace falta tocar archivos ni comandos.
ACCION → pide claramente hacer algo en el equipo (crear, leer, editar, borrar, listar archivos; ejecutar un comando; consultar el contenido de la carpeta de trabajo).
PREGUNTA: seguido de la pregunta concreta que le harías al usuario → quiere hacer algo en el equipo pero falta un dato imprescindible (qué archivo, qué texto, qué carpeta). Nunca para charla ni para preguntas de conocimiento.
Reglas: nunca elijas ACCION solo porque el mensaje mencione palabras como "tools", "modelo", "archivo" o "comando" si en realidad pregunta por ellas. Ante la duda entre CHARLA y ACCION, elige CHARLA.
Ejemplos:
"hola, que modelo usas?" → CHARLA
"y tus tools cuales son?" → CHARLA
"qué puedes hacer?" → CHARLA
"crea notas.txt con la lista de compras" → ACCION
"lista los archivos" → ACCION
"borra el archivo" → PREGUNTA: ¿Qué archivo quieres borrar?
"arregla eso" → PREGUNTA: ¿Qué quieres que arregle exactamente?`

// Decision es el resultado del gate.
type Decision struct {
	Kind     string `json:"kind"`
	Question string `json:"question,omitempty"`
}

var (
	chatRe = regexp.MustCompile(`(?i)^CHARLA\b`)
	actRe  = regexp.MustCompile(`(?i)^ACCI[OÓ]N\b`)
	askRe  = regexp.MustCompile(`(?i)^PREGUNTA:\s*(.{4,})$`)
)

// ParseGate interpreta la línea del modelo (nil si es rara).
func ParseGate(out string) *Decision {
	s := strings.TrimSpace(out)
	if chatRe.MatchString(s) {
		return &Decision{Kind: "chat"}
	}
	if actRe.MatchString(s) {
		return &Decision{Kind: "act"}
	}
	if m := askRe.FindStringSubmatch(s); m != nil {
		q := strings.TrimSpace(m[1])
		// modelos pequeños a veces copian la plantilla del prompt en vez de preguntar algo real
		if strings.ContainsAny(q, "<>") || strings.Contains(q, "→") || strings.Contains(strings.ToLower(q), "falta un dato") {
			return nil
		}
		return &Decision{Kind: "ask", Question: text.Trim(q, 160)}
	}
	return nil
}

// Decide consulta al modelo. Devuelve nil si falla o está desactivado.
func Decide(ctx context.Context, txt, recent string) (*Decision, error) {
	if !config.Get().Gate {
		return nil, nil
	}
	user := "Mensaje: " + txt
	if recent != "" {
		user = "Contexto reciente:\n" + recent + "\n\n" + user
	}
	out, err := llm.Structured(ctx, []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}, Grammar, 60, 0)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	d := ParseGate(out)
	// modelos pequeños preguntan «¿qué archivo?» aunque el mensaje lo nombre: si hay objeto, es ACCION
	if d != nil && d.Kind == "ask" && intent.HasObject(txt) {
		return &Decision{Kind: "act"}, nil
	}
	return d, nil
}
