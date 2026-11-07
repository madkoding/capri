// Package doctor comprueba que el entorno puede ejecutar Capri: plataforma,
// backend de modelos, memoria, modelos GGUF y aislamiento.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"capri/internal/backend"
	"capri/internal/config"
	"capri/internal/embed"
	"capri/internal/llm"
	"capri/internal/rag"
	"capri/internal/sandbox"
	"capri/internal/shell"
)

// Estados de una comprobación.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
)

// Check es el resultado de una comprobación.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Report agrupa todas las comprobaciones.
type Report struct {
	Checks []Check `json:"checks"`
}

// Failed indica si alguna comprobación es bloqueante.
func (r Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return true
		}
	}
	return false
}

// Run ejecuta todas las comprobaciones.
func Run(ctx context.Context) Report {
	cfg := config.Get()
	var checks []Check
	add := func(name, status, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	add("plataforma", OK, fmt.Sprintf("%s/%s · shell %s", runtime.GOOS, runtime.GOARCH, shell.ShellName()))

	accel := backend.ResolveAccel(cfg.Accel)
	if accel == backend.AccelCPU && cfg.Accel != backend.AccelCPU {
		add("acelerador", Warn, "no se detectó GPU; se usará CPU (más lento)")
	} else {
		add("acelerador", OK, accel)
	}

	if cfg.Backend == "llamacpp" {
		bin := cfg.LlamaBin
		if bin == "" {
			bin = backend.Installed(accel)
		}
		if bin == "" {
			add("llama.cpp", Warn, "no instalado: ejecuta `capri backend install`")
		} else {
			add("llama.cpp", OK, bin)
			devs := backend.Devices(bin)
			switch {
			case len(devs) > 0:
				add("dispositivos", OK, strings.Join(devs, " · "))
			case accel == backend.AccelCPU:
				add("dispositivos", OK, "solo CPU")
			default:
				add("dispositivos", Warn, "el binario no ve GPU "+accel+"; prueba `--accel vulkan` o `backend install --build`")
			}
		}
	} else {
		add("backend", OK, fmt.Sprintf("%s (%s)", cfg.Backend, cfg.LLMURL))
	}

	if llm.Health(ctx) {
		add("chat", OK, cfg.LLMURL)
	} else {
		add("chat", Fail, "sin respuesta en "+cfg.LLMURL+" (¿`capri backend start`?)")
	}
	if embed.Health(ctx) {
		add("embeddings", OK, cfg.EmbedURL)
	} else {
		add("embeddings", Fail, "sin respuesta en "+cfg.EmbedURL)
	}

	models := "models"
	chat := backend.PickModel(models, false)
	emb := backend.PickModel(models, true)
	switch {
	case chat == "" && emb == "":
		add("modelos", Warn, "no hay GGUF en "+models+"/ (scripts/get-models.sh)")
	case chat == "":
		add("modelos", Warn, "falta un GGUF de chat; solo hay embeddings")
	case emb == "":
		add("modelos", Warn, "falta un GGUF de embeddings")
	default:
		add("modelos", OK, filepath.Base(chat)+" + "+filepath.Base(emb))
	}

	if _, err := os.Stat(cfg.DBPath); err != nil {
		add("memoria", Warn, "se creará al arrancar: "+cfg.DBPath)
	} else if r, err := rag.Open(cfg.DBPath); err != nil {
		add("memoria", Fail, err.Error())
	} else {
		add("memoria", OK, fmt.Sprintf("%s (%d recuerdos)", cfg.DBPath, r.Count()))
		_ = r.Close()
	}

	if name := sandbox.Name(cfg.Sandbox); name != "" {
		add("aislamiento", OK, name+" (solo escritura en workspace/ y .capri/)")
	} else if cfg.Sandbox == sandbox.None {
		add("aislamiento", Warn, "desactivado por CAPRI_SANDBOX=none")
	} else {
		add("aislamiento", Warn, "no disponible en este equipo; el agente corre sin sandbox")
	}

	if _, err := exec.LookPath("git"); err == nil {
		add("git", OK, "disponible")
	} else {
		add("git", Warn, "no encontrado (afecta a tareas que clonan repos)")
	}

	return Report{Checks: checks}
}

// Print escribe el informe en texto.
func (r Report) Print() {
	for _, c := range r.Checks {
		icon := map[string]string{OK: "✓", Warn: "!", Fail: "✗"}[c.Status]
		fmt.Printf("%s %-14s %s\n", icon, c.Name, c.Detail)
	}
}

// Summary devuelve un resumen de una línea.
func (r Report) Summary() string {
	var ok, warn, fail int
	for _, c := range r.Checks {
		switch c.Status {
		case OK:
			ok++
		case Warn:
			warn++
		case Fail:
			fail++
		}
	}
	return fmt.Sprintf("%d ok · %d avisos · %d fallos", ok, warn, fail)
}
