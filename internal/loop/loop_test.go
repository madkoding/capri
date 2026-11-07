package loop_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"capri/internal/agent"
	"capri/internal/config"
	"capri/internal/event"
	"capri/internal/rag"
	"capri/internal/testutil"
	"capri/internal/tools"
	"capri/internal/trash"
)

var (
	fake *testutil.FakeLLM
	tmp  string
	ws   string
)

func TestMain(m *testing.M) {
	fake = testutil.StartFakeLLM()
	tmp, _ = os.MkdirTemp("", "loop-test-")
	ws = filepath.Join(tmp, "ws")
	os.Setenv("CAPRI_URL", fake.URL)
	os.Setenv("EMBED_URL", "http://127.0.0.1:9")
	os.Setenv("WORKDIR", ws)
	os.Setenv("RAG_DB", filepath.Join(tmp, "brain.db"))
	os.Setenv("CAPRI_TRACE", "0")
	os.Setenv("CAPRI_GATE", "0")
	os.Setenv("CAPRI_WEB", "0")
	config.Reload()
	code := m.Run()
	fake.Close()
	os.RemoveAll(tmp)
	os.Exit(code)
}

var n int

func makeAgent(t *testing.T, confirm tools.Confirm) (*agent.Agent, *rag.Rag, *trash.Trash) {
	t.Helper()
	r, err := rag.Open(filepath.Join(tmp, "rag-"+itoa(n)+".db"))
	if err != nil {
		t.Fatal(err)
	}
	n++
	tr, err := trash.New(trash.Options{Dir: filepath.Join(tmp, "trash-"+itoa(n)), Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	if confirm == nil {
		confirm = func(string, string, tools.ConfirmOptions) bool { return true }
	}
	return agent.New(r, confirm, tr), r, tr
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func resetWorld(t *testing.T) {
	t.Helper()
	fake.Reset()
	if err := os.RemoveAll(ws); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
}

func rd(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws, f))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatal(err)
	}
	return string(b)
}

func put(t *testing.T, f, c string) {
	t.Helper()
	p := filepath.Join(ws, f)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChainsStepsAndShowsRead(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_write: a.txt | hola", "fs_read: a.txt", "DONE")
	out, err := a.Run(context.Background(), "crea a.txt que diga hola y luego léelo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rd(t, "a.txt"); got != "hola" {
		t.Errorf("a.txt = %q", got)
	}
	if !strings.Contains(out, "✔ escribí `a.txt`") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(out, "1| hola") {
		t.Errorf("out = %q", out)
	}
}

func TestModelSeesRealObservation(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("shell: echo 14:30", "fs_write: hora.txt | 14:30", "DONE")
	if _, err := a.Run(context.Background(), "dime qué hora es y guárdala en hora.txt", nil); err != nil {
		t.Fatal(err)
	}
	if got := rd(t, "hora.txt"); got != "14:30" {
		t.Errorf("hora.txt = %q", got)
	}
	reqs := fake.Requests()
	last := reqs[1].Messages[len(reqs[1].Messages)-1].Content
	if !strings.Contains(last, "Resultado: 14:30") {
		t.Errorf("segundo paso no vio la observación: %q", last)
	}
}

func TestReconfirmsAfterFailure(t *testing.T) {
	resetWorld(t)
	put(t, "b.txt", "importante")
	var asked []struct {
		name  string
		force bool
	}
	a, _, _ := makeAgent(t, func(name, _ string, o tools.ConfirmOptions) bool {
		asked = append(asked, struct {
			name  string
			force bool
		}{name, o.Force})
		return !o.Force
	})
	fake.Script("fs_read: fantasma.txt", "fs_delete: b.txt")
	out, err := a.Run(context.Background(), "lee fantasma.txt y luego borra b.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rd(t, "b.txt"); got != "importante" {
		t.Errorf("b.txt = %q", got)
	}
	found := false
	for _, x := range asked {
		if x.name == "fs_delete" && x.force {
			found = true
		}
	}
	if !found {
		t.Errorf("no se reconfirmó con force: %+v", asked)
	}
	if !strings.Contains(out, "cancelado por ti") {
		t.Errorf("out = %q", out)
	}
}

func TestOverwriteAlwaysAsks(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "original")
	a, _, _ := makeAgent(t, func(_ string, _ string, o tools.ConfirmOptions) bool { return !o.Overwrite })
	fake.Script("fs_write: a.txt | pisado")
	out, err := a.Run(context.Background(), "crea a.txt con pisado y luego léelo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := rd(t, "a.txt"); got != "original" {
		t.Errorf("a.txt = %q", got)
	}
	if !strings.Contains(out, "rechazó") { // se rechazó la sobrescritura; el bucle puede seguir por otro camino
		t.Errorf("out = %q", out)
	}
}

func TestCoverageNudge(t *testing.T) {
	resetWorld(t)
	put(t, "datos.txt", "10\n20\n30\n")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_read: datos.txt", "DONE", "DONE", "DONE", "DONE", "La suma es 60.")
	out, err := a.Run(context.Background(), "suma los números de datos.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, req := range fake.Requests() {
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Escribe exactamente este paso") {
				found = true
			}
		}
	}
	if !found {
		t.Error("debe haber pedido el paso exacto")
	}
	if !strings.Contains(out, "60") {
		t.Errorf("out = %q", out)
	}
}

func TestDoesNotRepeatSteps(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "x")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_list: .", "fs_list: .", "fs_list: .")
	out, err := a.Run(context.Background(), "lista los archivos y luego dime cuántos hay", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "repetí un paso") {
		t.Errorf("out = %q", out)
	}
}

