package planner

import (
	"strings"
	"testing"
)

func ok(b bool) *bool { return &b }

func TestParsePlanWebSearch(t *testing.T) {
	st := ParsePlan("web_search: modelos heretic")
	if len(st) != 1 || st[0].Tool != "web_search" || st[0].Args["query"] != "modelos heretic" {
		t.Fatalf("%+v", st)
	}
}

func TestShowAfterEdit(t *testing.T) {
	goal := "cambia pan por leche en notas.txt y muéstralo"
	before := []Step{{Tool: "fs_read", Args: map[string]string{"path": "notas.txt"}, OK: ok(true)}, {Tool: "fs_edit", Args: map[string]string{"path": "notas.txt", "find": "pan", "replace": "leche"}, OK: ok(true)}}
	gaps := GapsOf(goal, before)
	found := false
	for _, g := range gaps {
		if g.Suggest != nil && *g.Suggest == "fs_read: notas.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("leer antes de editar no cuenta como mostrar: %+v", gaps)
	}
	after := append(before, Step{Tool: "fs_read", Args: map[string]string{"path": "notas.txt"}, OK: ok(true)})
	for _, g := range GapsOf(goal, after) {
		if strings.Contains(g.Hint, "MOSTRAR") {
			t.Fatalf("tras leer no debe quedar hueco: %+v", g)
		}
	}
	// «crea a.txt que diga hola y luego léelo»
	if gaps := GapsOf("crea a.txt que diga hola y luego léelo", []Step{{Tool: "fs_write", Args: map[string]string{"path": "a.txt", "content": "hola"}, OK: ok(true)}}); len(gaps) == 0 || gaps[0].Suggest == nil || *gaps[0].Suggest != "fs_read: a.txt" {
		t.Fatalf("léelo debe sugerir fs_read: %+v", gaps)
	}
}

func suggested(gaps []Gap) []string {
	var out []string
	for _, g := range gaps {
		if g.Suggest != nil {
			out = append(out, *g.Suggest)
		}
	}
	return out
}

func TestContentFor(t *testing.T) {
	cases := []struct{ goal, file, want string }{
		{"crea x.txt con hola, después y.txt con adiós, y lista los archivos", "x.txt", "hola"},
		{"crea x.txt con hola, después y.txt con adiós, y lista los archivos", "y.txt", "adiós"},
		{"lee noexiste.txt y si no existe créalo con la palabra nuevo", "noexiste.txt", "nuevo"},
		{"crea a.txt que diga hola mundo y luego léelo", "a.txt", "hola mundo"},
		{`escribe en b.txt con el texto "uno dos"`, "b.txt", "uno dos"},
		{"dime qué hora es y guárdala en hora.txt", "hora.txt", ""},
	}
	for _, c := range cases {
		if got := contentFor(c.goal, c.file); got != c.want {
			t.Errorf("contentFor(%q, %s) = %q, quería %q", c.goal, c.file, got, c.want)
		}
	}
}

