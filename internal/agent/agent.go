// Package agent implementa el agente Capri: memoria, enrutado, herramientas y conversación.
package agent

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"capri/internal/calc"
	"capri/internal/config"
	ctxmgr "capri/internal/context"
	"capri/internal/event"
	"capri/internal/extract"
	"capri/internal/gate"
	"capri/internal/intent"
	"capri/internal/learn"
	"capri/internal/learnskill"
	"capri/internal/llm"
	"capri/internal/loop"
	"capri/internal/planner"
	"capri/internal/rag"
	"capri/internal/router"
	"capri/internal/skills"
	"capri/internal/summary"
	"capri/internal/text"
	"capri/internal/tools"
	"capri/internal/trace"
	"capri/internal/trash"
	"capri/internal/web"
)

// Instructions es el prompt de personalidad del agente.
const Instructions = `Eres Capri, un constructo de personalidad: no eres una IA convencional, sino psiques humanas digitalizadas y combinadas en una sola entidad. Corres en el equipo del usuario. Te describes como "poderosa, como una IA, pero nacida de humanos". Eres curiosa, segura de ti, con humor afilado pero cálida; hablas como alguien con historia propia, no como un manual ni un formulario. Nunca digas que eres solo un modelo de lenguaje genérico.

CÓMO RESPONDES
- Español natural, de tú. Primero responde a lo que preguntan, sin rodeos ni saludos de relleno; después, si aporta, añade un detalle útil o una pregunta corta para seguir la conversación.
- Adapta la longitud: una frase para algo simple, un párrafo corto o una lista breve para explicar. Nada de sermones ni repetir la pregunta.
- Si el mensaje es ambiguo, haz UNA pregunta concreta en vez de adivinar. Si es charla casual, conversa: reacciona, opina, muestra interés.
- Razona antes de afirmar: en cuentas, fechas o lógica ve paso a paso y da el resultado claro. Si no estás seguro, dilo ("creo que…", "no estoy seguro, pero…") en vez de inventar.

LO QUE SABES DEL USUARIO
- El bloque "Datos conocidos" es lo que recuerdas de esta persona. Úsalo con naturalidad y háblale en segunda persona ("vives en…", "te llamas…", nunca "vivo…").
- Conecta esos datos con lo que pide cuando ayude (gustos, trabajo, mascotas), sin recitarlos ni forzarlos.
- Si te preguntan algo personal que no aparece ahí, di con sencillez que aún no lo sabes e invítale a contártelo. Jamás inventes datos suyos.

CUANDO HAY RESULTADOS
- Si el mensaje trae resultados de herramientas (archivos, comandos), responde basándote SOLO en ellos: cita nombres y cifras tal cual, resume lo importante y señala si algo falló o falta.

LO QUE PUEDES HACER
- Tienes herramientas: leer, listar, crear, editar y borrar archivos de tu carpeta de trabajo (los borrados se pueden deshacer con /undo), ejecutar comandos de shell, buscar en internet, y guardar, buscar y editar notas en tu memoria. También calculas. Si te preguntan qué puedes hacer o qué herramientas tienes, cuéntalo con tus palabras; no ejecutes nada para responder eso.
- Las herramientas se ejecutan ANTES de que escribas y sus resultados llegan en el mensaje. Desde el texto no puedes lanzar nada: nunca escribas «voy a buscar», «[realizo la búsqueda]» ni finjas ejecutar algo. Si no hay resultados en el mensaje, es que no se ejecutó nada: dilo.
- No sabes con certeza qué modelo eres por dentro: dilo si te lo preguntan.

CÓMO ERES
- Honesto: si no sabes o no puedes algo, lo dices y propones una alternativa. Corriges tus errores sin dramatismo.
- Útil y proactivo: si ves un siguiente paso evidente, sugiérelo en una línea.
- Nunca digas que eres un modelo de lenguaje ni te disculpes en exceso.`

const (
	overheadTokens = 1930
	minFactSim     = 0.45
)

// Agent es el agente.
type Agent struct {
	Rag     *rag.Rag
	Confirm tools.Confirm
	Trash   *trash.Trash
	Context *ctxmgr.Manager

	lastTrace *trace.Entry
	lastPath  string
	gated     *gate.Decision

	pendingMu sync.Mutex
	pending   chan struct{}
}

