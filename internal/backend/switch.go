package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"capri/internal/config"
)

// ChatModels lista los GGUF de chat de modelsDir (sin los de embeddings), por nombre.
func ChatModels(modelsDir string) []string {
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		low := strings.ToLower(e.Name())
		if e.IsDir() || !strings.HasSuffix(low, ".gguf") || strings.Contains(low, "embed") || strings.Contains(low, "nomic") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func choiceFile() string { return filepath.Join(config.Get().Home, "model") }

// Saved devuelve el modelo elegido con /model (vacío si no hay).
func Saved() string {
	b, err := os.ReadFile(choiceFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Current devuelve el GGUF con el que arrancó el servidor de chat.
func Current() string {
	b, err := os.ReadFile(filepath.Join("data", "run", "llm.model"))
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSpace(string(b)))
}

// Resolve convierte "3", un nombre o un trozo de nombre en un GGUF de modelsDir.
func Resolve(modelsDir, q string) (string, error) {
	list := ChatModels(modelsDir)
	if n, err := strconv.Atoi(q); err == nil {
		if n < 1 || n > len(list) {
			return "", fmt.Errorf("no hay modelo #%d (hay %d)", n, len(list))
		}
		return list[n-1], nil
	}
	var hits []string
	for _, m := range list {
		if strings.EqualFold(m, q) {
			return m, nil
		}
		if strings.Contains(strings.ToLower(m), strings.ToLower(q)) {
			hits = append(hits, m)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("ningún modelo coincide con %q", q)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("%q es ambiguo: %s", q, strings.Join(hits, ", "))
}

// SwitchLLM reinicia solo el servidor de chat con otro GGUF (el de embeddings no se toca),
// espera a que responda y recuerda la elección para los próximos arranques.
func SwitchLLM(ctx context.Context, modelsDir, gguf string) error {
	cfg := config.Get()
	if cfg.Backend != "llamacpp" {
		return fmt.Errorf("/model solo cambia el backend llamacpp; con %q cambia CAPRI_MODEL en el servidor", cfg.Backend)
	}
	path := filepath.Join(modelsDir, gguf)
	if _, err := os.Stat(path); err != nil {
		return err
	}
	accel := ResolveAccel(cfg.Accel)
	bin, err := EnsureRuntime(ctx, accel, func(string, ...any) {})
	if err != nil {
		return err
	}
	port := urlPortOr(cfg.LLMURL, 7869)
	o := Options{Bin: bin, LLMGGUF: path, LLMPort: port, Accel: accel, Ctx: cfg.ServerCtx, KVType: cfg.KVType}
	if accel != AccelCPU {
		o.NGL = 99
	}
	o.defaults()
	stopOne(o.LogDir, "llm")
	for i := 0; i < 50 && httpReady(ctx, cfg.LLMURL); i++ { // espera a que libere el puerto
		time.Sleep(200 * time.Millisecond)
	}
	args := llmArgs(o)
	srv, err := launch(o, "llm", args, port)
	if err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(o.LogDir, "llm.model"), []byte(gguf), 0o644)
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if httpReady(ctx, cfg.LLMURL) {
			_ = os.MkdirAll(cfg.Home, 0o755)
			return os.WriteFile(choiceFile(), []byte(gguf), 0o644)
		}
		if !alive(srv.PID) {
			return fmt.Errorf("el servidor se cerró al cargar %s (log: %s)", gguf, srv.Log)
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s no respondió en 3 min (log: %s)", gguf, srv.Log)
}

func urlPortOr(raw string, def int) int {
	i := strings.LastIndex(raw, ":")
	if i < 0 {
		return def
	}
	if n, err := strconv.Atoi(strings.Trim(raw[i+1:], "/")); err == nil && n > 0 {
		return n
	}
	return def
}

// Active devuelve el GGUF de chat en uso: el servidor en marcha, o el que se usaría al arrancar.
func Active() string {
	if m := Current(); m != "" {
		return m
	}
	if m := os.Getenv("LLM_GGUF"); m != "" {
		return filepath.Base(m)
	}
	if m := Saved(); m != "" {
		return m
	}
	return filepath.Base(PickModel("models", false))
}

// CurrentEmbed devuelve el GGUF con el que arrancó el servidor de embeddings.
func CurrentEmbed() string {
	b, err := os.ReadFile(filepath.Join("data", "run", "embed.model"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
