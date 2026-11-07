package backend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const hf = "https://huggingface.co/"

// Entry es un modelo de chat que /model puede cargar: ya descargado o del catálogo.
type Entry struct {
	File      string
	URL       string // vacío si solo existe en disco
	Installed bool
}

// catalogo: modelos de chat que se pueden bajar desde Hugging Face con /model
// (los mismos de scripts/get-models.sh). El archivo local es el nombre en models/.
var catalogo = []Entry{
	{File: "qwen3-8b-gsq-Q2_K.gguf", URL: hf + "ISTA-DASLab/Qwen3-8B-GGUF-GSQ/resolve/main/qwen3-8b-Q2_K.gguf"},
	{File: "ornith-1.5-9b-heretic-gsq-rco-iq3_m.gguf", URL: hf + "motchii709/Ornith-1.5-9B-heretic-GSQ-RCO-IQ3_M-GGUF/resolve/main/Ornith-1.5-9B-heretic-GSQ-RCO-IQ3_M.gguf"},
	{File: "qwen2.5-3b-instruct-q4_k_m.gguf", URL: hf + "Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf"},
	{File: "qwen2.5-3b-instruct-q2_k.gguf", URL: hf + "Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q2_k.gguf"},
	{File: "qwen2.5-7b-instruct-q4_k_m.gguf", URL: hf + "bartowski/Qwen2.5-7B-Instruct-GGUF/resolve/main/Qwen2.5-7B-Instruct-Q4_K_M.gguf"},
	{File: "qwen2.5-7b-instruct-q2_k.gguf", URL: hf + "Qwen/Qwen2.5-7B-Instruct-GGUF/resolve/main/qwen2.5-7b-instruct-q2_k.gguf"},
}

// Entries une lo instalado en modelsDir con el catálogo (lo instalado primero, por nombre).
func Entries(modelsDir string) []Entry {
	have := map[string]bool{}
	var out []Entry
	for _, f := range ChatModels(modelsDir) {
		have[f] = true
		e := Entry{File: f, Installed: true}
		for _, c := range catalogo {
			if c.File == f {
				e.URL = c.URL
			}
		}
		out = append(out, e)
	}
	var rest []Entry
	for _, c := range catalogo {
		if !have[c.File] {
			rest = append(rest, c)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].File < rest[j].File })
	return append(out, rest...)
}

// hfEntry interpreta `hf:usuario/repo/archivo.gguf` o una URL de huggingface.co.
func hfEntry(q string) (Entry, bool) {
	var u string
	switch {
	case strings.HasPrefix(q, "hf:"):
		parts := strings.SplitN(strings.TrimPrefix(q, "hf:"), "/", 3)
		if len(parts) != 3 || !strings.HasSuffix(strings.ToLower(parts[2]), ".gguf") {
			return Entry{}, false
		}
		u = hf + parts[0] + "/" + parts[1] + "/resolve/main/" + parts[2]
	case strings.HasPrefix(q, hf):
		u = strings.Replace(q, "/blob/", "/resolve/", 1)
	default:
		return Entry{}, false
	}
	pu, err := url.Parse(u)
	if err != nil || !strings.HasSuffix(strings.ToLower(pu.Path), ".gguf") {
		return Entry{}, false
	}
	return Entry{File: filepath.Base(pu.Path), URL: u}, true
}

// Pick elige una entrada por número, nombre o trozo de nombre; también acepta
// `hf:usuario/repo/archivo.gguf` o una URL de Hugging Face.
func Pick(modelsDir, q string) (Entry, error) {
	return pickFrom(modelsDir, Entries(modelsDir), q)
}