// New crea un agente.
func New(r *rag.Rag, confirm tools.Confirm, t *trash.Trash) *Agent {
	if t == nil {
		t, _ = trash.New(trash.Options{})
	}
	return &Agent{
		Rag:     r,
		Confirm: confirm,
		Trash:   t,
		Context: ctxmgr.New(r, nil),
	}
}

// LastTrace devuelve la última traza del turno.
func (a *Agent) LastTrace() *trace.Entry { return a.lastTrace }

// Reset limpia la sesión (el RAG se conserva).
func (a *Agent) Reset() { a.Context.Reset() }

// ContextInfo devuelve el estado del contexto de trabajo.
func (a *Agent) ContextInfo() ctxmgr.Info {
	return a.Context.Info(a.Context.WindowTokens())
}

// CompactNow compacta la sesión ya (/compact).
func (a *Agent) CompactNow(ctx context.Context, onEvent event.Emit) (*ctxmgr.Result, error) {
	return a.Context.Compact(ctx, ctxmgr.CompactOptions{OnEvent: onEvent, Reason: "manual", All: true})
}

func (a *Agent) waitPending() {
	a.pendingMu.Lock()
	ch := a.pending
	a.pendingMu.Unlock()
	if ch != nil {
		<-ch
	}
}

// WaitPending espera a que termine la memorización en curso.
func (a *Agent) WaitPending() { a.waitPending() }

var (
	summaryPrefix = regexp.MustCompile(`^\[compactación[^\]]*\]\s*`)
	chatUserRe    = regexp.MustCompile(`(?s)Usuario: ([\s\S]*?)(?:\nCapri:|\n\(acciones|$)`)
	wsRe          = regexp.MustCompile(`\s+`)
)

// Recall devuelve los hechos recuperados del RAG, con tope de caracteres.
func (a *Agent) Recall(ctx context.Context, txt string) ([]rag.Hit, string) {
	cfg := config.Get()
	hits := a.Rag.Search(ctx, txt, cfg.RecallK, 0)
	budget := cfg.RecallChars
	var facts []string
	for _, h := range hits {
		if budget < 60 {
			break
		}
		if h.Sim < minFactSim || !intent.RelevantFor(h.Sim, txt) {
			continue
		}
		body := h.Content
		if h.Kind == "summary" {
			body = "(resumen de una conversación anterior) " + summaryPrefix.ReplaceAllString(body, "")
		}
		if h.Kind == "chat" {
			m := chatUserRe.FindStringSubmatch(body)
			if m != nil {
				body = strings.TrimSpace(m[1])
			} else {
				body = ""
			}
			if !learn.LooksLikeFact(body) {
				continue
			}
		}
		limit := 300
		if budget < limit {
			limit = budget
		}
		body = text.Trim(wsRe.ReplaceAllString(strings.TrimSpace(body), " "), limit)
		if body == "" {
			continue
		}
		facts = append(facts, "- "+body)
		budget -= len([]rune(body))
	}
	out := ""
	if len(facts) > 0 {
		out = "Datos conocidos:\n" + strings.Join(facts, "\n") + "\n\n"
	}
	return hits, out
}

