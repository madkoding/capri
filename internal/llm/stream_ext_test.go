package llm_test

import (
	"context"
	"strings"
	"testing"

	"capri/internal/config"
	"capri/internal/llm"
	"capri/internal/testutil"
)

// ChatStream entrega el razonamiento y la respuesta según llegan, y devuelve
// la respuesta limpia; las estadísticas salen del último trozo.
func TestChatStreamDeliversDeltas(t *testing.T) {
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	t.Cleanup(func() { config.Reload() })
	t.Setenv("CAPRI_URL", fake.URL)
	config.Reload()

	var think, answer strings.Builder
	fake.Script("<think>déjame ver</think>La última línea es uva.")
	out, err := llm.ChatStream(context.Background(), []llm.Message{{Role: "user", Content: "hola"}}, func(kind, d string) {
		if kind == "think" {
			think.WriteString(d)
		} else {
			answer.WriteString(d)
		}
	})
	if err != nil || out != "La última línea es uva." {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if think.String() != "déjame ver" || answer.String() != "La última línea es uva." {
		t.Errorf("think=%q answer=%q", think.String(), answer.String())
	}
	if reqs := fake.Requests(); len(reqs) != 1 || !reqs[0].Stream {
		t.Errorf("debe pedir stream: %+v", reqs)
	}
	if st := llm.Snapshot(); st.Gen != 42 {
		t.Errorf("velocidad del último trozo: %+v", st)
	}

	// sin callback no hay streaming
	fake.Reset()
	fake.Script("Hola")
	if out, _ := llm.ChatDefault(context.Background(), nil); out != "Hola" || fake.Requests()[0].Stream {
		t.Errorf("ChatDefault no debe pedir stream")
	}
}
