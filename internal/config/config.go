// Package config centraliza la configuración de Capri (variables de entorno y valores por defecto).
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"capri/internal/sandbox"
)

// Config refleja src/config.js.
type Config struct {
	LLMURL   string
	EmbedURL string
	GPUFile  string
	DBPath   string
	Workdir  string

	// Home es la carpeta de estado de Capri (.capri/): memoria, trazas, copias de /undo.
	Home string
	// Profile es el perfil de modelos activado a mano (CAPRI_PROFILE o --profile); vacío = valores por defecto.
	Profile string

	// Backend del modelo: "llamacpp" (binario nativo gestionado por Capri) u
	// "openai" (cualquier servidor OpenAI-compatible: Ollama, vLLM, remoto…).
	Backend    string
	Model      string
	EmbedModel string
	// EmbedFamily decide los prefijos de consulta/documento: "nomic" o "qwen3".
	EmbedFamily string
	// Parámetros del servidor llama.cpp que arranca `capri backend start`.
	ServerCtx    int
	KVType       string
	EmbedPooling string
	Accel        string
	LlamaBin     string
	LlamaTag     string
	Sandbox      string

	MaxTokens     int
	Temperature   float64
	TopK          int
	MinP          float64
	RepeatPenalty float64
	Thinking      bool
	ThinkBudget   int
	Gate          bool
	MaxSteps      int
	RecallK       int
	RecallChars   int
	RecencyDays   int
	Mirror        bool

	CtxWindow    int
	CtxThreshold float64
	CtxKeep      int
	Session      bool

	ToolResultChars   int
	MinSim            float64
	Holo              bool
	HoloMinSim        float64
	SkillMinSim       float64
	Skills            bool
	SkillLearn        bool
	SkillMaxGenerated int
	Thr               Thresholds
	ShellTimeoutMs    int
	TraceEnabled      bool
	StoreAnswers      bool
	Web               bool
	SearchURL         string
	Isolated          bool
	AutoApprove       bool
}

var (
	mu      sync.Mutex
	current *Config
)

// Env devuelve una variable CAPRI_<name>.
func Env(name string) string { return os.Getenv("CAPRI_" + name) }

func num(v string, def float64) float64 {
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return n
}

func integer(v string, def int) int { return int(num(v, float64(def))) }

