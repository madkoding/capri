package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"capri/internal/config"
)

func TestMirrorPinRecencySync(t *testing.T) {
	t.Cleanup(func() { config.Reload() })       // registrado antes que t.Setenv: corre después de restaurar el entorno
	t.Setenv("EMBED_URL", "http://127.0.0.1:1") // sin servicio: sin vectores
	t.Setenv("CAPRI_HOLO", "0")
	t.Setenv("CAPRI_MIRROR", "")
	config.Reload()
	db := tmpDB(t)
	r, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	ids, err := r.Add(ctx, AddOptions{Title: "Mascota", Content: "El perro se llama Rocky", Kind: "note", Whole: true})
	if err != nil {
		t.Fatal(err)
	}
	id := ids[0]
	files := r.mirrorFiles(id)
	if len(files) != 1 || !strings.Contains(files[0], filepath.Join("memory", "note")) {
		t.Fatalf("espejo: %v", files)
	}
	idx, _ := os.ReadFile(filepath.Join(r.memDir(), "INDEX.md"))
	if !strings.Contains(string(idx), "Mascota") {
		t.Fatal("INDEX.md sin la memoria")
	}

	// edición manual → importación
	raw, _ := os.ReadFile(files[0])
	edited := strings.Replace(string(raw), "Rocky", "Toby", 1)
	_ = os.WriteFile(files[0], []byte(edited), 0o644)
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(files[0], future, future)
	if n, _ := r.SyncMarkdown(); n != 1 {
		t.Fatalf("importadas %d", n)
	}
	if got := r.Get(id).Content; !strings.Contains(got, "Toby") {
		t.Fatalf("contenido = %q", got)
	}

	// archivo nuevo sin cabecera → nota
	_ = os.WriteFile(filepath.Join(r.memDir(), "note", "mi-dato.md"), []byte("Vivo en Valdivia"), 0o644)
	if n, _ := r.SyncMarkdown(); n != 1 {
		t.Fatalf("alta manual: %d", n)
	}

	// pin sobrevive a prune y dedupe, y se refleja en el archivo
	cids, _ := r.Add(ctx, AddOptions{Title: "viejo", Content: "charla", Kind: "chat", Whole: true})
	r.Pin(cids[0], true)
	old := nowMillis() - 90*86400000
	_, _ = r.db.Exec(`UPDATE mem SET updated=?`, old)
	if n := r.PruneChats(30); n != 0 {
		t.Fatalf("prune borró un fijado: %d", n)
	}
	if !r.Get(cids[0]).Pinned {
		t.Fatal("pinned perdido")
	}
	r.Pin(cids[0], false)
	if n := r.PruneChats(30); n != 1 {
		t.Fatalf("prune = %d", n)
	}
	if len(r.mirrorFiles(cids[0])) != 0 {
		t.Fatal("borrar debe quitar el archivo")
	}
}

func TestWeight(t *testing.T) {
	now := nowMillis()
	if w := weight(&Row{Kind: "note", Updated: now - 365*86400000}, 30); w != 1 {
		t.Errorf("las notas no envejecen: %v", w)
	}
	if w := weight(&Row{Kind: "chat", Updated: now - 30*86400000}, 30); w < 0.45 || w > 0.55 {
		t.Errorf("vida media: %v", w)
	}
	if w := weight(&Row{Kind: "chat", Updated: now - 3650*86400000}, 30); w != 0.4 {
		t.Errorf("suelo: %v", w)
	}
	if w := weight(&Row{Kind: "chat", Pinned: true, Updated: 0}, 30); w != 1.5 {
		t.Errorf("fijado: %v", w)
	}
}
