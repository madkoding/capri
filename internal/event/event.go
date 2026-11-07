// Package event define los eventos que emite el agente hacia la TUI.
package event

// Event es un evento del turno. Los campos no usados quedan vacíos.
type Event struct {
	Type string

	// fase / enrutado / memoria
	Phase  string
	Intent string
	Score  float64
	Hits   any

	// pasos y herramientas
	Name     string
	Args     map[string]string
	OK       bool
	Rejected bool
	Text     string
	Index    int
	Total    int

	// compactación
	Scope   string
	Reason  string
	Before  int
	After   int
	Turns   int
	ID      *int64
	Summary string
	LLM     *bool
	Dropped bool

	// memorización
	IDs []int64
}

// Emit es una función que recibe eventos.
type Emit func(Event)