// Load construye la configuración desde el entorno. Si existe un .env en el
// directorio actual, sus claves se usan como valores por defecto (nunca pisan
// variables ya definidas).
func Load() *Config {
	applyProfile(os.Getenv("CAPRI_PROFILE"))
	loadDotEnv()
	env := os.Getenv
	home := env("CAPRI_HOME")
	if home == "" {
		home = HomeDir
	}
	home, _ = filepath.Abs(home)
	db := env("RAG_DB")
	if db == "" {
		db = filepath.Join(home, "brain.db")
	}
	wd := env("WORKDIR")
	if wd == "" {
		wd = "workspace"
	}
	wd, _ = filepath.Abs(wd)
	// modelos internos por defecto; un proveedor externo solo si lo guardó /provider
	// (o si lo fuerzas con CAPRI_BACKEND)
	prov := readProvider(home)
	backend := env("CAPRI_BACKEND")
	if backend == "" {
		backend = first(prov.Backend, "llamacpp")
	}
	llmURL := first(env("CAPRI_URL"), prov.URL)
	embedURL := first(env("EMBED_URL"), first(prov.EmbedURL, prov.URL))
	if backend == "llamacpp" {
		llmURL, embedURL = env("CAPRI_URL"), env("EMBED_URL")
	}
	// embeddings internos por defecto: Qwen3-Embedding-0.6B (nomic sigue disponible con EMBED_MODEL=nomic)
	embedModel := first(env("EMBED_MODEL"), prov.EmbedModel)
	if embedModel == "" && backend == "llamacpp" {
		embedModel = DefaultEmbedModel
		if f := SavedEmbed(home); f != "" && env("EMBED_GGUF") == "" { // elegido con /embed
			embedModel = embedNameFromFile(f)
		}
	}
	if llmURL == "" {
		if backend == "openai" {
			llmURL = "http://localhost:11434/v1"
		} else {
			llmURL = "http://localhost:7869"
		}
	}
	if embedURL == "" {
		if backend == "openai" {
			embedURL = "http://localhost:11434/v1"
		} else {
			embedURL = "http://localhost:8081"
		}
	}
	llmURL = normalizeURL(llmURL)
	embedURL = normalizeURL(embedURL)
	sandboxMode := first(env("CAPRI_SANDBOX"), "auto")
	thr := ThresholdsFor(embedFamily(first(embedModel, "nomic")))
	thr.MinSim = num(env("MIN_SIM"), thr.MinSim)
	thr.Skill = num(env("CAPRI_SKILL_MIN"), thr.Skill)
	return &Config{
		LLMURL:   llmURL,
		EmbedURL: embedURL,
		GPUFile:  env("GPU_FILE"),
		DBPath:   db,
		Workdir:  wd,
		Home:     home,
		Profile:  env("CAPRI_PROFILE"),

		Backend:      backend,
		Model:        first(env("CAPRI_MODEL"), prov.Model),
		EmbedModel:   first(embedModel, "nomic"),
		EmbedFamily:  embedFamily(first(embedModel, "nomic")),
		ServerCtx:    integer(env("CAPRI_SERVER_CTX"), 16384),
		KVType:       env("CAPRI_KV_TYPE"),
		EmbedPooling: first(env("CAPRI_EMBED_POOLING"), poolingFor(embedFamily(first(embedModel, "nomic")))),
		Accel:        first(env("CAPRI_ACCEL"), "auto"),
		LlamaBin:     env("CAPRI_LLAMA_BIN"),
		LlamaTag:     env("CAPRI_LLAMA_TAG"),
		Sandbox:      sandboxMode,

		MaxTokens:     integer(env("MAX_TOKENS"), 350),
		Temperature:   num(env("TEMPERATURE"), 0.3),
		TopK:          integer(env("TOP_K"), 40),
		MinP:          num(env("MIN_P"), 0.05),
		RepeatPenalty: num(env("REPEAT_PENALTY"), 1.0),
		Thinking:      env("LLM_THINKING") == "1",
		ThinkBudget:   integer(env("LLM_THINK_BUDGET"), 1024),
		Gate:          env("CAPRI_GATE") != "0",
		MaxSteps:      integer(env("MAX_STEPS"), 6),
		RecallK:       integer(env("RECALL_K"), 8),
		RecallChars:   integer(env("RECALL_CHARS"), 3000),
		RecencyDays:   integer(env("CAPRI_RECENCY_DAYS"), 30),
		Mirror:        env("CAPRI_MIRROR") != "0",

		CtxWindow:    integer(env("CAPRI_CTX_WINDOW"), 8192),
		CtxThreshold: num(env("CAPRI_CTX_THRESHOLD"), 0.75),
		CtxKeep:      integer(env("CAPRI_CTX_KEEP"), 2),
		Session:      env("CAPRI_SESSION") != "0",

		ToolResultChars:   integer(env("TOOL_RESULT_CHARS"), 3000),
		MinSim:            thr.MinSim,
		Holo:              env("CAPRI_HOLO") != "0",
		HoloMinSim:        num(env("CAPRI_HOLO_MIN"), 0.15),
		SkillMinSim:       thr.Skill,
		Thr:               thr,
		Skills:            env("CAPRI_SKILLS") != "0",
		SkillLearn:        env("CAPRI_SKILL_LEARN") != "0",
		SkillMaxGenerated: integer(env("CAPRI_SKILL_MAX_GENERATED"), 50),
		ShellTimeoutMs:    integer(env("SHELL_TIMEOUT_MS"), 30000),
		TraceEnabled:      env("CAPRI_TRACE") != "0",
		StoreAnswers:      env("STORE_ANSWERS") == "1",
		Web:               env("CAPRI_WEB") != "0",
		SearchURL:         env("CAPRI_SEARCH_URL"),
		Isolated:          sandbox.Active(sandboxMode) && env("CAPRI_ISOLATED") != "0",
		AutoApprove:       env("AUTO_APPROVE") == "1",
	}
}

// normalizeURL quita la barra final y el sufijo /v1: el código siempre añade
// la ruta completa (/v1/chat/completions, /v1/embeddings).
func normalizeURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	u = strings.TrimSuffix(u, "/v1")
	return strings.TrimRight(u, "/")
}

// loadDotEnv lee .env de forma minimalista (KEY=valor, # comentarios).
func loadDotEnv() {
	b, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func first(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Get devuelve la configuración (se carga la primera vez).
func Get() *Config {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		current = Load()
	}
	return current
}

// Reload vuelve a leer el entorno (útil en tests y en subcomandos).
func Reload() *Config {
	mu.Lock()
	defer mu.Unlock()
	current = Load()
	return current
}
