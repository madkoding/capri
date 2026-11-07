package loop

import "testing"

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"frutas.txt. (el nombre exacto incluye el punto)": "frutas.txt",
		`"notas.txt"`:            "notas.txt",
		"el archivo config.json": "config.json",
		"datos/x.txt":            "datos/x.txt",
		"carpeta":                "carpeta",
		"a.txt,":                 "a.txt",
	}
	for in, want := range cases {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestRmOutsideGoal(t *testing.T) {
	if got := RmOutsideGoal("borra a.txt y luego lista los archivos", "rm -f a.txt b.txt"); got != "b.txt" {
		t.Errorf("debe detectar b.txt, dio %q", got)
	}
	if got := RmOutsideGoal("borra a.txt y luego lista los archivos", "rm -f a.txt"); got != "" {
		t.Errorf("a.txt está pedido: %q", got)
	}
	if got := RmOutsideGoal("borra todos los archivos temporales .tmp", "rm -f *.tmp"); got != "" {
		t.Errorf("comodín con extensión pedida: %q", got)
	}
	if got := RmOutsideGoal("borra a.txt", "rm -rf *"); got != "*" {
		t.Errorf("comodín sin «todos»: %q", got)
	}
	if got := RmOutsideGoal("elimina la carpeta tmp", "rm -rf tmp/"); got != "" {
		t.Errorf("carpeta pedida: %q", got)
	}
	if got := RmOutsideGoal("lista los archivos", "ls -la"); got != "" {
		t.Errorf("sin rm: %q", got)
	}
}
