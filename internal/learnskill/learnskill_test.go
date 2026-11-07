package learnskill_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"capri/internal/config"
	"capri/internal/embed"
	"capri/internal/learnskill"
	"capri/internal/skills"
	"capri/internal/summary"
	"capri/internal/testutil"
)

func good(over func(*summary.Obs)) []summary.Obs {
	o := summary.Obs{Tool: "shell", Args: map[string]string{"command": "mv informe.txt docs/"}, OK: true, Text: "(sin salida)\n[exit 0]"}
	if over != nil {
		over(&o)
	}
	return []summary.Obs{o}
}

func TestStepLine(t *testing.T) {
	cases := []struct {
		o    summary.Obs
		want string
	}{
		{summary.Obs{Tool: "shell", Args: map[string]string{"command": "ls -S | head -1"}}, "shell: ls -S | head -1"},
		{summary.Obs{Tool: "fs_write", Args: map[string]string{"path": "a.sh", "content": "#!/bin/bash\necho hi"}}, `fs_write: a.sh | #!/bin/bash\necho hi`},
		{summary.Obs{Tool: "fs_edit", Args: map[string]string{"path": "c.json", "find": "80", "replace": "8080"}}, "fs_edit: c.json | 80 | 8080"},
	}
	for _, c := range cases {
		got, ok := learnskill.StepLine(c.o)
		if !ok || got != c.want {
			t.Errorf("StepLine = %q, %v; quería %q", got, ok, c.want)
		}
	}
	if _, ok := learnskill.StepLine(summary.Obs{Tool: "unknown", Args: map[string]string{}}); ok {
		t.Error("StepLine(unknown) debía ser false")
	}
}

func TestSkillTextReadable(t *testing.T) {
	text := learnskill.SkillText("mover-archivo", "mover un archivo", "mueve informe.txt a docs", good(nil))
	s := skills.ParseSkill(text)
	if s == nil {
		t.Fatal("ParseSkill = nil")
	}
	if s.Name != "mover-archivo" {
		t.Errorf("name = %q", s.Name)
	}
	if len(s.Turns) < 2 || s.Turns[0].Content != "Objetivo: mueve informe.txt a docs" {
		t.Errorf("turns = %#v", s.Turns)
	}
	if s.Turns[1].Content != "shell: mv informe.txt docs/" {
		t.Errorf("turno 1 = %q", s.Turns[1].Content)
	}
}

func TestFailedOrRejectedStepNoSkill(t *testing.T) {
	dir := t.TempDir()
	old := skills.GeneratedDir
	skills.GeneratedDir = dir
	defer func() { skills.GeneratedDir = old }()
	if s := learnskill.LearnSkill(context.Background(), "mueve informe.txt a docs", good(func(o *summary.Obs) { o.OK = false })); s != nil {
		t.Errorf("LearnSkill(ok=false) = %+v", s)
	}
	if s := learnskill.LearnSkill(context.Background(), "mueve informe.txt a docs", good(func(o *summary.Obs) { o.Rejected = true })); s != nil {
		t.Errorf("LearnSkill(rejected) = %+v", s)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("se escribieron %d archivos", len(entries))
	}
}

func TestTooManyStepsNoSkill(t *testing.T) {
	var many []summary.Obs
	for i := 0; i < 8; i++ {
		many = append(many, good(nil)[0])
	}
	if s := learnskill.LearnSkill(context.Background(), "haz muchas cosas", many); s != nil {
		t.Errorf("LearnSkill(8 pasos) = %+v", s)
	}
}

func TestSlug(t *testing.T) {
	if got := learnskill.Slug("Mover Archivo!!"); got != "mover-archivo" {
		t.Errorf("Slug = %q", got)
	}
	if got := learnskill.Slug("mostrar líneas"); got != "mostrar-lineas" {
		t.Errorf("Slug = %q", got)
	}
}

func TestDescribeRejectsBadFormat(t *testing.T) {
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	os.Setenv("CAPRI_URL", fake.URL)
	config.Reload()

	fake.Script("no sé")
	if d := learnskill.Describe(context.Background(), "x", []string{"shell: ls"}); d != nil {
		t.Errorf("Describe(no sé) = %+v", d)
	}
	fake.Script("nombre: Mi Receta!!\ndescripcion: corto")
	if d := learnskill.Describe(context.Background(), "x", []string{"shell: ls"}); d != nil {
		t.Errorf("Describe(desc corta) = %+v", d)
	}
	fake.Script("nombre: head -n 2 notas.txt\ndescripcion: muestra las primeras dos líneas de un archivo")
	d := learnskill.Describe(context.Background(), "x", []string{"shell: ls"})
	if d == nil {
		t.Fatal("Describe = nil")
	}
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(d.Name) {
		t.Errorf("nombre no normalizado: %q", d.Name)
	}
}

func TestCleanTaskSavesSkill(t *testing.T) {
	if !embed.Health(context.Background()) {
		t.Skip("servicio embed no disponible")
	}
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	os.Setenv("CAPRI_URL", fake.URL)
	config.Reload()
	dir := t.TempDir()
	old := skills.GeneratedDir
	skills.GeneratedDir = dir
	skills.ResetCache()
	defer func() { skills.GeneratedDir = old; skills.ResetCache() }()
	fake.Script("nombre: mover-archivo\ndescripcion: mover un archivo a otra carpeta sin copiarlo")
	s := learnskill.LearnSkill(context.Background(), "mueve informe.txt a la carpeta docs", good(nil))
	if s == nil {
		t.Fatal("LearnSkill = nil")
	}
	if s.Name != "mover-archivo" {
		t.Errorf("name = %q", s.Name)
	}
	b, err := os.ReadFile(filepath.Join(dir, "mover-archivo.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "origen: generada") {
		t.Errorf("skill = %q", b)
	}
}

func TestExistingSkillNotDuplicated(t *testing.T) {
	if !embed.Health(context.Background()) {
		t.Skip("servicio embed no disponible")
	}
	fake := testutil.StartFakeLLM()
	defer fake.Close()
	os.Setenv("CAPRI_URL", fake.URL)
	config.Reload()
	dir := t.TempDir()
	old := skills.GeneratedDir
	skills.GeneratedDir = dir
	skills.ResetCache()
	defer func() { skills.GeneratedDir = old; skills.ResetCache() }()
	if s := learnskill.LearnSkill(context.Background(), "cuántas líneas tiene notas.txt", good(nil)); s != nil {
		t.Errorf("LearnSkill duplicada = %+v", s)
	}
}
