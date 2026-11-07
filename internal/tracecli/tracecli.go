// Package tracecli convierte trazas de uso real en tareas de evaluación.
package tracecli

import (
	"encoding/json"
	"fmt"
	"strings"

	"capri/internal/trace"
)

// Run ejecuta el subcomando from-trace.
func Run(args []string) int {
	rows := trace.ReadAll()
	var turns []map[string]any
	marks := map[string]map[string]any{}
	for _, r := range rows {
		if r["type"] == "mark" {
			if id, ok := r["id"].(string); ok {
				marks[id] = r
			}
		} else {
			turns = append(turns, r)
		}
	}
	has := func(x string) bool {
		for _, a := range args {
			if a == x {
				return true
			}
		}
		return false
	}
	if has("--stats") {
		fails := 0
		for _, t := range turns {
			id, _ := t["id"].(string)
			if t["fail"] == true {
				fails++
			} else if _, ok := marks[id]; ok {
				fails++
			}
		}
		pct := 0
		if len(turns) > 0 {
			pct = (100 * fails) / len(turns)
		}
		modes := map[string]int{}
		var order []string
		for _, t := range turns {
			m, _ := t["mode"].(string)
			if _, ok := modes[m]; !ok {
				order = append(order, m)
			}
			modes[m]++
		}
		var parts []string
		for _, m := range order {
			parts = append(parts, fmt.Sprintf("%s: %d", m, modes[m]))
		}
		fmt.Printf("turnos: %d  fallidos/marcados: %d  (%d %%)  %s\n", len(turns), fails, pct, strings.Join(parts, ", "))
		return 0
	}

	var pick []map[string]any
	for _, t := range turns {
		id, _ := t["id"].(string)
		if has("--all") || t["fail"] == true || marks[id] != nil {
			pick = append(pick, t)
		}
	}
	if len(pick) == 0 {
		fmt.Println("(sin turnos fallidos ni marcados con /bad)")
		return 0
	}
	for i, t := range pick {
		id, _ := t["id"].(string)
		m := marks[id]
		ts, _ := t["ts"].(string)
		mode, _ := t["mode"].(string)
		line := fmt.Sprintf("// %s  modo=%s", ts, mode)
		if m != nil {
			note, _ := m["note"].(string)
			line += "  /bad: " + note
		}
		fmt.Println(line)
		var steps []string
		if ss, ok := t["steps"].([]any); ok {
			for _, s := range ss {
				sm, _ := s.(map[string]any)
				tool, _ := sm["tool"].(string)
				mark := "✔"
				if sm["ok"] == false {
					mark = "✗"
				}
				first := ""
				if am, ok := sm["args"].(map[string]any); ok {
					for _, v := range am {
						first = fmt.Sprint(v)
						break
					}
				}
				steps = append(steps, strings.TrimSpace(mark+" "+tool+" "+first))
			}
		}
		if len(steps) == 0 {
			steps = []string{"(ninguno)"}
		}
		fmt.Println("//   pasos: " + strings.Join(steps, " → "))
		final, _ := t["final"].(string)
		fr := []rune(final)
		if len(fr) > 120 {
			final = string(fr[:120])
		}
		fmt.Println("//   respuesta: " + final)
		goal, _ := t["goal"].(string)
		gj, _ := json.Marshal(goal)
		fmt.Printf("{ id: 'real-%d', goal: %s, check: (a) => true /* TODO: qué debería haber pasado */ },\n\n", i+1, gj)
	}
	return 0
}
