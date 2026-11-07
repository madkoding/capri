package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"capri/internal/config"
)

// Request son los valores explícitos de `capri backend start`; vacíos = por defecto.
type Request struct {
	LLM, Embed         string // GGUF (ruta, o nombre dentro de models/)
	LLMPort, EmbedPort int
	NGL                int
	Accel              string
}

const modelsDir = "models"

func inModels(f string) string {
	if f == "" || filepath.IsAbs(f) {
		return f
	}
	if _, err := os.Stat(f); err == nil {
		return f
	}
	return filepath.Join(modelsDir, f)
}

// StartInternal arranca los servidores internos (llama.cpp) que aún no respondan.
// Modelo de chat: el pedido, o LLM_GGUF, o el elegido con /model, o el mayor de models/.
// Con un proveedor externo activo no hace nada.
func StartInternal(ctx context.Context, r Request) ([]Server, error) {
	cfg := config.Get()
	if cfg.External() {
		return nil, nil
	}
	if r.LLM == "" {
		r.LLM = os.Getenv("LLM_GGUF")
	}
	if r.LLM == "" {
		r.LLM = Saved()
	}
	if r.LLM == "" {
		r.LLM = PickModel(modelsDir, false)
	}
	if r.Embed == "" {
		r.Embed = os.Getenv("EMBED_GGUF")
	}
	if r.Embed == "" {
		r.Embed = config.SavedEmbed(cfg.Home)
	}
	if r.Embed == "" {
		r.Embed = PickModel(modelsDir, true)
	}
	if r.Embed == "" {
		var err error
		if r.Embed, err = ensureEmbedModel(ctx, cfg); err != nil {
			return nil, err
		}
		cfg = config.Get() // puede haber cambiado de familia (ver ensureEmbedModel)
	}
	if r.Accel == "" {
		r.Accel = cfg.Accel
	}
	accel := ResolveAccel(r.Accel)
	bin, err := EnsureRuntime(ctx, accel, nil)
	if err != nil {
		return nil, err
	}
	if r.LLMPort == 0 {
		r.LLMPort = urlPortOr(cfg.LLMURL, 7869)
	}
	if r.EmbedPort == 0 {
		r.EmbedPort = urlPortOr(cfg.EmbedURL, 8081)
	}
	if r.NGL == 0 && accel != AccelCPU {
		r.NGL = 99
	}
	return Start(ctx, Options{
		Bin: bin, LLMGGUF: inModels(r.LLM), EmbedGGUF: inModels(r.Embed),
		LLMPort: r.LLMPort, EmbedPort: r.EmbedPort, NGL: r.NGL, Accel: accel,
		Ctx: cfg.ServerCtx, KVType: cfg.KVType, EmbedPooling: cfg.EmbedPooling,
	})
}

// WaitReady espera a que respondan los servidores internos: los embeddings y, si
// withChat, también el chat.
func WaitReady(ctx context.Context, timeout time.Duration, withChat bool) bool {
	cfg := config.Get()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if httpReady(ctx, cfg.EmbedURL) && (!withChat || httpReady(ctx, cfg.LLMURL)) {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// EnsureRuntime devuelve el llama-server de Capri y, si no hay ninguno instalado,
// descarga el oficial para esta plataforma (la primera vez). logf puede ser nil.
func EnsureRuntime(ctx context.Context, accel string, logf func(string, ...any)) (string, error) {
	cfg := config.Get()
	if cfg.LlamaBin != "" {
		return cfg.LlamaBin, nil
	}
	embedded := EmbeddedTag() != "" && accel == AccelVulkan
	if bin := Installed(accel); bin != "" && !embedded {
		return bin, nil
	}
	if logf == nil {
		logf = func(f string, a ...any) { fmt.Fprintf(os.Stderr, "capri: "+f+"\n", a...) }
	}
	tag := cfg.LlamaTag
	if embedded {
		tag = EmbeddedTag()
	}
	if tag == "" {
		logf("no hay llama.cpp instalado: buscando la última release…")
		t, err := LatestTag(ctx)
		if err != nil {
			return "", fmt.Errorf("no se pudo obtener llama.cpp (%w); instálalo con `capri backend install`", err)
		}
		tag = t
	}
	plan, err := AssetFor(runtime.GOOS, runtime.GOARCH, accel, tag)
	if err != nil {
		return "", err
	}
	if !embedded {
		logf("instalando llama.cpp %s (%s, solo la primera vez)…", tag, plan.Accel)
	}
	dir, err := Install(ctx, plan, tag, logf)
	if err != nil {
		return "", fmt.Errorf("falló la instalación de llama.cpp: %w", err)
	}
	bin := Binary(dir)
	if bin == "" {
		return "", fmt.Errorf("llama-server no está en %s", dir)
	}
	return bin, nil
}

// ensureEmbedModel devuelve el GGUF de embeddings de la familia configurada, bajándolo
// la primera vez. Si no se puede (sin red) y hay otro embedder instalado, usa ese y
// ajusta la configuración a él: nunca se mezclan los prefijos de una familia con los de otra.
func ensureEmbedModel(ctx context.Context, cfg *config.Config) (string, error) {
	if p := PickModel(modelsDir, true); p != "" {
		return p, nil
	}
	var want Entry
	for _, c := range embedCatalogo {
		if (cfg.EmbedFamily == "nomic") == strings.Contains(strings.ToLower(c.File), "nomic") {
			want = c
			break
		}
	}
	fmt.Fprintf(os.Stderr, "capri: descargando el modelo de embeddings %s (solo la primera vez)…\n", want.File)
	path, err := Download(ctx, modelsDir, want, nil)
	if err == nil {
		return path, nil
	}
	for _, e := range EmbedEntries(modelsDir) {
		if e.Installed {
			name := "nomic"
			if !strings.Contains(strings.ToLower(e.File), "nomic") {
				name = strings.TrimSuffix(strings.ToLower(e.File), ".gguf")
			}
			fmt.Fprintf(os.Stderr, "capri: no se pudo bajar %s (%v); uso %s\n", want.File, err, e.File)
			config.ForceEmbedModel(name)
			return filepath.Join(modelsDir, e.File), nil
		}
	}
	return "", fmt.Errorf("no se pudo descargar el modelo de embeddings: %w", err)
}
