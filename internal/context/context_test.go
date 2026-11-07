package context_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"capri/internal/agent"
	"capri/internal/config"
	ctxmgr "capri/internal/context"
	"capri/internal/event"
	"capri/internal/llm"
	"capri/internal/loop"
	"capri/internal/rag"
	"capri/internal/react"
	"capri/internal/skills"
	"capri/internal/testutil"
	"capri/internal/tools"
	"capri/internal/trash"
)

var (
	fake *testutil.FakeLLM
	tmp  string
)

func TestMain(m *testing.M) {
	fake = testutil.StartFakeLLM()
	tmp, _ = os.MkdirTemp("", "ctx-test-")
	os.Setenv("CAPRI_URL", fake.URL)
	os.Setenv("EMBED_URL", "http://127.0.0.1:9")
	os.Setenv("WORKDIR", filepath.Join(tmp, "ws"))
	os.Setenv("RAG_DB", filepath.Join(tmp, "brain.db"))
	os.Setenv("CAPRI_TRACE", "0")
	os.Setenv("CAPRI_GATE", "0")
	os.Setenv("CAPRI_WEB", "0")
	os.Setenv("CAPRI_CTX_WINDOW", "3000")
	os.Setenv("CAPRI_CTX_KEEP", "1")
	config.Reload()
	code := m.Run()
	fake.Close()
	os.RemoveAll(tmp)
	os.Exit(code)
}

var n int

