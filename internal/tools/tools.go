// Package tools implementa las herramientas del agente (memoria, archivos, shell, web).
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"capri/internal/config"
	"capri/internal/rag"
	"capri/internal/sandbox"
	"capri/internal/shell"
	"capri/internal/text"
	"capri/internal/trash"
	"capri/internal/web"
)

// ConfirmOptions matiza una confirmación.
type ConfirmOptions struct {
	Overwrite bool
	Force     bool
	Args      map[string]string
}

// Confirm pide autorización al usuario.
type Confirm func(name, what string, o ConfirmOptions) bool

// Context es lo que necesita una herramienta.
type Context struct {
	Rag     *rag.Rag
	Trash   *trash.Trash
	Confirm Confirm
}

// Result es el resultado de ejecutar una herramienta.
type Result struct {
	OK        bool
	Rejected  bool
	Overwrite bool // el rechazo fue por no sobrescribir un archivo existente
	Text      string
	Raw       string
}

type runResult struct {
	Text   string
	Raw    string
	Failed bool
}

// Tool describe una herramienta.
type Tool struct {
	Danger     func(map[string]string) bool
	Describe   func(map[string]string, *Context) string
	Overwrites func(map[string]string) bool
	Run        func(context.Context, map[string]string, *Context) (runResult, error)
}

func clip(s string, n int) string {
	if n == 0 {
		n = 3000
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + fmt.Sprintf("\n…[+%d chars]", len(r)-n)
	}
	return s
}

func resolve(p string) string {
	if p == "" {
		p = "."
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(config.Get().Workdir, p)
}

func need(args map[string]string, keys ...string) error {
	for _, k := range keys {
		if args[k] == "" {
			return fmt.Errorf("falta argumento <%s>", k)
		}
	}
	return nil
}

var globRe = regexp.MustCompile(`[*?]`)

func noGlob(args map[string]string, tool string) error {
	if globRe.MatchString(args["path"]) {
		return fmt.Errorf("%s no admite comodines (*, ?): para varios archivos usa shell, por ejemplo rm -f *.log", tool)
	}
	return nil
}

func intArg(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return def
		}
		return int(f)
	}
	return n
}

func fmtMem(m *rag.Row) string {
	body := text.Collapse(m.Content)
	if len([]rune(body)) > 200 {
		body = string([]rune(body)[:200]) + "…"
	}
	title := ""
	if m.Title != "" {
		title = m.Title + ": "
	}
	return fmt.Sprintf("#%d %s%s", m.ID, title, body)
}