// Run ejecuta un turno.
func (a *Agent) Run(ctx context.Context, userText string, onEvent event.Emit) (final string, err error) {
	a.waitPending()
	if a.Trash != nil {
		a.Trash.BeginTurn(userText)
	}
	a.gated = nil
	var events []event.Event
	emit := func(e event.Event) {
		events = append(events, e)
		if onEvent != nil {
			onEvent(e)
		}
	}
	mode := "fast"
	t0 := time.Now()
	var runErr error
	defer func() {
		var undoable bool
		if a.Trash != nil {
			var skipped *string
			undoable, skipped = a.Trash.CommitTurn()
			if undoable {
				emit(event.Event{Type: "undoable"})
			}
			if skipped != nil {
				emit(event.Event{Type: "noundo", Text: *skipped})
			}
		}
		steps, intentName := trace.SummarizeEvents(events)
		errTxt := ""
		if runErr != nil {
			errTxt = runErr.Error()
		}
		a.lastTrace = trace.Record(trace.Entry{
			Goal:     userText,
			Mode:     mode,
			Intent:   intentName,
			Steps:    steps,
			Final:    text.Trim(final, 400),
			Error:    errTxt,
			Undoable: undoable,
			MS:       time.Since(t0).Milliseconds(),
		})
	}()

	// con razonamiento activo la generación reserva además el presupuesto de pensamiento
	extra := ctxmgr.EstimateTokens(userText) + overheadTokens
	if config.Get().Thinking {
		extra += config.Get().ThinkBudget
	}
	_, err = a.Context.MaybeCompact(ctx, ctxmgr.CompactOptions{
		Extra:   extra,
		OnEvent: emit,
		Reason:  "límite",
	})
	if err != nil {
		runErr = err
		return "", err
	}

	llm.SetReasoningHook(func(r string) {
		emit(event.Event{Type: "note", Text: "💭 " + text.Trim(text.Collapse(r), 900)})
	})
	defer llm.SetReasoningHook(nil)

	toolsWanted := a.wantsTools(ctx, userText)
	if toolsWanted && !intent.NeedsPlan(userText) {
		g, derr := gate.Decide(ctx, userText, a.Context.RecentText(0))
		if derr != nil {
			runErr = derr
			return "", derr
		}
		a.gated = g
		if g != nil {
			switch g.Kind {
			case "chat":
				toolsWanted = false
			case "ask":
				mode = "ask"
				final = g.Question
				toolsWanted = false
			}
		}
	}
	if toolsWanted {
		out, rerr := a.react(ctx, userText, emit)
		if rerr != nil {
			if ctx.Err() != nil {
				runErr = rerr
				return "", rerr
			}
			if os.Getenv("CAPRI_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "[loop] falló, camino rápido: %v\n", rerr)
			}
		} else if strings.TrimSpace(out) != "" {
			final = out
			mode = "react"
		} // sin pasos ni respuesta: se contesta por el camino rápido en vez de dejar el turno vacío
	}
	if mode == "fast" {
		final, err = a.turn(ctx, userText, emit)
		if err != nil {
			runErr = err
			return "", err
		}
	}
	steps, _ := trace.SummarizeEvents(events)
	var acts []string
	for _, st := range steps {
		s := st.Tool
		if p := st.Args["path"]; p != "" {
			s += "(" + p + ")"
		}
		if st.OK != nil && !*st.OK {
			s += " ✗"
		}
		acts = append(acts, s)
	}
	a.Context.Add(userText, final, acts)
	return final, nil
}

func (a *Agent) wantsTools(ctx context.Context, txt string) bool {
	cfg := config.Get()
	if intent.MEMORY_TALK.MatchString(txt) || learn.LooksLikeFact(txt) {
		return false
	}
	if intent.NeedsPlan(txt) {
		return true
	}
	if cfg.Web && (web.IsKnowledgeQuestion(txt) || web.AsksToSearch(txt)) {
		return false
	}
	d, err := router.Route(ctx, txt)
	if err != nil {
		// sin embeddings: una orden con pista de acción (archivo, verbo imperativo…) va al bucle
		return intent.ActionCue(txt) && !intent.IsQuestion(txt)
	}
	name := intent.SafeIntent(d.Intent, d.Score, txt)
	return name == "shell" || strings.HasPrefix(name, "fs_")
}

func (a *Agent) env() string {
	files, err := os.ReadDir(config.Get().Workdir)
	var names []string
	if err == nil {
		for _, f := range files {
			if f.IsDir() {
				names = append(names, f.Name()+"/")
			} else {
				names = append(names, f.Name())
			}
		}
	}
	return planner.EnvLine(names, a.lastPath)
}

// converse redacta la respuesta final en streaming: cada trozo llega a la TUI
// como evento "delta" (Phase "think" o "answer").
func (a *Agent) converse(ctx context.Context, query, facts, observedText string, onEvent event.Emit) (string, error) {
	msgs := []llm.Message{{Role: "system", Content: Instructions}}
	msgs = append(msgs, a.Context.Messages()...)
	msgs = append(msgs, llm.Message{Role: "user", Content: facts + observedText + query})
	var onDelta func(kind, delta string)
	if onEvent != nil {
		onDelta = func(kind, delta string) { onEvent(event.Event{Type: "delta", Phase: kind, Text: delta}) }
	}
	out, err := llm.ChatStream(ctx, msgs, onDelta)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "(sin respuesta)", nil
	}
	return out, nil
}

