// Package learnskill guarda recetas (skills) de tareas que salieron bien.
package learnskill

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"capri/internal/config"
	"capri/internal/llm"
	"capri/internal/skills"
	"capri/internal/summary"
	"capri/internal/text"
)

const (
	dupSim   = 0.85
	maxSteps = 6
)

func flat(s string) string { return text.Collapse(s) }

// StepLine convierte un paso ejecutado en una línea con el formato de los ejemplos.
func StepLine(o summary.Obs) (string, bool) {
	a := o.Args
	switch o.Tool {
	case "fs_write":
		return "fs_write: " + a["path"] + " | " + strings.ReplaceAll(a["content"], "\n", `\n`), true
	case "fs_edit":
		return "fs_edit: " + a["path"] + " | " + a["find"] + " | " + a["replace"], true
	case "shell":
		return "shell: " + a["command"], true
	case "fs_read", "fs_list", "fs_delete":
		return o.Tool + ": " + a["path"], true
	}
	return "", false
}

// SkillText genera el texto de la skill con el mismo formato que los archivos de skills/.
func SkillText(name, description, goal string, obs []summary.Obs) string {
	var body []string
	for _, o := range obs {
		line, ok := StepLine(o)
		if !ok {
			continue
		}
		body = append(body, line, "Resultado: "+text.Trim(flat(o.Text), 80))
	}
	lines := []string{"---", "name: " + name, "description: " + description, "origen: generada", "---", "Objetivo: " + goal}
	lines = append(lines, body...)
	lines = append(lines, "DONE", "")
	return strings.Join(lines, "\n")
}

var (
	nonSlug = regexp.MustCompile(`[^a-z0-9\s-]+`)
	spaces  = regexp.MustCompile(`[\s-]+`)
)

// Slug crea un nombre legible para archivo: minúsculas, sin tildes, guiones, máximo 4 palabras.
func Slug(s string) string {
	s = strings.ToLower(s)
	s = norm.NFD.String(s)
	var b strings.Builder
	for _, r := range s {
		if r >= 0x300 && r <= 0x36f {
			continue
		}
		b.WriteRune(r)
	}
	s = nonSlug.ReplaceAllString(b.String(), " ")
	var words []string
	for _, w := range spaces.Split(strings.TrimSpace(s), -1) {
		if len([]rune(w)) > 1 {
			words = append(words, w)
		}
	}
	if len(words) > 4 {
		words = words[:4]
	}
	return strings.Join(words, "-")
}

var (
	nameRe = regexp.MustCompile(`(?im)^nombre:\s*(.+)$`)
	descRe = regexp.MustCompile(`(?im)^descripcion:\s*(.{12,200})$`)
)

// Describe pide al modelo nombre y descripción. Devuelve nil si la respuesta no sirve.
func Describe(ctx context.Context, goal string, steps []string) *struct{ Name, Description string } {
	out, err := llm.ChatDefault(ctx, []llm.Message{
		{Role: "system", Content: "Escribes recetas breves para un agente. Responde SOLO con dos líneas:\nnombre: <2 a 4 palabras en minúsculas con guiones, describiendo la tarea; ejemplos: contar-lineas, mover-archivo, mostrar-primeras-lineas>\ndescripcion: <una frase en español que diga CUÁNDO usar esta receta>"},
		{Role: "user", Content: "Objetivo: " + goal + "\nPasos:\n" + strings.Join(steps, "\n")},
	})
	if err != nil {
		return nil
	}
	name := Slug("")
	if m := nameRe.FindStringSubmatch(out); m != nil {
		name = Slug(m[1])
	}
	desc := ""
	if m := descRe.FindStringSubmatch(out); m != nil {
		desc = strings.TrimSpace(m[1])
	}
	if len(name) < 4 || desc == "" {
		return nil
	}
	return &struct{ Name, Description string }{name, desc}
}

func countGenerated(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// LearnSkill guarda la receta de una tarea limpia. Devuelve la skill guardada o nil.
func LearnSkill(ctx context.Context, goal string, obs []summary.Obs) *skills.Skill {
	cfg := config.Get()
	if !cfg.SkillLearn || len(obs) == 0 || len(obs) > maxSteps {
		return nil
	}
	for _, o := range obs {
		if _, ok := StepLine(o); !ok || !o.OK || o.Rejected {
			return nil
		}
	}
	dir := skills.GeneratedDir
	if countGenerated(dir) >= cfg.SkillMaxGenerated {
		return nil
	}
	close := skills.ClosestSkill(ctx, goal)
	if close == nil || close.Score >= dupSim {
		return nil
	}
	var lines []string
	for _, o := range obs {
		l, _ := StepLine(o)
		lines = append(lines, l)
	}
	meta := Describe(ctx, goal, lines)
	if meta == nil {
		return nil
	}
	name := meta.Name
	for n := 2; skills.NameTaken(name) || fileExists(filepath.Join(dir, name+".md")); n++ {
		name = meta.Name + "-" + itoa(n)
	}
	txt := SkillText(name, meta.Description, goal, obs)
	if skills.ParseSkill(txt) == nil {
		return nil
	}
	s, err := skills.AddSkill(txt, dir)
	if err != nil {
		return nil
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
