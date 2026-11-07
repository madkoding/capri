// Package loop implementa el bucle ReAct: el modelo decide UN paso cada vez viendo la observación real.
package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"capri/internal/config"
	ctxmgr "capri/internal/context"
	"capri/internal/event"
	"capri/internal/llm"
	"capri/internal/planner"
	"capri/internal/rag"
	"capri/internal/react"
	"capri/internal/shell"
	"capri/internal/skills"
	"capri/internal/summary"
	"capri/internal/tools"
	"capri/internal/trash"
)

// CompactInfo describe la compactación de una tarea.
type CompactInfo struct {
	Scope   string
	Reason  string
	Before  int
	After   int
	Turns   int
	Summary string
	Dropped bool
}

// Options configura el bucle.
type Options struct {
	Goal      string
	Env       string
	Rag       *rag.Rag
	Trash     *trash.Trash
	Confirm   tools.Confirm
	OnEvent   event.Emit
	OnPath    func(string)
	Window    int
	OnCompact func(ctx context.Context, c CompactInfo) *int64
	Skills    []skills.Skill
}

// Result es el resultado del bucle.
type Result struct {
	Obs    []summary.Obs
	Used   []string
	Halted string
}

var errCanceled = errors.New("cancelado")

// RunReact ejecuta el bucle. obs vacío = no se hizo ninguna acción.
func RunReact(ctx context.Context, o Options) (*Result, error) {
	if o.OnEvent == nil {
		o.OnEvent = func(event.Event) {}
	}
	if o.OnPath == nil {
		o.OnPath = func(string) {}
	}
	if o.OnCompact == nil {
		o.OnCompact = func(context.Context, CompactInfo) *int64 { return nil }
	}
	if o.Window == 0 {
		o.Window = config.Get().CtxWindow
	}
	failedBefore := false
	failedPaths := map[string]bool{}
	tctx := &tools.Context{
		Rag:   o.Rag,
		Trash: o.Trash,
		Confirm: func(name, what string, co tools.ConfirmOptions) bool {
			force := failedBefore && !failedPaths[co.Args["path"]]
			if force {
				what = "⚠ un paso anterior falló. ¿Continuar igual? " + what
			}
			co.Force = force
			if o.Confirm == nil {
				return false
			}
			return o.Confirm(name, what, co)
		},
	}

	convo := react.StartConversation(o.Goal, o.Env, o.Skills)
	var obs []summary.Obs
	var used []string
	seen := map[string]bool{}
	halted := ""
	nudges := 0
	consecutiveFails := 0
	usedSuggestions := map[string]bool{}
	cfg := config.Get()
	replaceAll := ReplaceAllWanted(o.Goal)

	// pending devuelve el primer paso sugerido por la cobertura que aún no se forzó.
	pending := func() (*planner.Step, string, []planner.Gap) {
		var steps []planner.Step
		for _, x := range obs {
			ok := x.OK
			steps = append(steps, planner.Step{Tool: x.Tool, Args: x.Args, OK: &ok})
		}
		gaps := planner.GapsOf(o.Goal, steps)
		for _, g := range gaps {
			if g.Suggest == nil || usedSuggestions[*g.Suggest] {
				continue
			}
			if plan := planner.ParsePlan(*g.Suggest); len(plan) > 0 {
				return &plan[0], *g.Suggest, gaps
			}
		}
		return nil, "", gaps
	}

	for n := 0; n < planner.MaxSteps; n++ {
		if ctx.Err() != nil {
			return nil, errCanceled
		}
		limit := int(cfg.CtxThreshold*float64(o.Window)) - 350
		if ctxmgr.MessagesTokens(convo.Messages) >= limit {
			before := ctxmgr.MessagesTokens(convo.Messages)
			o.OnEvent(event.Event{Type: "compacting", Scope: "task", Reason: "límite"})
			r := react.CompactConversation(convo, 2, false)
			if ctxmgr.MessagesTokens(convo.Messages) >= limit {
				second := react.CompactConversation(convo, 1, true)
				if r == nil {
					r = second
				} else if second != nil {
					r.Dropped = true
				}
			}
			if r != nil && (r.Removed > 0 || r.Dropped) {
				sum := r.Summary
				if sum == "" {
					sum = "(ejemplos descartados)"
				}
				info := CompactInfo{Scope: "task", Reason: "límite", Before: before, After: ctxmgr.MessagesTokens(convo.Messages), Turns: r.Removed, Summary: sum, Dropped: r.Dropped}
				id := o.OnCompact(ctx, info)
				o.OnEvent(event.Event{Type: "compact", Scope: info.Scope, Reason: info.Reason, Before: info.Before, After: info.After, Turns: info.Turns, ID: id, Summary: info.Summary, Dropped: info.Dropped})
			}
		}
		o.OnEvent(event.Event{Type: "phase", Phase: "think"})
		st, err := react.NextStep(ctx, convo.Messages)
		if err != nil {
			return nil, err
		}

		forced := false
		if st.Done {
			if len(obs) >= planner.MaxSteps {
				break
			}
			next, line, gaps := pending()
			if len(obs) == 0 && next == nil {
				// DONE sin haber hecho nada: si el objetivo nombra un archivo que existe, se empieza leyéndolo
				next, line = firstRead(o.Goal)
			}
			if len(gaps) == 0 && next == nil {
				break
			}
			// DONE sin haber hecho nada, o con los avisos agotados: el paso sugerido se ejecuta tal cual
			if next != nil && (len(obs) == 0 || nudges >= 2) {
				usedSuggestions[line] = true
				st = react.Decision{Step: *next, Line: line}
				forced = true
			} else if nudges < 2 && len(gaps) > 0 {
				nudges++
				var hints []string
				for _, g := range gaps {
					hints = append(hints, g.Hint)
				}
				nudge := "Aún no está cumplido: " + strings.Join(hints, " ")
				if next != nil {
					nudge += " Escribe exactamente este paso: " + line
				}
				nudge += "\nSiguiente paso:"
				convo.Messages = append(convo.Messages,
					llmMessage("assistant", "DONE"),
					llmMessage("user", nudge))
				continue
			} else {
				break
			}
		}

		key := stepKey(st.Step)
		if seen[key] && !forced && st.Step.Tool != "shell" {
			// repetir un paso no avanza: si la cobertura sabe qué falta, se hace eso; si no, se para
			next, line, _ := pending()
			if next == nil {
				halted = "repetí un paso sin avanzar"
				break
			}
			usedSuggestions[line] = true
			st = react.Decision{Step: *next, Line: line}
			key = stepKey(st.Step)
		}
		seen[key] = true
		o.OnEvent(event.Event{Type: "step", Index: n + 1, Total: planner.MaxSteps, Name: st.Step.Tool})

		args := map[string]string{}
		for k, v := range st.Step.Args {
			args[k] = v
		}
		var res tools.Result
		err = nil
		if p, ok := args["path"]; ok {
			args["path"] = CleanPath(p)
		}
		if st.Step.Tool == "fs_list" {
			args["path"] = ListDir(args["path"])
		}
		if st.Step.Tool == "fs_edit" && replaceAll {
			args["all"] = "1"
		}
		if st.Step.Tool == "shell" {
			args["command"] = react.CleanCommand(args["command"])
			if args["command"] == "" || hasFSName(args["command"]) {
				err = errors.New("comando inválido: escribe un comando de Linux")
			} else {
				args["command"] = shell.WithAliases(args["command"])
				if missing := shell.MissingExecutable(args["command"]); missing != "" {
					err = fmt.Errorf("comando no encontrado: %s", missing)
				} else if extra := RmOutsideGoal(o.Goal, args["command"]); extra != "" {
					err = fmt.Errorf("no borres lo que no pidió el usuario (%s): borra solo lo que nombra el objetivo", extra)
				}
			}
		}
		if err == nil {
			o.OnEvent(event.Event{Type: "phase", Phase: "tool", Name: st.Step.Tool})
			o.OnEvent(event.Event{Type: "tool", Name: st.Step.Tool, Args: args})
			res = tools.RunTool(ctx, st.Step.Tool, args, tctx)
			used = append(used, st.Step.Tool+pathSuffix(args))
			if p := args["path"]; p != "" {
				o.OnPath(p)
			}
		} else {
			res = tools.Result{OK: false, Text: "error: " + err.Error()}
		}
		o.OnEvent(event.Event{Type: "tool_result", Name: st.Step.Tool, Args: args, OK: res.OK, Rejected: res.Rejected, Text: res.Text})
		obs = append(obs, summary.Obs{Tool: st.Step.Tool, Args: args, OK: res.OK, Rejected: res.Rejected, Text: res.Text, Raw: res.Raw, Line: st.Line})

		obsText := res.Text
		if res.Rejected && res.Overwrite {
			// no sobrescribir no es «cancela la tarea»: el modelo debe buscar otro camino
			obsText = "el archivo ya existe y NO se sobrescribe. Para añadir al final usa shell: echo 'texto' >> archivo; para cambiar una parte usa fs_edit."
		}
		convo.Messages = append(convo.Messages,
			llmMessage("assistant", st.Line),
			llmMessage("user", react.Observation(res.OK, obsText, 600, st.Step.Tool)))
		if res.Rejected && !res.Overwrite {
			halted = "cancelado por ti"
			break
		}
		if !res.OK {
			if summary.OBSERVING[st.Step.Tool] && !planner.COND_CUE.MatchString(o.Goal) {
				failedBefore = true
				if p := st.Step.Args["path"]; p != "" {
					failedPaths[p] = true
				}
			}
			consecutiveFails++
			if consecutiveFails >= 2 {
				halted = res.Text
				break
			}
		} else {
			consecutiveFails = 0
		}
	}
	return &Result{Obs: obs, Used: used, Halted: halted}, nil
}

