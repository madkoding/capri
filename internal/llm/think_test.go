package llm_test

import (
	"context"
	"testing"

	"capri/internal/config"
	"capri/internal/llm"
	"capri/internal/testutil"
)

// Si el razonamiento se come todo el presupuesto y no queda respuesta, se
// repite la llamada sin pensar en vez de devolver «(sin respuesta)».
func TestRetriesWithoutThinkingWhenAnswerIsEmpty(t *testing.T) {
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	t.Cleanup(func() { config.Reload() })
	t.Setenv("CAPRI_URL", fake.URL)
	t.Setenv("LLM_THINKING", "1")
	t.Setenv("LLM_THINK_BUDGET", "100")
	t.Setenv("MAX_TOKENS", "50")
	config.Reload()

	var notes []string
	llm.SetReasoningHook(func(r string) { notes = append(notes, r) })
	defer llm.SetReasoningHook(nil)

	fake.Script("<think>El usuario pregunta… déjame pensar… y pensar…", "Un modelo heretic es…")
	out, err := llm.ChatDefault(context.Background(), []llm.Message{{Role: "user", Content: "qué es heretic"}})
	if err != nil || out != "Un modelo heretic es…" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	reqs := fake.Requests()
	if len(reqs) != 2 {
		t.Fatalf("peticiones = %d", len(reqs))
	}
	if reqs[0].MaxTokens != 150 || reqs[0].ChatTemplateKwargs["enable_thinking"] != true {
		t.Errorf("primera: %+v", reqs[0])
	}
	if reqs[1].MaxTokens != 50 || reqs[1].ChatTemplateKwargs["enable_thinking"] != false {
		t.Errorf("reintento sin pensar: %+v", reqs[1])
	}
	if len(notes) < 2 {
		t.Errorf("debe avisar de que el razonamiento se agotó: %v", notes)
	}

	// con respuesta a la primera no hay reintento
	fake.Reset()
	fake.Script("<think>breve</think>Hola")
	if out, _ := llm.ChatDefault(context.Background(), nil); out != "Hola" || len(fake.Requests()) != 1 {
		t.Errorf("sin reintento: %q %d", out, len(fake.Requests()))
	}
}
