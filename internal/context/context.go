// Package context gestiona la sesión de trabajo y su compactación (resumen → RAG).
package context

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf16"

	"capri/internal/config"
	"capri/internal/event"
	"capri/internal/llm"
	"capri/internal/rag"
	"capri/internal/text"
	"capri/internal/ui/hud"
)

// EstimateTokens aproxima los tokens de un texto (~3,2 caracteres por token en español).
func EstimateTokens(s string) int {
	return int(math.Ceil(float64(len(utf16.Encode([]rune(s)))) / 3.2))
}

// MessagesTokens cuenta los tokens de una lista de mensajes.
func MessagesTokens(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateTokens(m.Content) + 4
	}
	return n
}

const summarySystem = "Eres un compactador de conversaciones. Resume en español, en un solo párrafo de máximo 120 palabras, la conversación dada. Conserva: datos del usuario (nombre, gustos, trabajo, mascotas), decisiones tomadas, archivos creados/modificados/borrados, comandos ejecutados y tareas pendientes. No inventes nada que no aparezca. No saludes ni añadas comentarios."

// Turn es un turno de la sesión.
type Turn struct {
	User    string
	Answer  string
	Actions []string
}

// FallbackSummary es el resumen determinista de respaldo: una línea por turno.
func FallbackSummary(turns []Turn, previous string) string {
	var lines []string
	for _, t := range turns {
		actions := ""
		if len(t.Actions) > 0 {
			a := t.Actions
			if len(a) > 3 {
				a = a[:3]
			}
			actions = " [" + strings.Join(a, ", ") + "]"
		}
		lines = append(lines, "- "+text.Trim(text.Collapse(t.User), 90)+actions+" → "+text.Trim(text.Collapse(t.Answer), 90))
	}
	out := strings.Join(lines, "\n")
	if previous != "" {
		out = previous + "\n" + out
	}
	return out
}

// Result es el resultado de una compactación.
type Result struct {
	Scope   string `json:"scope"`
	Reason  string `json:"reason"`
	Before  int    `json:"before"`
	After   int    `json:"after"`
	Turns   int    `json:"turns"`
	ID      *int64 `json:"id"`
	Summary string `json:"summary"`
	LLM     bool   `json:"llm"`
	Dropped bool   `json:"dropped"`
}

// Info es el estado del contexto de trabajo.
type Info struct {
	Window      int  `json:"window"`
	ServerCtx   int  `json:"serverCtx"`
	Threshold   int  `json:"threshold"`
	Used        int  `json:"used"`
	Turns       int  `json:"turns"`
	HasSummary  bool `json:"hasSummary"`
	Compactions int  `json:"compactions"`
	Session     bool `json:"session"`
}

// Summarizer produce el resumen de los turnos.
type Summarizer func(ctx context.Context, previous string, turns []Turn) (string, error)

// Manager acumula los turnos de la sesión.
type Manager struct {
	Rag         *rag.Rag
	Summarize   Summarizer
	Turns       []Turn
	Summary     string
	Compactions []Result
	ServerCtx   int
}

// New crea un gestor de contexto.
func New(r *rag.Rag, summarize Summarizer) *Manager {
	if summarize == nil {
		summarize = defaultSummarize
	}
	return &Manager{Rag: r, Summarize: summarize}
}

// Reset limpia la sesión (el RAG se conserva).
func (m *Manager) Reset() {
	m.Turns = nil
	m.Summary = ""
}

// WindowTokens devuelve la ventana efectiva (la del servidor si es menor).
func (m *Manager) WindowTokens() int {
	cfg := config.Get()
	if m.ServerCtx == 0 {
		if s := hud.ReadServer(); s != nil {
			m.ServerCtx = s.Nctx
		}
	}
	if m.ServerCtx > 0 {
		return int(math.Min(float64(m.ServerCtx), float64(cfg.CtxWindow)))
	}
	return cfg.CtxWindow
}

// Messages devuelve los mensajes de sesión para el prompt.
func (m *Manager) Messages() []llm.Message {
	cfg := config.Get()
	if !cfg.Session {
		return nil
	}
	var out []llm.Message
	if m.Summary != "" {
		out = append(out,
			llm.Message{Role: "user", Content: "[Resumen de la conversación hasta ahora]\n" + m.Summary},
			llm.Message{Role: "assistant", Content: "Entendido."})
	}
	for _, t := range m.Turns {
		out = append(out,
			llm.Message{Role: "user", Content: t.User},
			llm.Message{Role: "assistant", Content: t.Answer})
	}
	return out
}

// RecentText devuelve los últimos turnos en una línea cada uno.
func (m *Manager) RecentText(n int) string {
	if n == 0 {
		n = 2
	}
	start := len(m.Turns) - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, t := range m.Turns[start:] {
		lines = append(lines, "Usuario: "+text.Trim(t.User, 120)+" | Capri: "+text.Trim(t.Answer, 120))
	}
	return strings.Join(lines, "\n")
}

// Used devuelve los tokens usados por la sesión.
func (m *Manager) Used() int { return MessagesTokens(m.Messages()) }