func pathSuffix(args map[string]string) string {
	if p := args["path"]; p != "" {
		return "(" + p + ")"
	}
	return ""
}

func stepKey(st planner.Step) string {
	b, _ := json.Marshal([]any{st.Tool, st.Args})
	return string(b)
}

func llmMessage(role, content string) llm.Message {
	return llm.Message{Role: role, Content: content}
}

var fsNameRe = regexp.MustCompile(`\bfs_\w+\b`)

func hasFSName(cmd string) bool { return fsNameRe.MatchString(cmd) }

var (
	pathNoise  = regexp.MustCompile(`\s*[(\[].*$`)
	fileTok    = regexp.MustCompile(`[\w./-]+\.[a-zA-Z]\w{0,7}`)
	fileOnly   = regexp.MustCompile(`^[\w./-]+\.[a-zA-Z]\w{0,7}$`)
	rmRe       = regexp.MustCompile(`(?:^|[;&|]\s*)rm\s+(.*)`)
	replaceRe  = regexp.MustCompile(`(?i)\b(reemplaza\w*|cambia\w*|sustituye\w*)\b`)
	onceRe     = regexp.MustCompile(`(?i)\b(primer[ao]?|segund[ao]|tercer[ao]?|[uú]ltim[ao]|una (sola )?vez|solo un[ao]|s[oó]lo un[ao]|una de)\b`)
	goalFileRe = regexp.MustCompile(`[\w-]+(?:/[\w.-]+)*\.[a-zA-Z]\w{1,7}\b`)
)

