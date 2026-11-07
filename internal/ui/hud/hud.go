// Package hud lee telemetría real para el HUD: GPU (nvidia-smi o el fichero del sidecar), CPU y llama-server.
package hud

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"capri/internal/config"
	"capri/internal/ui/cpu"
)

// Fields son las columnas pedidas a nvidia-smi.
const Fields = "temperature.gpu,clocks.sm,clocks.max.sm,utilization.gpu,power.draw,power.limit,memory.used,memory.total,clocks_throttle_reasons.active"

// GPU es la telemetría de la tarjeta gráfica.
type GPU struct {
	Temp     float64 `json:"temp"`
	Sm       float64 `json:"sm"`
	SmMax    float64 `json:"smMax"`
	Util     float64 `json:"util"`
	Power    float64 `json:"power"`
	PowerMax float64 `json:"powerMax"`
	MemUsed  float64 `json:"memUsed"`
	MemTotal float64 `json:"memTotal"`
	Thermal  bool    `json:"thermal"`
	PowerCap bool    `json:"powerCap"`
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// ParseGpuLine convierte una línea CSV de nvidia-smi en un objeto GPU (nil si no encaja).
func ParseGpuLine(out string) *GPU {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	raw := strings.Split(line, ",")
	if len(raw) < 9 {
		return nil
	}
	f := make([]string, len(raw))
	for i, x := range raw {
		f[i] = strings.TrimSpace(x)
	}
	reasons, _ := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(f[8]), "0x"), 16, 64)
	g := &GPU{
		Temp:     num(f[0]),
		Sm:       num(f[1]),
		SmMax:    num(f[2]),
		Util:     num(f[3]),
		Power:    num(f[4]),
		PowerMax: num(f[5]),
		MemUsed:  num(f[6]),
		MemTotal: num(f[7]),
		Thermal:  reasons&(0x08|0x20|0x40) != 0,
		PowerCap: reasons&(0x04|0x80) != 0,
	}
	if g.SmMax == 0 {
		g.SmMax = 1
	}
	if g.PowerMax == 0 {
		g.PowerMax = 1
	}
	if g.MemTotal == 0 {
		g.MemTotal = 1
	}
	return g
}

// QueryGpu consulta nvidia-smi en este equipo (solo funciona donde hay driver).
func QueryGpu() *GPU {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu="+Fields, "--format=csv,noheader,nounits").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	return ParseGpuLine(string(out))
}

type gpuFile struct {
	TS  int64 `json:"ts"`
	GPU *GPU  `json:"gpu"`
}

// ReadGpuFile lee el fichero que escribe el sidecar del host. Si es viejo, la GPU no está.
func ReadGpuFile(file string) *GPU {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var f gpuFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil
	}
	if time.Now().UnixMilli()-f.TS > 10000 {
		return nil
	}
	return f.GPU
}

// ReadGpu devuelve la GPU del HUD: el fichero del sidecar si está configurado, si no nvidia-smi directo.
func ReadGpu() *GPU {
	if f := config.Get().GPUFile; f != "" {
		return ReadGpuFile(f)
	}
	return QueryGpu()
}

// ServerInfo es el nombre corto del modelo y el tamaño de contexto de llama-server.
type ServerInfo struct {
	Name string
	Nctx int
}

func getJSON(ctx context.Context, url string) (map[string]any, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, false
	}
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, false
	}
	return m, true
}

// ReadServer consulta /props (y /v1/models) de llama-server.
func ReadServer() *ServerInfo {
	cfg := config.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	props, _ := getJSON(ctx, cfg.LLMURL+"/props")
	name := ""
	if props != nil {
		if v, ok := props["model_path"].(string); ok {
			name = v
		}
		if name == "" {
			if v, ok := props["model_alias"].(string); ok {
				name = v
			}
		}
	}
	if name == "" {
		if models, ok := getJSON(ctx, cfg.LLMURL+"/v1/models"); ok {
			if data, ok := models["data"].([]any); ok && len(data) > 0 {
				if m, ok := data[0].(map[string]any); ok {
					if v, ok := m["id"].(string); ok {
						name = v
					}
				}
			}
		}
	}
	if name == "" {
		return nil
	}
	short := name
	if i := strings.LastIndexByte(short, '/'); i >= 0 {
		short = short[i+1:]
	}
	short = strings.TrimSuffix(short, ".gguf")
	short = strings.TrimSuffix(short, "-instruct")
	nctx := 0
	if props != nil {
		if gen, ok := props["default_generation_settings"].(map[string]any); ok {
			if v, ok := gen["n_ctx"].(float64); ok {
				nctx = int(v)
			}
		}
	}
	return &ServerInfo{Name: short, Nctx: nctx}
}

// ReadModel devuelve solo el nombre del modelo.
func ReadModel() string {
	if s := ReadServer(); s != nil {
		return s.Name
	}
	return ""
}

// Update es una actualización del HUD.
type Update struct {
	Gpu   *GPU
	Cpu   *cpu.Info
	Model string
	Nctx  int
}

// StartHud sondea cada `every` y entrega actualizaciones. Devuelve la función para parar.
func StartHud(onUpdate func(Update), every time.Duration) func() {
	if every == 0 {
		every = 3 * time.Second
	}
	var (
		mu    sync.Mutex
		alive = true
	)
	poll := func() {
		var wg sync.WaitGroup
		var g *GPU
		var c *cpu.Info
		var s *ServerInfo
		wg.Add(3)
		go func() { defer wg.Done(); g = ReadGpu() }()
		go func() { defer wg.Done(); c = cpu.ReadCpu() }()
		go func() { defer wg.Done(); s = ReadServer() }()
		wg.Wait()
		u := Update{Gpu: g, Cpu: c}
		if s != nil {
			u.Model = s.Name
			u.Nctx = s.Nctx
		}
		mu.Lock()
		ok := alive
		mu.Unlock()
		if ok {
			onUpdate(u)
		}
	}
	go poll()
	ticker := time.NewTicker(every)
	go func() {
		for range ticker.C {
			mu.Lock()
			ok := alive
			mu.Unlock()
			if !ok {
				ticker.Stop()
				return
			}
			poll()
		}
	}()
	return func() {
		mu.Lock()
		alive = false
		mu.Unlock()
	}
}

// GpuTelemetryFile devuelve la ruta del fichero del sidecar.
func GpuTelemetryFile(dir string) string {
	if dir == "" {
		dir = "telemetry"
	}
	return filepath.Join(dir, "gpu.json")
}