// Add añade un turno.
func (m *Manager) Add(user, answer string, actions []string) {
	cfg := config.Get()
	if !cfg.Session || user == "" {
		return
	}
	m.Turns = append(m.Turns, Turn{User: text.Trim(user, 500), Answer: text.Trim(answer, 600), Actions: actions})
}

// ShouldCompact indica si la sesión se acerca al límite.
func (m *Manager) ShouldCompact(extra int) bool {
	cfg := config.Get()
	if !cfg.Session || len(m.Turns) == 0 {
		return false
	}
	return float64(m.Used()+extra) >= cfg.CtxThreshold*float64(m.WindowTokens())
}

// Info devuelve el estado del contexto.
func (m *Manager) Info(win int) Info {
	cfg := config.Get()
	w := win
	if w == 0 {
		if m.ServerCtx > 0 {
			w = int(math.Min(float64(m.ServerCtx), float64(cfg.CtxWindow)))
		} else {
			w = cfg.CtxWindow
		}
	}
	return Info{
		Window:      w,
		ServerCtx:   m.ServerCtx,
		Threshold:   int(math.Round(cfg.CtxThreshold * float64(w))),
		Used:        m.Used(),
		Turns:       len(m.Turns),
		HasSummary:  m.Summary != "",
		Compactions: len(m.Compactions),
		Session:     cfg.Session,
	}
}

// CompactOptions controla una compactación.
type CompactOptions struct {
	All     bool
	Reason  string
	OnEvent event.Emit
	Extra   int
}

// MaybeCompact compacta si hace falta.
func (m *Manager) MaybeCompact(ctx context.Context, o CompactOptions) (*Result, error) {
	if !m.ShouldCompact(o.Extra) {
		return nil, nil
	}
	if o.Reason == "" {
		o.Reason = "límite"
	}
	return m.Compact(ctx, o)
}

// Compact resume los turnos antiguos y los sustituye por el resumen.
func (m *Manager) Compact(ctx context.Context, o CompactOptions) (*Result, error) {
	cfg := config.Get()
	keep := 0
	if !o.All {
		keep = cfg.CtxKeep
		if max := len(m.Turns) - 1; max < keep {
			keep = max
		}
		if keep < 0 {
			keep = 0
		}
	}
	old := m.Turns[:len(m.Turns)-keep]
	if len(old) == 0 {
		return nil, nil
	}
	reason := o.Reason
	if reason == "" {
		reason = "manual"
	}
	before := m.Used()
	if o.OnEvent != nil {
		o.OnEvent(event.Event{Type: "compacting", Scope: "session", Reason: reason})
	}
	txt, err := m.Summarize(ctx, m.Summary, old)
	if err != nil && ctx.Err() != nil {
		return nil, err
	}
	txt = strings.TrimSpace(txt)
	llmOK := txt != ""
	if !llmOK {
		txt = FallbackSummary(old, m.Summary)
	}
	var id *int64
	if m.Rag != nil {
		stamp := time.Now().UTC().Format("2006-01-02 15:04")
		ids, err := m.Rag.Add(ctx, rag.AddOptions{
			Title:   fmt.Sprintf("Resumen de sesión %s (%d turnos)", stamp[11:], len(old)),
			Content: fmt.Sprintf("[compactación %s] %s", stamp, txt),
			Tags:    "compactación,sesión",
			Kind:    "summary",
		})
		if err == nil && len(ids) > 0 {
			id = &ids[0]
		}
	}
	m.Summary = txt
	m.Turns = m.Turns[len(old):]
	after := m.Used()
	res := Result{Scope: "session", Reason: reason, Before: before, After: after, Turns: len(old), ID: id, Summary: txt, LLM: llmOK}
	m.Compactions = append(m.Compactions, res)
	if o.OnEvent != nil {
		llmVal := llmOK
		o.OnEvent(event.Event{
			Type: "compact", Scope: res.Scope, Reason: res.Reason, Before: res.Before, After: res.After,
			Turns: res.Turns, ID: res.ID, Summary: res.Summary, LLM: &llmVal,
		})
	}
	return &res, nil
}

func defaultSummarize(ctx context.Context, previous string, turns []Turn) (string, error) {
	var parts []string
	if previous != "" {
		parts = append(parts, "Resumen previo: "+previous)
	}
	for _, t := range turns {
		actions := ""
		if len(t.Actions) > 0 {
			actions = "\n(acciones: " + strings.Join(t.Actions, ", ") + ")"
		}
		parts = append(parts, "Usuario: "+t.User+actions+"\nCapri: "+t.Answer)
	}
	return llm.Complete(ctx, []llm.Message{
		{Role: "system", Content: summarySystem},
		{Role: "user", Content: strings.Join(parts, "\n")},
	}, llm.Options{MaxTokens: 260, Temperature: 0.2})
}

// LastUser devuelve lo último que dijo el usuario ("" si no hay turnos).
func (m *Manager) LastUser() string {
	if len(m.Turns) == 0 {
		return ""
	}
	return m.Turns[len(m.Turns)-1].User
}

// Users devuelve los mensajes del usuario de la sesión, del más antiguo al más reciente.
func (m *Manager) Users() []string {
	out := make([]string, 0, len(m.Turns))
	for _, t := range m.Turns {
		out = append(out, t.User)
	}
	return out
}
