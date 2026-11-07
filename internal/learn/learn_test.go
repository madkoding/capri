package learn_test

import (
	"regexp"
	"testing"

	"capri/internal/learn"
)

var POS = []string{"Me llamo Matías y vivo en Valdivia", "Mi perro se llama Rocky", "Trabajo como ingeniero de software", "Me encanta el jazz", "Tengo dos hijos, Ana y Pedro", "Mi cumpleaños es el 9 de julio", "Soy vegetariano desde hace años", "Prefiero programar en Python", "Mi color favorito es el azul", "Mi servidor tiene la IP 10.0.0.5", "My wife is called Laura", "Estoy construyendo una app de recetas, se llama Cocinita", "Mi hermana vive en Chile", "No me gusta el café"}

var NEG = []string{"hola, cómo estás", "gracias!", "¿Cuánto es 15 por 4?", "Cuéntame un chiste corto", "crea un archivo hola.txt que diga hola", "ok, entiendo", "qué es Docker", "Estoy cansado hoy", "lista los archivos", "buenos días", "La capital de Francia es París", "¿Cómo se llama mi perro?", "¿Cuál es mi color favorito?", "ejecuta uname -a", "dame una receta de panqueques", "no entiendo lo que dices"}

func TestLooksLikeFactPositive(t *testing.T) {
	for _, txt := range POS {
		if !learn.LooksLikeFact(txt) {
			t.Errorf("LooksLikeFact(%q) = false", txt)
		}
	}
}

func TestLooksLikeFactNegative(t *testing.T) {
	for _, txt := range NEG {
		if learn.LooksLikeFact(txt) {
			t.Errorf("LooksLikeFact(%q) = true", txt)
		}
	}
}

func TestSplitFacts(t *testing.T) {
	facts, rest := learn.SplitFacts("Me llamo Matías. ¿Cuánto es 15 por 4?")
	if len(facts) != 1 || facts[0] != "Me llamo Matías" {
		t.Errorf("facts = %#v", facts)
	}
	if rest != "¿Cuánto es 15 por 4?" {
		t.Errorf("rest = %q", rest)
	}
}

func TestSplitFactsGreeting(t *testing.T) {
	facts, rest := learn.SplitFacts("Hola, me llamo Matías y mi perro se llama Rocky.")
	if len(facts) != 1 {
		t.Fatalf("facts = %#v", facts)
	}
	if rest != "" {
		t.Errorf("rest = %q", rest)
	}
	if !regexp.MustCompile(`(?i)^me llamo Matías`).MatchString(facts[0]) {
		t.Errorf("facts[0] = %q", facts[0])
	}
}

func TestSplitFactsCommaBeforeQuestion(t *testing.T) {
	facts, rest := learn.SplitFacts("Me encanta el jazz, ¿qué es Docker?")
	if len(facts) != 1 || facts[0] != "Me encanta el jazz" {
		t.Errorf("facts = %#v", facts)
	}
	if rest != "¿qué es Docker?" {
		t.Errorf("rest = %q", rest)
	}
}
