// Package trash implementa /undo: copia la carpeta de trabajo antes de la primera modificación de un turno.
package trash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"capri/internal/config"
	"capri/internal/rag"
)

const (
	maxBytesDefault = 50 * 1024 * 1024
	maxFilesDefault = 2000
	keepDefault     = 20
)

// MemRow es una memoria tal como se guarda para deshacer.
type MemRow struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Tags    string `json:"tags"`
	Kind    string `json:"kind"`
}

// Before es el valor anterior de una memoria editada.
type Before struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Tags    string `json:"tags"`
}

// MemOp es una operación sobre la memoria.
type MemOp struct {
	Type   string  `json:"type"`
	IDs    []int64 `json:"ids,omitempty"`
	Row    *MemRow `json:"row,omitempty"`
	ID     int64   `json:"id,omitempty"`
	Before *Before `json:"before,omitempty"`
}

// Turn es un turno con cambios reversibles.
type Turn struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	TS       int64   `json:"ts"`
	Snapshot bool    `json:"snapshot"`
	Skipped  *string `json:"skipped"`
	MemOps   []MemOp `json:"memOps"`
}

// ListItem es una entrada del historial de /undo.
type ListItem struct {
	When   string
	Label  string
	Files  bool
	Memory int
}

// Rag es lo que /undo necesita del cerebro.
type Rag interface {
	Delete(id int64) bool
	Add(ctx context.Context, o rag.AddOptions) ([]int64, error)
	Update(ctx context.Context, id int64, title, content, tags *string) (*rag.Row, error)
}

// Trash gestiona las copias de seguridad de los turnos.
type Trash struct {
	Workdir  string
	Dir      string
	MaxBytes int64
	MaxFiles int
	Keep     int

	journal     []Turn
	journalFile string
	turn        *Turn
}

// Options de construcción.
type Options struct {
	Dir      string
	Workdir  string
	MaxBytes int64
	MaxFiles int
	Keep     int
}

// New crea el gestor y carga el diario.
func New(o Options) (*Trash, error) {
	cfg := config.Get()
	if o.Workdir == "" {
		o.Workdir = cfg.Workdir
	}
	if o.Dir == "" {
		o.Dir = filepath.Join(filepath.Dir(cfg.DBPath), "trash")
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = maxBytesDefault
	}
	if o.MaxFiles == 0 {
		o.MaxFiles = maxFilesDefault
	}
	if o.Keep == 0 {
		o.Keep = keepDefault
	}
	t := &Trash{Workdir: o.Workdir, Dir: o.Dir, MaxBytes: o.MaxBytes, MaxFiles: o.MaxFiles, Keep: o.Keep}
	t.journalFile = filepath.Join(t.Dir, "journal.json")
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(t.journalFile); err == nil {
		_ = json.Unmarshal(b, &t.journal)
	}
	return t, nil
}

// Journal devuelve una copia del diario (para tests y listados).
func (t *Trash) Journal() []Turn {
	out := make([]Turn, len(t.journal))
	copy(out, t.journal)
	return out
}

func (t *Trash) save() {
	b, err := json.Marshal(t.journal)
	if err != nil {
		return
	}
	_ = os.WriteFile(t.journalFile, b, 0o644)
}

