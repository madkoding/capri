package backend

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"capri/internal/config"
	"time"
)

// Options configura el arranque de los servidores.
type Options struct {
	Bin       string
	LLMGGUF   string
	EmbedGGUF string
	LLMPort   int
	EmbedPort int
	NGL       int
	LogDir    string
	Accel     string
	// Ctx es la ventana del servidor de chat (-c); KVType cuantiza la caché KV (p. ej. q8_0).
	Ctx          int
	KVType       string
	EmbedPooling string
}

// Server describe un proceso lanzado.
type Server struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
	URL  string `json:"url"`
	Log  string `json:"log"`
}

// State es el estado de un servidor.
type State struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	PID   int    `json:"pid"`
	Alive bool   `json:"alive"`
	Ready bool   `json:"ready"`
	Log   string `json:"log"`
}

func (o *Options) defaults() {
	if o.LLMPort == 0 {
		o.LLMPort = 7869
	}
	if o.EmbedPort == 0 {
		o.EmbedPort = 8081
	}
	if o.LogDir == "" {
		o.LogDir = filepath.Join("data", "run")
	}
	if o.Ctx == 0 {
		o.Ctx = 16384
	}
	if o.EmbedPooling == "" {
		o.EmbedPooling = "mean"
	}
	if o.NGL == 0 && o.Accel != AccelCPU {
		o.NGL = 99
	}
}

// Start lanza llama-server para chat y embeddings.
func Start(ctx context.Context, o Options) ([]Server, error) {
	o.defaults()
	if o.Bin == "" {
		return nil, fmt.Errorf("no se encontró llama-server: ejecuta `capri backend install`")
	}
	if o.LLMGGUF == "" || o.EmbedGGUF == "" {
		return nil, fmt.Errorf("faltan modelos GGUF en models/ (usa /model para bajar uno, o --llm y --embed)")
	}
	if err := os.MkdirAll(o.LogDir, 0o755); err != nil {
		return nil, err
	}
	llmArgs := llmArgs(o)
	embedArgs := []string{
		"-m", o.EmbedGGUF, "-ngl", strconv.Itoa(o.NGL), "--embeddings", "--pooling", o.EmbedPooling,
		"-c", "2048", "-b", "2048", "-ub", "2048", "--no-webui",
		"--host", "127.0.0.1", "--port", strconv.Itoa(o.EmbedPort),
	}
	var out []Server
	for _, s := range []struct {
		name string
		args []string
		port int
	}{
		{"llm", llmArgs, o.LLMPort},
		{"embed", embedArgs, o.EmbedPort},
	} {
		url := fmt.Sprintf("http://127.0.0.1:%d", s.port)
		gguf := o.LLMGGUF
		if s.name == "embed" {
			gguf = o.EmbedGGUF
		}
		if httpReady(ctx, url) {
			// los embeddings deben ser del modelo configurado: si el servidor en marcha usa
			// otro (o no sabemos cuál), se reinicia; el chat se respeta (se cambia con /model)
			prev, _ := os.ReadFile(filepath.Join(o.LogDir, s.name+".model"))
			if s.name != "embed" || strings.TrimSpace(string(prev)) == filepath.Base(gguf) {
				continue // ya está en marcha: no se duplica
			}
			stopOne(o.LogDir, s.name)
			for i := 0; i < 50 && httpReady(ctx, url); i++ {
				time.Sleep(200 * time.Millisecond)
			}
		}
		srv, err := launch(o, s.name, s.args, s.port)
		if err != nil {
			return out, err
		}
		_ = os.WriteFile(filepath.Join(o.LogDir, s.name+".model"), []byte(filepath.Base(gguf)), 0o644)
		out = append(out, srv)
	}
	return out, nil
}

func launch(o Options, name string, args []string, port int) (Server, error) {
	logPath := filepath.Join(o.LogDir, name+".log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return Server{}, err
	}
	cmd := exec.Command(o.Bin, args...)
	cmd.Env = libEnv(o.Bin)
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.Stdin = nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		lf.Close()
		return Server{}, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	_ = lf.Close()
	_ = os.WriteFile(filepath.Join(o.LogDir, name+".pid"), []byte(strconv.Itoa(pid)), 0o644)
	return Server{Name: name, PID: pid, URL: fmt.Sprintf("http://127.0.0.1:%d", port), Log: logPath}, nil
}

