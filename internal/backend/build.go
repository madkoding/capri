package backend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Build compila llama-server desde el código fuente para la GPU de este
// equipo. Es la salida para GPUs que no están en las builds oficiales
// (Pascal sm_61, CUDA antiguas…). Requiere git, cmake y el toolkit del
// acelerador.
func Build(ctx context.Context, accel, tag string, logf func(string, ...any)) (string, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	for _, tool := range []string{"git", "cmake"} {
		if _, err := exec.LookPath(tool); err != nil {
			return "", fmt.Errorf("falta %s en el PATH", tool)
		}
	}
	if tag == "" {
		return "", fmt.Errorf("falta el build de llama.cpp (--tag bNNNN)")
	}
	plan, err := AssetFor(runtime.GOOS, runtime.GOARCH, accel, tag)
	if err != nil {
		return "", err
	}
	dir, err := Dir(plan, tag)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "capri-llama-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	logf("clonando llama.cpp (%s)…", tag)
	clone := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", tag,
		"https://github.com/ggml-org/llama.cpp", tmp)
	clone.Stdout = os.Stderr
	clone.Stderr = os.Stderr
	if err := clone.Run(); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}
	args := []string{
		"-B", filepath.Join(tmp, "build"),
		"-DCMAKE_BUILD_TYPE=Release",
		"-DLLAMA_CURL=OFF",
		"-DLLAMA_BUILD_TESTS=OFF",
		"-DLLAMA_BUILD_EXAMPLES=OFF",
		"-DLLAMA_BUILD_SERVER=ON",
	}
	switch plan.Accel {
	case AccelCUDA:
		args = append(args, "-DGGML_CUDA=ON")
		if cc := ComputeCap(); cc != "" {
			args = append(args, "-DCMAKE_CUDA_ARCHITECTURES="+cc)
			logf("arquitectura CUDA detectada: %s", cc)
		}
	case AccelROCm:
		args = append(args, "-DGGML_HIP=ON")
	case AccelVulkan:
		args = append(args, "-DGGML_VULKAN=ON")
	case AccelMetal:
		args = append(args, "-DGGML_METAL=ON")
	}
	logf("configurando…")
	cfg := exec.CommandContext(ctx, "cmake", args...)
	cfg.Dir = tmp
	cfg.Stdout = os.Stderr
	cfg.Stderr = os.Stderr
	if err := cfg.Run(); err != nil {
		return "", fmt.Errorf("cmake configure: %w", err)
	}
	logf("compilando llama-server (puede tardar varios minutos)…")
	bld := exec.CommandContext(ctx, "cmake", "--build", filepath.Join(tmp, "build"),
		"--target", "llama-server", "-j")
	bld.Stdout = os.Stderr
	bld.Stderr = os.Stderr
	if err := bld.Run(); err != nil {
		return "", fmt.Errorf("cmake build: %w", err)
	}
	binDir := filepath.Join(tmp, "build", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "llama-server") && !strings.Contains(name, ".so") &&
			!strings.HasSuffix(name, ".dylib") && !strings.HasSuffix(name, ".dll") {
			continue
		}
		src := filepath.Join(binDir, name)
		dst := filepath.Join(dir, name)
		if err := copyFile(src, dst); err != nil {
			return "", err
		}
	}
	bin := Binary(dir)
	if bin == "" {
		return "", fmt.Errorf("la compilación no produjo llama-server")
	}
	return dir, nil
}

// ComputeCap devuelve la capacidad de cómputo de la primera GPU NVIDIA
// ("61" para una P4), o "" si no hay nvidia-smi.
func ComputeCap() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	return strings.ReplaceAll(line, ".", "")
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if st, err := os.Stat(src); err == nil {
		mode = st.Mode()
	}
	return os.WriteFile(dst, b, mode)
}