func randomBase36(n int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

// BeginTurn empieza un turno.
func (t *Trash) BeginTurn(label string) {
	r := []rune(label)
	if len(r) > 80 {
		label = string(r[:80])
	}
	t.turn = &Turn{
		ID:     fmt.Sprintf("%d-%s", time.Now().UnixMilli(), randomBase36(4)),
		Label:  label,
		TS:     time.Now().UnixMilli(),
		MemOps: []MemOp{},
	}
}

// SnapshotOnce copia el estado previo de la carpeta de trabajo, una sola vez por turno.
func (t *Trash) SnapshotOnce() error {
	turn := t.turn
	if turn == nil || turn.Snapshot || turn.Skipped != nil {
		return nil
	}
	if err := os.MkdirAll(t.Workdir, 0o755); err != nil {
		return err
	}
	if ok, _, _ := t.measure(); !ok {
		msg := fmt.Sprintf("la carpeta de trabajo es demasiado grande para copiarla (más de %d archivos o %d MB)", t.MaxFiles, t.MaxBytes/1048576)
		turn.Skipped = &msg
		return nil
	}
	dst := filepath.Join(t.Dir, turn.ID, "tree")
	if err := copyTree(t.Workdir, dst); err != nil {
		return err
	}
	turn.Snapshot = true
	return nil
}

// NoteMemory anota una operación de memoria.
func (t *Trash) NoteMemory(op MemOp) {
	if t.turn != nil {
		t.turn.MemOps = append(t.turn.MemOps, op)
	}
}

// CommitTurn cierra el turno y devuelve si se puede deshacer y por qué no.
func (t *Trash) CommitTurn() (undoable bool, skipped *string) {
	turn := t.turn
	t.turn = nil
	if turn == nil {
		return false, nil
	}
	undoable = turn.Snapshot || len(turn.MemOps) > 0
	if undoable {
		t.journal = append(t.journal, *turn)
		for len(t.journal) > t.Keep {
			old := t.journal[0]
			t.journal = t.journal[1:]
			_ = os.RemoveAll(filepath.Join(t.Dir, old.ID))
		}
		t.save()
	}
	return undoable, turn.Skipped
}

// CanUndo indica si hay algo que deshacer.
func (t *Trash) CanUndo() bool { return len(t.journal) > 0 }

// List devuelve los últimos n turnos.
func (t *Trash) List(n int) []ListItem {
	if n <= 0 || n > len(t.journal) {
		n = len(t.journal)
	}
	out := make([]ListItem, 0, n)
	for i := len(t.journal) - 1; i >= len(t.journal)-n; i-- {
		turn := t.journal[i]
		out = append(out, ListItem{
			When:   time.UnixMilli(turn.TS).Format("15:04:05"),
			Label:  turn.Label,
			Files:  turn.Snapshot,
			Memory: len(turn.MemOps),
		})
	}
	return out
}

// Undo revierte el último turno que modificó algo.
func (t *Trash) Undo(ctx context.Context, r Rag) (bool, string) {
	if len(t.journal) == 0 {
		return false, "no hay nada que deshacer"
	}
	turn := t.journal[len(t.journal)-1]
	t.journal = t.journal[:len(t.journal)-1]
	var parts []string
	if turn.Snapshot {
		tree := filepath.Join(t.Dir, turn.ID, "tree")
		_ = os.MkdirAll(t.Workdir, 0o755)
		entries, _ := os.ReadDir(t.Workdir)
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(t.Workdir, e.Name()))
		}
		entries, _ = os.ReadDir(tree)
		for _, e := range entries {
			_ = copyTree(filepath.Join(tree, e.Name()), filepath.Join(t.Workdir, e.Name()))
		}
		_ = os.RemoveAll(filepath.Join(t.Dir, turn.ID))
		parts = append(parts, "archivos restaurados")
	}
	if len(turn.MemOps) > 0 && r != nil {
		for i := len(turn.MemOps) - 1; i >= 0; i-- {
			op := turn.MemOps[i]
			switch op.Type {
			case "add":
				for _, id := range op.IDs {
					r.Delete(id)
				}
			case "delete":
				if op.Row != nil {
					_, _ = r.Add(ctx, rag.AddOptions{Title: op.Row.Title, Content: op.Row.Content, Tags: op.Row.Tags, Kind: op.Row.Kind})
				}
			case "update":
				if op.Before != nil {
					_, _ = r.Update(ctx, op.ID, &op.Before.Title, &op.Before.Content, &op.Before.Tags)
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%d cambio(s) de memoria revertido(s)", len(turn.MemOps)))
	}
	t.save()
	msg := "sin cambios"
	if len(parts) > 0 {
		msg = join(parts, ", ")
	}
	return true, fmt.Sprintf("deshecho «%s»: %s", turn.Label, msg)
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// measure recorre la carpeta y devuelve si cabe en los límites.
func (t *Trash) measure() (ok bool, files int, bytes int64) {
	err := filepath.WalkDir(t.Workdir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		files++
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
		}
		if files > t.MaxFiles || bytes > t.MaxBytes {
			return errStop
		}
		return nil
	})
	if err == errStop {
		return false, files, bytes
	}
	return true, files, bytes
}

var errStop = fmt.Errorf("stop")

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return copyFile(src, dst, info.Mode(), info.ModTime())
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func copyFile(src, dst string, mode os.FileMode, mod time.Time) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, mod, mod)
}
