package intent

import (
	"testing"

	"capri/internal/config"
)

func TestActionCueAndHasObject(t *testing.T) {
	cue := map[string]bool{
		"hola capri": false,
		"hola capri, sabes de qué se tratan los modelos de ia que tienen un \"heretic\"?": false,
		"qué modelo usas?": false,
		"si no lo sabes deberías buscarlo en la web o no?": false,
		"hola, ¿qué tal? y luego dime qué es Linux":        false,
		"qué es docker":                            false,
		"lista los archivos":                       true,
		"crea notas.txt con hola":                  true,
		"cuántos archivos .txt hay":                true,
		"qué hora es":                              true,
		"borra todos los archivos temporales .tmp": true,
		"ejecuta el comando echo hola y guarda la salida en salida.txt": true,
		"qué hay en esta carpeta":                                       true,
		"git status":                                                    true,
	}
	for txt, want := range cue {
		if got := ActionCue(txt); got != want {
			t.Errorf("ActionCue(%q) = %v, quiero %v", txt, got, want)
		}
	}
	obj := map[string]bool{
		"lista los archivos":                       true,
		"borra todos los archivos temporales .tmp": true,
		"reemplaza gato por pez en a.txt":          true,
		"borra el archivo":                         true, // nombra «archivo»: el gate no debe preguntar «¿qué archivo?»… pero sí es ambiguo
		"arregla eso":                              false,
		"haz lo de antes":                          false,
	}
	for txt, want := range obj {
		if got := HasObject(txt); got != want {
			t.Errorf("HasObject(%q) = %v, quiero %v", txt, got, want)
		}
	}
}

func TestSafeIntentRequiresCueAndMemoryTalk(t *testing.T) {
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "nomic")
	config.Reload()
	if got := SafeIntent("shell", 0.93, "hola capri"); got != "none" {
		t.Errorf("saludo → shell debe caer a none, dio %s", got)
	}
	if got := SafeIntent("fs_write", 0.9, "hola, ¿qué tal? y luego dime qué es Linux"); got != "none" {
		t.Errorf("charla → fs_write debe caer a none, dio %s", got)
	}
	if got := SafeIntent("shell", 0.84, "cuántos archivos .txt hay"); got != "shell" {
		t.Errorf("con pista debe mantenerse, dio %s", got)
	}
	if got := SafeIntent("rag_list", 0.9, "lee noexiste.txt y si no existe créalo"); got != "none" {
		t.Errorf("rag_list sin hablar de memoria → none, dio %s", got)
	}
	if got := SafeIntent("rag_list", 0.9, "muestra todo lo que has guardado en tu memoria"); got != "rag_list" {
		t.Errorf("rag_list con memoria debe mantenerse, dio %s", got)
	}
	// guardar en memoria: solo con orden o dato personal
	for _, s := range []string{"parece que no hiciste nada", "y la busqueda?", "eso está mal"} {
		if got := SafeIntent("rag_add", 0.6, s); got != "none" {
			t.Errorf("%q no es un recuerdo, dio %s", s, got)
		}
	}
	for _, s := range []string{"anota que el servidor es 10.0.0.5", "me llamo Ana", "guarda esto: la clave es 1234", "mi perro se llama Rocky"} {
		if got := SafeIntent("rag_add", 0.6, s); got != "rag_add" {
			t.Errorf("%q sí es un recuerdo, dio %s", s, got)
		}
	}
}

func TestNeedsPlanRequiresCue(t *testing.T) {
	if NeedsPlan("hola, ¿qué tal? y luego dime qué es Linux") {
		t.Error("conector sin pista de acción no debe disparar el bucle")
	}
	if !NeedsPlan("crea a.txt que diga hola y luego léelo") {
		t.Error("conector con archivo sí")
	}
	if !NeedsPlan("cuántos archivos hay") {
		t.Error("pregunta de entorno sí")
	}
}

func TestStripGreeting(t *testing.T) {
	if got := StripGreeting("hola capri, crea notas.txt"); got != "crea notas.txt" {
		t.Errorf("%q", got)
	}
	if got := StripGreeting("hola"); got != "hola" {
		t.Errorf("un saludo solo se conserva: %q", got)
	}
}