func newRag(t *testing.T) *rag.Rag {
	t.Helper()
	n++
	r, err := rag.Open(filepath.Join(tmp, "rag-"+itoa(n)+".db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func long(c string) string { return strings.Repeat(c, 1500) }

func resetWorld(t *testing.T) {
	fake.Reset()
	if err := os.RemoveAll(filepath.Join(tmp, "ws")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "ws"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEstimateTokens(t *testing.T) {
	if got := ctxmgr.EstimateTokens(strings.Repeat("a", 320)); got != 100 {
		t.Errorf("EstimateTokens = %d", got)
	}
	if got := ctxmgr.MessagesTokens([]llm.Message{{Content: strings.Repeat("a", 320)}}); got != 104 {
		t.Errorf("MessagesTokens = %d", got)
	}
}

func TestDoesNotCompactWhileItFits(t *testing.T) {
	cm := ctxmgr.New(newRag(t), nil)
	cm.Add("hola", "hola, ¿en qué te ayudo?", nil)
	r, err := cm.MaybeCompact(context.Background(), ctxmgr.CompactOptions{Extra: 100})
	if err != nil || r != nil {
		t.Fatalf("MaybeCompact = %+v, %v", r, err)
	}
	if len(cm.Turns) != 1 {
		t.Errorf("turns = %d", len(cm.Turns))
	}
}

func TestCompactSummarizesAndSaves(t *testing.T) {
	fake.Reset()
	r := newRag(t)
	cm := ctxmgr.New(r, nil)
	for _, c := range []string{"a", "b", "c", "d"} {
		var actions []string
		if c == "a" {
			actions = []string{"fs_write(a.txt)"}
		}
		cm.Add("pregunta "+c, long(c), actions)
	}
	before := cm.Used()
	fake.Script("El usuario hizo cuatro preguntas y se creó a.txt.")
	var events []string
	res, err := cm.MaybeCompact(context.Background(), ctxmgr.CompactOptions{
		Extra:   1540,
		OnEvent: func(e event.Event) { events = append(events, e.Type) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("debía compactar")
	}
	if !res.LLM {
		t.Error("res.LLM = false")
	}
	if res.Turns != 3 {
		t.Errorf("res.Turns = %d", res.Turns)
	}
	if res.After >= before/2 {
		t.Errorf("after %d vs before %d", res.After, before)
	}
	if strings.Join(events, ",") != "compacting,compact" {
		t.Errorf("eventos = %#v", events)
	}
	if len(cm.Turns) != 1 || cm.Turns[0].User != "pregunta d" {
		t.Errorf("turns = %+v", cm.Turns)
	}
	if !strings.Contains(cm.Messages()[0].Content, "[Resumen de la conversación hasta ahora]") ||
		!strings.Contains(cm.Messages()[0].Content, "cuatro preguntas") {
		t.Errorf("mensaje resumen = %q", cm.Messages()[0].Content)
	}
	saved := r.List(0, "summary")
	if len(saved) != 1 {
		t.Fatalf("resúmenes = %#v", saved)
	}
	if res.ID == nil || saved[0].ID != *res.ID {
		t.Errorf("id resumen = %v vs %d", res.ID, saved[0].ID)
	}
	row := r.Get(saved[0].ID)
	if row == nil || !strings.Contains(row.Content, "[compactación ") || !strings.Contains(row.Content, "El usuario hizo cuatro preguntas") {
		t.Errorf("contenido = %+v", row)
	}
	reqs := fake.Requests()
	last := reqs[0].Messages[len(reqs[0].Messages)-1].Content
	if !strings.Contains(last, "pregunta a") || !strings.Contains(last, "fs_write(a.txt)") {
		t.Errorf("el LLM no vio las acciones: %q", last)
	}
}

func TestCompactFallsBackWhenLLMFails(t *testing.T) {
	fake.Reset()
	r := newRag(t)
	cm := ctxmgr.New(r, nil)
	for _, c := range []string{"a", "b", "c"} {
		cm.Add("tarea "+c, long(c), []string{"shell"})
	}
	fake.Fail(1)
	res, err := cm.Compact(context.Background(), ctxmgr.CompactOptions{Reason: "límite"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("Compact = nil")
	}
	if res.LLM {
		t.Error("res.LLM = true")
	}
	if !strings.Contains(res.Summary, "- tarea a [shell] →") {
		t.Errorf("summary = %q", res.Summary)
	}
	if got := r.List(0, "summary"); len(got) != 1 {
		t.Errorf("resúmenes = %d", len(got))
	}
}

func TestSuccessiveCompactionsAccumulate(t *testing.T) {
	fake.Reset()
	cm := ctxmgr.New(newRag(t), nil)
	cm.Add("uno", "a", nil)
	cm.Add("dos", "b", nil)
	fake.Script("R1")
	if _, err := cm.Compact(context.Background(), ctxmgr.CompactOptions{}); err != nil {
		t.Fatal(err)
	}
	cm.Add("tres", "c", nil)
	cm.Add("cuatro", "d", nil)
	fake.Script("R2")
	if _, err := cm.Compact(context.Background(), ctxmgr.CompactOptions{}); err != nil {
		t.Fatal(err)
	}
	reqs := fake.Requests()
	last := reqs[1].Messages[len(reqs[1].Messages)-1].Content
	if !strings.Contains(last, "Resumen previo: R1") {
		t.Errorf("segundo resumen = %q", last)
	}
	if cm.Summary != "R2" {
		t.Errorf("Summary = %q", cm.Summary)
	}
	if len(cm.Compactions) != 2 {
		t.Errorf("compactions = %d", len(cm.Compactions))
	}
}

func TestManualCompactAll(t *testing.T) {
	fake.Reset()
	cm := ctxmgr.New(newRag(t), nil)
	cm.Add("solo uno", "x", nil)
	fake.Script("resumen manual")
	res, err := cm.Compact(context.Background(), ctxmgr.CompactOptions{All: true, Reason: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Reason != "manual" {
		t.Fatalf("res = %+v", res)
	}
	if len(cm.Turns) != 0 || cm.Summary != "resumen manual" {
		t.Errorf("turns=%d summary=%q", len(cm.Turns), cm.Summary)
	}
	res, err = cm.Compact(context.Background(), ctxmgr.CompactOptions{All: true})
	if err != nil || res != nil {
		t.Errorf("segundo compact = %+v, %v", res, err)
	}
}

func TestAgentCompactsBeforeOverflow(t *testing.T) {
	fake.Reset()
	cfg := config.Get()
	win0 := cfg.CtxWindow
	cfg.CtxWindow = 3520
	defer func() { cfg.CtxWindow = win0 }()

	r := newRag(t)
	tr, err := trash.New(trash.Options{Dir: filepath.Join(tmp, "trash-a"), Workdir: filepath.Join(tmp, "ws")})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(r, func(string, string, tools.ConfirmOptions) bool { return true }, tr)
	var events []event.Event
	onEvent := func(e event.Event) { events = append(events, e) }
	fake.Script(long("a"), long("b"), long("c"), long("d"))
	for _, q := range []string{"cuéntame algo sobre el mar", "cuéntame algo sobre el cielo", "cuéntame algo sobre el bosque", "cuéntame algo sobre la luna"} {
		if _, err := a.Run(context.Background(), q, onEvent); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range events {
		if e.Type == "compact" {
			t.Fatal("compactó cuando aún cabía")
		}
	}
	fake.Script("Resumen: se habló del mar, el cielo, el bosque y la luna.", "Respuesta final del 5.º turno.")
	out, err := a.Run(context.Background(), "cuéntame algo sobre el río", onEvent)
	if err != nil {
		t.Fatal(err)
	}
	var comp *event.Event
	for i := range events {
		if events[i].Type == "compact" {
			comp = &events[i]
		}
	}
	if comp == nil {
		t.Fatal("debía compactar antes del 5.º turno")
	}
	if comp.Scope != "session" {
		t.Errorf("scope = %q", comp.Scope)
	}
	if out != "Respuesta final del 5.º turno." {
		t.Errorf("out = %q", out)
	}
	if got := r.List(0, "summary"); len(got) != 1 {
		t.Errorf("resúmenes = %d", len(got))
	}
	reqs := fake.Requests()
	last := reqs[len(reqs)-1].Messages
	foundSummary := false
	for _, m := range last {
		if strings.Contains(m.Content, "Resumen de la conversación hasta ahora") {
			foundSummary = true
		}
		if strings.Contains(m.Content, strings.Repeat("a", 300)) {
			t.Error("los turnos viejos siguen en el prompt")
		}
	}
	if !foundSummary {
		t.Error("el prompt no llevaba el resumen")
	}
}

func TestResetKeepsRagSummaries(t *testing.T) {
	fake.Reset()
	r := newRag(t)
	tr, err := trash.New(trash.Options{Dir: filepath.Join(tmp, "trash-b"), Workdir: filepath.Join(tmp, "ws")})
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(r, func(string, string, tools.ConfirmOptions) bool { return true }, tr)
	a.Context.Add("a", "b", nil)
	a.Context.Add("c", "d", nil)
	fake.Script("resumen guardado")
	if _, err := a.Context.Compact(context.Background(), ctxmgr.CompactOptions{}); err != nil {
		t.Fatal(err)
	}
	a.Reset()
	if len(a.Context.Turns) != 0 || a.Context.Summary != "" {
		t.Errorf("contexto no limpio: %+v", a.Context)
	}
	if got := r.List(0, "summary"); len(got) != 1 {
		t.Errorf("resúmenes = %d", len(got))
	}
}

func TestCompactConversation(t *testing.T) {
	lib := skills.LoadSkills(skills.SkillsDir)
	if len(lib) < 2 {
		t.Skip("biblioteca de skills vacía")
	}
	convo := react.StartConversation("revisa todo", "Archivos: a", lib[:2])
	for i := 1; i <= 5; i++ {
		convo.Messages = append(convo.Messages,
			llm.Message{Role: "assistant", Content: "fs_read: f" + itoa(i) + ".txt"},
			llm.Message{Role: "user", Content: "Resultado: contenido " + itoa(i)})
	}
	len0 := len(convo.Messages)
	r := react.CompactConversation(convo, 2, false)
	if r == nil || r.Removed != 3 {
		t.Fatalf("CompactConversation = %+v", r)
	}
	if len(convo.Messages) != len0-6 {
		t.Errorf("len = %d, quería %d", len(convo.Messages), len0-6)
	}
	goal := convo.Messages[convo.GoalIdx].Content
	if !strings.Contains(goal, "Pasos ya hechos (contexto compactado):") || !strings.Contains(goal, "f1.txt") || !strings.Contains(goal, "f3.txt") {
		t.Errorf("objetivo = %q", goal)
	}
	if strings.Contains(goal, "f4.txt") {
		t.Error("los 2 recientes deben seguir literales")
	}
	if convo.Messages[len(convo.Messages)-2].Content != "fs_read: f5.txt" {
		t.Errorf("último paso = %q", convo.Messages[len(convo.Messages)-2].Content)
	}
	shots := convo.Shots
	r2 := react.CompactConversation(convo, 1, true)
	if r2 == nil || !r2.Dropped {
		t.Fatalf("segunda compactación = %+v", r2)
	}
	if convo.Shots != 0 || convo.Messages[0].Role != "system" {
		t.Errorf("shots=%d rol0=%q", convo.Shots, convo.Messages[0].Role)
	}
	if len(convo.Messages) >= len0-shots {
		t.Errorf("len = %d", len(convo.Messages))
	}
	if !strings.Contains(convo.Messages[convo.GoalIdx].Content, "Objetivo: revisa todo") {
		t.Errorf("goalIdx perdido: %q", convo.Messages[convo.GoalIdx].Content)
	}
}

func TestRunReactCompactsLongTask(t *testing.T) {
	fake.Reset()
	resetWorld(t)
	ws := filepath.Join(tmp, "ws")
	for i := 1; i <= 7; i++ {
		if err := os.WriteFile(filepath.Join(ws, "f"+itoa(i)+".txt"), []byte(strings.Repeat("contenido "+itoa(i)+" ", 120)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := ctxmgr.MessagesTokens(react.StartConversation("revisa todo", "", nil).Messages)
	window := int(math.Ceil((float64(base) + 2*230 + 350) / 0.75))
	var script []string
	for i := 1; i <= 7; i++ {
		script = append(script, "fs_read: f"+itoa(i)+".txt")
	}
	script = append(script, "DONE")
	fake.Script(script...)
	var events []event.Event
	var saved []loop.CompactInfo
	res, err := loop.RunReact(context.Background(), loop.Options{
		Goal:    "revisa todo",
		Env:     "",
		Rag:     newRag(t),
		Confirm: func(string, string, tools.ConfirmOptions) bool { return true },
		Window:  window,
		OnEvent: func(e event.Event) { events = append(events, e) },
		OnCompact: func(_ context.Context, c loop.CompactInfo) *int64 {
			saved = append(saved, c)
			id := int64(99)
			return &id
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Halted != "" {
		t.Errorf("Halted = %q", res.Halted)
	}
	ok := 0
	for _, o := range res.Obs {
		if o.OK {
			ok++
		}
	}
	if ok != 7 {
		t.Errorf("pasos ok = %d", ok)
	}
	if len(saved) == 0 {
		t.Fatal("debía compactar al menos una vez")
	}
	var ev *event.Event
	for i := range events {
		if events[i].Type == "compact" {
			ev = &events[i]
		}
	}
	if ev == nil {
		t.Fatal("no hubo evento compact")
	}
	if ev.Scope != "task" || ev.ID == nil || *ev.ID != 99 {
		t.Errorf("evento = %+v", ev)
	}
	if ev.After >= ev.Before {
		t.Errorf("after %d >= before %d", ev.After, ev.Before)
	}
	found := false
	for _, req := range fake.Requests() {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Pasos ya hechos (contexto compactado)") {
				found = true
			}
		}
	}
	if !found {
		t.Error("el prompt no llevaba el resumen de pasos")
	}
}
