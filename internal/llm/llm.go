// Package llm es el cliente de llama-server (API OpenAI-compatible) y sus estadísticas.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"capri/internal/config"
)

// Message es un mensaje del chat.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Stats es la última velocidad medida por llama-server.
type Stats struct {
	Gen    float64
	Prompt float64
	Ctx    int
	At     int64
}

var (
	statsMu sync.RWMutex
	stats   Stats
)

// Snapshot devuelve una copia segura de las estadísticas.
func Snapshot() Stats {
	statsMu.RLock()
	defer statsMu.RUnlock()
	return stats
}

func setStats(gen, prompt float64, ctx int) {
	statsMu.Lock()
	defer statsMu.Unlock()
	if gen != 0 {
		stats.Gen = gen
	}
	if prompt != 0 {
		stats.Prompt = prompt
	}
	if ctx != 0 {
		stats.Ctx = ctx
	}
	stats.At = time.Now().UnixMilli()
}

type response struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
	Timings struct {
		PredictedPerSecond float64 `json:"predicted_per_second"`
		PromptPerSecond    float64 `json:"prompt_per_second"`
		PromptN            int     `json:"prompt_n"`
		CacheN             int     `json:"cache_n"`
		PredictedN         int     `json:"predicted_n"`
	} `json:"timings"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func post(ctx context.Context, path string, body any) (*response, error) {
	cfg := config.Get()
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.LLMURL+path, bytes.NewReader(b))
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
		t, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return nil, fmt.Errorf("LLM %d: %s", res.StatusCode, string(t))
	}
	var out response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	gen := out.Timings.PredictedPerSecond
	prompt := out.Timings.PromptPerSecond
	used := 0
	if out.Usage != nil {
		used = out.Usage.PromptTokens + out.Usage.CompletionTokens
	} else {
		used = out.Timings.PromptN + out.Timings.CacheN + out.Timings.PredictedN
	}
	setStats(gen, prompt, used)
	return &out, nil
}

// chunk es un trozo de la respuesta en streaming (SSE).
type chunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Timings struct {
		PredictedPerSecond float64 `json:"predicted_per_second"`
		PromptPerSecond    float64 `json:"prompt_per_second"`
		PromptN            int     `json:"prompt_n"`
		CacheN             int     `json:"cache_n"`
		PredictedN         int     `json:"predicted_n"`
	} `json:"timings"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// postStream hace la misma petición con stream:true y entrega cada trozo a
// onDelta (kind "think" o "answer") según llega. Devuelve la respuesta completa
// con la misma forma que post, así el resto del cliente no cambia.
func postStream(ctx context.Context, path string, body map[string]any, onDelta func(kind, delta string)) (*response, error) {
	cfg := config.Get()
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.LLMURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		t, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return nil, fmt.Errorf("LLM %d: %s", res.StatusCode, string(t))
	}
	var out response
	var content, reasoning strings.Builder
	var sp thinkSplitter
	emit := func(think, answer string) {
		if think != "" {
			reasoning.WriteString(think)
			onDelta("think", think)
		}
		if answer != "" {
			onDelta("answer", answer)
		}
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var c chunk
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			continue
		}
		if c.Timings.PredictedPerSecond != 0 || c.Timings.PredictedN != 0 {
			out.Timings = c.Timings
		}
		if c.Usage != nil {
			out.Usage = c.Usage
		}
		for _, ch := range c.Choices {
			if ch.Delta.ReasoningContent != "" {
				emit(ch.Delta.ReasoningContent, "")
			}
			if ch.Delta.Content != "" {
				content.WriteString(ch.Delta.Content)
				emit(sp.Feed(ch.Delta.Content))
			}
		}
	}
	emit(sp.Flush())
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if content.Len() == 0 && reasoning.Len() == 0 {
			return nil, err
		}
	}
	out.Choices = make([]struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	}, 1)
	out.Choices[0].Message.Content = content.String()
	// el razonamiento que venía dentro de <think> ya está en content; solo el
	// que llega aparte (reasoning_content) se guarda como tal
	if !strings.Contains(content.String(), "<think>") {
		out.Choices[0].Message.ReasoningContent = reasoning.String()
	}
	used := 0
	if out.Usage != nil {
		used = out.Usage.PromptTokens + out.Usage.CompletionTokens
	} else {
		used = out.Timings.PromptN + out.Timings.CacheN + out.Timings.PredictedN
	}
	setStats(out.Timings.PredictedPerSecond, out.Timings.PromptPerSecond, used)
	return &out, nil
}

