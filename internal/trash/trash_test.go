package trash_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"capri/internal/rag"
	"capri/internal/trash"
)

func mk(t *testing.T, o trash.Options) (string, *trash.Trash) {
	t.Helper()
	ws := t.TempDir()
	o.Workdir = ws
	if o.Dir == "" {
		o.Dir = t.TempDir()
	}
	tr, err := trash.New(o)
	if err != nil {
		t.Fatal(err)
	}
	return ws, tr
}

func TestSnapshotRestoresEverything(t *testing.T) {
	ws, tr := mk(t, trash.Options{})
	write(t, filepath.Join(ws, "a.txt"), "uno")
	if err := os.MkdirAll(filepath.Join(ws, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(ws, "sub", "b.txt"), "dos")
	tr.BeginTurn("prueba")
	if err := tr.SnapshotOnce(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(ws, "a.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(ws, "sub", "b.txt"), "CAMBIADO")
	write(t, filepath.Join(ws, "nuevo.txt"), "x")
	if ok, _ := tr.CommitTurn(); !ok {
		t.Fatal("CommitTurn no es deshacible")
	}
	if ok, msg := tr.Undo(context.Background(), nil); !ok {
		t.Fatalf("Undo = %v: %s", ok, msg)
	}
	if got := read(t, filepath.Join(ws, "a.txt")); got != "uno" {
		t.Errorf("a.txt = %q", got)
	}
	if got := read(t, filepath.Join(ws, "sub", "b.txt")); got != "dos" {
		t.Errorf("b.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(ws, "nuevo.txt")); !os.IsNotExist(err) {
		t.Error("nuevo.txt debería haberse borrado")
	}
}

func TestOnlyOneSnapshotPerTurn(t *testing.T) {
	ws, tr := mk(t, trash.Options{})
	write(t, filepath.Join(ws, "a.txt"), "original")
	tr.BeginTurn("x")
	_ = tr.SnapshotOnce()
	write(t, filepath.Join(ws, "a.txt"), "intermedio")
	_ = tr.SnapshotOnce()
	write(t, filepath.Join(ws, "a.txt"), "final")
	tr.CommitTurn()
	tr.Undo(context.Background(), nil)
	if got := read(t, filepath.Join(ws, "a.txt")); got != "original" {
		t.Errorf("a.txt = %q", got)
	}
}

func TestTooBigDoesNotCopy(t *testing.T) {
	ws, tr := mk(t, trash.Options{MaxFiles: 2})
	for _, n := range []string{"a", "b", "c"} {
		write(t, filepath.Join(ws, n), n)
	}
	tr.BeginTurn("grande")
	_ = tr.SnapshotOnce()
	ok, skipped := tr.CommitTurn()
	if ok {
		t.Error("no debía ser deshacible")
	}
	if skipped == nil || !strings.Contains(*skipped, "demasiado grande") {
		t.Errorf("skipped = %v", skipped)
	}
}

func TestJournalPersistsAndRespectsMax(t *testing.T) {
	ws, tr := mk(t, trash.Options{Keep: 2})
	for i := 0; i < 4; i++ {
		tr.BeginTurn("t" + string(rune('0'+i)))
		_ = tr.SnapshotOnce()
		write(t, filepath.Join(ws, "f"), string(rune('0'+i)))
		tr.CommitTurn()
	}
	if n := len(tr.Journal()); n != 2 {
		t.Fatalf("journal = %d", n)
	}
	again, err := trash.New(trash.Options{Dir: tr.Dir, Workdir: ws})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(again.Journal()); n != 2 {
		t.Errorf("journal recargado = %d", n)
	}
	list := again.List(2)
	if len(list) != 2 || list[0].Label != "t3" || list[1].Label != "t2" {
		t.Errorf("List = %+v", list)
	}
}

type fakeRag struct{ calls [][]any }

func (f *fakeRag) Delete(id int64) bool { f.calls = append(f.calls, []any{"delete", id}); return true }
func (f *fakeRag) Add(_ context.Context, o rag.AddOptions) ([]int64, error) {
	f.calls = append(f.calls, []any{"add", o.Title})
	return []int64{1}, nil
}
func (f *fakeRag) Update(_ context.Context, id int64, title, _, _ *string) (*rag.Row, error) {
	t := ""
	if title != nil {
		t = *title
	}
	f.calls = append(f.calls, []any{"update", id, t})
	return &rag.Row{ID: id}, nil
}

func TestMemoryUndoReverseOrder(t *testing.T) {
	_, tr := mk(t, trash.Options{})
	r := &fakeRag{}
	tr.BeginTurn("mem")
	tr.NoteMemory(trash.MemOp{Type: "add", IDs: []int64{7}})
	tr.NoteMemory(trash.MemOp{Type: "delete", Row: &trash.MemRow{Title: "viejo", Content: "c", Tags: "", Kind: "note"}})
	tr.NoteMemory(trash.MemOp{Type: "update", ID: 3, Before: &trash.Before{Title: "antes", Content: "c", Tags: ""}})
	tr.CommitTurn()
	if ok, msg := tr.Undo(context.Background(), r); !ok {
		t.Fatalf("Undo = false: %s", msg)
	}
	want := [][]any{{"update", int64(3), "antes"}, {"add", "viejo"}, {"delete", int64(7)}}
	if len(r.calls) != len(want) {
		t.Fatalf("calls = %#v", r.calls)
	}
	for i := range want {
		if len(r.calls[i]) != len(want[i]) || r.calls[i][0] != want[i][0] {
			t.Errorf("call %d = %#v, quería %#v", i, r.calls[i], want[i])
		}
	}
}

func write(t *testing.T, p, c string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