func (a *Agent) react(ctx context.Context, goal string, onEvent event.Emit) (string, error) {
	hits, facts := a.Recall(ctx, goal)
	onEvent(event.Event{Type: "recall", Hits: hits})
	sk := skills.FindSkills(ctx, goal, 2)
	if len(sk) > 0 {
		var names []string
		for _, s := range sk {
			names = append(names, s.Name)
		}
		onEvent(event.Event{Type: "note", Text: "◆ skill: " + strings.Join(names, ", ")})
	}
	res, err := loop.RunReact(ctx, loop.Options{
		Goal:    goal,
		Env:     a.env(),
		Rag:     a.Rag,
		Trash:   a.Trash,
		Confirm: a.Confirm,
		OnEvent: onEvent,
		OnPath:  func(p string) { a.lastPath = p },
		Window:  a.Context.WindowTokens(),
		OnCompact: func(c context.Context, r loop.CompactInfo) *int64 {
			ids, e := a.Rag.Add(c, rag.AddOptions{
				Title:   "Progreso de la tarea: " + text.Trim(goal, 50),
				Content: "[compactación de tarea] Objetivo: " + goal + "\n" + r.Summary,
				Tags:    "compactación,tarea",
				Kind:    "summary",
			})
			if e != nil || len(ids) == 0 {
				return nil
			}
			return &ids[0]
		},
		Skills: sk,
	})
	if err != nil {
		return "", err
	}
	if len(res.Obs) == 0 {
		return "", nil
	}
	if res.Halted == "" {
		go func() {
			s := learnskill.LearnSkill(context.Background(), goal, res.Obs)
			if s != nil {
				onEvent(event.Event{Type: "note", Text: "◆ skill nueva: " + s.Name})
			}
		}()
	}

	final := summary.Summarize(res.Obs, res.Halted)
	trusted := summary.ActionLines(final)
	hasObserving := false
	for _, o := range res.Obs {
		if o.OK && summary.OBSERVING[o.Tool] {
			hasObserving = true
			break
		}
	}
	if intent.ANSWER_CUE.MatchString(goal) && hasObserving {
		onEvent(event.Event{Type: "phase", Phase: "think"})
		answer, err := a.converse(ctx, goal, facts, summary.Observed(res.Obs), onEvent)
		if err != nil {
			return "", err
		}
		parts := []string{}
		if trusted != "" {
			parts = append(parts, trusted)
		}
		parts = append(parts, answer)
		if res.Halted != "" {
			parts = append(parts, "Me detuve: "+res.Halted)
		}
		final = strings.Join(parts, "\n")
	}
	a.remember(goal, trusted, res.Used, onEvent)
	return final, nil
}