// thinkSplitter separa en streaming lo que va dentro de <think>…</think> de la
// respuesta, aunque la etiqueta llegue partida entre trozos.
type thinkSplitter struct {
	inThink bool
	buf     string
}

const openTag, closeTag = "<think>", "</think>"

// partialTag devuelve cuántos caracteres finales de s son el inicio (incompleto) de tag.
func partialTag(s, tag string) int {
	for n := len(tag) - 1; n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}
	return 0
}

// Feed procesa un trozo y devuelve lo que ya se puede entregar (think, answer).
func (t *thinkSplitter) Feed(s string) (think, answer string) {
	t.buf += s
	for {
		if t.inThink {
			if i := strings.Index(t.buf, closeTag); i >= 0 {
				think += t.buf[:i]
				t.buf = t.buf[i+len(closeTag):]
				t.inThink = false
				continue
			}
			hold := partialTag(t.buf, closeTag)
			think += t.buf[:len(t.buf)-hold]
			t.buf = t.buf[len(t.buf)-hold:]
			return
		}
		if i := strings.Index(t.buf, openTag); i >= 0 {
			answer += t.buf[:i]
			t.buf = t.buf[i+len(openTag):]
			t.inThink = true
			continue
		}
		hold := partialTag(t.buf, openTag)
		answer += t.buf[:len(t.buf)-hold]
		t.buf = t.buf[len(t.buf)-hold:]
		return
	}
}

// Flush entrega lo retenido al terminar el stream.
func (t *thinkSplitter) Flush() (think, answer string) {
	rest := t.buf
	t.buf = ""
	if t.inThink {
		return rest, ""
	}
	return "", rest
}

var (
	thinkBlock = regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	thinkOpen  = regexp.MustCompile(`(?s)<think>.*$`)
)

var (
	hookMu        sync.RWMutex
	reasoningHook func(string)
)

// SetReasoningHook registra quién recibe el razonamiento del modelo (nil lo quita).
func SetReasoningHook(f func(string)) {
	hookMu.Lock()
	reasoningHook = f
	hookMu.Unlock()
}

func emitReasoning(r string) {
	hookMu.RLock()
	f := reasoningHook
	hookMu.RUnlock()
	if f != nil && strings.TrimSpace(r) != "" {
		f(strings.TrimSpace(r))
	}
}

// leakedTags son formatos de llamada a herramienta que algunos modelos pequeños
// escupen dentro del texto libre; no son para el usuario.
var leakedTags = regexp.MustCompile(`(?s)<(execute_command|tool_call|function_call|invoke|tool_use)>.*?</\x60?(execute_command|tool_call|function_call|invoke|tool_use)>`)

