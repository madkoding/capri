package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"capri/internal/config"
)

// fakeEmbed sirve /health y /v1/embeddings con vectores de la dimensión pedida
// y registra los textos recibidos.
func fakeEmbed(t *testing.T, dim int, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			return
		}
		var in struct{ Input []string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		*seen = append(*seen, in.Input...)
		type d struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		var out struct {
			Data []d `json:"data"`
		}
		for i := range in.Input {
			v := make([]float64, dim)
			v[i%dim] = 1
			out.Data = append(out.Data, d{i, v})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
}

func TestEnsureEmbeddingsReindexesOnModelChange(t *testing.T) {
	t.Cleanup(func() { config.Reload() }) // registrado antes que t.Setenv: corre después de restaurar el entorno
	var seen []string
	nomic := fakeEmbed(t, 4, &seen)
	defer nomic.Close()
	t.Setenv("EMBED_URL", nomic.URL)
	t.Setenv("EMBED_MODEL", "nomic")
	t.Setenv("CAPRI_HOLO", "0")
	config.Reload()
	r, err := Open(tmpDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	if _, err := r.EnsureEmbeddings(ctx); err != nil { // base vacía: solo sella
		t.Fatal(err)
	}
	if _, err := r.Add(ctx, AddOptions{Title: "a", Content: "uno"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := r.EnsureEmbeddings(ctx); n != 0 {
		t.Fatalf("mismo modelo: no debe reindexar, hizo %d", n)
	}

	seen = nil
	qwen := fakeEmbed(t, 8, &seen)
	defer qwen.Close()
	t.Setenv("EMBED_URL", qwen.URL)
	t.Setenv("EMBED_MODEL", "qwen3-embedding-0.6b")
	config.Reload()
	n, err := r.EnsureEmbeddings(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reindexado: n=%d err=%v", n, err)
	}
	for _, v := range r.vecs {
		if len(v) != 8 {
			t.Fatalf("vector de %d dimensiones tras reindexar", len(v))
		}
	}
	// qwen3: documentos sin prefijo
	if len(seen) == 0 || strings.HasPrefix(seen[0], "search_document:") {
		t.Fatalf("prefijo de documento inesperado: %q", seen)
	}
	if n, _ := r.EnsureEmbeddings(ctx); n != 0 {
		t.Fatalf("segunda pasada no debe repetir trabajo, hizo %d", n)
	}
	_ = os.Unsetenv("EMBED_URL")
	config.Reload()
}

func TestEnsureEmbeddingsReindexesWhenDimensionDiffers(t *testing.T) {
	t.Cleanup(func() { config.Reload() })
	var seen []string
	small := fakeEmbed(t, 4, &seen)
	defer small.Close()
	t.Setenv("EMBED_URL", small.URL)
	t.Setenv("EMBED_MODEL", "qwen3-embedding-0.6b")
	t.Setenv("CAPRI_HOLO", "0")
	config.Reload()
	r, err := Open(tmpDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	_, _ = r.EnsureEmbeddings(ctx)
	_, _ = r.Add(ctx, AddOptions{Title: "a", Content: "uno", Whole: true})
	// el sello coincide, pero el servidor ahora devuelve otra dimensión
	big := fakeEmbed(t, 8, &seen)
	defer big.Close()
	t.Setenv("EMBED_URL", big.URL)
	config.Reload()
	if n, err := r.EnsureEmbeddings(ctx); err != nil || n != 1 {
		t.Fatalf("debe reindexar por dimensión: n=%d err=%v", n, err)
	}
	for _, v := range r.vecs {
		if len(v) != 8 {
			t.Fatalf("dimensión %d", len(v))
		}
	}
}
