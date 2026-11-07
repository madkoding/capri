package backend

import (
	"capri/internal/config"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAssetFor(t *testing.T) {
	cases := []struct {
		goos, goarch, accel string
		asset, cudart       string
	}{
		{"linux", "amd64", "cuda", "llama-b100-bin-ubuntu-cuda-12.8-x64.tar.gz", "cudart-llama-b100-bin-ubuntu-cuda-12.8-x64.tar.gz"},
		{"linux", "amd64", "rocm", "llama-b100-bin-ubuntu-rocm-10.0-x64.tar.gz", ""},
		{"linux", "amd64", "vulkan", "llama-b100-bin-ubuntu-vulkan-x64.tar.gz", ""},
		{"linux", "amd64", "cpu", "llama-b100-bin-ubuntu-x64.tar.gz", ""},
		{"linux", "arm64", "cpu", "llama-b100-bin-ubuntu-arm64.tar.gz", ""},
		{"linux", "arm64", "cuda", "llama-b100-bin-ubuntu-cuda-13.4-arm64.tar.gz", "cudart-llama-b100-bin-ubuntu-cuda-13.4-arm64.tar.gz"},
		{"darwin", "arm64", "auto", "llama-b100-bin-macos-arm64.tar.gz", ""},
		{"darwin", "amd64", "cpu", "llama-b100-bin-macos-x64.tar.gz", ""},
		{"windows", "amd64", "cuda", "llama-b100-bin-win-cuda-12.4-x64.zip", "cudart-llama-b100-bin-win-cuda-12.4-x64.zip"},
		{"windows", "amd64", "cpu", "llama-b100-bin-win-cpu-x64.zip", ""},
		{"windows", "arm64", "vulkan", "llama-b100-bin-win-vulkan-arm64.zip", ""},
		{"windows", "arm64", "cpu", "llama-b100-bin-win-cpu-arm64.zip", ""},
	}
	for _, c := range cases {
		p, err := AssetFor(c.goos, c.goarch, c.accel, "b100")
		if err != nil {
			t.Fatalf("%s/%s/%s: %v", c.goos, c.goarch, c.accel, err)
		}
		if p.Asset != c.asset {
			t.Errorf("%s/%s/%s: asset=%q, quiero %q", c.goos, c.goarch, c.accel, p.Asset, c.asset)
		}
		if p.CUDART != c.cudart {
			t.Errorf("%s/%s/%s: cudart=%q, quiero %q", c.goos, c.goarch, c.accel, p.CUDART, c.cudart)
		}
	}
}

func TestAssetForUnsupported(t *testing.T) {
	if _, err := AssetFor("plan9", "amd64", "cpu", "b100"); err == nil {
		t.Fatal("esperaba error para plataforma sin binarios")
	}
	if _, err := AssetFor("linux", "riscv64", "cpu", "b100"); err == nil {
		t.Fatal("esperaba error para arquitectura sin binarios")
	}
}

func TestPlanExt(t *testing.T) {
	if got := (Plan{Asset: "x.zip"}).Ext(); got != "zip" {
		t.Errorf("zip: %q", got)
	}
	if got := (Plan{Asset: "x.tar.gz"}).Ext(); got != "tar.gz" {
		t.Errorf("tar.gz: %q", got)
	}
}

func TestPickModel(t *testing.T) {
	t.Cleanup(func() { config.Reload() }) // antes que Setenv: corre después de restaurar el entorno
	t.Setenv("EMBED_MODEL", "nomic")      // el embedder elegido es de la familia nomic
	config.Reload()
	dir := t.TempDir()
	write := func(name string, size int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nomic-embed-text.Q8_0.gguf", 10)
	write("small.gguf", 20)
	write("big.gguf", 40)
	write("notas.txt", 100)

	if got, want := PickModel(dir, true), filepath.Join(dir, "nomic-embed-text.Q8_0.gguf"); got != want {
		t.Errorf("embed=%q, quiero %q", got, want)
	}
	if got, want := PickModel(dir, false), filepath.Join(dir, "big.gguf"); got != want {
		t.Errorf("chat=%q, quiero %q", got, want)
	}
	if got := PickModel(filepath.Join(dir, "no-existe"), false); got != "" {
		t.Errorf("directorio inexistente: %q", got)
	}
}

func TestPickModelPrefersConfiguredEmbedder(t *testing.T) {
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "") // familia por defecto: qwen3
	config.Reload()
	d := t.TempDir()
	for _, f := range []string{"Qwen3-Embedding-0.6B-Q8_0.gguf", "nomic-embed-text-v1.5.Q8_0.gguf", "chat.gguf"} {
		if err := os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(PickModel(d, true)); got != "Qwen3-Embedding-0.6B-Q8_0.gguf" {
		t.Errorf("embed por defecto = %s", got)
	}
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "nomic")
	config.Reload()
	if got := filepath.Base(PickModel(d, true)); got != "nomic-embed-text-v1.5.Q8_0.gguf" {
		t.Errorf("embed nomic = %s", got)
	}
	if got := filepath.Base(PickModel(d, false)); got != "chat.gguf" {
		t.Errorf("chat = %s", got)
	}
}

func TestPickEntries(t *testing.T) {
	d := t.TempDir()
	for _, f := range []string{"b-ornith.gguf", "a-qwen3.gguf", "nomic-embed.gguf"} {
		_ = os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644)
	}
	if e, err := Pick(d, "1"); err != nil || e.File != "a-qwen3.gguf" || !e.Installed {
		t.Errorf("por número: %+v %v", e, err)
	}
	if e, err := Pick(d, "ornith"); err != nil || e.File != "b-ornith.gguf" || !e.Installed {
		t.Errorf("instalado: %+v %v", e, err)
	}
	// no instalado pero en el catálogo → descargable
	if e, err := Pick(d, "heretic"); err != nil || e.Installed || e.URL == "" {
		t.Errorf("catálogo: %+v %v", e, err)
	}
	if e, err := Pick(d, "hf:usuario/repo/mi-modelo.Q4_K_M.gguf"); err != nil || e.File != "mi-modelo.Q4_K_M.gguf" ||
		e.URL != "https://huggingface.co/usuario/repo/resolve/main/mi-modelo.Q4_K_M.gguf" {
		t.Errorf("hf: %+v %v", e, err)
	}
	if _, err := Pick(d, "qwen2.5-7b"); err == nil {
		t.Error("qwen2.5-7b es ambiguo (q2 y q4)")
	}
	if _, err := Pick(d, "99"); err == nil {
		t.Error("fuera de rango")
	}
}

