package planner_test

import (
	"testing"

	"capri/internal/planner"
)

func TestCoverageGaps(t *testing.T) {
	g := func(goal, plan string) int {
		return len(planner.CoverageGaps(goal, planner.ParsePlan(plan)))
	}
	if g("cambia pan por leche en n.txt", "fs_read: n.txt") == 0 {
		t.Error("debía faltar el paso de edición")
	}
	if g("cambia pan por leche en n.txt", "fs_edit: n.txt | pan | leche") != 0 {
		t.Error("no debía faltar nada")
	}
	if g("lee fantasma.txt", "fs_read: b.txt") == 0 {
		t.Error("no puede cambiar el nombre del archivo")
	}
	if g("lee a.txt y b.txt", "fs_read: a.txt") == 0 {
		t.Error("faltaba leer b.txt")
	}
	if g("cuántos archivos .txt hay", "shell: ls | wc -l") == 0 {
		t.Error("conteo sin filtrar")
	}
	if g("cuántos archivos .txt hay", "shell: ls *.txt | wc -l\nanswer: x") != 0 {
		t.Error("no debía faltar nada")
	}
	if g("crea hola.py y ejecútalo", "fs_write: hola.py | x") == 0 {
		t.Error("faltaba ejecutarlo")
	}
}
