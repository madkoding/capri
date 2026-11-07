// Command capri es el agente local con segundo cerebro: TUI, preguntas sueltas,
// deshacer, evaluaciones y utilidades de trazas y telemetría.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"capri/internal/agent"
	"capri/internal/backend"
	"capri/internal/config"
	"capri/internal/eval"
	"capri/internal/event"
	"capri/internal/gputel"
	"capri/internal/rag"
	"capri/internal/tools"
	"capri/internal/tracecli"
	"capri/internal/trash"
	"capri/internal/tui"
)

const usage = `capri — agente local con segundo cerebro

uso: capri                     abre la TUI
     capri ask "..."           una pregunta, sin TUI (útil en scripts)
     capri undo                deshace el último turno que modificó archivos o memoria
     capri eval [flags]        evalúa tareas multi-paso (--heldout, --fresh, --blind, --blind2, --trace)
     capri from-trace [flags]  convierte trazas de uso real en tareas de evaluación (--stats)
     capri gpu-telemetry       publica telemetry/gpu.json para la TUI aislada
     capri backend <cmd>       instala/arranca/para llama.cpp nativo
     capri doctor [--json]     comprueba plataforma, backend, memoria y sandbox

env: CAPRI_URL EMBED_URL CAPRI_BACKEND RAG_DB WORKDIR AUTO_APPROVE=1 ...  (ver internal/config)`

func main() {
	args := takeProfile(os.Args[1:])
	config.MigrateLegacy()
	config.ApplyPreset(backend.Active())
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			fmt.Println(usage)
			return
		case "eval":
			os.Exit(eval.Run(args[1:]))
		case "from-trace":
			os.Exit(tracecli.Run(args[1:]))
		case "gpu-telemetry":
			runGpuTelemetry(args[1:])
			return
		case "backend":
			runBackend(args[1:])
			return
		case "doctor":
			runDoctor(args[1:])
			return
		}
	}

	cfg := config.Get()
	if err := os.MkdirAll(cfg.Workdir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "capri:", err)
		os.Exit(1)
	}
	if len(args) == 0 || args[0] == "ask" {
		autoStart(len(args) > 0)
	}
	r, err := rag.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "capri: no se pudo abrir la base:", err)
		os.Exit(1)
	}
	defer r.Close()
	if n, err := r.EnsureEmbeddings(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "capri: reindexado de memoria:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "capri: %d recuerdos reindexados con %s\n", n, cfg.EmbedModel)
	}

	if n, err := r.SyncMarkdown(); err != nil {
		fmt.Fprintln(os.Stderr, "capri: espejo de memoria:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "capri: %d recuerdos importados desde .capri/memory/\n", n)
	}

	if len(args) > 0 {
		switch args[0] {
		case "undo":
			t, err := trash.New(trash.Options{})
			if err != nil {
				fmt.Fprintln(os.Stderr, "capri:", err)
				os.Exit(1)
			}
			_, msg := t.Undo(context.Background(), r)
			fmt.Println(msg)
			return
		case "ask":
			text := strings.TrimSpace(strings.Join(args[1:], " "))
			if text == "" {
				fmt.Fprintln(os.Stderr, `capri ask necesita una pregunta: capri ask "..."`)
				os.Exit(1)
			}
			a := agent.New(r, func(string, string, tools.ConfirmOptions) bool {
				return config.Get().AutoApprove
			}, nil)
			// la respuesta se imprime según llega; el razonamiento va a stderr
			var streamed strings.Builder
			ans, err := a.Run(context.Background(), text, func(e event.Event) {
				if e.Type != "delta" {
					return
				}
				if e.Phase == "think" {
					fmt.Fprint(os.Stderr, e.Text)
				} else {
					fmt.Print(e.Text)
					streamed.WriteString(e.Text)
				}
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "capri:", err)
				os.Exit(1)
			}
			if s := strings.TrimSpace(streamed.String()); s != "" && strings.Contains(ans, s) {
				fmt.Println()
			} else {
				fmt.Println(ans)
			}
			a.WaitPending()
			return
		default:
			fmt.Fprintf(os.Stderr, "capri: subcomando desconocido %q\n\n%s\n", args[0], usage)
			os.Exit(1)
		}
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "capri necesita una terminal interactiva (usa `capri ask \"...\"` en scripts).")
		os.Exit(1)
	}
	if err := tui.Start(r); err != nil {
		fmt.Fprintln(os.Stderr, "capri:", err)
		os.Exit(1)
	}
}

func runGpuTelemetry(args []string) {
	dir := ""
	var every time.Duration
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dir":
			if i+1 < len(args) {
				i++
				dir = args[i]
			}
		case "--every":
			if i+1 < len(args) {
				i++
				if n, err := strconv.Atoi(args[i]); err == nil {
					every = time.Duration(n) * time.Millisecond
				}
			}
		}
	}
	if err := gputel.Run(dir, every); err != nil {
		fmt.Fprintln(os.Stderr, "capri:", err)
		os.Exit(1)
	}
}

// takeProfile extrae `--profile <nombre>` / `--profile=<nombre>` de los argumentos,
// lo activa y devuelve el resto. Los perfiles nunca se activan solos.
func takeProfile(args []string) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := ""
		switch {
		case a == "--profile" && i+1 < len(args):
			i++
			name = args[i]
		case strings.HasPrefix(a, "--profile="):
			name = strings.TrimPrefix(a, "--profile=")
		default:
			rest = append(rest, a)
			continue
		}
		if err := config.SetProfile(name); err != nil {
			fmt.Fprintln(os.Stderr, "capri:", err)
			os.Exit(1)
		}
	}
	return rest
}

// autoStart levanta los modelos internos (llama.cpp) si no están en marcha: es el
// comportamiento por defecto. Con un proveedor externo (/provider) no hace nada.
// `ask` espera también al chat; la TUI solo a los embeddings y sigue cargando el modelo en segundo plano.
func autoStart(waitChat bool) {
	if config.Get().External() {
		return
	}
	servers, err := backend.StartInternal(context.Background(), backend.Request{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "capri: modelos internos:", err)
		return
	}
	if len(servers) > 0 {
		fmt.Fprintln(os.Stderr, "capri: arrancando modelos internos…")
	}
	timeout := 45 * time.Second
	if waitChat {
		timeout = 3 * time.Minute
	}
	backend.WaitReady(context.Background(), timeout, waitChat)
}
