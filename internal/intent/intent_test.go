package intent_test

import (
	"capri/internal/config"
	"testing"

	"capri/internal/intent"
)

func TestSafeIntentQuestionsDoNotMutate(t *testing.T) {
	if got := intent.SafeIntent("rag_update", 0.79, "¿Cómo se llama mi perro?"); got != "none" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("rag_add", 0.9, "cómo me llamo?"); got != "none" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("rag_add", 0.9, "mi perro es Rocky"); got != "rag_add" {
		t.Errorf("got %q", got)
	}
}

func TestSafeIntentThresholds(t *testing.T) {
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "nomic") // umbrales medidos para nomic
	config.Reload()
	if got := intent.SafeIntent("rag_delete", 0.7, "olvida eso"); got != "none" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("rag_delete", 0.8, "olvida eso"); got != "rag_delete" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("fs_read", 0.5, "lee a.txt"); got != "none" {
		t.Errorf("got %q", got)
	}
}

func TestSafeIntentRememberWins(t *testing.T) {
	cases := []struct {
		name  string
		score float64
		txt   string
		want  string
	}{
		{"none", 0.8, "Trabajo como ingeniero en Valdivia, anótalo", "rag_add"},
		{"none", 0.3, "no olvides que mi cumple es el 3 de mayo", "rag_add"},
		{"none", 0.8, "¿puedes recordarme qué te dije?", "none"},
		{"rag_update", 0.67, "Trabajo como ingeniero en Valdivia, anótalo", "rag_add"},
		{"fs_write", 0.9, "crea a.txt y recuérdalo", "fs_write"},
	}
	for _, c := range cases {
		if got := intent.SafeIntent(c.name, c.score, c.txt); got != c.want {
			t.Errorf("SafeIntent(%s, %v, %q) = %q, quería %q", c.name, c.score, c.txt, got, c.want)
		}
	}
}

func TestCleanFact(t *testing.T) {
	cases := map[string]string{
		"Trabajo como ingeniero en Valdivia, anótalo":                  "Trabajo como ingeniero en Valdivia",
		"Hola, me llamo Matías y mi perro se llama Rocky. Recuérdalo.": "Hola, me llamo Matías y mi perro se llama Rocky",
		"recuerda que mi cumple es el 3 de mayo":                       "mi cumple es el 3 de mayo",
		"anótalo":                                                      "anótalo",
	}
	for in, want := range cases {
		if got := intent.CleanFact(in); got != want {
			t.Errorf("CleanFact(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestSafeIntentRagListConfidence(t *testing.T) {
	if got := intent.SafeIntent("rag_list", 0.65, "cuál es la capital de Francia"); got != "none" {
		t.Errorf("got %q", got)
	}
}

func TestSafeIntentEditWithFile(t *testing.T) {
	if got := intent.SafeIntent("none", 0.4, "en suma.py cambia sumar por calcular"); got != "fs_edit" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("none", 0.4, "¿cambia esto por aquello en a.txt?"); got != "none" {
		t.Errorf("got %q", got)
	}
}

func TestSafeIntentCreateFile(t *testing.T) {
	if got := intent.SafeIntent("shell", 0.8, "crea suma.py con una función"); got != "fs_write" {
		t.Errorf("got %q", got)
	}
	if got := intent.SafeIntent("shell", 0.8, "crea una carpeta llamada fotos.v2"); got != "shell" {
		t.Errorf("got %q", got)
	}
}

func TestSafeIntentStatementsDoNotUpdateOrDelete(t *testing.T) {
	cases := []struct {
		name  string
		score float64
		txt   string
		want  string
	}{
		{"rag_update", 0.9, "Me llamo Matías y vivo en Valdivia", "none"},
		{"rag_update", 0.9, "ya no vivo en Santiago, vivo en Valdivia", "rag_update"},
		{"rag_delete", 0.9, "mi gato duerme mucho", "none"},
		{"rag_delete", 0.9, "olvida lo de mi gato", "rag_delete"},
		{"fs_edit", 0.9, "crea b.txt con la palabra casa", "fs_write"},
	}
	for _, c := range cases {
		if got := intent.SafeIntent(c.name, c.score, c.txt); got != c.want {
			t.Errorf("SafeIntent(%s, %v, %q) = %q, quería %q", c.name, c.score, c.txt, got, c.want)
		}
	}
}

func TestNeedsPlan(t *testing.T) {
	for _, txt := range []string{"suma los números de datos.txt", "cuántas palabras tiene a.txt"} {
		if !intent.NeedsPlan(txt) {
			t.Errorf("NeedsPlan(%q) = false", txt)
		}
	}
	for _, txt := range []string{"hola, cómo estás", "me llamo Matías y tengo un perro"} {
		if intent.NeedsPlan(txt) {
			t.Errorf("NeedsPlan(%q) = true", txt)
		}
	}
}

func TestRelevantFor(t *testing.T) {
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "nomic") // umbrales medidos para nomic
	config.Reload()
	cases := []struct {
		sim  float64
		q    string
		want bool
	}{
		{0.55, "cuéntame algo breve sobre las montañas", false},
		{0.54, "¿cuál es la capital de Francia?", false},
		{0.66, "el perro de Matías", true},
		{0.5, "¿cómo se llama mi perro?", true},
		{0.5, "¿qué música me gusta?", true},
		{0.5, "¿dónde vivo?", true},
		{0.5, "qué sabes de mí", true},
		{0.5, "¿de qué hablamos antes?", true},
		{0.4, "¿cómo se llama mi perro?", false},
	}
	for _, c := range cases {
		if got := intent.RelevantFor(c.sim, c.q); got != c.want {
			t.Errorf("RelevantFor(%v, %q) = %v, quería %v", c.sim, c.q, got, c.want)
		}
	}
	if intent.Personal("¿qué es un volcán?") {
		t.Error("Personal(volcán) = true")
	}
}
