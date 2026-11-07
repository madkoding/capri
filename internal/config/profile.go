package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultEmbedModel es el modelo de embeddings interno por defecto.
const DefaultEmbedModel = "qwen3-embedding-0.6b"

// HomeDir es la carpeta de estado por defecto (relativa al directorio de trabajo).
const HomeDir = ".capri"

// perfiles son conjuntos de valores por defecto que solo se aplican si el usuario
// los activa a mano (CAPRI_PROFILE=<nombre> o --profile <nombre>). Nunca pisan
// variables ya definidas en el entorno real.
var perfiles = map[string]map[string]string{
	// Qwen3-8B cuantizado con GSQ (Q2_K) + Qwen3-Embedding-0.6B.
	// Qwen3-8B admite 32 768 tokens de contexto nativos (40 960 posiciones en su
	// config; hasta 131 072 con YaRN). Se arranca con 16 384 porque con 8 GB de
	// VRAM (pesos ≈3,3 GB + KV fp16 ≈2,4 GB) es lo que cabe sin apretar; para
	// 32 768 usa CAPRI_SERVER_CTX=32768 CAPRI_KV_TYPE=q8_0.
	"qwen3": {
		"LLM_GGUF":            "qwen3-8b-gsq-Q2_K.gguf",
		"EMBED_GGUF":          "Qwen3-Embedding-0.6B-Q8_0.gguf",
		"EMBED_MODEL":         "qwen3-embedding-0.6b",
		"CAPRI_EMBED_POOLING": "last",
		"CAPRI_SERVER_CTX":    "16384",
		"CAPRI_CTX_WINDOW":    "16384",
	},
}

// Profiles devuelve los nombres de perfil disponibles.
func Profiles() []string {
	var out []string
	for k := range perfiles {
		out = append(out, k)
	}
	return out
}

// SetProfile activa un perfil para este proceso (y sus hijos). Devuelve error si no existe.
func SetProfile(name string) error {
	if _, ok := perfiles[name]; !ok {
		return fmt.Errorf("perfil desconocido %q (disponibles: %s)", name, strings.Join(Profiles(), ", "))
	}
	return os.Setenv("CAPRI_PROFILE", name)
}

// applyProfile rellena las variables del perfil que el entorno real no defina.
func applyProfile(name string) {
	for k, v := range perfiles[name] {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

// embedFamily deduce los prefijos que espera el modelo de embeddings.
func embedFamily(model string) string {
	if strings.Contains(strings.ToLower(model), "qwen") {
		return "qwen3"
	}
	return "nomic"
}

// MigrateLegacy mueve el estado antiguo de data/ a .capri/ la primera vez
// (memoria, trazas, copias de /undo, transcripciones). No toca nada si ya
// existe .capri/brain.db o si RAG_DB apunta a otra parte.
func MigrateLegacy() {
	if os.Getenv("RAG_DB") != "" {
		return
	}
	cfg := Get()
	if _, err := os.Stat(cfg.DBPath); err == nil {
		return
	}
	old := "data"
	if _, err := os.Stat(filepath.Join(old, "brain.db")); err != nil {
		return
	}
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		return
	}
	// la base va primero: si falla, el resto se queda donde estaba
	for _, f := range []string{"brain.db", "brain.db-wal", "brain.db-shm", "traces.jsonl", "trash", "transcripts"} {
		src := filepath.Join(old, f)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, filepath.Join(cfg.Home, f)); err != nil && f == "brain.db" {
			return
		}
	}
	fmt.Fprintln(os.Stderr, "capri: memoria migrada de data/ a "+cfg.Home)
}

// poolingFor es el pooling que necesita cada familia de embeddings.
func poolingFor(family string) string {
	if family == "qwen3" {
		return "last"
	}
	return "mean"
}

var quantSuffix = regexp.MustCompile(`[-.](q\d\w*|f16|f32|bf16)$`)

// embedNameFromFile deduce el nombre del modelo de embeddings a partir de su GGUF
// ("Qwen3-Embedding-0.6B-Q8_0.gguf" → "qwen3-embedding-0.6b").
func embedNameFromFile(file string) string {
	n := strings.ToLower(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	return quantSuffix.ReplaceAllString(n, "")
}

func embedChoice(home string) string { return filepath.Join(home, "embed") }

// SavedEmbed devuelve el GGUF de embeddings elegido con /embed ("" si no hay).
func SavedEmbed(home string) string {
	b, err := os.ReadFile(embedChoice(home))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetEmbedGGUF recuerda el modelo de embeddings elegido ("" = volver al predeterminado)
// y lo aplica en caliente conservando el muestreo.
func SetEmbedGGUF(file string) error {
	home := Get().Home
	if file == "" {
		if err := os.Remove(embedChoice(home)); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		if err := os.MkdirAll(home, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(embedChoice(home), []byte(file), 0o644); err != nil {
			return err
		}
	}
	reloadKeepingSampling()
	return nil
}

// ForceEmbedModel fija el modelo de embeddings solo para este proceso (p. ej. si no
// se pudo bajar el predeterminado y se usa el que hay en disco).
func ForceEmbedModel(name string) {
	_ = os.Setenv("EMBED_MODEL", name)
	reloadKeepingSampling()
}
