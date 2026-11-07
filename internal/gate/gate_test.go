package gate_test

import (
	"context"
	"os"
	"testing"

	"capri/internal/config"
	"capri/internal/gate"
	"capri/internal/testutil"
)

func TestParseGate(t *testing.T) {
	cases := []struct {
		in   string
		kind string
		q    string
	}{
		{"CHARLA", "chat", ""},
		{" ACCION ", "act", ""},
		{"PREGUNTA: ¿Qué archivo quieres borrar?", "ask", "¿Qué archivo quieres borrar?"},
	}
	for _, c := range cases {
		d := gate.ParseGate(c.in)
		if d == nil || d.Kind != c.kind || d.Question != c.q {
			t.Errorf("ParseGate(%q) = %+v", c.in, d)
		}
	}
	for _, in := range []string{"quizás", ""} {
		if d := gate.ParseGate(in); d != nil {
			t.Errorf("ParseGate(%q) = %+v, quería nil", in, d)
		}
	}
}

func TestDecideUsesModelAndFallsBack(t *testing.T) {
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	os.Setenv("CAPRI_URL", fake.URL)
	os.Setenv("CAPRI_GATE", "1")
	config.Reload()

	fake.Script("CHARLA")
	d, err := gate.Decide(context.Background(), "y tus tools cuales son?", "")
	if err != nil || d == nil || d.Kind != "chat" {
		t.Fatalf("Decide = %+v, %v", d, err)
	}
	fake.Fail(1)
	d, err = gate.Decide(context.Background(), "lista los archivos", "")
	if err != nil {
		t.Fatalf("Decide devolvió error: %v", err)
	}
	if d != nil {
		t.Errorf("Decide tras fallo = %+v, quería nil", d)
	}
}

func TestParseGateRejectsTemplateEcho(t *testing.T) {
	for _, s := range []string{
		"PREGUNTA: <pregunta corta> → quiere algo pero falta un dato imprescindible (qué archivo, qué texto, qué carpeta) o el mensaje es ambiguo.",
		"PREGUNTA: ¿qué pasa → falta un dato?",
	} {
		if d := gate.ParseGate(s); d != nil {
			t.Errorf("%q debe descartarse, dio %+v", s, d)
		}
	}
	if d := gate.ParseGate("PREGUNTA: ¿Qué archivo quieres borrar?"); d == nil || d.Kind != "ask" {
		t.Errorf("una pregunta real debe pasar: %+v", d)
	}
}

func TestAskWithObjectBecomesAct(t *testing.T) {
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	os.Setenv("CAPRI_URL", fake.URL)
	os.Setenv("CAPRI_GATE", "1")
	config.Reload()
	fake.Script("PREGUNTA: ¿Qué archivo quieres que liste?", "PREGUNTA: ¿Qué quieres que arregle?")
	d, err := gate.Decide(context.Background(), "lista los archivos", "")
	if err != nil || d == nil || d.Kind != "act" {
		t.Fatalf("con objeto debe ser act: %+v %v", d, err)
	}
	d, err = gate.Decide(context.Background(), "arregla eso", "")
	if err != nil || d == nil || d.Kind != "ask" {
		t.Fatalf("sin objeto se respeta la pregunta: %+v %v", d, err)
	}
}
