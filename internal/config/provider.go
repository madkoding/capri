package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Provider describe un servidor externo OpenAI-compatible (Ollama, vLLM, LM Studio…).
// Por defecto Capri usa sus modelos internos (llama.cpp + GGUF de models/); un
// proveedor externo solo se activa a mano con /provider y se guarda en .capri/provider.json.
type Provider struct {
	Backend    string `json:"backend"` // "openai"
	URL        string `json:"url"`
	EmbedURL   string `json:"embed_url,omitempty"`
	Model      string `json:"model,omitempty"`
	EmbedModel string `json:"embed_model,omitempty"`
}

func providerPath(home string) string { return filepath.Join(home, "provider.json") }

func readProvider(home string) Provider {
	var p Provider
	if b, err := os.ReadFile(providerPath(home)); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

// External indica si hay un proveedor externo activo (no los modelos internos).
func (c *Config) External() bool { return c.Backend != "llamacpp" }

// SetProvider guarda el proveedor (nil = volver a los modelos internos) y lo aplica
// en caliente, conservando el muestreo actual.
func SetProvider(p *Provider) error {
	home := Get().Home
	if p == nil {
		if err := os.Remove(providerPath(home)); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		if p.Backend == "" {
			p.Backend = "openai"
		}
		if strings.TrimSpace(p.URL) == "" {
			return fmt.Errorf("falta la URL del proveedor")
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		if err := os.MkdirAll(home, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(providerPath(home), b, 0o644); err != nil {
			return err
		}
	}
	reloadKeepingSampling()
	return nil
}

// reloadKeepingSampling relee la configuración conservando muestreo y modo auto.
func reloadKeepingSampling() {
	fresh := Load()
	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		fresh.Temperature, fresh.TopK, fresh.MinP, fresh.RepeatPenalty = current.Temperature, current.TopK, current.MinP, current.RepeatPenalty
		fresh.AutoApprove = current.AutoApprove
	}
	current = fresh
}

// ProviderModel recuerda el modelo elegido en el proveedor externo activo.
func ProviderModel(model string) error {
	p := readProvider(Get().Home)
	if p.Backend == "" {
		return fmt.Errorf("no hay proveedor externo activo")
	}
	p.Model = model
	return SetProvider(&p)
}
