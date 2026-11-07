package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"http://localhost:7869":      "http://localhost:7869",
		"http://localhost:7869/":     "http://localhost:7869",
		"http://localhost:11434/v1":  "http://localhost:11434",
		"http://localhost:11434/v1/": "http://localhost:11434",
		" http://x/v1 ":              "http://x",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q)=%q, quiero %q", in, got, want)
		}
	}
}

func TestProfileOnlyWhenActivated(t *testing.T) {
	t.Setenv("CAPRI_PROFILE", "")
	t.Setenv("CAPRI_CTX_WINDOW", "")
	t.Setenv("EMBED_MODEL", "")
	t.Chdir(t.TempDir())
	if c := Load(); c.CtxWindow != 8192 || c.EmbedFamily != "qwen3" || c.EmbedPooling != "last" || c.Profile != "" {
		t.Fatalf("sin perfil debe quedar el comportamiento por defecto: %+v", c)
	}
	t.Setenv("CAPRI_PROFILE", "qwen3")
	t.Setenv("EMBED_MODEL", "")
	t.Setenv("CAPRI_CTX_WINDOW", "")
	c := Load()
	if c.CtxWindow != 16384 || c.EmbedFamily != "qwen3" || c.EmbedPooling != "last" {
		t.Fatalf("perfil qwen3 mal aplicado: %+v", c)
	}
	t.Setenv("CAPRI_CTX_WINDOW", "4096") // el entorno real manda sobre el perfil
	if c := Load(); c.CtxWindow != 4096 {
		t.Fatalf("el entorno debe ganar al perfil: %d", c.CtxWindow)
	}
}

func TestHomeDefaultsToDotCapri(t *testing.T) {
	t.Setenv("RAG_DB", "")
	t.Setenv("CAPRI_HOME", "")
	d := t.TempDir()
	t.Chdir(d)
	if c := Load(); filepath.Base(filepath.Dir(c.DBPath)) != ".capri" {
		t.Fatalf("DBPath = %s", c.DBPath)
	}
}

func TestMigrateLegacy(t *testing.T) {
	t.Setenv("RAG_DB", "")
	t.Setenv("CAPRI_HOME", "")
	d := t.TempDir()
	t.Chdir(d)
	_ = os.MkdirAll("data", 0o755)
	_ = os.WriteFile("data/brain.db", []byte("x"), 0o644)
	_ = os.WriteFile("data/traces.jsonl", []byte("t"), 0o644)
	Reload()
	MigrateLegacy()
	for _, f := range []string{".capri/brain.db", ".capri/traces.jsonl"} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("falta %s tras migrar", f)
		}
	}
	if _, err := os.Stat("data/brain.db"); err == nil {
		t.Error("data/brain.db debería haberse movido")
	}
}
