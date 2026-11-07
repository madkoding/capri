package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"

	"capri/internal/backend"
	"capri/internal/config"
)

// runBackend implementa `capri backend <install|start|stop|status>`.
func runBackend(args []string) {
	if len(args) == 0 {
		fmt.Println(`uso: capri backend <comando>

  status            muestra el estado de los servidores
  install [flags]   descarga el runtime de llama.cpp para este equipo
  start [flags]     arranca chat (7869) y embeddings (8081)
  stop              detiene ambos servidores

flags de install: --accel auto|cuda|rocm|metal|vulkan|cpu   --tag bNNNN   --build
flags globales:   --profile qwen3   activa a mano un perfil de modelos (o CAPRI_PROFILE=qwen3)
flags de start:   --llm archivo.gguf  --embed archivo.gguf  --port N  --embed-port N  --ngl N  --accel X`)
		return
	}
	cfg := config.Get()
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("backend install", flag.ExitOnError)
		accel := fs.String("accel", cfg.Accel, "acelerador: auto|cuda|rocm|metal|vulkan|cpu")
		tag := fs.String("tag", cfg.LlamaTag, "build de llama.cpp (por defecto, el último)")
		build := fs.Bool("build", false, "compila desde fuente (GPUs no incluidas en las builds oficiales)")
		_ = fs.Parse(args[1:])
		ctx := context.Background()
		if *tag == "" {
			*tag = backend.EmbeddedTag() // binario con llama.cpp incluido: sin red
		}
		if *tag == "" {
			fmt.Println("buscando la última release de llama.cpp…")
			t, err := backend.LatestTag(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "capri:", err)
				os.Exit(1)
			}
			*tag = t
		}
		a := backend.ResolveAccel(*accel)
		plan, err := backend.AssetFor(runtime.GOOS, runtime.GOARCH, a, *tag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "capri:", err)
			os.Exit(1)
		}
		fmt.Printf("plataforma %s/%s · acelerador %s\n", plan.OS, plan.Arch, plan.Accel)
		logf := func(f string, a ...any) { fmt.Printf(f+"\n", a...) }
		var dir string
		if *build {
			dir, err = backend.Build(ctx, a, *tag, logf)
		} else {
			dir, err = backend.Install(ctx, plan, *tag, logf)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "capri:", err)
			os.Exit(1)
		}
		fmt.Println("instalado en", dir)
		if devs := backend.Devices(backend.Binary(dir)); len(devs) > 0 {
			for _, d := range devs {
				fmt.Println("  dispositivo:", d)
			}
		} else if plan.Accel != backend.AccelCPU {
			fmt.Println("aviso: el binario no ve ningún dispositivo", plan.Accel+";")
			fmt.Println("       prueba `--accel vulkan` o compila con `--build`")
		}
	case "start":
		fs := flag.NewFlagSet("backend start", flag.ExitOnError)
		llm := fs.String("llm", os.Getenv("LLM_GGUF"), "GGUF de chat")
		embed := fs.String("embed", os.Getenv("EMBED_GGUF"), "GGUF de embeddings")
		port := fs.Int("port", urlPort(cfg.LLMURL, 7869), "puerto del chat")
		eport := fs.Int("embed-port", urlPort(cfg.EmbedURL, 8081), "puerto de embeddings")
		ngl := fs.Int("ngl", 0, "capas en GPU (0 = automático)")
		accelFlag := fs.String("accel", cfg.Accel, "acelerador: auto|cuda|rocm|metal|vulkan|cpu")
		_ = fs.Parse(args[1:])
		servers, err := backend.StartInternal(context.Background(), backend.Request{
			LLM: *llm, Embed: *embed, LLMPort: *port, EmbedPort: *eport, NGL: *ngl, Accel: *accelFlag,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "capri:", err)
			os.Exit(1)
		}
		for _, s := range servers {
			fmt.Printf("%s  pid %d  %s  (log: %s)\n", s.Name, s.PID, s.URL, s.Log)
		}
	case "stop":
		if err := backend.Stop(""); err != nil {
			fmt.Fprintln(os.Stderr, "capri:", err)
			os.Exit(1)
		}
		fmt.Println("servidores detenidos")
	case "status":
		for _, st := range backend.Status(context.Background(), "") {
			state := "parado"
			if st.Alive {
				state = "vivo"
			}
			ready := "sin respuesta"
			if st.Ready {
				ready = "listo"
			}
			fmt.Printf("%-6s pid %-6d %-28s %s · %s\n", st.Name, st.PID, st.URL, state, ready)
		}
	default:
		fmt.Fprintf(os.Stderr, "capri: subcomando desconocido %q\n", args[0])
		os.Exit(1)
	}
}

func urlPort(raw string, def int) int {
	u, err := url.Parse(raw)
	if err != nil {
		return def
	}
	if p, err := strconv.Atoi(strings.TrimSpace(u.Port())); err == nil && p > 0 {
		return p
	}
	return def
}
