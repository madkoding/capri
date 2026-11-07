package loop_test

import (
	"context"
	"strings"
	"testing"

	"capri/internal/event"
	"capri/internal/loop"
)

// DONE sin haber hecho nada y el objetivo nombra un archivo que existe: se lee primero.
func TestDoneWithoutStepsReadsNamedFile(t *testing.T) {
	resetWorld(t)
	put(t, "frutas.txt", "manzana\npera\nuva\n")
	a, _, _ := makeAgent(t, nil)
	fake.Script("DONE", "DONE", "La última línea es uva.")
	out, err := a.Run(context.Background(), "dime la última línea de frutas.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "uva") || strings.Contains(out, "manzana") {
		t.Errorf("out = %q", out)
	}
	if n := len(fake.Requests()); n != 3 {
		t.Errorf("peticiones = %d (DONE → fs_read forzado → DONE → respuesta)", n)
	}
}

// Repetir un paso ya no para la tarea si la cobertura sabe qué falta.
func TestRepeatedStepForcesMissingRead(t *testing.T) {
	resetWorld(t)
	put(t, "notas.txt", "comprar pan")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_edit: notas.txt | pan | leche", "fs_edit: notas.txt | pan | leche", "DONE")
	out, err := a.Run(context.Background(), "cambia pan por leche en notas.txt y muéstralo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rd(t, "notas.txt") != "comprar leche" {
		t.Errorf("notas.txt = %q", rd(t, "notas.txt"))
	}
	if !strings.Contains(out, "1| comprar leche") || strings.Contains(out, "repetí") {
		t.Errorf("out = %q", out)
	}
}

// «reemplaza gato por pez»: todas las apariciones, sin pedir más contexto.
func TestReplaceAllWhenGoalAsksForIt(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "gato gato perro")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_edit: a.txt | gato | pez", "DONE")
	if _, err := a.Run(context.Background(), "reemplaza gato por pez en a.txt", nil); err != nil {
		t.Fatal(err)
	}
	if got := rd(t, "a.txt"); got != "pez pez perro" {
		t.Errorf("a.txt = %q", got)
	}
	if !loop.ReplaceAllWanted("reemplaza gato por pez en a.txt") || loop.ReplaceAllWanted("reemplaza la primera gato por pez en a.txt") {
		t.Error("ReplaceAllWanted")
	}
}

// Borrar y listar: el borrado se fuerza desde la cobertura y el listado se repite tras el cambio.
func TestDeleteIsForcedAndListingIsAfterIt(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "x")
	put(t, "b.txt", "y")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_list: .", "DONE", "DONE", "DONE", "DONE", "DONE")
	out, err := a.Run(context.Background(), "borra a.txt y luego lista los archivos", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rd(t, "a.txt") != "" {
		t.Error("a.txt debía borrarse")
	}
	lines := strings.Split(out, "\n")
	tail := strings.Join(lines[len(lines)-3:], "\n")
	if !strings.Contains(out, "✔ eliminé `a.txt`") || strings.Contains(tail, "a.txt") || !strings.Contains(tail, "b.txt") {
		t.Errorf("out = %q", out)
	}
}

// La respuesta final llega a la TUI en eventos "delta" mientras se genera.
func TestAnswerIsStreamedAsDeltaEvents(t *testing.T) {
	resetWorld(t)
	put(t, "frutas.txt", "manzana\npera\nuva\n")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_read: frutas.txt", "DONE", "<think>miro el archivo</think>La última línea es uva.")
	var think, answer strings.Builder
	out, err := a.Run(context.Background(), "dime la última línea de frutas.txt", func(e event.Event) {
		if e.Type == "delta" {
			if e.Phase == "think" {
				think.WriteString(e.Text)
			} else {
				answer.WriteString(e.Text)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.String() != "La última línea es uva." || think.String() != "miro el archivo" {
		t.Errorf("delta answer=%q think=%q", answer.String(), think.String())
	}
	if !strings.Contains(out, "La última línea es uva.") || strings.Contains(out, "<think>") {
		t.Errorf("out = %q", out)
	}
}

// Pedir una búsqueda con la web apagada se contesta en seco, sin que el modelo finja buscar.
func TestSearchRequestWithWebOffIsAnsweredPlainly(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("[Realizo la búsqueda] Déjame ver…")
	out, err := a.Run(context.Background(), "busca en internet mejor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "desactivada") || strings.Contains(out, "Realizo") {
		t.Errorf("out = %q", out)
	}
	if n := len(fake.Requests()); n != 0 {
		t.Errorf("no debe consultar al modelo: %d peticiones", n)
	}
}

func TestListDirAndFirstRead(t *testing.T) {
	cases := map[string]string{"x.txt": ".", "sub/x.txt": "sub", "sub": "sub", "": "", ".": "."}
	for in, want := range cases {
		if got := loop.ListDir(in); got != want {
			t.Errorf("ListDir(%q) = %q, quería %q", in, got, want)
		}
	}
}