// llmArgs construye la línea de comandos del servidor de chat.
func llmArgs(o Options) []string {
	args := []string{
		"-m", o.LLMGGUF, "-ngl", strconv.Itoa(o.NGL), "-c", strconv.Itoa(o.Ctx),
		"--temp", "0.7", "--no-webui", "--host", "127.0.0.1", "--port", strconv.Itoa(o.LLMPort),
	}
	if o.KVType != "" {
		args = append(args, "-ctk", o.KVType, "-ctv", o.KVType)
	}
	return args
}

// Stop termina los servidores arrancados por Start.
func Stop(logDir string) error {
	if logDir == "" {
		logDir = filepath.Join("data", "run")
	}
	for _, name := range []string{"llm", "embed"} {
		stopOne(logDir, name)
	}
	return nil
}

// Status consulta los PIDs y la disponibilidad HTTP de cada servidor.
func Status(ctx context.Context, logDir string) []State {
	if logDir == "" {
		logDir = filepath.Join("data", "run")
	}
	ports := map[string]int{"llm": 7869, "embed": 8081}
	var out []State
	for _, name := range []string{"llm", "embed"} {
		st := State{Name: name, URL: fmt.Sprintf("http://127.0.0.1:%d", ports[name])}
		if b, err := os.ReadFile(filepath.Join(logDir, name+".pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				st.PID = pid
				st.Alive = alive(pid)
			}
		}
		st.Ready = httpReady(ctx, st.URL)
		out = append(out, st)
	}
	return out
}

func httpReady(ctx context.Context, base string) bool {
	for _, path := range []string{"/health", "/v1/models"} {
		c, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		req, err := http.NewRequestWithContext(c, http.MethodGet, base+path, nil)
		if err != nil {
			cancel()
			continue
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
			cancel()
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				return true
			}
			continue
		}
		cancel()
	}
	return false
}

// PickModel elige un GGUF de modelsDir. Si embed es true busca modelos de
// embeddings (el de la familia configurada primero: Qwen3-Embedding por defecto,
// nomic si EMBED_MODEL=nomic); si no, descarta los de embeddings y devuelve el mayor.
func PickModel(modelsDir string, embed bool) string {
	entries, err := os.ReadDir(modelsDir)
	if err != nil {
		return ""
	}
	want := "qwen"
	if config.Get().EmbedFamily == "nomic" {
		want = "nomic"
	}
	var best string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".gguf") {
			continue
		}
		low := strings.ToLower(e.Name())
		isEmbed := strings.Contains(low, "embed") || strings.Contains(low, "nomic")
		if embed != isEmbed {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if embed {
			// solo el de la familia configurada: usar otro con los prefijos de esta degrada
			// el enrutado y la memoria sin avisar (StartInternal decide qué hacer si falta)
			if strings.Contains(low, want) {
				return filepath.Join(modelsDir, e.Name())
			}
			continue
		}
		if info.Size() > bestSize {
			best, bestSize = filepath.Join(modelsDir, e.Name()), info.Size()
		}
	}
	return best
}

// libEnv añade el directorio del binario a la ruta de librerías dinámicas,
// porque los paquetes de llama.cpp traen .so/.dll junto al ejecutable.
func libEnv(bin string) []string {
	env := os.Environ()
	dir := filepath.Dir(bin)
	if runtime.GOOS == "windows" {
		key, sep := "PATH", ";"
		for i, v := range env {
			if strings.HasPrefix(strings.ToUpper(v), key+"=") {
				env[i] = v + sep + dir
				return env
			}
		}
		return append(env, key+"="+dir)
	}
	val := dir
	if old := os.Getenv("LD_LIBRARY_PATH"); old != "" {
		val += string(os.PathListSeparator) + old
	}
	return append(env, "LD_LIBRARY_PATH="+val)
}

// stopOne termina un servidor concreto a partir de su archivo .pid.
func stopOne(logDir, name string) {
	pidPath := filepath.Join(logDir, name+".pid")
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
	_ = os.Remove(pidPath)
}