// ListDir corrige un fs_list apuntado a un archivo («fs_list: x.txt»): se lista
// su carpeta. Listar un archivo nunca es lo que se quiere.
func ListDir(p string) string {
	if p == "" || !fileOnly.MatchString(p) {
		return p
	}
	if d := filepath.Dir(p); d != "." {
		return d
	}
	return "."
}

// ReplaceAllWanted indica si «reemplaza X por Y» debe tocar todas las apariciones:
// sí, salvo que el objetivo acote («la primera», «solo una vez»…).
func ReplaceAllWanted(goal string) bool {
	return replaceRe.MatchString(goal) && !onceRe.MatchString(goal)
}

// firstRead da el primer paso cuando el modelo responde DONE sin haber hecho nada:
// leer el archivo que nombra el objetivo, si existe (el resto lo decide el modelo
// con la observación delante).
func firstRead(goal string) (*planner.Step, string) {
	for _, f := range goalFileRe.FindAllString(goal, -1) {
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(config.Get().Workdir, f)
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			line := "fs_read: " + f
			return &planner.Step{Tool: "fs_read", Args: map[string]string{"path": f}}, line
		}
	}
	return nil, ""
}

// CleanPath quita lo que los modelos pequeños pegan a una ruta: comillas,
// explicaciones entre paréntesis («frutas.txt. (el nombre incluye el punto)»),
// puntuación final.
func CleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "\"'`«»")
	p = pathNoise.ReplaceAllString(p, "")
	p = strings.TrimRight(p, ".,;:! ")
	if strings.ContainsAny(p, " \t") {
		if f := fileTok.FindString(p); f != "" {
			return f
		}
		return strings.Fields(p)[0]
	}
	return p
}

// RmOutsideGoal devuelve el primer objetivo de un rm que el usuario no nombró
// ("" si todo está pedido). Los comodines valen si el objetivo habla de «todos»
// o de esa extensión.
func RmOutsideGoal(goal, cmd string) string {
	m := rmRe.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	g := strings.ToLower(goal)
	allOK := regexp.MustCompile(`\btod[oa]s?\b`).MatchString(g)
	for _, tok := range strings.Fields(m[1]) {
		if strings.HasPrefix(tok, "-") || tok == "&&" || tok == ";" || tok == "|" {
			continue
		}
		if strings.HasPrefix(tok, "&") || strings.HasPrefix(tok, ";") || strings.HasPrefix(tok, "|") {
			break
		}
		t := strings.Trim(tok, "\"'")
		if strings.ContainsAny(t, "*?") {
			ext := regexp.MustCompile(`\.\w{1,8}$`).FindString(t)
			if allOK || (ext != "" && strings.Contains(g, strings.ToLower(ext))) {
				continue
			}
			return tok
		}
		base := strings.ToLower(strings.TrimSuffix(t, "/"))
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if base == "" || base == "." || strings.Contains(g, base) {
			continue
		}
		return tok
	}
	return ""
}
