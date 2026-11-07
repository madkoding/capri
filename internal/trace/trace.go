// Package trace guarda trazas locales de cada turno (data/traces.jsonl).
package trace

import (
	"bufio"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"capri/internal/config"
	"capri/internal/event"
)

// Step es un paso de una traza.
type Step struct {
	Tool     string            `json:"tool"`
	Args     map[string]string `json:"args"`
	OK       *bool             `json:"ok,omitempty"`
	Rejected bool              `json:"rejected,omitempty"`
	Text     string            `json:"text,omitempty"`
}

// Entry es una traza de turno.
type Entry struct {
	ID       string  `json:"id"`
	TS       string  `json:"ts"`
	Goal     string  `json:"goal"`
	Mode     string  `json:"mode"`
	Intent   *string `json:"intent"`
	Steps    []Step  `json:"steps"`
	Final    string  `json:"final"`
	Error    string  `json:"error,omitempty"`
	Undoable bool    `json:"undoable"`
	MS       int64   `json:"ms"`
	Fail     bool    `json:"fail"`
}

var failRe = regexp.MustCompile(`Me detuve|rechaz|cancelado`)

// File devuelve la ruta del fichero de trazas.
func File() string {
	return filepath.Join(filepath.Dir(config.Get().DBPath), "traces.jsonl")
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// SummarizeEvents resume los eventos de un turno.
func SummarizeEvents(events []event.Event) ([]Step, *string) {
	var steps []Step
	var intent *string
	for _, e := range events {
		switch e.Type {
		case "route":
			v := e.Intent
			intent = &v
		case "tool":
			args := map[string]string{}
			for k, v := range e.Args {
				args[k] = clip(v, 120)
			}
			steps = append(steps, Step{Tool: e.Name, Args: args})
		case "tool_result":
			if len(steps) > 0 {
				ok := e.OK
				last := &steps[len(steps)-1]
				last.OK = &ok
				last.Rejected = e.Rejected
				last.Text = clip(e.Text, 160)
			}
		}
	}
	return steps, intent
}

// Record escribe una traza. Devuelve la entrada completa o nil si está desactivado.
func Record(entry Entry) *Entry {
	cfg := config.Get()
	if !cfg.TraceEnabled {
		return nil
	}
	if entry.ID == "" {
		entry.ID = strconv.FormatInt(time.Now().UnixMilli(), 36) + randomBase36(3)
	}
	if entry.TS == "" {
		entry.TS = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if entry.Steps == nil {
		entry.Steps = []Step{}
	}
	entry.Fail = failRe.MatchString(entry.Final)
	for _, s := range entry.Steps {
		if s.OK != nil && !*s.OK {
			entry.Fail = true
		}
	}
	if err := appendLine(entry); err != nil {
		return nil
	}
	return &entry
}

// Mark anota un turno (por ejemplo /bad).
func Mark(id, flag, note string) bool {
	cfg := config.Get()
	if !cfg.TraceEnabled || id == "" {
		return false
	}
	line := map[string]any{
		"type": "mark",
		"id":   id,
		"flag": flag,
		"note": clip(note, 300),
		"ts":   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	return appendLine(line) == nil
}

func appendLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(File()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(File(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadAll lee todas las líneas del fichero de trazas.
func ReadAll() []map[string]any {
	f, err := os.Open(File())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func randomBase36(n int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}
