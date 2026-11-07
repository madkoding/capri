// Package rag implementa el «segundo cerebro»: SQLite con FTS5, vectores nomic y memoria holográfica.
package rag

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"capri/internal/config"
	"capri/internal/embed"
	"capri/internal/holo"
)

// Row es una memoria.
type Row struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Tags    string `json:"tags"`
	Kind    string `json:"kind"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
	Pinned  bool   `json:"pinned"`
}

// Hit es un resultado de búsqueda.
type Hit struct {
	Row
	Sim    float64 `json:"sim"`
	HasSim bool    `json:"-"`
	Score  float64 `json:"score"`
}

// Stats cuenta memorias por tipo.
type Stats struct {
	Note    int `json:"note"`
	Chat    int `json:"chat"`
	Summary int `json:"summary"`
}

// AddOptions describe una memoria nueva.
type AddOptions struct {
	Title   string
	Content string
	Tags    string
	Kind    string
	Whole   bool
}

// DedupeResult es un par de notas fundidas.
type DedupeResult struct {
	Keep  int64
	Drop  int64
	Sim   float64
	Title string
}

// Rag es la base de memoria.
type Rag struct {
	db    *sql.DB
	dir   string // carpeta de estado (.capri): aquí vive el espejo Markdown
	mu    sync.RWMutex
	vecs  map[int64][]float32
	holos map[int64][]float32
}

// Open abre (o crea) la base de datos.
func Open(dbPath string) (*Rag, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	r := &Rag{db: db, dir: filepath.Dir(dbPath), vecs: map[int64][]float32{}, holos: map[int64][]float32{}}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mem (
			id INTEGER PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			tags TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'note',
			created INTEGER NOT NULL,
			updated INTEGER NOT NULL,
			vec BLOB
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS mem_fts USING fts5(
			title, content, tags, content='mem', content_rowid='id',
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER IF NOT EXISTS mem_ai AFTER INSERT ON mem BEGIN
			INSERT INTO mem_fts(rowid,title,content,tags) VALUES (new.id,new.title,new.content,new.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS mem_ad AFTER DELETE ON mem BEGIN
			INSERT INTO mem_fts(mem_fts,rowid,title,content,tags) VALUES ('delete',old.id,old.title,old.content,old.tags);
		END`,
		`CREATE TRIGGER IF NOT EXISTS mem_au AFTER UPDATE ON mem BEGIN
			INSERT INTO mem_fts(mem_fts,rowid,title,content,tags) VALUES ('delete',old.id,old.title,old.content,old.tags);
			INSERT INTO mem_fts(rowid,title,content,tags) VALUES (new.id,new.title,new.content,new.tags);
		END`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, err
		}
	}
	// bases creadas antes de la memoria holográfica: se añade la columna sin tocar los datos
	hasHolo := false
	rows, err := db.Query(`PRAGMA table_info(mem)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		if name == "holo" {
			hasHolo = true
		}
	}
	rows.Close()
	if !hasHolo {
		if _, err := db.Exec(`ALTER TABLE mem ADD COLUMN holo BLOB`); err != nil {
			db.Close()
			return nil, err
		}
	}
	// columna pinned: los recuerdos fijados no se podan ni se funden, y puntúan más
	if _, err := db.Exec(`ALTER TABLE mem ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	if err := r.loadVectors(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

// Close cierra la base.
func (r *Rag) Close() error { return r.db.Close() }

func (r *Rag) loadVectors() error {
	rows, err := r.db.Query(`SELECT id, vec FROM mem WHERE vec IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return err
		}
		r.vecs[id] = toVec(b)
	}
	rows.Close()
	rows, err = r.db.Query(`SELECT id, holo FROM mem WHERE holo IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			rows.Close()
			return err
		}
		r.holos[id] = toVec(b)
	}
	return rows.Close()
}

func toVec(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func vecBytes(v []float32) []byte {
	if v == nil {
		return nil
	}
	out := make([]byte, len(v)*4)
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(x))
	}
	return out
}

// Count devuelve el número total de memorias.
func (r *Rag) Count() int {
	var n int
	_ = r.db.QueryRow(`SELECT COUNT(*) FROM mem`).Scan(&n)
	return n
}

// Stats devuelve el conteo por tipo.
func (r *Rag) Stats() Stats {
	out := Stats{}
	rows, err := r.db.Query(`SELECT kind, COUNT(*) FROM mem GROUP BY kind`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var n int
		if rows.Scan(&kind, &n) == nil {
			switch kind {
			case "note":
				out.Note = n
			case "chat":
				out.Chat = n
			case "summary":
				out.Summary = n
			}
		}
	}
	return out
}

