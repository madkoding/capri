package backend

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseURL = "https://github.com/ggml-org/llama.cpp/releases/download/"

// Install descarga y extrae el runtime de llama.cpp para el plan dado.
// Devuelve el directorio donde quedó llama-server.
func Install(ctx context.Context, plan Plan, tag string, logf func(string, ...any)) (string, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	dir, err := Dir(plan, tag)
	if err != nil {
		return "", err
	}
	if bin := Binary(dir); bin != "" {
		logf("ya instalado: %s", bin)
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if data, etag, easset, ok := embeddedRuntime(); ok && plan.Accel == AccelVulkan && tag == etag {
		logf("extrayendo el llama.cpp Vulkan incluido en Capri (%s)", etag)
		tmp, err := os.CreateTemp("", "capri-llama-*")
		if err != nil {
			return "", err
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.Write(data)
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", werr
		}
		if err := extract(tmp.Name(), easset, dir); err != nil {
			return "", err
		}
		bin := Binary(dir)
		if bin == "" {
			return "", fmt.Errorf("el runtime incluido no trae llama-server")
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(bin, 0o755)
		}
		return dir, nil
	}
	assets := []string{plan.Asset}
	if plan.CUDART != "" {
		assets = append(assets, plan.CUDART)
	}
	for _, asset := range assets {
		logf("descargando %s", asset)
		tmp, err := download(ctx, releaseURL+tag+"/"+asset)
		if err != nil {
			return "", fmt.Errorf("%s: %w", asset, err)
		}
		err = extract(tmp, asset, dir)
		_ = os.Remove(tmp)
		if err != nil {
			return "", fmt.Errorf("%s: %w", asset, err)
		}
	}
	bin := Binary(dir)
	if bin == "" {
		return "", fmt.Errorf("el paquete no trae llama-server (revisa %s)", dir)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(bin, 0o755)
	}
	return dir, nil
}

func download(ctx context.Context, url string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	f, err := os.CreateTemp("", "capri-llama-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func extract(file, asset, dir string) error {
	if strings.HasSuffix(asset, ".zip") {
		return extractZip(file, dir)
	}
	return extractTarGz(file, dir)
}

func safeJoin(dir, name string) (string, error) {
	name = filepath.FromSlash(name)
	p := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, p)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("ruta insegura en el paquete: %s", name)
	}
	return p, nil
}

func extractZip(file, dir string) error {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		dst, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if err := writeFile(dst, rc, f.Mode()); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return nil
}

func extractTarGz(file, dir string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		dst, err := safeJoin(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(h.Mode)
			if err := writeFile(dst, tr, mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Los paquetes de llama.cpp usan enlaces para los sonames
			// (libllama.so.0 → libllama.so.0.8.0).
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			_ = os.Remove(dst)
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return err
			}
		case tar.TypeLink:
			target, err := safeJoin(dir, h.Linkname)
			if err != nil {
				return err
			}
			_ = os.Remove(dst)
			if err := os.Link(target, dst); err != nil {
				return err
			}
		}
	}
}

func writeFile(dst string, r io.Reader, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	mode |= 0o600
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, r)
	return err
}
