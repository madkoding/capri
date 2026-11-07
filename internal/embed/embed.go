// Package embed habla con el servicio de embeddings (llama-server + nomic-embed).
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"

	"capri/internal/config"
)

// prefixes por familia de modelo: nomic usa etiquetas; Qwen3-Embedding no lleva
// prefijo en los documentos y pide una instrucción en la consulta.
var prefixes = map[string]map[string]string{
	"nomic": {"doc": "search_document: ", "query": "search_query: "},
	"qwen3": {"doc": "", "query": "Instruct: Given a user message, retrieve the stored memories relevant to it\nQuery: "},
}

func normalize(v []float64) []float32 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	if n == 0 {
		n = 1
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x / n)
	}
	return out
}

// Embed devuelve los vectores normalizados de los textos.
func Embed(ctx context.Context, texts []string, kind string) ([][]float32, error) {
	cfg := config.Get()
	input := make([]string, len(texts))
	prefix := prefixes[cfg.EmbedFamily]
	if prefix == nil {
		prefix = prefixes["nomic"]
	}
	for i, t := range texts {
		input[i] = prefix[kind] + t
	}
	body, err := json.Marshal(map[string]any{"model": cfg.EmbedModel, "input": input})
	if err != nil {
		return nil, err
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodPost, cfg.EmbedURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return nil, fmt.Errorf("embed %d: %s", res.StatusCode, string(b))
	}
	var data struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, err
	}
	sort.Slice(data.Data, func(i, j int) bool { return data.Data[i].Index < data.Data[j].Index })
	out := make([][]float32, len(data.Data))
	for i, d := range data.Data {
		out[i] = normalize(d.Embedding)
	}
	return out, nil
}

// Health comprueba que el servicio de embeddings responde.
func Health(ctx context.Context) bool {
	cfg := config.Get()
	return probe(ctx, cfg.EmbedURL)
}

// probe acepta tanto llama-server (/health) como servidores OpenAI-compatible
// que solo exponen /v1/models.
func probe(ctx context.Context, base string) bool {
	for _, path := range []string{"/health", "/v1/models"} {
		c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
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