func TestMissingCommandCanBeFixed(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("shell: nopeexiste_xyz --x", "shell: echo ok", "DONE", "Salió: ok.")
	out, err := a.Run(context.Background(), "ejecuta el comando echo ok y luego dime qué salió", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqs := fake.Requests()
	last := reqs[1].Messages[len(reqs[1].Messages)-1].Content
	if !strings.Contains(last, "comando no encontrado: nopeexiste_xyz") {
		t.Errorf("observación = %q", last)
	}
	if !strings.Contains(out, "Salió: ok") {
		t.Errorf("out = %q", out)
	}
	if !regexp.MustCompile(`✗ shell nopeexiste_xyz`).MatchString(out) {
		t.Errorf("out = %q", out)
	}
}

func TestTwoFailuresHalt(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_read: a.txt", "fs_read: b.txt", "fs_read: c.txt")
	out, err := a.Run(context.Background(), "lee a.txt y luego lee b.txt y luego lee c.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Me detuve") {
		t.Errorf("out = %q", out)
	}
	if n := len(fake.Requests()); n != 2 {
		t.Errorf("peticiones = %d", n)
	}
}

func TestConversationQuestionDoesNotTouchFiles(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("Es la capital de Francia: París.")
	out, err := a.Run(context.Background(), "¿cuál es la capital de Francia?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "París") {
		t.Errorf("out = %q", out)
	}
	entries, err := os.ReadDir(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("el workdir no está vacío: %d", len(entries))
	}
}

func TestUndoRestoresFilesAndMemory(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "orig")
	a, r, tr := makeAgent(t, nil)
	fake.Script("fs_delete: a.txt", "fs_list: .", "DONE")
	if _, err := a.Run(context.Background(), "borra a.txt y luego lista los archivos", nil); err != nil {
		t.Fatal(err)
	}
	if rd(t, "a.txt") != "" {
		t.Error("a.txt no se borró")
	}
	if !tr.CanUndo() {
		t.Fatal("no hay nada que deshacer")
	}
	ok, msg := tr.Undo(context.Background(), r)
	if !ok {
		t.Fatalf("Undo = false: %s", msg)
	}
	if got := rd(t, "a.txt"); got != "orig" {
		t.Errorf("a.txt = %q", got)
	}
	if ok, _ := tr.Undo(context.Background(), r); ok {
		t.Error("segundo Undo debía fallar")
	}
}

func TestReadOnlyTurnHasNoUndo(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "x")
	a, _, tr := makeAgent(t, nil)
	fake.Script("fs_read: a.txt", "DONE")
	if _, err := a.Run(context.Background(), "lee a.txt y luego dime qué dice", nil); err != nil {
		t.Fatal(err)
	}
	if tr.CanUndo() {
		t.Error("un turno de solo lectura no debe poder deshacerse")
	}
}

func TestWebSearchIsAttemptedNotSwallowed(t *testing.T) {
	resetWorld(t)
	a, _, _ := makeAgent(t, nil)
	fake.Script("web_search: qué es GSQ", "fs_write: gsq.txt | sin datos", "DONE")
	var tools []string
	out, err := a.Run(context.Background(), "busca en internet qué es GSQ y guarda un resumen en gsq.txt", func(e event.Event) {
		if e.Type == "tool" {
			tools = append(tools, e.Name)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 || tools[0] != "web_search" {
		t.Fatalf("web_search debe ejecutarse como paso, tools=%v out=%q", tools, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("la respuesta no puede quedar vacía")
	}
}

func TestOverwriteRejectionLetsModelAppend(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "x\n")
	a, _, _ := makeAgent(t, func(_, _ string, o tools.ConfirmOptions) bool { return !o.Overwrite })
	fake.Script("fs_write: a.txt | fin", "shell: echo 'fin' >> a.txt", "DONE")
	if _, err := a.Run(context.Background(), "añade la palabra fin al final de a.txt", nil); err != nil {
		t.Fatal(err)
	}
	got := rd(t, "a.txt")
	if !strings.Contains(got, "x") || !strings.Contains(got, "fin") {
		t.Fatalf("a.txt = %q: tras rechazar la sobrescritura el bucle debe seguir y añadir", got)
	}
}

func TestRmLimitedToGoal(t *testing.T) {
	resetWorld(t)
	put(t, "a.txt", "x")
	put(t, "b.txt", "y")
	a, _, _ := makeAgent(t, nil)
	fake.Script("shell: rm -f a.txt b.txt", "fs_delete: a.txt", "fs_list: .", "DONE")
	if _, err := a.Run(context.Background(), "borra a.txt y luego lista los archivos", nil); err != nil {
		t.Fatal(err)
	}
	if rd(t, "b.txt") != "y" {
		t.Fatal("b.txt no estaba pedido y se borró")
	}
	if rd(t, "a.txt") != "" {
		t.Fatal("a.txt debía borrarse")
	}
}

func TestDirtyPathIsCleaned(t *testing.T) {
	resetWorld(t)
	put(t, "frutas.txt", "manzana\npera\nuva\n")
	a, _, _ := makeAgent(t, nil)
	fake.Script("fs_read: frutas.txt. (el nombre exacto incluye el punto)", "DONE", "La última línea es uva.")
	out, err := a.Run(context.Background(), "dime la última línea de frutas.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "uva") {
		t.Fatalf("out = %q", out)
	}
}