func TestDeleteCreateListSuggestions(t *testing.T) {
	// borrar un archivo nombrado: se sugiere fs_delete; un listado previo no cuenta
	goal := "borra a.txt y luego lista los archivos"
	s := suggested(GapsOf(goal, []Step{{Tool: "fs_list", Args: map[string]string{"path": "."}, OK: ok(true)}}))
	if strings.Join(s, ";") != "fs_delete: a.txt" {
		t.Fatalf("borrar: %v", s)
	}
	after := []Step{{Tool: "fs_list", Args: map[string]string{"path": "."}, OK: ok(true)}, {Tool: "fs_delete", Args: map[string]string{"path": "a.txt"}, OK: ok(true)}}
	if s := suggested(GapsOf(goal, after)); strings.Join(s, ";") != "fs_list: ." {
		t.Fatalf("listar tras borrar: %v", s)
	}
	done := append(after, Step{Tool: "fs_list", Args: map[string]string{"path": "."}, OK: ok(true)})
	if g := GapsOf(goal, done); len(g) != 0 {
		t.Fatalf("cubierto: %+v", g)
	}
	// «borra la línea 3 de a.txt» no es borrar el archivo
	if s := suggested(GapsOf("borra la línea 3 de a.txt", nil)); len(s) != 0 {
		t.Fatalf("línea: %v", s)
	}
	// crear con contenido conocido: se sugiere fs_write con ese contenido, por orden
	goal = "crea x.txt con hola, después y.txt con adiós, y lista los archivos"
	if s := suggested(GapsOf(goal, nil)); len(s) == 0 || s[0] != "fs_write: x.txt | hola" {
		t.Fatalf("crear: %v", s)
	}
	if s := suggested(GapsOf(goal, []Step{{Tool: "fs_write", Args: map[string]string{"path": "x.txt"}, OK: ok(true)}})); len(s) == 0 || s[0] != "fs_write: y.txt | adiós" {
		t.Fatalf("segundo: %v", s)
	}
	// recuperar de un error: créalo con la palabra nuevo
	if s := suggested(GapsOf("lee noexiste.txt y si no existe créalo con la palabra nuevo", []Step{{Tool: "fs_read", Args: map[string]string{"path": "noexiste.txt"}, OK: ok(false)}})); len(s) == 0 || s[0] != "fs_write: noexiste.txt | nuevo" {
		t.Fatalf("recuperar: %v", s)
	}
	// la fuente de una copia no hay que crearla
	if g := GapsOf("lee a.txt y guarda su contenido en b.txt", []Step{{Tool: "fs_read", Args: map[string]string{"path": "a.txt"}, OK: ok(true)}, {Tool: "fs_write", Args: map[string]string{"path": "b.txt"}, OK: ok(true)}}); len(g) != 0 {
		t.Fatalf("fuente: %+v", g)
	}
}

func TestDateComesFromShell(t *testing.T) {
	s := suggested(GapsOf("dime la fecha de hoy y escríbela en hoy.txt", nil))
	if len(s) == 0 || s[0] != "shell: date +%F" {
		t.Fatalf("fecha: %v", s)
	}
	s = suggested(GapsOf("dime qué hora es y guárdala en hora.txt", nil))
	if len(s) == 0 || s[0] != "shell: date +%H:%M" {
		t.Fatalf("hora: %v", s)
	}
	steps := []Step{{Tool: "shell", Args: map[string]string{"command": "date +%H:%M"}, OK: ok(true)}}
	create := false
	for _, g := range GapsOf("dime qué hora es y guárdala en hora.txt", steps) {
		if strings.Contains(g.Hint, "date") {
			t.Fatalf("ya se usó date: %+v", g)
		}
		if strings.Contains(g.Hint, "CREAR") {
			create = true
		}
	}
	if !create {
		t.Fatal("falta guardar hora.txt: debe quedar el hueco de CREAR")
	}
	for _, s := range suggested(GapsOf("lee hora.txt", nil)) {
		if strings.Contains(s, "date") {
			t.Fatalf("hora.txt no es la hora: %v", s)
		}
	}
}

func TestFoldersAndAppend(t *testing.T) {
	if n := folderNames("crea tres carpetas llamadas a, b y c"); strings.Join(n, " ") != "a b c" {
		t.Fatalf("carpetas = %v", n)
	}
	if n := folderNames("crea una carpeta datos y dentro un archivo x.txt con la palabra azul"); strings.Join(n, " ") != "datos" {
		t.Fatalf("carpeta = %v", n)
	}
	gaps := GapsOf("crea tres carpetas llamadas a, b y c", []Step{{Tool: "fs_write", Args: map[string]string{"path": "a.txt"}, OK: ok(true)}})
	if len(gaps) == 0 || gaps[0].Suggest == nil || *gaps[0].Suggest != "shell: mkdir -p a b c" {
		t.Fatalf("mkdir: %+v", gaps)
	}
	if gaps := GapsOf("crea tres carpetas llamadas a, b y c", []Step{{Tool: "shell", Args: map[string]string{"command": "mkdir -p a b c"}, OK: ok(true)}}); len(gaps) != 0 {
		t.Fatalf("cubierto: %+v", gaps)
	}
	gaps = GapsOf("añade la palabra fin al final de a.txt", []Step{{Tool: "fs_write", Args: map[string]string{"path": "a.txt"}, OK: ok(false)}})
	want := "shell: echo 'fin' >> a.txt"
	hit := false
	for _, g := range gaps {
		if g.Suggest != nil && *g.Suggest == want {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("append: %+v", gaps)
	}
}