func pickFrom(modelsDir string, list []Entry, q string) (Entry, error) {
	if e, ok := hfEntry(q); ok {
		if _, err := os.Stat(filepath.Join(modelsDir, e.File)); err == nil {
			e.Installed = true
		}
		return e, nil
	}
	var n int
	if _, err := fmt.Sscanf(q, "%d", &n); err == nil && fmt.Sprint(n) == q {
		if n < 1 || n > len(list) {
			return Entry{}, fmt.Errorf("no hay modelo #%d (hay %d)", n, len(list))
		}
		return list[n-1], nil
	}
	var hits []Entry
	for _, e := range list {
		if strings.EqualFold(e.File, q) {
			return e, nil
		}
		if strings.Contains(strings.ToLower(e.File), strings.ToLower(q)) {
			hits = append(hits, e)
		}
	}
	var local []Entry // si algo instalado coincide, manda sobre lo descargable
	for _, h := range hits {
		if h.Installed {
			local = append(local, h)
		}
	}
	if len(local) > 0 {
		hits = local
	}
	switch len(hits) {
	case 0:
		return Entry{}, fmt.Errorf("ningún modelo coincide con %q (también vale hf:usuario/repo/archivo.gguf)", q)
	case 1:
		return hits[0], nil
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.File
	}
	return Entry{}, fmt.Errorf("%q es ambiguo: %s", q, strings.Join(names, ", "))
}

// Download baja un GGUF a modelsDir con reanudación (.part) y devuelve su ruta.
// progress recibe bytes hechos y totales (total 0 si se desconoce). HF_TOKEN se usa si existe.
func Download(ctx context.Context, modelsDir string, e Entry, progress func(done, total int64)) (string, error) {
	if e.URL == "" {
		return "", fmt.Errorf("%s no está en models/ y no tiene URL de descarga", e.File)
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(modelsDir, e.File)
	part := dest + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL, nil)
	if err != nil {
		return "", err
	}
	if tok := os.Getenv("HF_TOKEN"); tok != "" && strings.HasPrefix(e.URL, hf) {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch res.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusOK:
		have = 0 // el servidor ignoró Range: se empieza de cero
		flags |= os.O_TRUNC
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("Hugging Face rechazó la descarga (%d): ¿modelo con acceso restringido? define HF_TOKEN", res.StatusCode)
	default:
		return "", fmt.Errorf("descarga fallida: %s", res.Status)
	}
	total := res.ContentLength
	if total > 0 {
		total += have
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 1<<20)
	done := have
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return "", fmt.Errorf("descarga interrumpida (se puede reanudar): %w", rerr)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if total > 0 && done != total {
		return "", fmt.Errorf("descarga incompleta: %d de %d bytes (se puede reanudar)", done, total)
	}
	return dest, os.Rename(part, dest)
}

var embedCatalogo = []Entry{
	{File: "Qwen3-Embedding-0.6B-Q8_0.gguf", URL: hf + "Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf"},
	{File: "nomic-embed-text-v1.5.Q8_0.gguf", URL: hf + "nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf"},
}

// EmbedEntries une los GGUF de embeddings instalados con el catálogo.
func EmbedEntries(modelsDir string) []Entry {
	have := map[string]bool{}
	var out []Entry
	if des, err := os.ReadDir(modelsDir); err == nil {
		for _, d := range des {
			low := strings.ToLower(d.Name())
			if d.IsDir() || !strings.HasSuffix(low, ".gguf") || !(strings.Contains(low, "embed") || strings.Contains(low, "nomic")) {
				continue
			}
			have[d.Name()] = true
			e := Entry{File: d.Name(), Installed: true}
			for _, c := range embedCatalogo {
				if c.File == d.Name() {
					e.URL = c.URL
				}
			}
			out = append(out, e)
		}
	}
	for _, c := range embedCatalogo {
		if !have[c.File] {
			out = append(out, c)
		}
	}
	return out
}

// PickEmbed elige un modelo de embeddings por número, nombre o hf:usuario/repo/archivo.gguf.
func PickEmbed(modelsDir, q string) (Entry, error) {
	return pickFrom(modelsDir, EmbedEntries(modelsDir), q)
}
