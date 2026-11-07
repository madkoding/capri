// Package backend gestiona el runtime nativo de llama.cpp: detecta la
// plataforma y el acelerador, descarga el binario oficial y arranca los dos
// servidores (chat y embeddings) sin contenedores.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Aceleradores soportados.
const (
	AccelAuto   = "auto"
	AccelCUDA   = "cuda"
	AccelROCm   = "rocm"
	AccelMetal  = "metal"
	AccelVulkan = "vulkan"
	AccelCPU    = "cpu"
)

// Plan describe qué asset de llama.cpp corresponde a una plataforma.
type Plan struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Accel  string `json:"accel"`
	Asset  string `json:"asset"`
	CUDART string `json:"cudart,omitempty"`
}

// Ext devuelve la extensión del asset ("zip" o "tar.gz").
func (p Plan) Ext() string {
	if strings.HasSuffix(p.Asset, ".zip") {
		return "zip"
	}
	return "tar.gz"
}

// AssetFor calcula el asset de la release para OS/arquitectura/acelerador.
// tag es el build de llama.cpp (p. ej. "b11972").
func AssetFor(goos, goarch, accel, tag string) (Plan, error) {
	if accel == "" || accel == AccelAuto {
		accel = DefaultAccel(goos)
	}
	if goos == "darwin" {
		accel = AccelMetal
	}
	p := Plan{OS: goos, Arch: goarch, Accel: accel}
	prefix := "llama-" + tag + "-bin-"
	cudart := "cudart-llama-" + tag + "-bin-"
	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			switch accel {
			case AccelCUDA:
				p.Asset = prefix + "ubuntu-cuda-12.8-x64.tar.gz"
				p.CUDART = cudart + "ubuntu-cuda-12.8-x64.tar.gz"
			case AccelROCm:
				p.Asset = prefix + "ubuntu-rocm-10.0-x64.tar.gz"
			case AccelVulkan:
				p.Asset = prefix + "ubuntu-vulkan-x64.tar.gz"
			case AccelCPU:
				p.Asset = prefix + "ubuntu-x64.tar.gz"
			}
		case "arm64":
			switch accel {
			case AccelCUDA:
				p.Asset = prefix + "ubuntu-cuda-13.4-arm64.tar.gz"
				p.CUDART = cudart + "ubuntu-cuda-13.4-arm64.tar.gz"
			case AccelVulkan:
				p.Asset = prefix + "ubuntu-vulkan-arm64.tar.gz"
			case AccelCPU:
				p.Asset = prefix + "ubuntu-arm64.tar.gz"
			}
		}
	case "darwin":
		switch goarch {
		case "arm64":
			p.Asset = prefix + "macos-arm64.tar.gz"
		case "amd64":
			p.Asset = prefix + "macos-x64.tar.gz"
		}
	case "windows":
		switch goarch {
		case "amd64":
			switch accel {
			case AccelCUDA:
				p.Asset = prefix + "win-cuda-12.4-x64.zip"
				p.CUDART = cudart + "win-cuda-12.4-x64.zip"
			case AccelVulkan:
				p.Asset = prefix + "win-vulkan-x64.zip"
			case AccelCPU:
				p.Asset = prefix + "win-cpu-x64.zip"
			}
		case "arm64":
			switch accel {
			case AccelCUDA:
				p.Asset = prefix + "win-cuda-13.4-arm64.zip"
				p.CUDART = cudart + "win-cuda-13.4-arm64.zip"
			case AccelVulkan:
				p.Asset = prefix + "win-vulkan-arm64.zip"
			case AccelCPU:
				p.Asset = prefix + "win-cpu-arm64.zip"
			}
		}
	}
	if p.Asset == "" {
		return p, fmt.Errorf("no hay build de llama.cpp para %s/%s con acelerador %s", goos, goarch, accel)
	}
	return p, nil
}

// DefaultAccel elige el acelerador más razonable para un SO.
func DefaultAccel(goos string) string {
	switch goos {
	case "darwin":
		return AccelMetal
	default:
		return AccelCPU
	}
}

// DetectAccel mira el hardware real (nvidia-smi / rocm-smi).
func DetectAccel() string {
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "nvidia-smi", "-L").Run(); err == nil {
			return AccelCUDA
		}
	}
	if _, err := exec.LookPath("rocm-smi"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "rocm-smi", "--showproductname").Run(); err == nil {
			return AccelROCm
		}
	}
	if runtime.GOOS == "darwin" {
		return AccelMetal
	}
	return AccelCPU
}

// ResolveAccel aplica CAPRI_ACCEL sobre la detección automática.
func ResolveAccel(want string) string {
	if want == "" || want == AccelAuto {
		if EmbeddedTag() != "" { // binario con llama.cpp Vulkan incluido
			return AccelVulkan
		}
		return DetectAccel()
	}
	return want
}

// Dir devuelve el directorio de instalación del runtime para un plan.
func Dir(plan Plan, tag string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "capri", "llama", tag+"-"+plan.Accel), nil
}

// Binary busca el ejecutable de llama-server dentro de dir.
func Binary(dir string) string {
	name := "llama-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var found string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), name) && found == "" {
			found = p
		}
		return nil
	})
	return found
}

// LatestTag obtiene el último build con assets publicados de llama.cpp.
func LatestTag(ctx context.Context) (string, error) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet,
		"https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=10", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github %d", res.StatusCode)
	}
	var rels []struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rels); err != nil {
		return "", err
	}
	for _, r := range rels {
		if len(r.Assets) > 1 && strings.HasPrefix(r.Tag, "b") {
			return r.Tag, nil
		}
	}
	return "", fmt.Errorf("no se encontró una release de llama.cpp con binarios")
}

// Installed busca un runtime ya descargado en la caché del usuario. Si accel
// no está vacío, prefiere la instalación de ese acelerador; si no hay,
// devuelve cualquiera ("" si no hay ninguno).
func Installed(accel string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(base, "capri", "llama")
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var fallback string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		bin := Binary(filepath.Join(root, e.Name()))
		if bin == "" {
			continue
		}
		if accel != "" && strings.HasSuffix(e.Name(), "-"+accel) {
			return bin
		}
		if fallback == "" {
			fallback = bin
		}
	}
	return fallback
}

// Devices ejecuta `llama-server --list-devices` y devuelve los dispositivos
// que el binario puede usar (vacío si solo hay CPU o no arranca).
func Devices(bin string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--list-devices").Output()
	if err != nil {
		return nil
	}
	var devs []string
	inList := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Available devices:") {
			inList = true
			continue
		}
		if !inList || line == "" || strings.HasPrefix(line, "(") {
			continue
		}
		devs = append(devs, line)
	}
	return devs
}
