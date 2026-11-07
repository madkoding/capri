// Package skills gestiona la biblioteca de recetas (skills) y su búsqueda por embeddings.
package skills

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"capri/internal/config"
	"capri/internal/embed"
	"capri/internal/llm"
	"capri/internal/text"
)

// Turn es un turno de ejemplo (usuario/asistente).
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Skill es una receta con su descripción y ejemplos.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Turns       []Turn `json:"turns"`
}

// Closest es la skill más parecida a un texto.
type Closest struct {
	Skill Skill
	Score float64
}

func projectDir() string {
	// 1) directorio de trabajo (el binario se lanza desde la raíz del repo o /app)
	if wd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(wd, "skills")); err == nil {
			return wd
		}
	}
	// 2) junto al ejecutable (instalación empaquetada)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "skills")); err == nil {
			return dir
		}
	}
	// 3) raíz del módulo en tiempo de compilación (tests y `go run`)
	if _, file, _, ok := runtime.Caller(0); ok {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	}
	wd, _ := os.Getwd()
	return wd
}

func pickDir(name string) string {
	env := os.Getenv("CAPRI_SKILLS_DIR")
	if env != "" {
		if name == "generated" {
			return filepath.Join(env, "generated")
		}
		return env
	}
	root := projectDir()
	dir := filepath.Join(root, name)
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	abs, _ := filepath.Abs(filepath.Join(name))
	return abs
}

var (
	SkillsDir    = pickDir("skills")
	GeneratedDir = filepath.Join(SkillsDir, "generated")
	CachePath    = filepath.Join(projectDir(), "data", "skills-index.json")
)

var (
	mu      sync.Mutex
	library []Skill
	loaded  bool
	index   []indexItem
)

type indexItem struct {
	Skill Skill
	Vec   []float32
}