func clean(t string) string {
	t = thinkBlock.ReplaceAllString(t, "")
	t = thinkOpen.ReplaceAllString(t, "")
	t = leakedTags.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// Options de una completación.
type Options struct {
	MaxTokens   int
	Temperature float64
	Stop        []string
	Grammar     string
	// OnDelta recibe el texto según llega (kind "think" o "answer"); con él la
	// petición va en streaming. LLM_STREAM=0 lo desactiva.
	OnDelta func(kind, delta string)
}

func complete(ctx context.Context, messages []Message, o Options) (string, error) {
	cfg := config.Get()
	think := cfg.Thinking && o.Grammar == ""
	out, err := completeOnce(ctx, messages, o, think)
	if err != nil || out != "" || !think {
		return out, err
	}
	// el razonamiento agotó el presupuesto y no quedó respuesta: se repite sin pensar
	emitReasoning("(el razonamiento agotó su presupuesto; respondo sin pensar)")
	return completeOnce(ctx, messages, o, false)
}

func completeOnce(ctx context.Context, messages []Message, o Options, think bool) (string, error) {
	cfg := config.Get()
	maxTokens := o.MaxTokens
	if think {
		maxTokens += cfg.ThinkBudget
	}
	body := map[string]any{
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": o.Temperature,
		"top_p":       0.9,
	}
	if cfg.Model != "" {
		body["model"] = cfg.Model
	}
	// Las extensiones (grammar, cache_prompt, top_k…) solo las entiende
	// llama-server; enviarlas a otros servidores OpenAI-compatible puede
	// provocar un 400.
	if cfg.Backend == "llamacpp" {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": think}
		body["top_k"] = cfg.TopK
		body["min_p"] = cfg.MinP
		body["repeat_penalty"] = cfg.RepeatPenalty
		body["cache_prompt"] = true
	}
	if len(o.Stop) > 0 {
		body["stop"] = o.Stop
	}
	if o.Grammar != "" && cfg.Backend == "llamacpp" {
		body["grammar"] = o.Grammar
	}
	var d *response
	var err error
	if o.OnDelta != nil && o.Grammar == "" && os.Getenv("LLM_STREAM") != "0" {
		d, err = postStream(ctx, "/v1/chat/completions", body, o.OnDelta)
	} else {
		d, err = post(ctx, "/v1/chat/completions", body)
	}
	if err != nil {
		return "", err
	}
	if len(d.Choices) == 0 {
		return "", nil
	}
	m := d.Choices[0].Message
	if o.Grammar == "" { // las llamadas con gramática (gate, bucle ReAct) no se muestran
		reasoning := m.ReasoningContent
		if reasoning == "" {
			if mm := thinkBlock.FindStringSubmatch(m.Content); mm != nil {
				reasoning = mm[1]
			} else if i := strings.Index(m.Content, "<think>"); i >= 0 {
				reasoning = m.Content[i+len("<think>"):] // se cortó sin cerrar: aun así se muestra
			}
		}
		emitReasoning(reasoning)
	}
	out := m.Content
	if out == "" && o.Grammar != "" {
		out = m.ReasoningContent
	}
	return clean(out), nil
}

// Complete es la completación con opciones explícitas.
func Complete(ctx context.Context, messages []Message, o Options) (string, error) {
	return complete(ctx, messages, o)
}

// ChatDefault usa los valores por defecto de configuración.
func ChatDefault(ctx context.Context, messages []Message) (string, error) {
	cfg := config.Get()
	return complete(ctx, messages, Options{MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature})
}

// ChatStream es ChatDefault entregando el razonamiento y la respuesta según llegan.
func ChatStream(ctx context.Context, messages []Message, onDelta func(kind, delta string)) (string, error) {
	cfg := config.Get()
	return complete(ctx, messages, Options{MaxTokens: cfg.MaxTokens, Temperature: cfg.Temperature, OnDelta: onDelta})
}

// Structured pide una salida garantizada por gramática GBNF (valores explícitos).
func Structured(ctx context.Context, messages []Message, grammar string, maxTokens int, temperature float64) (string, error) {
	return complete(ctx, messages, Options{MaxTokens: maxTokens, Temperature: temperature, Grammar: grammar})
}

// Health comprueba que llama-server responde.
func Health(ctx context.Context) bool {
	cfg := config.Get()
	return probe(ctx, cfg.LLMURL)
}

// probe acepta /health (llama-server) o /v1/models (cualquier servidor
// OpenAI-compatible: Ollama, vLLM, LM Studio…).
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
