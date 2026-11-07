package extract_test

import (
	"testing"

	"capri/internal/extract"
)

func TestHeuristicsShell(t *testing.T) {
	cases := map[string]string{
		"ejecuta el comando uname -a": "uname -a",
		"corre npm test":              "npm test",
		"run git status":              "git status",
	}
	for in, want := range cases {
		if got := extract.Heuristics("shell", in)["command"]; got != want {
			t.Errorf("Heuristics(shell, %q).command = %q, quería %q", in, got, want)
		}
	}
	if _, ok := extract.Heuristics("shell", "ejecuta un script que limpie")["command"]; ok {
		t.Error("no debía extraer comando de un script genérico")
	}
}

func TestHeuristicsPaths(t *testing.T) {
	cases := []struct {
		tool, in, key, want string
	}{
		{"fs_read", "Muéstrame el contenido de notas.txt", "path", "notas.txt"},
		{"fs_read", "lee src/app.js por favor", "path", "src/app.js"},
		{"fs_delete", "borra el archivo viejo.txt.", "path", "viejo.txt"},
		{"fs_list", "lista los archivos de la carpeta actual", "path", "."},
	}
	for _, c := range cases {
		if got := extract.Heuristics(c.tool, c.in)[c.key]; got != c.want {
			t.Errorf("Heuristics(%s, %q).%s = %q, quería %q", c.tool, c.in, c.key, got, c.want)
		}
	}
	if _, ok := extract.Heuristics("fs_list", "qué hay en docs")["path"]; ok {
		t.Error("no debía inventar una ruta")
	}
}
