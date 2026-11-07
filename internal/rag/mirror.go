package rag

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"capri/internal/config"
)

// Espejo en Markdown: cada recuerdo es un archivo .capri/memory/<tipo>/<id>-<slug>.md
// con cabecera (id, tipo, etiquetas, fijado, fechas) y su contenido, más un INDEX.md.
// La base SQLite es la fuente de verdad; los archivos se pueden leer y editar a
// mano, y SyncMarkdown trae a la base lo editado.

func (r *Rag) memDir() string { return filepath.Join(r.dir, "memory") }

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c < unicode.MaxASCII {
			b.WriteRune(c)
		}
	}
	out := strings.Trim(slugRe.ReplaceAllString(b.String(), "-"), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if out == "" {
		out = "memoria"
	}
	return out
}

func (r *Rag) mirrorFiles(id int64) []string {
	m, _ := filepath.Glob(filepath.Join(r.memDir(), "*", fmt.Sprintf("%d-*.md", id)))
	return m
}

func (r *Rag) mirror(id int64) {
	if !config.Get().Mirror {
		return
	}
	row := r.Get(id)
	if row == nil {
		return
	}
	for _, f := range r.mirrorFiles(id) {
		_ = os.Remove(f)
	}
	dir := filepath.Join(r.memDir(), row.Kind)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	name := fmt.Sprintf("%d-%s.md", id, slug(row.Title))
	_ = os.WriteFile(filepath.Join(dir, name), []byte(render(row)), 0o644)
	r.writeIndex()
}

func (r *Rag) unmirror(id int64) {
	if !config.Get().Mirror {
		return
	}
	for _, f := range r.mirrorFiles(id) {
		_ = os.Remove(f)
	}
	r.writeIndex()
}

func render(row *Row) string {
	return fmt.Sprintf("---\nid: %d\nkind: %s\ntitle: %s\ntags: %s\npinned: %t\ncreated: %d\nupdated: %d\n---\n%s\n",
		row.ID, row.Kind, oneLine(row.Title), oneLine(row.Tags), row.Pinned, row.Created, row.Updated, row.Content)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// parse lee un archivo del espejo. id es 0 si el archivo es nuevo (sin cabecera).
func parse(b string) (meta map[string]string, body string) {
	meta = map[string]string{}
	rest, ok := strings.CutPrefix(b, "---\n")
	if !ok {
		return meta, strings.TrimSpace(b)
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return meta, strings.TrimSpace(b)
	}
	for _, l := range strings.Split(head, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok {
			meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return meta, strings.TrimSpace(body)
}

// writeIndex regenera INDEX.md: fijados primero y después los títulos por tipo.
func (r *Rag) writeIndex() {
	rows, err := r.db.Query(`SELECT id,kind,title,pinned FROM mem ORDER BY pinned DESC, updated DESC`)
	if err != nil {
		return
	}
	defer rows.Close()
	byKind := map[string][]string{}
	var pinned []string
	for rows.Next() {
		var id int64
		var kind, title string
		var pin int
		if rows.Scan(&id, &kind, &title, &pin) != nil {
			continue
		}
		line := fmt.Sprintf("- [%s](%s/%d-%s.md)", oneLine(title), kind, id, slug(title))
		if pin != 0 {
			pinned = append(pinned, line)
		}
		if len(byKind[kind]) < 200 {
			byKind[kind] = append(byKind[kind], line)
		}
	}
	var b strings.Builder
	b.WriteString("# Memoria de Capri\n\nÍndice generado; la fuente de verdad es brain.db. Edita los .md y Capri los importa al arrancar.\n")
	if len(pinned) > 0 {
		b.WriteString("\n## Fijados\n" + strings.Join(pinned, "\n") + "\n")
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		b.WriteString("\n## " + k + "\n" + strings.Join(byKind[k], "\n") + "\n")
	}
	_ = os.MkdirAll(r.memDir(), 0o755)
	_ = os.WriteFile(filepath.Join(r.memDir(), "INDEX.md"), []byte(b.String()), 0o644)
}

// SyncMarkdown reconcilia el espejo con la base: reescribe los archivos que faltan,
// importa los editados a mano (más nuevos que la base) y da de alta los archivos
// nuevos sin cabecera como notas. Devuelve cuántos recuerdos importó.
func (r *Rag) SyncMarkdown() (int, error) {
	if !config.Get().Mirror {
		return 0, nil
	}
	ctx := context.Background()
	imported := 0
	files, _ := filepath.Glob(filepath.Join(r.memDir(), "*", "*.md"))
	seen := map[int64]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		meta, body := parse(string(raw))
		id, _ := strconv.ParseInt(meta["id"], 10, 64)
		if id == 0 {
			if body == "" {
				continue
			}
			title := strings.TrimSuffix(filepath.Base(f), ".md")
			ids, err := r.Add(ctx, AddOptions{Title: title, Content: body, Kind: "note", Whole: true})
			if err == nil {
				_ = os.Remove(f)
				imported += len(ids)
			}
			continue
		}
		seen[id] = true
		row := r.Get(id)
		if row == nil {
			continue
		}
		pin := meta["pinned"] == "true"
		st, err := os.Stat(f)
		if err != nil || st.ModTime().UnixMilli() <= row.Updated {
			continue
		}
		title, tags := meta["title"], meta["tags"]
		if body != strings.TrimSpace(row.Content) || title != oneLine(row.Title) || tags != oneLine(row.Tags) {
			if _, err := r.Update(ctx, id, &title, &body, &tags); err == nil {
				imported++
			}
		}
		if pin != row.Pinned {
			r.Pin(id, pin)
		}
	}
	// recuerdos sin archivo (p. ej. creados antes del espejo): se escriben
	ids, _ := r.db.Query(`SELECT id FROM mem`)
	if ids != nil {
		var missing []int64
		for ids.Next() {
			var id int64
			if ids.Scan(&id) == nil && !seen[id] {
				missing = append(missing, id)
			}
		}
		ids.Close()
		for _, id := range missing {
			if len(r.mirrorFiles(id)) == 0 {
				r.mirror(id)
			}
		}
	}
	r.writeIndex()
	return imported, nil
}