// Get devuelve una memoria o nil.
func (r *Rag) Get(id int64) *Row {
	var row Row
	var pin int
	err := r.db.QueryRow(`SELECT id,title,content,tags,kind,created,updated,pinned FROM mem WHERE id=?`, id).
		Scan(&row.ID, &row.Title, &row.Content, &row.Tags, &row.Kind, &row.Created, &row.Updated, &pin)
	row.Pinned = pin != 0
	if err != nil {
		return nil
	}
	return &row
}

// List devuelve las últimas memorias, opcionalmente filtradas por tipo.
func (r *Rag) List(limit int, kind string) []Row {
	if limit == 0 {
		limit = 20
	}
	sqlText := `SELECT id,title,substr(content,1,160) AS content,tags,kind,created,updated,pinned FROM mem`
	var args []any
	if kind != "" {
		sqlText += ` WHERE kind=?`
		args = append(args, kind)
	}
	sqlText += ` ORDER BY updated DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.Query(sqlText, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var row Row
		var pin int
		if rows.Scan(&row.ID, &row.Title, &row.Content, &row.Tags, &row.Kind, &row.Created, &row.Updated, &pin) == nil {
			row.Pinned = pin != 0
			out = append(out, row)
		}
	}
	return out
}

// Add guarda una memoria (troceándola salvo Whole) y devuelve sus ids.
func (r *Rag) Add(ctx context.Context, o AddOptions) ([]int64, error) {
	content := strings.TrimSpace(o.Content)
	var parts []string
	if o.Whole {
		parts = []string{content}
	} else {
		parts = chunk(content, 900)
	}
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return nil, fmt.Errorf("contenido vacío")
	}
	kind := o.Kind
	if kind == "" {
		kind = "note"
	}
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = docText(o.Title, p)
	}
	vecs := r.embedDocs(ctx, texts)
	now := nowMillis()
	cfg := config.Get()
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	stmt, err := tx.Prepare(`INSERT INTO mem(title,content,tags,kind,created,updated,vec,holo) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	var ids []int64
	for i, p := range parts {
		title := o.Title
		if len(parts) > 1 {
			title = fmt.Sprintf("%s (%d/%d)", o.Title, i+1, len(parts))
		}
		var hv []float32
		if cfg.Holo {
			hv = holo.Encode(docText(title, p))
		}
		var vb any
		if vecs[i] != nil {
			vb = vecBytes(vecs[i])
		}
		var hb any
		if hv != nil {
			hb = vecBytes(hv)
		}
		res, err := stmt.Exec(title, p, o.Tags, kind, now, now, vb, hb)
		if err != nil {
			stmt.Close()
			tx.Rollback()
			return nil, err
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
		if vecs[i] != nil {
			r.mu.Lock()
			r.vecs[id] = vecs[i]
			r.mu.Unlock()
		}
		if hv != nil {
			r.mu.Lock()
			r.holos[id] = hv
			r.mu.Unlock()
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		r.mirror(id)
	}
	return ids, nil
}

// HasSource indica si ya hay una memoria web de esta URL.
func (r *Rag) HasSource(url string) bool {
	var one int
	err := r.db.QueryRow(`SELECT 1 FROM mem WHERE kind = 'web' AND instr(content, ?) > 0 LIMIT 1`, "Fuente: "+url).Scan(&one)
	return err == nil
}

// Update modifica una memoria.
func (r *Rag) Update(ctx context.Context, id int64, title, content, tags *string) (*Row, error) {
	cur := r.Get(id)
	if cur == nil {
		return nil, fmt.Errorf("no existe la memoria #%d", id)
	}
	next := *cur
	if title != nil {
		next.Title = *title
	}
	if content != nil {
		next.Content = *content
	}
	if tags != nil {
		next.Tags = *tags
	}
	var vec []float32
	var hv []float32
	if title != nil || content != nil {
		vecs := r.embedDocs(ctx, []string{docText(next.Title, next.Content)})
		vec = vecs[0]
		if config.Get().Holo {
			hv = holo.Encode(docText(next.Title, next.Content))
		}
	}
	sqlText := `UPDATE mem SET title=?, content=?, tags=?, updated=?`
	args := []any{next.Title, next.Content, next.Tags, nowMillis()}
	if vec != nil {
		sqlText += `, vec=?`
		args = append(args, vecBytes(vec))
	}
	if hv != nil {
		sqlText += `, holo=?`
		args = append(args, vecBytes(hv))
	}
	sqlText += ` WHERE id=?`
	args = append(args, id)
	if _, err := r.db.Exec(sqlText, args...); err != nil {
		return nil, err
	}
	if vec != nil {
		r.mu.Lock()
		r.vecs[id] = vec
		r.mu.Unlock()
	}
	if hv != nil {
		r.mu.Lock()
		r.holos[id] = hv
		r.mu.Unlock()
	}
	r.mirror(id)
	return r.Get(id), nil
}

// Delete borra una memoria.
func (r *Rag) Delete(id int64) bool {
	r.mu.Lock()
	delete(r.vecs, id)
	delete(r.holos, id)
	r.mu.Unlock()
	res, err := r.db.Exec(`DELETE FROM mem WHERE id=?`, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		r.unmirror(id)
	}
	return n > 0
}

// Dedupe funde notas casi idénticas (deja la más antigua).
func (r *Rag) Dedupe(threshold float64, kind string, apply bool) []DedupeResult {
	if threshold == 0 {
		threshold = 0.92
	}
	if kind == "" {
		kind = "note"
	}
	rows, err := r.db.Query(`SELECT id,title,content,pinned FROM mem WHERE kind=? ORDER BY id`, kind)
	if err != nil {
		return nil
	}
	type item struct {
		id      int64
		title   string
		content string
		pinned  bool
	}
	var items []item
	for rows.Next() {
		var it item
		var pin int
		if rows.Scan(&it.id, &it.title, &it.content, &pin) == nil {
			it.pinned = pin != 0
			items = append(items, it)
		}
	}
	rows.Close()
	norm := func(t string) string { return strings.Join(strings.Fields(strings.ToLower(t)), " ") }
	gone := map[int64]bool{}
	var merged []DedupeResult
	r.mu.RLock()
	vecs := make(map[int64][]float32, len(r.vecs))
	for k, v := range r.vecs {
		vecs[k] = v
	}
	r.mu.RUnlock()
	for i := 0; i < len(items); i++ {
		if gone[items[i].id] {
			continue
		}
		for j := i + 1; j < len(items); j++ {
			if gone[items[j].id] || items[j].pinned {
				continue
			}
			a, b := items[i], items[j]
			va, vb := vecs[a.id], vecs[b.id]
			exact := norm(a.content) == norm(b.content)
			sim := 0.0
			if exact {
				sim = 1
			} else if va != nil && vb != nil {
				sim = dot(va, vb)
			}
			if sim >= threshold {
				merged = append(merged, DedupeResult{Keep: a.id, Drop: b.id, Sim: sim, Title: b.title})
				gone[b.id] = true
			}
		}
	}
	if apply {
		for _, m := range merged {
			r.Delete(m.Drop)
		}
	}
	return merged
}

// PruneChats borra turnos de chat con más de days días.
func (r *Rag) PruneChats(days int) int {
	if days == 0 {
		days = 30
	}
	cutoff := nowMillis() - int64(days)*86400000
	rows, err := r.db.Query(`SELECT id FROM mem WHERE kind='chat' AND pinned=0 AND updated < ?`, cutoff)
	if err != nil {
		return 0
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		r.Delete(id)
	}
	return len(ids)
}

type rankEntry struct {
	rrf    float64
	sim    float64
	hasSim bool
}

// Search hace la búsqueda híbrida (vectores + holográfica + FTS5 con fusión RRF).
func (r *Rag) Search(ctx context.Context, query string, k int, minSim float64) []Hit {
	cfg := config.Get()
	if k == 0 {
		k = cfg.RecallK
	}
	if minSim == 0 {
		minSim = cfg.MinSim
	}
	rank := map[int64]*rankEntry{}
	bump := func(id int64, pos int, sim *float64) {
		e := rank[id]
		if e == nil {
			e = &rankEntry{}
			rank[id] = e
		}
		e.rrf += 1.0 / float64(60+pos)
		if sim != nil {
			e.sim = *sim
			e.hasSim = true
		}
	}

	r.mu.RLock()
	vecs := make(map[int64][]float32, len(r.vecs))
	for id, v := range r.vecs {
		vecs[id] = v
	}
	holos := make(map[int64][]float32, len(r.holos))
	for id, v := range r.holos {
		holos[id] = v
	}
	r.mu.RUnlock()

	if len(vecs) > 0 {
		if q, err := embed.Embed(ctx, []string{query}, "query"); err == nil && len(q) > 0 {
			type scored struct {
				id int64
				s  float64
			}
			var all []scored
			for id, v := range vecs {
				if len(v) != len(q[0]) { // vector de otro modelo de embeddings: se ignora hasta reindexar
					continue
				}
				all = append(all, scored{id, dot(q[0], v)})
			}
			sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
			if len(all) > 20 {
				all = all[:20]
			}
			for i, sc := range all {
				if sc.s >= minSim {
					s := sc.s
					bump(sc.id, i, &s)
				}
			}
		}
	}

	if len(holos) > 0 {
		q := holo.Encode(query)
		type scored struct {
			id int64
			s  float64
		}
		var all []scored
		for id, v := range holos {
			all = append(all, scored{id, holo.Sim(q, v)})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
		if len(all) > 20 {
			all = all[:20]
		}
		for i, sc := range all {
			if sc.s >= cfg.HoloMinSim {
				bump(sc.id, i, nil)
			}
		}
	}

	if fts := ftsQuery(query); fts != "" {
		rows, err := r.db.Query(`SELECT rowid FROM mem_fts WHERE mem_fts MATCH ? ORDER BY bm25(mem_fts,4,1,2) LIMIT 20`, fts)
		if err == nil {
			i := 0
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					bump(id, i, nil)
					i++
				}
			}
			rows.Close()
		}
	}

	type entry struct {
		id int64
		e  *rankEntry
	}
	var entries []entry
	for id, e := range rank {
		entries = append(entries, entry{id, e})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].e.rrf > entries[j].e.rrf })
	if len(entries) > k {
		entries = entries[:k]
	}
	var hits []Hit
	for _, en := range entries {
		row := r.Get(en.id)
		if row == nil {
			continue
		}
		score := en.e.rrf
		if row.Kind == "note" {
			score *= 1.25
		}
		score *= weight(row, cfg.RecencyDays)
		hits = append(hits, Hit{Row: *row, Sim: en.e.sim, HasSim: en.e.hasSim, Score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

// EnsureEmbeddings reindexa los vectores cuando cambió el modelo de embeddings
// (o faltan) y devuelve cuántos recuerdos recalculó. Sin servicio de embeddings
// no hace nada y reintenta en el siguiente arranque.
func (r *Rag) EnsureEmbeddings(ctx context.Context) (int, error) {
	model := config.Get().EmbedModel
	var stamp string
	_ = r.db.QueryRow(`SELECT v FROM meta WHERE k='embed_model'`).Scan(&stamp)
	if stamp == "" && r.Count() == 0 {
		_, err := r.db.Exec(`INSERT OR REPLACE INTO meta(k,v) VALUES ('embed_model',?)`, model)
		return 0, err
	}
	if stamp == model && !r.dimsDiffer(ctx) {
		return r.fillMissing(ctx)
	}
	// sello distinto, o base anterior a este cambio (sin sello): se recalculan todos
	if !embed.Health(ctx) {
		return 0, nil
	}
	rows, err := r.db.Query(`SELECT id, title, content FROM mem ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	var texts []string
	for rows.Next() {
		var id int64
		var title, content string
		if rows.Scan(&id, &title, &content) == nil {
			ids = append(ids, id)
			texts = append(texts, docText(title, content))
		}
	}
	rows.Close()
	n, err := r.reembed(ctx, ids, texts)
	if err != nil {
		return n, err
	}
	_, err = r.db.Exec(`INSERT OR REPLACE INTO meta(k,v) VALUES ('embed_model',?)`, model)
	return n, err
}

// dimsDiffer detecta vectores guardados con otro modelo aunque el sello coincida
// (p. ej. un servidor distinto al configurado): compara su dimensión con la actual.
func (r *Rag) dimsDiffer(ctx context.Context) bool {
	r.mu.RLock()
	stored := 0
	for _, v := range r.vecs {
		stored = len(v)
		break
	}
	r.mu.RUnlock()
	if stored == 0 || !embed.Health(ctx) {
		return false
	}
	q, err := embed.Embed(ctx, []string{"dimensión"}, "doc")
	return err == nil && len(q) > 0 && len(q[0]) != stored
}

// fillMissing calcula el vector de los recuerdos guardados mientras el servicio estaba caído.
func (r *Rag) fillMissing(ctx context.Context) (int, error) {
	rows, err := r.db.Query(`SELECT id, title, content FROM mem WHERE vec IS NULL ORDER BY id`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	var texts []string
	for rows.Next() {
		var id int64
		var title, content string
		if rows.Scan(&id, &title, &content) == nil {
			ids = append(ids, id)
			texts = append(texts, docText(title, content))
		}
	}
	rows.Close()
	if len(ids) == 0 || !embed.Health(ctx) {
		return 0, nil
	}
	return r.reembed(ctx, ids, texts)
}

func (r *Rag) reembed(ctx context.Context, ids []int64, texts []string) (int, error) {
	const batch = 16
	done := 0
	for i := 0; i < len(ids); i += batch {
		j := min(i+batch, len(ids))
		vecs, err := embed.Embed(ctx, texts[i:j], "doc")
		if err != nil {
			return done, err
		}
		if len(vecs) != j-i {
			return done, fmt.Errorf("embeddings: %d vectores para %d textos", len(vecs), j-i)
		}
		for k, v := range vecs {
			if _, err := r.db.Exec(`UPDATE mem SET vec=? WHERE id=?`, vecBytes(v), ids[i+k]); err != nil {
				return done, err
			}
			r.mu.Lock()
			r.vecs[ids[i+k]] = v
			r.mu.Unlock()
			done++
		}
	}
	return done, nil
}

func (r *Rag) embedDocs(ctx context.Context, texts []string) [][]float32 {
	vecs, err := embed.Embed(ctx, texts, "doc")
	if err != nil {
		out := make([][]float32, len(texts))
		return out
	}
	return vecs
}

func docText(title, content string) string {
	if title != "" {
		return title + "\n" + content
	}
	return content
}

func dot(a, b []float32) float64 {
	var s float64
	for i := 0; i < len(a) && i < len(b); i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

var tokenRe = regexp.MustCompile(`[\p{L}\p{N}]{4,}`)

func ftsQuery(q string) string {
	seen := map[string]bool{}
	var toks []string
	for _, t := range tokenRe.FindAllString(strings.ToLower(q), -1) {
		if !seen[t] {
			seen[t] = true
			toks = append(toks, t)
		}
		if len(toks) == 12 {
			break
		}
	}
	for i, t := range toks {
		toks[i] = `"` + t + `"`
	}
	return strings.Join(toks, " OR ")
}

// chunk trocea un texto largo respetando párrafos y frases.
func chunk(text string, max int) []string {
	if text == "" {
		return nil
	}
	if len([]rune(text)) <= max {
		return []string{text}
	}
	var out []string
	cur := ""
	for _, para := range splitParagraphs(text) {
		if cur != "" && len([]rune(cur))+len([]rune(para))+1 > max {
			out = append(out, cur)
			cur = ""
		}
		if cur != "" {
			cur += "\n" + para
		} else {
			cur = para
		}
		for len([]rune(cur)) > max {
			r := []rune(cur)
			out = append(out, string(r[:max]))
			cur = string(r[max:])
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func splitParagraphs(text string) []string {
	r := []rune(text)
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	for i := 0; i < len(r); i++ {
		// dos o más saltos de línea: separador de párrafo
		if r[i] == '\n' && i+1 < len(r) && r[i+1] == '\n' {
			flush()
			for i < len(r) && r[i] == '\n' {
				i++
			}
			i--
			continue
		}
		cur = append(cur, r[i])
		// fin de frase: [.!?] seguido de espacio en blanco
		if (r[i] == '.' || r[i] == '!' || r[i] == '?') && i+1 < len(r) && isSpace(r[i+1]) {
			flush()
			for i+1 < len(r) && isSpace(r[i+1]) {
				i++
			}
		}
	}
	flush()
	return out
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v'
}

func nowMillis() int64 { return time.Now().UnixMilli() }

// weight pondera un recuerdo por importancia y antigüedad: lo fijado puntúa más;
// lo que no es una nota (chat, web, resúmenes) pierde peso con vida media de
// `days` días, con suelo en 0,4 para que nunca desaparezca del ranking.
func weight(row *Row, days int) float64 {
	if row.Pinned {
		return 1.5
	}
	if days <= 0 || row.Kind == "note" {
		return 1
	}
	age := float64(nowMillis()-row.Updated) / 86400000
	if age <= 0 {
		return 1
	}
	return math.Max(0.4, math.Pow(0.5, age/float64(days)))
}

// Pin fija o suelta un recuerdo. Devuelve false si no existe.
func (r *Rag) Pin(id int64, on bool) bool {
	v := 0
	if on {
		v = 1
	}
	res, err := r.db.Exec(`UPDATE mem SET pinned=? WHERE id=?`, v, id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		r.mirror(id)
	}
	return n > 0
}
