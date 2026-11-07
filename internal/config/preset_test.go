package config

import (
	"os"
	"testing"
)

func TestApplyPreset(t *testing.T) {
	t.Cleanup(func() { Reload() })
	for _, k := range []string{"TEMPERATURE", "TOP_K", "MIN_P", "REPEAT_PENALTY", "CAPRI_PROFILE", "RAG_DB", "CAPRI_HOME"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	Reload()
	if d := ApplyPreset("ornith-1.5-9b-heretic-gsq-rco-iq3_m.gguf"); d == "" {
		t.Fatal("ornith debe tener preset")
	}
	if c := Get(); c.RepeatPenalty != 1.05 || c.TopK != 20 || c.Temperature != 0.3 {
		t.Fatalf("ornith: %+v", c)
	}
	// otro modelo sin preset vuelve a los valores por defecto
	if d := ApplyPreset("qwen2.5-7b-instruct-q4_k_m.gguf"); d != "" {
		t.Fatalf("no debería haber preset: %s", d)
	}
	if c := Get(); c.RepeatPenalty != 1.0 || c.TopK != 40 {
		t.Fatalf("no se restauró: %+v", c)
	}
	// el entorno explícito gana
	t.Setenv("REPEAT_PENALTY", "1.2")
	ApplyPreset("ornith.gguf")
	if c := Get(); c.RepeatPenalty != 1.2 {
		t.Fatalf("env debe ganar: %v", c.RepeatPenalty)
	}
	t.Setenv("REPEAT_PENALTY", "")
	// preset del usuario, más específico que el integrado
	_ = os.MkdirAll(".capri", 0o755)
	_ = os.WriteFile(".capri/presets.json", []byte(`{"ornith-1.5-9b-heretic":{"repeat_penalty":1.1}}`), 0o644)
	Reload()
	ApplyPreset("Ornith-1.5-9B-heretic-x.gguf")
	if c := Get(); c.RepeatPenalty != 1.1 {
		t.Fatalf("usuario: %v", c.RepeatPenalty)
	}
}

func TestProviderDefaultsToInternal(t *testing.T) {
	t.Cleanup(func() { Reload() })
	for _, k := range []string{"CAPRI_BACKEND", "CAPRI_URL", "EMBED_URL", "CAPRI_MODEL", "EMBED_MODEL", "CAPRI_PROFILE", "RAG_DB", "CAPRI_HOME"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	if c := Reload(); c.External() || c.LLMURL != "http://localhost:7869" || c.EmbedURL != "http://localhost:8081" {
		t.Fatalf("por defecto debe ser interno: %+v", c)
	}
	if err := SetProvider(&Provider{URL: "http://localhost:11434/v1/", Model: "qwen2.5:7b", EmbedModel: "nomic-embed-text"}); err != nil {
		t.Fatal(err)
	}
	c := Get()
	if !c.External() || c.LLMURL != "http://localhost:11434" || c.EmbedURL != "http://localhost:11434" || c.Model != "qwen2.5:7b" || c.EmbedModel != "nomic-embed-text" {
		t.Fatalf("externo: %+v", c)
	}
	if c := Reload(); !c.External() { // persiste en .capri/provider.json
		t.Fatal("el proveedor debe persistir")
	}
	if err := SetProvider(nil); err != nil {
		t.Fatal(err)
	}
	if c := Get(); c.External() || c.LLMURL != "http://localhost:7869" {
		t.Fatalf("de vuelta a interno: %+v", c)
	}
	if err := SetProvider(&Provider{}); err == nil {
		t.Fatal("sin URL debe fallar")
	}
}

func TestEmbedNameAndSavedChoice(t *testing.T) {
	t.Cleanup(func() { Reload() })
	for k, want := range map[string]string{
		"Qwen3-Embedding-0.6B-Q8_0.gguf":  "qwen3-embedding-0.6b",
		"nomic-embed-text-v1.5.Q8_0.gguf": "nomic-embed-text-v1.5",
		"Qwen3-Embedding-4B-f16.gguf":     "qwen3-embedding-4b",
	} {
		if got := embedNameFromFile(k); got != want {
			t.Errorf("%s → %s, quiero %s", k, got, want)
		}
	}
	for _, k := range []string{"EMBED_MODEL", "EMBED_GGUF", "CAPRI_BACKEND", "CAPRI_PROFILE", "RAG_DB", "CAPRI_HOME", "CAPRI_EMBED_POOLING"} {
		t.Setenv(k, "")
	}
	t.Chdir(t.TempDir())
	Reload()
	if err := SetEmbedGGUF("nomic-embed-text-v1.5.Q8_0.gguf"); err != nil {
		t.Fatal(err)
	}
	if c := Get(); c.EmbedModel != "nomic-embed-text-v1.5" || c.EmbedFamily != "nomic" || c.EmbedPooling != "mean" {
		t.Fatalf("elegido: %+v", c)
	}
	if err := SetEmbedGGUF(""); err != nil {
		t.Fatal(err)
	}
	if c := Get(); c.EmbedModel != DefaultEmbedModel || c.EmbedPooling != "last" {
		t.Fatalf("predeterminado: %+v", c)
	}
}

func TestThresholdsFollowEmbedFamily(t *testing.T) {
	t.Cleanup(func() { Reload() })
	t.Setenv("MIN_SIM", "")
	t.Setenv("CAPRI_SKILL_MIN", "")
	t.Setenv("EMBED_MODEL", "nomic")
	if c := Reload(); c.Thr.Route != 0.6 || c.MinSim != 0.45 || c.SkillMinSim != 0.67 {
		t.Fatalf("nomic: %+v", c.Thr)
	}
	t.Setenv("EMBED_MODEL", "qwen3-embedding-0.6b")
	if c := Reload(); c.Thr.Route != 0.45 || c.MinSim != 0.4 || c.SkillMinSim != 0.55 {
		t.Fatalf("qwen3: %+v", c.Thr)
	}
	t.Setenv("MIN_SIM", "0.33")
	if c := Reload(); c.MinSim != 0.33 || c.Thr.MinSim != 0.33 {
		t.Fatalf("el entorno manda: %v", c.MinSim)
	}
}