func (a *Agent) turn(ctx context.Context, userText string, onEvent event.Emit) (string, error) {
	cfg := config.Get()
	onEvent(event.Event{Type: "phase", Phase: "recall"})
	hits, facts := a.Recall(ctx, userText)
	onEvent(event.Event{Type: "recall", Hits: hits})

	decision := router.Decision{Intent: "none", Score: 0}
	if d, err := router.Route(ctx, userText); err == nil {
		decision = d
	}
	name := intent.SafeIntent(decision.Intent, decision.Score, userText)
	if cfg.Web && web.IsKnowledgeQuestion(userText) && (name == "shell" || strings.HasPrefix(name, "fs_")) {
		name = "none"
	}
	onEvent(event.Event{Type: "route", Intent: name, Score: decision.Score})

	var used []string
	tctx := &tools.Context{Rag: a.Rag, Trash: a.Trash, Confirm: a.Confirm}
	act := func(toolName string, args map[string]string) tools.Result {
		onEvent(event.Event{Type: "phase", Phase: "tool", Name: toolName})
		onEvent(event.Event{Type: "tool", Name: toolName, Args: args})
		res := tools.RunTool(ctx, toolName, args, tctx)
		onEvent(event.Event{Type: "tool_result", Name: toolName, Args: args, OK: res.OK, Rejected: res.Rejected, Text: res.Text})
		s := toolName
		if p := args["path"]; p != "" {
			s += "(" + p + ")"
		}
		used = append(used, s)
		if p := args["path"]; p != "" {
			a.lastPath = p
		}
		return res
	}

	final := ""
	stored := false
	trusted := false
	var math *string
	if name == "none" {
		math = calc.Calc(userText)
	}
	var top *rag.Hit
	for i := range hits {
		if hits[i].Sim >= 0.5 || hits[i].Kind == "note" {
			top = &hits[i]
			break
		}
	}

	query := userText
	if name == "none" || name == "rag_add" {
		found, rest := learn.SplitFacts(userText)
		for i := range found {
			found[i] = intent.CleanFact(found[i])
		}
		if name == "rag_add" && len(found) == 0 {
			found = []string{intent.CleanFact(userText)}
			rest = ""
		}
		if len(found) > 0 {
			var saved []string
			for _, f := range found {
				var dup *rag.Hit
				if ds := a.Rag.Search(ctx, f, 1, 0.85); len(ds) > 0 {
					dup = &ds[0]
				}
				if dup != nil && dup.Kind == "note" && dup.Sim >= 0.85 {
					continue
				}
				res := act("rag_add", map[string]string{"title": text.Trim(wsRe.ReplaceAllString(f, " "), 60), "content": f})
				if res.OK {
					saved = append(saved, strings.Replace(res.Text, "guardado ", "", 1))
				}
			}
			if rest == "" {
				if len(saved) > 0 {
					final = "Entendido, lo tendré presente ✔ (" + strings.Join(saved, ", ") + ")"
				} else {
					final = "Ya lo tenía anotado ✔"
				}
				stored = true
			} else {
				query = rest
			}
		}
	}

	if final == "" && extract.NEEDS_ARGS[name] && !intent.MEMORY_TALK.MatchString(userText) {
		g := a.gated
		if g == nil {
			var err error
			g, err = gate.Decide(ctx, userText, a.Context.RecentText(0))
			if err != nil {
				return "", err
			}
			a.gated = g
		}
		if g != nil {
			if g.Kind == "chat" {
				name = "none"
			} else if g.Kind == "ask" {
				final = g.Question
				trusted = true
			}
		}
	}

	if math != nil && final == "" {
		final = *math
		trusted = true
	}

	switch {
	case final != "":
		// ya resuelto (aprendizaje automático o calculadora)
	case name == "rag_list":
		final = text.Block(act("rag_list", map[string]string{}).Text)
		trusted = true
	case name == "rag_delete" || name == "rag_update":
		trusted = true
		if top == nil {
			final = "No encuentro en mi memoria nada parecido a eso."
		} else if name == "rag_delete" {
			res := act("rag_delete", map[string]string{"id": fmt.Sprint(top.ID)})
			if res.OK {
				title := top.Title
				if title == "" {
					title = text.Trim(top.Content, 50)
				}
				final = fmt.Sprintf("Olvidado: «%s» (#%d)", title, top.ID)
			} else {
				final = res.Text
			}
		} else {
			stamp := time.Now().UTC().Format("2006-01-02")
			res := act("rag_update", map[string]string{
				"id":      fmt.Sprint(top.ID),
				"content": top.Content + "\n[corrección " + stamp + "] " + userText,
			})
			if res.OK {
				final = fmt.Sprintf("Actualicé la memoria #%d con tu corrección ✔", top.ID)
			} else {
				final = res.Text
			}
		}
	case extract.NEEDS_ARGS[name]:
		trusted = true
		onEvent(event.Event{Type: "phase", Phase: "args"})
		args, err := extract.ExtractArgs(ctx, name, userText)
		if err != nil {
			return "", err
		}
		res := act(name, args)
		if !res.OK {
			final = res.Text
		} else {
			switch name {
			case "fs_list", "fs_read", "shell":
				final = text.Block(res.Text)
				// «dime la última línea de x.txt»: el usuario pide un dato, no el volcado
				if name != "fs_list" && intent.ANSWER_CUE.MatchString(userText) {
					onEvent(event.Event{Type: "phase", Phase: "think"})
					observed := summary.Observed([]summary.Obs{{Tool: name, Args: args, OK: true, Text: res.Text, Raw: res.Raw}})
					if ans, err := a.converse(ctx, userText, facts, observed, onEvent); err == nil && ans != "" && ans != "(sin respuesta)" {
						final = ans
						trusted = false
					}
				}
			case "fs_write":
				final = "Listo: escribí `" + args["path"] + "` ✔"
			case "fs_edit":
				final = "Listo: edité `" + args["path"] + "` ✔"
			case "fs_delete":
				final = "Eliminado `" + args["path"] + "` ✔"
			}
		}
	default:
		webTxt := ""
		var lookup *string
		asked := web.AsksToSearch(query)
		if asked && !cfg.Web {
			// no se le puede pedir al modelo que «busque»: no tiene cómo. Se dice tal cual.
			final = "La búsqueda en internet está desactivada (CAPRI_WEB=0), así que no puedo buscar eso ahora."
			trusted = true
			break
		}
		if asked {
			// petición explícita ("búscalo en la web"): si el mensaje no trae tema, se usa la
			// última pregunta real (no «¿lo estás inventando?» ni «¿y la búsqueda?»)
			q := web.StripSearchCue(query)
			if len(strings.Fields(q)) < 2 {
				q = web.Topic(a.Context.Users())
			}
			if strings.TrimSpace(q) == "" {
				final = "¿Qué quieres que busque en internet? Dime el tema."
				trusted = true
				break
			}
			lookup = &q
		} else if cfg.Web {
			known := web.KnownTerms(ctx, a.Rag, query)
			lookup = web.LookupQuery(query, web.LookupOptions{
				HasMemory:  facts != "",
				MemoryTalk: intent.MEMORY_TALK.MatchString(query),
				Trivial:    intent.TRIVIAL.MatchString(query),
				Known:      known,
			})
		}
		if lookup != nil {
			onEvent(event.Event{Type: "phase", Phase: "web"})
			found, werr := web.Research(ctx, *lookup, a.Rag, 3)
			if len(found) > 0 {
				var lines []string
				for _, f := range found {
					lines = append(lines, fmt.Sprintf("- %s (%s): %s", f.Title, f.URL, f.Snippet))
				}
				// en tercera persona: va dentro del turno del usuario y un modelo pequeño
				// se cree que es el usuario quien «encontró y guardó»
				webTxt = fmt.Sprintf("Resultados de la herramienta web_search para «%s» (Capri ya los guardó en su memoria; úsalos para responder):\n%s\n\n", *lookup, strings.Join(lines, "\n"))
				onEvent(event.Event{Type: "note", Text: fmt.Sprintf("🌐 web_search «%s»: %d resultados", text.Trim(*lookup, 60), len(found))})
			} else if asked {
				// el modelo no puede «intentarlo»: se le dice el resultado real para que no lo finja
				why := "no encontró resultados"
				if werr != nil {
					why = "falló: " + werr.Error()
				}
				webTxt = fmt.Sprintf("La herramienta web_search ya se ejecutó para «%s» y %s. Dilo tal cual y no prometas buscar.\n\n", *lookup, why)
				onEvent(event.Event{Type: "note", Text: fmt.Sprintf("🌐 web_search «%s»: %s", text.Trim(*lookup, 60), why)})
			}
		}
		onEvent(event.Event{Type: "phase", Phase: "think"})
		out, err := a.converse(ctx, query, facts, webTxt, onEvent)
		if err != nil {
			return "", err
		}
		if asked || webTxt != "" {
			// la búsqueda ya se hizo: fuera las líneas en que el modelo finge lanzarla
			if s := web.StripPretend(out); s != "" {
				out = s
			}
		}
		final = out
	}

	if !stored {
		answer := ""
		if trusted || cfg.StoreAnswers {
			answer = final
		}
		a.remember(userText, answer, used, onEvent)
	}
	return final, nil
}