// Tools es el registro de herramientas.
var Tools = map[string]*Tool{
	"rag_search": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "query"); err != nil {
				return runResult{}, err
			}
			hits := c.Rag.Search(ctx, a["query"], intArg(a["k"], 5), 0.35)
			if len(hits) == 0 {
				return runResult{Text: "sin resultados"}, nil
			}
			var lines []string
			for i := range hits {
				lines = append(lines, fmtMem(&hits[i].Row))
			}
			return runResult{Text: strings.Join(lines, "\n")}, nil
		},
	},
	"rag_add": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "content"); err != nil {
				return runResult{}, err
			}
			title := a["title"]
			if title == "" {
				r := []rune(a["content"])
				if len(r) > 50 {
					r = r[:50]
				}
				title = string(r)
			}
			ids, err := c.Rag.Add(ctx, rag.AddOptions{Title: title, Content: a["content"], Tags: a["tags"], Kind: "note"})
			if err != nil {
				return runResult{}, err
			}
			if c.Trash != nil {
				c.Trash.NoteMemory(trash.MemOp{Type: "add", IDs: ids})
			}
			var parts []string
			for _, id := range ids {
				parts = append(parts, fmt.Sprintf("#%d", id))
			}
			return runResult{Text: "guardado " + strings.Join(parts, ", ")}, nil
		},
	},
	"rag_update": {
		Danger: func(map[string]string) bool { return true },
		Describe: func(a map[string]string, c *Context) string {
			title := "?"
			if row := c.Rag.Get(int64(intArg(a["id"], 0))); row != nil {
				title = clip(row.Title, 50)
			}
			return fmt.Sprintf("corregir memoria #%s: «%s»", a["id"], title)
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "id"); err != nil {
				return runResult{}, err
			}
			id := int64(intArg(a["id"], 0))
			prev := c.Rag.Get(id)
			if prev != nil && c.Trash != nil {
				c.Trash.NoteMemory(trash.MemOp{Type: "update", ID: prev.ID, Before: &trash.Before{Title: prev.Title, Content: prev.Content, Tags: prev.Tags}})
			}
			var title, content, tags *string
			if v, ok := a["title"]; ok && v != "" {
				title = &v
			}
			if v, ok := a["content"]; ok && v != "" {
				content = &v
			}
			if v, ok := a["tags"]; ok && v != "" {
				tags = &v
			}
			m, err := c.Rag.Update(ctx, id, title, content, tags)
			if err != nil {
				return runResult{}, err
			}
			return runResult{Text: "actualizado " + fmtMem(m)}, nil
		},
	},
	"rag_delete": {
		Danger: func(map[string]string) bool { return true },
		Describe: func(a map[string]string, c *Context) string {
			title := "?"
			if row := c.Rag.Get(int64(intArg(a["id"], 0))); row != nil {
				title = clip(row.Title, 60)
			}
			return fmt.Sprintf("OLVIDAR memoria #%s: «%s»", a["id"], title)
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "id"); err != nil {
				return runResult{}, err
			}
			id := int64(intArg(a["id"], 0))
			if row := c.Rag.Get(id); row != nil && c.Trash != nil {
				c.Trash.NoteMemory(trash.MemOp{Type: "delete", Row: &trash.MemRow{Title: row.Title, Content: row.Content, Tags: row.Tags, Kind: row.Kind}})
			}
			if c.Rag.Delete(id) {
				return runResult{Text: fmt.Sprintf("eliminado #%d", id)}, nil
			}
			return runResult{Text: fmt.Sprintf("no existe #%d", id)}, nil
		},
	},
	"rag_list": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			rows := c.Rag.List(intArg(a["limit"], 15), "")
			if len(rows) == 0 {
				return runResult{Text: "el cerebro está vacío"}, nil
			}
			var lines []string
			for i := range rows {
				lines = append(lines, fmtMem(&rows[i]))
			}
			return runResult{Text: strings.Join(lines, "\n")}, nil
		},
	},
	"web_search": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "query"); err != nil {
				return runResult{}, err
			}
			if !config.Get().Web {
				return runResult{}, fmt.Errorf("la búsqueda en internet está desactivada (CAPRI_WEB=0)")
			}
			k := intArg(a["k"], 3)
			if k > 6 {
				k = 6
			}
			found, err := web.Research(ctx, a["query"], c.Rag, k)
			if err != nil {
				return runResult{}, err
			}
			if len(found) == 0 {
				return runResult{}, fmt.Errorf("sin resultados útiles en internet")
			}
			var lines []string
			for _, f := range found {
				extra := fmt.Sprintf("[guardado #%s]", joinIDs(f.IDs))
				if f.Known {
					extra = "[ya estaba en memoria]"
				}
				lines = append(lines, fmt.Sprintf("%s (%s): %s %s", f.Title, f.URL, f.Snippet, extra))
			}
			return runResult{Text: strings.Join(lines, "\n")}, nil
		},
	},
	"fs_list": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := noGlob(a, "fs_list"); err != nil {
				return runResult{}, err
			}
			entries, err := os.ReadDir(resolve(a["path"]))
			if err != nil {
				return runResult{}, err
			}
			var items []string
			for _, e := range entries {
				if e.IsDir() {
					items = append(items, e.Name()+"/")
				} else {
					items = append(items, e.Name())
				}
			}
			raw := strings.Join(items, "\n")
			if raw == "" {
				return runResult{Text: "(vacío)", Raw: raw}, nil
			}
			return runResult{Text: clip(raw, config.Get().ToolResultChars), Raw: raw}, nil
		},
	},
	"fs_read": {
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "path"); err != nil {
				return runResult{}, err
			}
			b, err := os.ReadFile(resolve(a["path"]))
			if err != nil {
				return runResult{}, err
			}
			content := string(b)
			lines := strings.Split(content, "\n")
			from := intArg(a["from"], 1)
			if from < 1 {
				from = 1
			}
			to := intArg(a["to"], len(lines))
			if to > len(lines) {
				to = len(lines)
			}
			if from > len(lines) {
				from = len(lines)
			}
			var body []string
			for i := from - 1; i < to; i++ {
				body = append(body, fmt.Sprintf("%d| %s", from+(i-(from-1)), lines[i]))
			}
			txt := clip(strings.Join(body, "\n"), config.Get().ToolResultChars)
			if to < len(lines) && a["to"] == "" {
				txt += fmt.Sprintf("\n(%d líneas en total)", len(lines))
			}
			return runResult{Text: txt, Raw: content}, nil
		},
	},
	"fs_write": {
		Danger: func(map[string]string) bool { return true },
		Describe: func(a map[string]string, c *Context) string {
			p := resolve(a["path"])
			if a["path"] != "" {
				if st, err := os.Stat(p); err == nil {
					return fmt.Sprintf("⚠ SOBRESCRIBIR %s (ya existe, %d bytes)", a["path"], st.Size())
				}
			}
			return fmt.Sprintf("escribir %s (%d chars)", a["path"], len(a["content"]))
		},
		Overwrites: func(a map[string]string) bool {
			if a["path"] == "" {
				return false
			}
			_, err := os.Stat(resolve(a["path"]))
			return err == nil
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "path"); err != nil {
				return runResult{}, err
			}
			if regexp.MustCompile(`(?s)^\s*(\$\(.*\)|` + "`" + `.*` + "`" + `)\s*$`).MatchString(a["content"]) {
				return runResult{}, fmt.Errorf("fs_write guarda texto, no ejecuta comandos: para copiar un archivo usa shell: cp origen destino")
			}
			p := resolve(a["path"])
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return runResult{}, err
			}
			if err := os.WriteFile(p, []byte(a["content"]), 0o644); err != nil {
				return runResult{}, err
			}
			rel, err := filepath.Rel(config.Get().Workdir, p)
			if err != nil {
				rel = p
			}
			return runResult{Text: "escrito " + rel}, nil
		},
	},
	"fs_edit": {
		Danger: func(map[string]string) bool { return true },
		Describe: func(a map[string]string, c *Context) string {
			return fmt.Sprintf("editar %s: reemplazar «%s»", a["path"], clip(a["find"], 40))
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "path", "find"); err != nil {
				return runResult{}, err
			}
			p := resolve(a["path"])
			b, err := os.ReadFile(p)
			if err != nil {
				return runResult{}, err
			}
			src := string(b)
			n := strings.Count(src, a["find"])
			if n == 0 {
				return runResult{}, fmt.Errorf("el texto <find> no aparece en el archivo")
			}
			// all=1 (el objetivo era «reemplaza X por Y» sin acotar): todas las apariciones
			if n > 1 && a["all"] == "" {
				return runResult{}, fmt.Errorf("<find> aparece %d veces; dame más contexto para que sea único", n)
			}
			limit := 1
			if a["all"] != "" {
				limit = -1
			}
			out := strings.Replace(src, a["find"], a["replace"], limit)
			if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
				return runResult{}, err
			}
			return runResult{Text: "editado " + a["path"]}, nil
		},
	},
	"fs_delete": {
		Danger: func(map[string]string) bool { return true },
		Describe: func(a map[string]string, c *Context) string {
			return "ELIMINAR " + a["path"]
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "path"); err != nil {
				return runResult{}, err
			}
			if err := noGlob(a, "fs_delete"); err != nil {
				return runResult{}, err
			}
			if err := os.RemoveAll(resolve(a["path"])); err != nil {
				return runResult{}, err
			}
			return runResult{Text: "eliminado " + a["path"]}, nil
		},
	},
	"shell": {
		Danger: func(a map[string]string) bool { return !shell.IsReadOnlyCommand(a["command"]) },
		Describe: func(a map[string]string, c *Context) string {
			return "$ " + a["command"]
		},
		Run: func(ctx context.Context, a map[string]string, c *Context) (runResult, error) {
			if err := need(a, "command"); err != nil {
				return runResult{}, err
			}
			cfg := config.Get()
			timeout := time.Duration(cfg.ShellTimeoutMs) * time.Millisecond
			if timeout <= 0 {
				timeout = 30 * time.Second
			}
			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := shell.CommandContext(cctx, a["command"])
			cmd.Dir = cfg.Workdir
			if cfg.Isolated {
				writable := []string{cfg.Workdir, filepath.Dir(cfg.DBPath), os.TempDir()}
				cmd = sandbox.Wrap(cctx, cmd, cfg.Workdir, writable)
			}
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			out := strings.TrimSpace(strings.Join(nonEmpty(stdout.String(), stderr.String()), "\n"))
			code := "exit 0"
			if err != nil {
				if cctx.Err() == context.DeadlineExceeded {
					code = "timeout"
				} else if ee, ok := err.(*exec.ExitError); ok {
					code = fmt.Sprintf("exit %d", ee.ExitCode())
				} else {
					code = "exit 1"
				}
			}
			shown := clip(out, cfg.ToolResultChars)
			if shown == "" {
				shown = "(sin salida)"
			}
			return runResult{Text: shown + "\n[" + code + "]", Raw: out, Failed: err != nil}, nil
		},
	},
}

