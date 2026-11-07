package rag

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"capri/internal/embed"
)

func tmpDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "t.db")
}

func TestChunkRespectsMax(t *testing.T) {
	c := chunk(strings.Repeat("a", 2500), 900)
	if len(c) == 0 {
		t.Fatal("chunk vacío")
	}
	total := 0
	for _, x := range c {
		if n := len([]rune(x)); n > 900 {
			t.Errorf("trozo de %d caracteres", n)
		}
		total += len(x)
	}
	if total != 2500 {
		t.Errorf("total = %d, quería 2500", total)
	}
}

func TestCRUDAndHybridSearch(t *testing.T) {
	if !embed.Health(context.Background()) {
		t.Skip("servicio embed no disponible")
	}
	r, err := Open(tmpDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	ids, err := r.Add(ctx, AddOptions{Title: "Mascota", Content: "El perro del usuario se llama Rocky y le gusta nadar."})
	if err != nil {
		t.Fatal(err)
	}
	a := ids[0]
	for _, o := range []AddOptions{
		{Title: "Trabajo", Content: "El usuario trabaja como ingeniero de software en Santiago de Chile."},
		{Title: "Cocina", Content: "La receta favorita es pastel de choclo con albahaca."},
	} {
		if _, err := r.Add(ctx, o); err != nil {
			t.Fatal(err)
		}
	}

	r1 := r.Search(ctx, "¿cómo se llama mi mascota?", 0, 0)
	if len(r1) == 0 || r1[0].Title != "Mascota" {
		t.Errorf("búsqueda mascota = %+v", r1)
	}
	r2 := r.Search(ctx, "dónde vive y en qué trabaja", 0, 0)
	if len(r2) == 0 || r2[0].Title != "Trabajo" {
		t.Errorf("búsqueda trabajo = %+v", r2)
	}

	nuevo := "El perro del usuario se llama Max."
	if _, err := r.Update(ctx, a, nil, &nuevo, nil); err != nil {
		t.Fatal(err)
	}
	if row := r.Get(a); row == nil || !strings.Contains(row.Content, "Max") {
		t.Errorf("tras update: %+v", row)
	}
	if !r.Delete(a) {
		t.Error("Delete = false")
	}
	if r.Get(a) != nil {
		t.Error("Get tras borrar no es nil")
	}
	if n := r.Count(); n != 2 {
		t.Errorf("Count = %d", n)
	}
}

func TestFTSWithoutEmbeddings(t *testing.T) {
	r, err := Open(tmpDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.mu.Lock()
	r.vecs = map[int64][]float32{}
	r.holos = map[int64][]float32{}
	r.mu.Unlock()
	if _, err := r.db.Exec("INSERT INTO mem(title,content,tags,kind,created,updated) VALUES ('x','el gato duerme en el sofá','','note',1,1)"); err != nil {
		t.Fatal(err)
	}
	got := r.Search(context.Background(), "gato sofá", 0, 0)
	if len(got) != 1 {
		t.Errorf("Search = %+v", got)
	}
}

func TestHoloFollowsMemory(t *testing.T) {
	r, err := Open(tmpDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	ids, err := r.Add(ctx, AddOptions{Title: "Mascota", Content: "El perro del usuario se llama Rocky."})
	if err != nil {
		t.Fatal(err)
	}
	a := ids[0]
	r.mu.RLock()
	_, has := r.holos[a]
	r.mu.RUnlock()
	if !has {
		t.Fatal("no se creó el holo")
	}
	nuevo := "El perro del usuario se llama Max."
	if _, err := r.Update(ctx, a, nil, &nuevo, nil); err != nil {
		t.Fatal(err)
	}
	r.mu.RLock()
	_, has = r.holos[a]
	r.mu.RUnlock()
	if !has {
		t.Error("el holo desapareció tras update")
	}
	r.Delete(a)
	r.mu.RLock()
	_, has = r.holos[a]
	r.mu.RUnlock()
	if has {
		t.Error("el holo no se borró")
	}
}