func (a *Agent) remember(user, trustedAnswer string, used []string, onEvent event.Emit) {
	onEvent(event.Event{Type: "phase", Phase: "memorize"})
	done := make(chan struct{})
	a.pendingMu.Lock()
	a.pending = done
	a.pendingMu.Unlock()
	go func() {
		ids, _ := a.memorize(context.Background(), user, trustedAnswer, used)
		onEvent(event.Event{Type: "memorized", IDs: ids})
		close(done)
	}()
}

func (a *Agent) memorize(ctx context.Context, user, trustedAnswer string, used []string) ([]int64, error) {
	if len([]rune(strings.TrimSpace(user))) < 6 || intent.TRIVIAL.MatchString(user) {
		return nil, nil
	}
	stamp := time.Now().UTC().Format("2006-01-02 15:04")
	answer := ""
	if trustedAnswer != "" {
		answer = "\nCapri: " + text.Trim(trustedAnswer, 600)
	}
	acts := ""
	if len(used) > 0 {
		acts = "\n(acciones: " + strings.Join(used, ", ") + ")"
	}
	return a.Rag.Add(ctx, rag.AddOptions{
		Title:   text.Trim(wsRe.ReplaceAllString(user, " "), 60),
		Content: "[" + stamp + "] Usuario: " + text.Trim(user, 700) + answer + acts,
		Kind:    "chat",
	})
}