func nonEmpty(a, b string) []string {
	var out []string
	if a != "" {
		out = append(out, a)
	}
	if b != "" {
		out = append(out, b)
	}
	return out
}

func joinIDs(ids []int64) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ", #")
}

// RunTool ejecuta una herramienta con confirmación y copia previa para /undo.
func RunTool(ctx context.Context, name string, args map[string]string, c *Context) Result {
	tool := Tools[name]
	if tool == nil {
		var names []string
		for k := range Tools {
			names = append(names, k)
		}
		return Result{OK: false, Text: fmt.Sprintf("herramienta desconocida «%s». Usa: %s", name, strings.Join(names, ", "))}
	}
	risky := tool.Danger != nil && tool.Danger(args)
	if risky {
		what := ""
		if tool.Describe != nil {
			what = tool.Describe(args, c)
		}
		overwrite := tool.Overwrites != nil && tool.Overwrites(args)
		if c.Confirm == nil || !c.Confirm(name, what, ConfirmOptions{Overwrite: overwrite, Args: args}) {
			return Result{OK: false, Rejected: true, Overwrite: overwrite, Text: "el usuario rechazó la acción"}
		}
	}
	if risky && !strings.HasPrefix(name, "rag_") && c.Trash != nil {
		_ = c.Trash.SnapshotOnce()
	}
	r, err := tool.Run(ctx, args, c)
	if err != nil {
		return Result{OK: false, Text: "error: " + err.Error()}
	}
	raw := r.Raw
	if raw == "" {
		raw = r.Text
	}
	if len([]rune(raw)) > 20000 {
		raw = string([]rune(raw)[:20000])
	}
	return Result{OK: !r.Failed, Text: clip(r.Text, config.Get().ToolResultChars), Raw: raw}
}

// FmtMem formatea una memoria en una línea.
func FmtMem(m *rag.Row) string { return fmtMem(m) }