var headerRe = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?(.*)$`)
var metaRe = regexp.MustCompile(`(?m)^(\w+):\s*(.+)$`)

// ParseSkill convierte el texto de un archivo en una Skill (nil si no tiene cabecera válida).
func ParseSkill(src string) *Skill {
	m := headerRe.FindStringSubmatch(src)
	if m == nil {
		return nil
	}
	meta := map[string]string{}
	for _, mm := range metaRe.FindAllStringSubmatch(m[1], -1) {
		meta[mm[1]] = strings.TrimSpace(mm[2])
	}
	if meta["name"] == "" || meta["description"] == "" {
		return nil
	}
	var examples [][]Turn
	for _, raw := range strings.Split(m[2], "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Objetivo:") {
			examples = append(examples, []Turn{{Role: "user", Content: line}})
		} else if len(examples) > 0 {
			ex := &examples[len(examples)-1]
			role := "user"
			if len(*ex)%2 == 1 {
				role = "assistant"
			}
			*ex = append(*ex, Turn{Role: role, Content: line})
		}
	}
	var turns []Turn
	for _, ex := range examples {
		turns = append(turns, ex...)
	}
	return &Skill{Name: meta["name"], Description: meta["description"], Turns: turns}
}

// LoadSkills lee los .md de un directorio, ordenados por nombre.
func LoadSkills(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out []Skill
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if s := ParseSkill(string(b)); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func allSkills() []Skill {
	var out []Skill
	out = append(out, LoadSkills(SkillsDir)...)
	out = append(out, LoadSkills(GeneratedDir)...)
	return out
}

func ensureLibrary() {
	if !loaded {
		library = allSkills()
		loaded = true
	}
}

func dot(a, b []float32) float64 {
	var s float64
	for i := 0; i < len(a) && i < len(b); i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

type cacheFile struct {
	Key  string      `json:"key"`
	Vecs [][]float32 `json:"vecs"`
}

type buildItem struct {
	Skill Skill
	Text  string
}

func build(ctx context.Context) ([]indexItem, error) {
	ensureLibrary()
	var items []buildItem
	for _, s := range library {
		items = append(items, buildItem{Skill: s, Text: s.Description})
		for _, t := range s.Turns {
			if t.Role == "user" {
				items = append(items, buildItem{Skill: s, Text: strings.TrimPrefix(t.Content, "Objetivo: ")})
			}
		}
	}
	h := sha1.New()
	fmt.Fprintf(h, "%s|%s\n", config.Get().EmbedURL, config.Get().EmbedModel) // otro modelo = otros vectores
	for _, it := range items {
		fmt.Fprintf(h, "%s\n", it.Text)
	}
	key := hex.EncodeToString(h.Sum(nil))
	if b, err := os.ReadFile(CachePath); err == nil {
		var c cacheFile
		if json.Unmarshal(b, &c) == nil && c.Key == key && len(c.Vecs) == len(items) {
			out := make([]indexItem, len(items))
			for i, it := range items {
				out[i] = indexItem{Skill: it.Skill, Vec: c.Vecs[i]}
			}
			return out, nil
		}
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Text
	}
	vecs, err := embed.Embed(ctx, texts, "query")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(CachePath), 0o755); err == nil {
		if b, err := json.Marshal(cacheFile{Key: key, Vecs: vecs}); err == nil {
			_ = os.WriteFile(CachePath, b, 0o644)
		}
	}
	out := make([]indexItem, len(items))
	for i, it := range items {
		out[i] = indexItem{Skill: it.Skill, Vec: vecs[i]}
	}
	return out, nil
}

func getIndex(ctx context.Context) ([]indexItem, error) {
	mu.Lock()
	defer mu.Unlock()
	if index != nil {
		return index, nil
	}
	idx, err := build(ctx)
	if err != nil {
		return nil, err
	}
	index = idx
	return index, nil
}

// FindSkills devuelve las k skills más parecidas al objetivo.
func FindSkills(ctx context.Context, goal string, k int) []Skill {
	if k == 0 {
		k = 2
	}
	ensureLibrary()
	cfg := config.Get()
	if !cfg.Skills || len(library) == 0 || strings.TrimSpace(goal) == "" {
		return nil
	}
	idx, err := getIndex(ctx)
	if err == nil {
		q, e := embed.Embed(ctx, []string{goal}, "query")
		if e == nil && len(q) > 0 {
			best := map[string]float64{}
			order := map[string]Skill{}
			for _, it := range idx {
				score := dot(q[0], it.Vec)
				if old, ok := best[it.Skill.Name]; !ok || score > old {
					best[it.Skill.Name] = score
					order[it.Skill.Name] = it.Skill
				}
			}
			type pair struct {
				s     Skill
				score float64
			}
			var pairs []pair
			for name, score := range best {
				if score >= cfg.SkillMinSim {
					pairs = append(pairs, pair{order[name], score})
				}
			}
			sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })
			if len(pairs) > k {
				pairs = pairs[:k]
			}
			out := make([]Skill, len(pairs))
			for i, p := range pairs {
				out[i] = p.s
			}
			return out
		}
	}
	// sin embeddings: palabras de la descripción presentes en el objetivo (mínimo dos)
	g := map[string]bool{}
	for _, w := range text.Words(goal) {
		g[w] = true
	}
	type scored struct {
		s    Skill
		hits int
	}
	var all []scored
	for _, s := range library {
		hits := 0
		for _, w := range text.Words(s.Description) {
			if len([]rune(w)) >= 4 && g[w] {
				hits++
			}
		}
		if hits >= 2 {
			all = append(all, scored{s, hits})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].hits > all[j].hits })
	if len(all) > k {
		all = all[:k]
	}
	out := make([]Skill, len(all))
	for i, x := range all {
		out[i] = x.s
	}
	return out
}

// ClosestSkill devuelve la skill más parecida al texto (nil sin embeddings).
func ClosestSkill(ctx context.Context, txt string) *Closest {
	ensureLibrary()
	if len(library) == 0 {
		return nil
	}
	idx, err := getIndex(ctx)
	if err != nil {
		return nil
	}
	q, err := embed.Embed(ctx, []string{txt}, "query")
	if err != nil || len(q) == 0 {
		return nil
	}
	var best *Closest
	for _, it := range idx {
		score := dot(q[0], it.Vec)
		if best == nil || score > best.Score {
			best = &Closest{Skill: it.Skill, Score: score}
		}
	}
	return best
}

// AddSkill guarda una skill nueva en disco y la añade a la biblioteca del proceso.
func AddSkill(txt, dir string) (*Skill, error) {
	if dir == "" {
		dir = GeneratedDir
	}
	s := ParseSkill(txt)
	if s == nil {
		return nil, fmt.Errorf("skill inválida")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, s.Name+".md"), []byte(txt), 0o644); err != nil {
		return nil, err
	}
	mu.Lock()
	ensureLibrary()
	library = append(library, *s)
	index = nil
	mu.Unlock()
	return s, nil
}

// NameTaken indica si ya existe una skill con ese nombre.
func NameTaken(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	ensureLibrary()
	for _, s := range library {
		if s.Name == name {
			return true
		}
	}
	return false
}

// ResetCache olvida la biblioteca y el índice (tests).
func ResetCache() {
	mu.Lock()
	library = nil
	loaded = false
	index = nil
	mu.Unlock()
}

// Messages devuelve los turnos de ejemplo como mensajes del LLM.
func (s Skill) Messages() []llm.Message {
	out := make([]llm.Message, len(s.Turns))
	for i, t := range s.Turns {
		out[i] = llm.Message{Role: t.Role, Content: t.Content}
	}
	return out
}
