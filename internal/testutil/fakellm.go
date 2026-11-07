// Package testutil ofrece un servidor LLM falso para los tests (respuestas guionadas).
package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"capri/internal/llm"
)

// ChatRequest es una petición de chat recibida.
type ChatRequest struct {
	Messages           []llm.Message  `json:"messages"`
	MaxTokens          int            `json:"max_tokens"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
	Stream             bool           `json:"stream"`
}

// FakeLLM imita /v1/chat/completions con una cola de respuestas.
type FakeLLM struct {
	URL string

	mu       sync.Mutex
	queue    []string
	requests []ChatRequest
	failures int
	srv      *httptest.Server
}

// StartFakeLLM arranca el servidor en un puerto libre.
func StartFakeLLM() *FakeLLM {
	f := &FakeLLM{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.requests = append(f.requests, req)
		fail := f.failures > 0
		if fail {
			f.failures--
		}
		reply := "DONE"
		if !fail && len(f.queue) > 0 {
			reply = f.queue[0]
			f.queue = f.queue[1:]
		}
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"simulado"}`))
			return
		}
		if req.Stream {
			// SSE en trozos de 4 runas: así se prueba que las etiquetas <think> partidas se recomponen
			w.Header().Set("content-type", "text/event-stream")
			r := []rune(reply)
			for i := 0; i < len(r); i += 4 {
				piece := string(r[i:min(i+4, len(r))])
				b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": piece}}}})
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
			}
			_, _ = w.Write([]byte(`data: {"choices":[],"timings":{"predicted_per_second":42,"predicted_n":` + fmt.Sprint(len(r)) + `}}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": reply}}},
		})
	}))
	f.URL = f.srv.URL
	return f
}

// Script encola respuestas.
func (f *FakeLLM) Script(replies ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, replies...)
}

// Fail hace que las próximas n llamadas de chat devuelvan 500.
func (f *FakeLLM) Fail(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = n
}

// Reset vacía la cola y el historial.
func (f *FakeLLM) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = nil
	f.requests = nil
	f.failures = 0
}

// Requests devuelve una copia del historial de peticiones.
func (f *FakeLLM) Requests() []ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ChatRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// Close apaga el servidor.
func (f *FakeLLM) Close() { f.srv.Close() }
