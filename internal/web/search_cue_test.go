package web

import "testing"

func TestAsksToSearch(t *testing.T) {
	yes := []string{"si no lo sabes deberías buscarlo en la web o no?", "busca en internet los modelos heretic", "búscalo en google", "investiga en la web qué es GSQ"}
	no := []string{"hola capri", "cuál es la capital de Francia", "crea un archivo web.txt", "qué es internet"}
	for _, s := range yes {
		if !AsksToSearch(s) {
			t.Errorf("debería pedir búsqueda: %q", s)
		}
	}
	for _, s := range no {
		if AsksToSearch(s) {
			t.Errorf("no pide búsqueda: %q", s)
		}
	}
	if got := StripSearchCue("busca en internet sobre los modelos heretic"); got != "los modelos heretic" {
		t.Errorf("tema = %q", got)
	}
	if got := StripSearchCue("búscalo en la web"); len(got) > 1 {
		t.Errorf("sin tema debe quedar vacío, dio %q", got)
	}
}

func TestStripPretend(t *testing.T) {
	in := "Voy a buscar información sobre heretic.\n\n[Realizo la búsqueda en internet sobre \"heretic\"]\n\nEntiendo. **Abliterated** es un método.\n\nDéjame ver qué sale…"
	got := StripPretend(in)
	if got != "Entiendo. **Abliterated** es un método." {
		t.Errorf("StripPretend = %q", got)
	}
	if got := StripPretend("Sin fingir nada.\n- un [enlace] en medio de la línea"); got != "Sin fingir nada.\n- un [enlace] en medio de la línea" {
		t.Errorf("no debe tocar texto normal: %q", got)
	}
}

// Seguimientos tras una búsqueda fallida: «busca mejor», «¿y la búsqueda?».
func TestSearchFollowUpsAndTopic(t *testing.T) {
	for _, s := range []string{"busca en internet mejor", "y la busqueda?", "¿y la búsqueda?", "búscalo de verdad", "haz la búsqueda"} {
		if !AsksToSearch(s) {
			t.Errorf("debería pedir búsqueda: %q", s)
		}
		if got := StripSearchCue(s); got != "" {
			t.Errorf("%q no trae tema, dio %q", s, got)
		}
	}
	if AsksToSearch("mi búsqueda de empleo va bien") {
		t.Error("hablar de una búsqueda no es pedir buscar")
	}
	users := []string{
		"hola capri",
		`que significa cuando un modelo de ia es "heretic" o "abliterated" ?`,
		"lo estas inventando o lo buscaste en internet?",
		"busca en internet mejor",
		"parece que no hiciste nada",
	}
	if got := Topic(users); got != users[1] {
		t.Errorf("tema = %q", got)
	}
	if got := Topic([]string{"hola", "y la búsqueda?"}); got != "" {
		t.Errorf("sin pregunta real debe quedar vacío, dio %q", got)
	}
}
