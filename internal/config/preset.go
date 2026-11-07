package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Preset son los parámetros de muestreo recomendados para una familia de modelos.
// Los campos nulos no se tocan. Se aplican al arrancar y con /model.
type Preset struct {
	Temperature   *float64 `json:"temperature,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	MinP          *float64 `json:"min_p,omitempty"`
	RepeatPenalty *float64 `json:"repeat_penalty,omitempty"`
}

func f(v float64) *float64 { return &v }
func i(v int) *int         { return &v }

// presets integrados, por trozo del nombre del GGUF (en minúsculas). Se pueden
// ampliar o pisar en .capri/presets.json con el mismo formato.
var presets = map[string]Preset{
	// ficha de Ornith-heretic: repeat-penalty 1.05 ("1.0 puede entrar en bucles"), top-k 20
	"ornith": {RepeatPenalty: f(1.05), TopK: i(20)},
	// Qwen3 sin razonamiento: top-k 20, min-p 0; temperatura moderada (no voraz)
	"qwen3-8b": {Temperature: f(0.6), TopK: i(20), MinP: f(0)},
}

func loadUserPresets() map[string]Preset {
	out := map[string]Preset{}
	b, err := os.ReadFile(filepath.Join(Get().Home, "presets.json"))
	if err != nil {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}

// PresetFor busca el preset de un GGUF: primero los del usuario, luego los
// integrados; gana el trozo de nombre más largo que coincida.
func PresetFor(gguf string) (name string, p Preset, ok bool) {
	low := strings.ToLower(gguf)
	for _, src := range []map[string]Preset{loadUserPresets(), presets} {
		var keys []string
		for k := range src {
			if k != "" && strings.Contains(low, strings.ToLower(k)) {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			sort.Slice(keys, func(a, b int) bool { return len(keys[a]) > len(keys[b]) })
			return keys[0], src[keys[0]], true
		}
	}
	return "", Preset{}, false
}

// ApplyPreset deja el muestreo en los valores por defecto + el preset del modelo
// (si lo hay). Las variables de entorno explícitas (TEMPERATURE, TOP_K, MIN_P,
// REPEAT_PENALTY) siempre ganan. Devuelve una descripción de lo aplicado.
// Sustituye la configuración por una copia, así que los lectores concurrentes no se ven afectados.
func ApplyPreset(gguf string) string {
	base := Load()
	name, p, ok := PresetFor(gguf) // antes del candado: lee .capri/presets.json con Get()
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		current = base
	}
	next := *current
	next.Temperature, next.TopK, next.MinP, next.RepeatPenalty = base.Temperature, base.TopK, base.MinP, base.RepeatPenalty
	var parts []string
	if ok {
		if p.Temperature != nil && os.Getenv("TEMPERATURE") == "" {
			next.Temperature = *p.Temperature
			parts = append(parts, fmt.Sprintf("temp %.2g", next.Temperature))
		}
		if p.TopK != nil && os.Getenv("TOP_K") == "" {
			next.TopK = *p.TopK
			parts = append(parts, fmt.Sprintf("top-k %d", next.TopK))
		}
		if p.MinP != nil && os.Getenv("MIN_P") == "" {
			next.MinP = *p.MinP
			parts = append(parts, fmt.Sprintf("min-p %.2g", next.MinP))
		}
		if p.RepeatPenalty != nil && os.Getenv("REPEAT_PENALTY") == "" {
			next.RepeatPenalty = *p.RepeatPenalty
			parts = append(parts, fmt.Sprintf("rep %.2g", next.RepeatPenalty))
		}
	}
	current = &next
	if len(parts) == 0 {
		return ""
	}
	return "preset «" + name + "»: " + strings.Join(parts, " · ")
}

// SetThinking activa o desactiva el razonamiento del modelo en caliente.
func SetThinking(on bool) {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		current = Load()
	}
	next := *current
	next.Thinking = on
	current = &next
}