func TestDownloadResumes(t *testing.T) {
	payload := strings.Repeat("abcdefgh", 1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "m.gguf", time.Time{}, strings.NewReader(payload)) // soporta Range
	}))
	defer srv.Close()
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "m.gguf.part"), []byte(payload[:3000]), 0o644)
	var lastDone, lastTotal int64
	path, err := Download(context.Background(), d, Entry{File: "m.gguf", URL: srv.URL + "/m.gguf"}, func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != payload || lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Fatalf("contenido/progreso incorrectos: %d %d %d", len(got), lastDone, lastTotal)
	}
	if _, err := os.Stat(path + ".part"); err == nil {
		t.Error("el .part debe desaparecer")
	}
}

func TestLLMArgs(t *testing.T) {
	a := llmArgs(Options{LLMGGUF: "m.gguf", Ctx: 8192, KVType: "q8_0", LLMPort: 7869})
	j := " " + strings.Join(a, " ") + " "
	for _, want := range []string{" -c 8192 ", " -ctk q8_0 -ctv q8_0", " -m m.gguf "} {
		if !strings.Contains(j, want) {
			t.Errorf("falta %q en %s", want, j)
		}
	}
}

func TestEnsureRuntimeUsesConfiguredBinary(t *testing.T) {
	t.Setenv("CAPRI_LLAMA_BIN", "/opt/mi/llama-server")
	config.Reload()
	t.Cleanup(func() { config.Reload() })
	if got, err := EnsureRuntime(context.Background(), AccelCPU, func(string, ...any) {}); err != nil || got != "/opt/mi/llama-server" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestPickEmbed(t *testing.T) {
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "nomic-embed-text-v1.5.Q8_0.gguf"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(d, "chat.gguf"), []byte("x"), 0o644)
	list := EmbedEntries(d)
	if len(list) != 2 || !list[0].Installed || list[1].Installed || list[1].URL == "" {
		t.Fatalf("lista: %+v", list)
	}
	if e, err := PickEmbed(d, "qwen3"); err != nil || e.Installed || e.File != "Qwen3-Embedding-0.6B-Q8_0.gguf" {
		t.Errorf("catálogo: %+v %v", e, err)
	}
	if e, err := PickEmbed(d, "1"); err != nil || !e.Installed {
		t.Errorf("número: %+v %v", e, err)
	}
}

func TestPickModelDoesNotMixEmbedFamilies(t *testing.T) {
	d := t.TempDir()
	_ = os.WriteFile(filepath.Join(d, "nomic-embed-text-v1.5.Q8_0.gguf"), []byte("x"), 0o644)
	t.Cleanup(func() { config.Reload() })
	t.Setenv("EMBED_MODEL", "")
	config.Reload()
	if got := PickModel(d, true); got != "" {
		t.Errorf("con familia qwen3 no debe devolver nomic: %q", got)
	}
}
