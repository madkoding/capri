// Package tui es la TUI de pantalla completa (pantalla alternativa, como vim/htop).
// La pantalla la dibuja internal/ui/frame como cadenas ANSI y este paquete solo reescribe las filas cambiadas.
package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"capri/internal/agent"
	"capri/internal/backend"
	"capri/internal/config"
	"capri/internal/embed"
	"capri/internal/event"
	"capri/internal/llm"
	"capri/internal/rag"
	"capri/internal/tools"
	"capri/internal/trace"
	"capri/internal/trash"
	"capri/internal/ui/frame"
	"capri/internal/ui/hud"
	"capri/internal/ui/lines"
)

var phaseNode = map[string]string{"recall": "recall", "args": "think", "think": "think", "tool": "act", "web": "act", "memorize": "memory"}

// ms convierte una duración a milisegundos: es la unidad de todo el motor de animación (frame/widgets).
func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func hhmm() string { return time.Now().Format("15:04:05") }

// ---------- pintor por diferencias ----------

type painter struct {
	out  *os.File
	prev []string
}

func (p *painter) invalidate() {
	p.prev = nil
	_, _ = p.out.WriteString("\x1b[2J")
}

func (p *painter) paint(rows []string) {
	var b strings.Builder
	for i, r := range rows {
		if i >= len(p.prev) || p.prev[i] != r {
			fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[0m", i+1, r)
		}
	}
	next := make([]string, len(rows))
	copy(next, rows)
	p.prev = next
	if b.Len() > 0 {
		_, _ = p.out.WriteString("\x1b[?2026h" + b.String() + "\x1b[?2026l")
	}
}

// ---------- mensajes ----------

type keyMsg struct {
	key   string
	input string
	ctrl  bool
}

type tickMsg time.Time

type agentEv struct{ e event.Event }

type turnDone struct {
	answer   string
	err      error
	canceled bool
	t0       time.Time
}

type confirmReq struct {
	name, what string
	opts       tools.ConfirmOptions
	resp       chan bool
}

type hudUpd struct{ u hud.Update }

type healthUpd struct{ llm, emb *bool }

type sysMsg struct {
	text string
	err  bool
}

type compactDone struct {
	empty bool
	err   error
}

type slashBrowser struct{ b *frame.Browser }

type pending struct {
	name, what string
	resp       chan bool
}

// ---------- app ----------

type app struct {
	rag     *rag.Rag
	agent   *agent.Agent
	painter *painter
	out     *os.File
	msg     chan any
	quit    chan struct{}

	cols, rows int
	t0         time.Time

	items  []lines.Item
	nextID int64
	// líneas ya renderizadas por elemento (los elementos no cambian una vez
	// añadidos): a 60 fps no se puede volver a ajustar todo el historial cada tick
	lineCache map[int64][][]lines.Seg
	cacheW    int
	tick      time.Duration

	busy        *time.Time
	pipe        frame.Pipe
	memorizing  bool
	typing      bool
	typingText  string
	typingN     int
	typingStep  int
	typingAt    string
	typingMeta  string
	liveThink   string // razonamiento que va llegando en streaming
	liveAnswer  string // respuesta que va llegando en streaming
	liveAt      string
	pending     *pending
	auto        atomic.Bool
	switching   atomic.Bool
	modelPick   atomic.Int32 // tras listar /model (1) o /embed (2), un número suelto elige
	health      frame.Health
	hud         frame.HudState
	stats       frame.Stats
	hits        []frame.Hit
	browser     *frame.Browser
	boot        *time.Time
	offset      int
	compacting  bool
	compactions int
	history     []string
	histIdx     int
	value       string
	cur         int
	cancel      context.CancelFunc
	toolsCount  int
	viewTotal   int
	viewH       int
}

// tickInterval es el periodo de repintado: 60 fps por defecto, CAPRI_FPS lo
// cambia y CAPRI_ANIM=0 deja un fotograma por segundo.
func tickInterval() time.Duration {
	if os.Getenv("CAPRI_ANIM") == "0" {
		return time.Second
	}
	fps := 60
	if f := os.Getenv("CAPRI_FPS"); f != "" {
		var n int
		if _, err := fmt.Sscanf(f, "%d", &n); err == nil && n > 0 {
			fps = min(n, 240)
		}
	}
	return time.Second / time.Duration(fps)
}

// itemLines devuelve las líneas de un elemento, de la caché si ya se ajustaron a este ancho.
func (a *app) itemLines(it lines.Item, W int) [][]lines.Seg {
	if a.cacheW != W || a.lineCache == nil {
		a.lineCache = map[int64][][]lines.Seg{}
		a.cacheW = W
	}
	if rows, ok := a.lineCache[it.ID]; ok {
		return rows
	}
	rows := lines.ItemLines(it, W)
	a.lineCache[it.ID] = rows
	return rows
}

func (a *app) push(it lines.Item) {
	it.ID = a.nextID
	a.nextID++
	a.items = append(a.items, it)
}

func (a *app) refreshStats() {
	st := a.rag.Stats()
	a.stats = frame.Stats{Note: st.Note, Chat: st.Chat, Summary: st.Summary}
}

func (a *app) enter(name string) {
	if a.pipe.Active == name {
		return
	}
	if a.pipe.Active != "" && !contains(a.pipe.Done, a.pipe.Active) {
		a.pipe.Done = append(a.pipe.Done, a.pipe.Active)
	}
	a.pipe.Active = name
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (a *app) finishBoot() {
	a.boot = nil
	a.items = append([]lines.Item{{ID: 0, Kind: "banner"}}, a.items...)
}

func (a *app) onCompactEvent(e event.Event) {
	if e.Type == "compacting" {
		a.compacting = true
		return
	}
	a.compacting = false
	a.compactions++
	it := lines.Item{Kind: "compact", Scope: e.Scope, Reason: e.Reason, Before: e.Before, After: e.After, Turns: e.Turns, Summary: e.Summary, Dropped: e.Dropped, RagID: e.ID, LLM: e.LLM}
	a.push(it)
	a.refreshStats()
}

func (a *app) onAgentEvent(e event.Event) {
	switch e.Type {
	case "phase":
		if node := phaseNode[e.Phase]; node != "" {
			a.enter(node)
		}
		if e.Phase == "memorize" {
			a.memorizing = true
		}
	case "step":
		a.pipe.Step = e.Index
		a.pipe.Total = e.Total
	case "recall":
		a.hits = nil
		if hs, ok := e.Hits.([]rag.Hit); ok {
			for _, h := range hs {
				a.hits = append(a.hits, frame.Hit{ID: h.ID, Sim: h.Sim, Title: h.Title})
			}
		}
	case "route":
		a.enter("route")
		if e.Intent != "none" {
			a.push(lines.Item{Kind: "note", Text: fmt.Sprintf("⇢ %s  %.0f%%", e.Intent, e.Score*100)})
		}
	case "note":
		if strings.HasPrefix(e.Text, "💭") {
			a.liveThink = "" // la nota definitiva sustituye al razonamiento en vivo
		}
		a.push(lines.Item{Kind: "note", Text: e.Text})
	case "delta":
		if a.liveAt == "" {
			a.liveAt = hhmm()
		}
		if e.Phase == "think" {
			a.liveThink += e.Text
		} else {
			a.liveAnswer += e.Text
		}
	case "tool_result":
		a.toolsCount++
		a.push(lines.Item{Kind: "tool", Name: e.Name, Args: e.Args, OK: e.OK, Result: e.Text})
	case "compacting", "compact":
		a.onCompactEvent(e)
	case "undoable":
		a.push(lines.Item{Kind: "note", Text: "↩ /undo para deshacer este turno"})
	case "noundo":
		a.push(lines.Item{Kind: "note", Text: "⚠ sin copia para /undo: " + e.Text})
	case "memorized":
		a.memorizing = false
		a.refreshStats()
	}
}

// ---------- arranque ----------

// Start abre la TUI y devuelve cuando el usuario sale.
func Start(r *rag.Rag) error {
	out := os.Stdout
	enableVT()
	in := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(in)
	if err != nil {
		return err
	}
	left := false
	restore := func() {
		if left {
			return
		}
		left = true
		_ = term.Restore(in, oldState)
		_, _ = out.WriteString("\x1b[?7h\x1b[?25h\x1b[?1049l")
	}
	defer restore()

	_, _ = out.WriteString("\x1b[?1049h\x1b[2J\x1b[H\x1b[?7l\x1b[?25l")

	p := &painter{out: out}
	a := &app{
		rag:     r,
		painter: p,
		out:     out,
		msg:     make(chan any, 256),
		quit:    make(chan struct{}),
		t0:      time.Now(),
		histIdx: -1,
		nextID:  1, // el 0 es el banner
		tick:    tickInterval(),
	}
	cfg := config.Get()
	a.auto.Store(cfg.AutoApprove)
	a.refreshStats()
	now := time.Now()
	a.boot = &now
	a.agent = agent.New(r, a.confirm, nil)

	a.resize()
	cols, rows := a.cols, a.rows
	_ = cols
	_ = rows

	stopHud := hud.StartHud(func(u hud.Update) {
		select {
		case a.msg <- hudUpd{u}:
		default:
		}
	}, 3*time.Second)
	defer stopHud()
	a.checkHealth()
	go readKeys(a.msg)

	ticker := time.NewTicker(a.tick)
	defer ticker.Stop()
	healthTicker := time.NewTicker(8 * time.Second)
	defer healthTicker.Stop()

	winch, stopResize := watchResize()
	defer stopResize()

	a.draw(time.Now())
	for {
		select {
		case m := <-a.msg:
			a.handle(m)
		case now := <-ticker.C:
			a.advanceTyping()
			if a.boot != nil && now.Sub(*a.boot).Milliseconds() > frame.BootMs {
				a.finishBoot()
			}
			a.draw(now)
		case <-healthTicker.C:
			a.checkHealth()
		case <-winch:
			a.resize()
			a.painter.invalidate()
			a.draw(time.Now())
		case <-a.quit:
			txt := transcript(a.items)
			restore()
			if txt != "" {
				_, _ = out.WriteString(txt + "\n")
			}
			return nil
		}
	}
}

func (a *app) confirm(name, what string, o tools.ConfirmOptions) bool {
	if a.auto.Load() && !o.Force && !o.Overwrite {
		return true
	}
	resp := make(chan bool)
	select {
	case a.msg <- confirmReq{name: name, what: what, opts: o, resp: resp}:
	case <-a.quit:
		return false
	}
	select {
	case v := <-resp:
		return v
	case <-a.quit:
		return false
	}
}

func (a *app) resize() {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 100, 36
	}
	a.cols = max(40, cols)
	a.rows = max(10, rows)
}

func (a *app) checkHealth() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		l := llm.Health(ctx)
		e := embed.Health(ctx)
		select {
		case a.msg <- healthUpd{llm: &l, emb: &e}:
		default:
		}
	}()
}

// ---------- entrada de teclado ----------

func readKeys(ch chan<- any) {
	bytes := make(chan byte, 128)
	go func() {
		br := bufio.NewReader(os.Stdin)
		for {
			b, err := br.ReadByte()
			if err != nil {
				close(bytes)
				return
			}
			bytes <- b
		}
	}()
	for {
		b, ok := <-bytes
		if !ok {
			return
		}
		if b >= 0x80 {
			// UTF-8 multibyte: completa la secuencia con los bytes siguientes.
			n := utf8Len(b)
			buf := []byte{b}
			for len(buf) < n {
				select {
				case x, ok := <-bytes:
					if !ok {
						return
					}
					buf = append(buf, x)
				case <-time.After(20 * time.Millisecond):
					buf = nil
				}
				if buf == nil {
					break
				}
			}
			if r, size := utf8.DecodeRune(buf); r != utf8.RuneError && size == len(buf) {
				ch <- keyMsg{input: string(r)}
			}
			continue
		}
		if b != 0x1b {
			ch <- decodeByte(b)
			continue
		}
		seq := []byte{}
		timer := time.NewTimer(20 * time.Millisecond)
		complete := false
	loop:
		for {
			select {
			case x, ok := <-bytes:
				if !ok {
					break loop
				}
				seq = append(seq, x)
				c := x
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '~' {
					complete = true
					break loop
				}
			case <-timer.C:
				break loop
			}
		}
		timer.Stop()
		if !complete || len(seq) == 0 {
			ch <- keyMsg{key: "escape"}
			continue
		}
		s := string(seq)
		switch s {
		case "[A":
			ch <- keyMsg{key: "up"}
		case "[B":
			ch <- keyMsg{key: "down"}
		case "[C":
			ch <- keyMsg{key: "right"}
		case "[D":
			ch <- keyMsg{key: "left"}
		case "[5~":
			ch <- keyMsg{key: "pageup"}
		case "[6~":
			ch <- keyMsg{key: "pagedown"}
		case "[3~":
			ch <- keyMsg{key: "delete"}
		case "[H", "[1~":
			ch <- keyMsg{key: "home"}
		case "[F", "[4~":
			ch <- keyMsg{key: "end"}
		}
	}
}

func utf8Len(b byte) int {
	switch {
	case b&0xE0 == 0xC0:
		return 2
	case b&0xF0 == 0xE0:
		return 3
	case b&0xF8 == 0xF0:
		return 4
	}
	return 1
}

func decodeByte(b byte) keyMsg {
	switch b {
	case 3:
		return keyMsg{key: "ctrl-c"}
	case 13, 10:
		return keyMsg{key: "enter"}
	case 127, 8:
		return keyMsg{key: "backspace"}
	case 21:
		return keyMsg{key: "ctrl-u", ctrl: true}
	case 1:
		return keyMsg{key: "ctrl-a", ctrl: true}
	case 5:
		return keyMsg{key: "ctrl-e", ctrl: true}
	case 9:
		return keyMsg{key: "tab"}
	}
	if b < 32 {
		return keyMsg{key: "ctrl-" + string(rune(b+'a'-1)), ctrl: true}
	}
	return keyMsg{input: string(rune(b))}
}

// ---------- bucle ----------

func (a *app) handle(m any) {
	now := time.Now()
	switch v := m.(type) {
	case keyMsg:
		a.onKey(v, now)
	case agentEv:
		a.onAgentEvent(v.e)
		if v.e.Type == "delta" {
			return // llegan muchos por segundo: el tick (100 ms) ya repinta
		}
	case hudUpd:
		a.hud = frame.HudState{Gpu: v.u.Gpu, Cpu: v.u.Cpu, Model: v.u.Model, Nctx: v.u.Nctx}
	case healthUpd:
		a.health = frame.Health{LLM: v.llm, Emb: v.emb}
	case sysMsg:
		if v.err {
			a.push(lines.Item{Kind: "error", Text: v.text})
		} else {
			a.push(lines.Item{Kind: "sys", Text: v.text})
		}
		a.refreshStats()
	case slashBrowser:
		a.browser = v.b
	case compactDone:
		a.compacting = false
		a.busy = nil
		if v.err != nil {
			a.push(lines.Item{Kind: "error", Text: "no se pudo compactar: " + v.err.Error()})
		} else if v.empty {
			a.push(lines.Item{Kind: "sys", Text: "no hay nada que compactar todavía"})
		}
	case confirmReq:
		if a.auto.Load() && !v.opts.Force && !v.opts.Overwrite {
			v.resp <- true
			return
		}
		a.pending = &pending{name: v.name, what: v.what, resp: v.resp}
	case turnDone:
		secs := time.Since(v.t0).Seconds()
		meta := []string{fmt.Sprintf("%.1fs", secs)}
		if a.toolsCount == 1 {
			meta = append(meta, "1 acción")
		} else if a.toolsCount > 1 {
			meta = append(meta, fmt.Sprintf("%d acciones", a.toolsCount))
		}
		if st := llm.Snapshot(); st.Gen > 0 {
			meta = append(meta, fmt.Sprintf("%.0f t/s", st.Gen))
		}
		a.typing = true
		a.typingText = v.answer
		a.typingN = 0
		// ~80 caracteres/s y el texto entero en ≤1,4 s, sea cual sea la tasa de fotogramas
		perSecond := float64(max(8, int((len([]rune(v.answer))+13)/14))) * 10
		a.typingStep = max(1, int(perSecond*a.tick.Seconds()+0.5))
		a.typingAt = hhmm()
		if a.liveAt != "" {
			a.typingAt = a.liveAt
		}
		if live := strings.TrimSpace(a.liveAnswer); live != "" && strings.Contains(v.answer, live) {
			a.typingN = len([]rune(v.answer)) // ya se vio llegar: sin animación
		}
		a.liveThink, a.liveAnswer, a.liveAt = "", "", ""
		a.typingMeta = strings.Join(meta, " · ")
		a.pipe.Active = ""
		a.busy = nil
		a.cancel = nil
		if v.err != nil {
			msg := v.err.Error()
			if v.canceled {
				msg = "turno cancelado"
			}
			a.push(lines.Item{Kind: "error", Text: msg})
		}
	}
	a.draw(now)
}

func (a *app) advanceTyping() {
	if !a.typing {
		return
	}
	n := len([]rune(a.typingText))
	if a.typingN < n {
		a.typingN = min(n, a.typingN+a.typingStep)
	}
	if a.typingN >= n {
		a.push(lines.Item{Kind: "assistant", Text: a.typingText, At: a.typingAt, Meta: a.typingMeta})
		a.typing = false
		a.typingText = ""
	}
}

func (a *app) onKey(k keyMsg, now time.Time) {
	if k.key == "ctrl-c" {
		a.quitNow()
		return
	}
	if a.boot != nil {
		a.finishBoot()
		return
	}
	if k.key == "pageup" {
		step := max(1, a.viewH-2)
		a.offset = min(max(0, a.viewTotal-a.viewH), a.offset+step)
		return
	}
	if k.key == "pagedown" {
		a.offset = max(0, a.offset-max(1, a.viewH-2))
		return
	}
	if a.typing {
		a.typingN = len([]rune(a.typingText))
		a.advanceTyping()
		return
	}
	if a.browser != nil {
		a.onBrowserKey(k)
		return
	}
	if k.key == "escape" && a.busy != nil {
		if a.cancel != nil {
			a.cancel()
		}
		return
	}
	if a.pending != nil {
		switch {
		case k.key == "enter" || k.input == "y" || k.input == "Y":
			a.pending.resp <- true
			a.pending = nil
		case k.key == "escape" || k.input == "n" || k.input == "N":
			a.pending.resp <- false
			a.pending = nil
		case k.input == "a":
			if !dangerRe.MatchString(a.pending.what) {
				a.auto.Store(true)
			}
			a.pending.resp <- true
			a.pending = nil
		}
		return
	}
	if a.busy != nil || a.browser != nil || a.boot != nil {
		return
	}
	a.onLineKey(k)
}

func (a *app) onLineKey(k keyMsg) {
	switch k.key {
	case "enter":
		v := strings.TrimSpace(a.value)
		if v != "" {
			a.history = append([]string{v}, a.history...)
			a.histIdx = -1
			a.value = ""
			a.cur = 0
			a.submit(v)
		}
	case "backspace", "delete":
		r := []rune(a.value)
		if a.cur > 0 && a.cur <= len(r) {
			r = append(r[:a.cur-1], r[a.cur:]...)
			a.value = string(r)
			a.cur--
		}
	case "left":
		a.cur = max(0, a.cur-1)
	case "right":
		a.cur = min(len([]rune(a.value)), a.cur+1)
	case "up", "down":
		i := a.histIdx
		if k.key == "up" {
			i++
		} else {
			i--
		}
		i = max(-1, min(len(a.history)-1, i))
		a.histIdx = i
		if i < 0 {
			a.value = ""
		} else {
			a.value = a.history[i]
		}
		a.cur = len([]rune(a.value))
	case "ctrl-u":
		a.value = ""
		a.cur = 0
	case "ctrl-a":
		a.cur = 0
	case "ctrl-e":
		a.cur = len([]rune(a.value))
	default:
		if k.input != "" && !k.ctrl {
			t := strings.ReplaceAll(strings.ReplaceAll(k.input, "\r", " "), "\n", " ")
			r := []rune(a.value)
			if a.cur > len(r) {
				a.cur = len(r)
			}
			r = append(r[:a.cur], append([]rune(t), r[a.cur:]...)...)
			a.value = string(r)
			a.cur += len([]rune(t))
		}
	}
}

func (a *app) onBrowserKey(k keyMsg) {
	b := a.browser
	if b.Confirm != 0 {
		if k.input == "y" || k.input == "Y" {
			row := a.rag.Get(b.Confirm)
			if row != nil {
				a.agent.Trash.BeginTurn(fmt.Sprintf("olvidar #%d", row.ID))
				a.agent.Trash.NoteMemory(trash.MemOp{Type: "delete", Row: &trash.MemRow{Title: row.Title, Content: row.Content, Tags: row.Tags, Kind: row.Kind}})
				a.rag.Delete(row.ID)
				a.agent.Trash.CommitTurn()
			}
			a.browser = a.loadBrowser(b.Sel)
			a.refreshStats()
		} else if k.input == "n" || k.input == "N" || k.key == "escape" {
			b.Confirm = 0
		}
		return
	}
	switch {
	case k.key == "escape" || k.input == "q":
		a.browser = nil
	case k.key == "up":
		b.Sel = max(0, b.Sel-1)
	case k.key == "down":
		b.Sel = min(len(b.Rows)-1, b.Sel+1)
	case k.input == "d" && b.Sel >= 0 && b.Sel < len(b.Rows):
		b.Confirm = b.Rows[b.Sel].ID
	}
}

func (a *app) loadBrowser(sel int) *frame.Browser {
	rows := a.rag.List(60, "")
	b := &frame.Browser{}
	for _, r := range rows {
		b.Rows = append(b.Rows, frame.BrowserRow{ID: r.ID, Kind: r.Kind, Title: r.Title, Content: r.Content, Updated: r.Updated})
	}
	if len(b.Rows) == 0 {
		b.Sel = 0
	} else {
		b.Sel = min(sel, len(b.Rows)-1)
	}
	return b
}

func (a *app) quitNow() {
	select {
	case <-a.quit:
	default:
		close(a.quit)
	}
}

// ---------- turnos ----------

func (a *app) submit(text string) {
	a.offset = 0
	pick := a.modelPick.Swap(0)
	if _, err := strconv.Atoi(text); pick != 0 && err == nil {
		if pick == 2 {
			a.embedCmd(text)
		} else {
			a.modelCmd(text)
		}
		return
	}
	if strings.HasPrefix(text, "/") {
		a.slash(text)
		return
	}
	if a.switching.Load() {
		a.push(lines.Item{Kind: "sys", Text: "cargando modelo: espera a que termine"})
		return
	}
	a.push(lines.Item{Kind: "user", Text: text, At: hhmm()})
	t0 := time.Now()
	a.busy = &t0
	a.pipe = frame.Pipe{Active: "recall"}
	a.toolsCount = 0
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	go func() {
		ans, err := a.agent.Run(ctx, text, func(e event.Event) {
			a.msg <- agentEv{e}
		})
		a.msg <- turnDone{answer: ans, err: err, canceled: ctx.Err() != nil, t0: t0}
	}()
}

// ---------- pintado ----------

func (a *app) draw(now time.Time) {
	W := max(20, a.cols-3)
	var ls [][]lines.Seg
	for _, it := range a.items {
		for _, row := range a.itemLines(it, W) {
			if len(row) == 0 && len(ls) > 0 && len(ls[len(ls)-1]) == 0 {
				continue
			}
			ls = append(ls, row)
		}
	}
	dots := strings.Repeat("▸", 1+int(now.Sub(a.t0).Milliseconds()/140)%3)
	if a.liveThink != "" {
		// razonamiento en vivo: se enseña la cola, como un ticker
		t := []rune(strings.Join(strings.Fields(a.liveThink), " "))
		if len(t) > 700 {
			t = append([]rune("…"), t[len(t)-700:]...)
		}
		ls = append(ls, lines.ItemLines(lines.Item{Kind: "note", Text: "💭 " + string(t) + " " + dots}, W)...)
	}
	if a.liveAnswer != "" && !a.typing {
		part := lines.Item{Kind: "assistant", Text: a.liveAnswer, At: a.liveAt, Meta: "transmitiendo " + dots}
		ls = append(ls, lines.ItemLines(part, W)...)
	}
	if a.typing {
		r := []rune(a.typingText)
		n := min(a.typingN, len(r))
		part := lines.Item{Kind: "assistant", Text: string(r[:n]), At: a.typingAt, Meta: "transmitiendo " + dots}
		ls = append(ls, lines.ItemLines(part, W)...)
	}
	var boot *frame.Boot
	if a.boot != nil {
		boot = &frame.Boot{T: ms(now.Sub(*a.boot))}
	}
	var elapsed float64
	if a.busy != nil {
		elapsed = now.Sub(*a.busy).Seconds()
	}
	var pend *frame.Pending
	if a.pending != nil {
		pend = &frame.Pending{Name: a.pending.name, What: a.pending.what}
	}
	st := frame.State{
		Cols:        a.cols,
		Rows:        a.rows,
		T:           ms(now.Sub(a.t0)),
		Boot:        boot,
		Lines:       ls,
		Offset:      a.offset,
		Busy:        a.busy != nil,
		Elapsed:     elapsed,
		Pipe:        a.pipe,
		Memorizing:  a.memorizing,
		Typing:      a.typing,
		Compacting:  a.compacting,
		Compactions: a.compactions,
		Pending:     pend,
		Browser:     a.browser,
		Health:      a.health,
		Hud:         a.hud,
		Stats:       a.stats,
		Hits:        a.hits,
		Auto:        a.auto.Load(),
		Value:       a.value,
		Cur:         a.cur,
		Model:       a.hud.Model,
		Count:       a.stats.Note + a.stats.Chat + a.stats.Summary,
		Locked:      a.browser != nil || a.typing,
	}
	f := frame.BuildFrame(st)
	a.viewTotal = f.Total
	a.viewH = f.H
	a.painter.paint(f.Rows)
}

// ---------- transcripción ----------

var blankRunRe = regexp.MustCompile(`\n{3,}`)

func transcript(items []lines.Item) string {
	var out []string
	for _, it := range items {
		if it.Kind == "banner" {
			continue
		}
		for _, row := range lines.ItemLines(it, 100) {
			out = append(out, lines.LineToPlain(row))
		}
	}
	s := strings.Join(out, "\n")
	return strings.TrimSpace(blankRunRe.ReplaceAllString(s, "\n\n"))
}

func (a *app) exportChat(file string) string {
	dir := filepath.Join(filepath.Dir(config.Get().DBPath), "transcripts")
	_ = os.MkdirAll(dir, 0o755)
	name := file
	if name == "" {
		name = "capri-" + strings.NewReplacer("T", "-", ":", "-").Replace(time.Now().Format("2006-01-02T15:04")) + ".txt"
	}
	dest := name
	if !filepath.IsAbs(name) {
		dest = filepath.Join(dir, name)
	}
	_ = os.WriteFile(dest, []byte(transcript(a.items)+"\n"), 0o644)
	return dest
}

// ---------- comandos / ----------

var helpLines = []string{
	"/undo [list]    deshace el último turno que modificó archivos o memoria",
	"/mem            explorador de memoria (↑↓, d olvidar)   ·  /mem <n> lista las n últimas",
	"/search <txt>   buscar en el cerebro",
	"/forget <id>    borrar una memoria",
	"/pin <id>       fija un recuerdo (no se poda ni se funde y puntúa más)  ·  /unpin <id> lo suelta",
	"/model [n|texto] lista los modelos (instalados y descargables) y cambia el de chat; si falta lo baja de Hugging Face",
	"/provider       modelos internos (por defecto) o un servidor externo: /provider ollama|openai <url>|internal",
	"/embed [n|texto] lista o cambia el modelo de embeddings (lo baja de Hugging Face si falta y reindexa la memoria)  ·  /embed default",
	"/think [on|off] muestra (💭) u oculta el razonamiento del modelo; sin argumento cambia el estado",
	"/dedupe         funde notas casi idénticas (se puede deshacer)",
	"/prune [días]   borra turnos de chat guardados de hace más de N días (30)",
	"/bad [nota]     marca el último turno como mal resuelto (alimenta from-trace)",
	"/compact         compacta ya el contexto de la sesión (resumen → memoria)  ·  /context muestra el estado",
	"/export [file]  guarda la conversación en un archivo de texto",
	"/auto | /ask    ejecutar sin confirmar / confirmar lo que modifica",
	"/clear          limpiar memoria de trabajo (el RAG se conserva)",
	"/quit           salir  (también Ctrl+C; al salir se imprime la conversación)",
	"RePág/AvPág     desplazar el chat · Esc cancela el turno en curso",
}

func (a *app) slash(line string) {
	parts := strings.Fields(strings.TrimPrefix(line, "/"))
	if len(parts) == 0 {
		a.push(lines.Item{Kind: "sys", Text: "comando desconocido: /"})
		return
	}
	cmd := parts[0]
	arg := strings.Join(parts[1:], " ")
	cfg := config.Get()

	switch cmd {
	case "help":
		a.push(lines.Item{Kind: "sys", Text: strings.Join(helpLines, "\n")})

	case "compact":
		a.pipe = frame.Pipe{}
		t0 := time.Now()
		a.busy = &t0
		a.compacting = true
		go func() {
			r, err := a.agent.CompactNow(context.Background(), func(e event.Event) {
				a.msg <- agentEv{e}
			})
			a.msg <- compactDone{empty: r == nil && err == nil, err: err}
		}()

	case "context":
		c := a.agent.ContextInfo()
		session := ""
		if !c.Session {
			session = "  (memoria de sesión desactivada: CAPRI_SESSION=0)"
		}
		server := ""
		if c.ServerCtx > 0 {
			server = fmt.Sprintf("  (el servidor admite %d)", c.ServerCtx)
		}
		turns := "turnos"
		if c.Turns == 1 {
			turns = "turno"
		}
		summary := ""
		if c.HasSummary {
			summary = " · con resumen previo"
		}
		a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf(
			"Contexto de trabajo%s\n  ventana efectiva   %d tokens%s\n  compacta al llegar a %d tokens (%.0f %%) · conserva los últimos %d turnos\n  sesión ahora       %d tokens · %d %s%s\n  compactaciones     %d en esta sesión (los resúmenes quedan en la memoria, ⇣)",
			session, c.Window, server, c.Threshold, cfg.CtxThreshold*100, cfg.CtxKeep, c.Used, c.Turns, turns, summary, c.Compactions)})

	case "export":
		a.push(lines.Item{Kind: "sys", Text: "conversación guardada en " + a.exportChat(arg)})

	case "undo":
		if arg == "list" {
			l := a.agent.Trash.List(5)
			if len(l) == 0 {
				a.push(lines.Item{Kind: "sys", Text: "no hay nada que deshacer"})
				return
			}
			var rows []string
			for _, x := range l {
				files := ""
				if x.Files {
					files = "archivos"
				}
				mem := ""
				if x.Memory > 0 {
					mem = fmt.Sprintf("%d de memoria", x.Memory)
				}
				sep := ""
				if x.Files && x.Memory > 0 {
					sep = " + "
				}
				rows = append(rows, fmt.Sprintf("%s  %s  (%s%s%s)", x.When, x.Label, files, sep, mem))
			}
			a.push(lines.Item{Kind: "sys", Text: strings.Join(rows, "\n")})
			return
		}
		ok, msg := a.agent.Trash.Undo(context.Background(), a.rag)
		a.refreshStats()
		prefix := ""
		if ok {
			prefix = "↩ "
		}
		a.push(lines.Item{Kind: "sys", Text: prefix + msg})

	case "mem":
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			rows := a.rag.List(n, "")
			if len(rows) == 0 {
				a.push(lines.Item{Kind: "sys", Text: "el cerebro está vacío"})
				return
			}
			var out []string
			for i := range rows {
				out = append(out, tools.FmtMem(&rows[i]))
			}
			a.push(lines.Item{Kind: "sys", Text: strings.Join(out, "\n")})
			return
		}
		a.browser = a.loadBrowser(0)

	case "search":
		go func(q string) {
			found := a.rag.Search(context.Background(), q, 6, 0.35)
			if len(found) == 0 {
				a.msg <- sysMsg{text: "sin resultados"}
				return
			}
			var out []string
			for i := range found {
				out = append(out, tools.FmtMem(&found[i].Row))
			}
			a.msg <- sysMsg{text: strings.Join(out, "\n")}
		}(arg)

	case "forget":
		n, _ := strconv.ParseInt(arg, 10, 64)
		if a.rag.Delete(n) {
			a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("#%s eliminada", arg)})
		} else {
			a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("no existe #%s", arg)})
		}
		a.refreshStats()

	case "model":
		a.modelCmd(arg)

	case "provider":
		a.providerCmd(arg)

	case "embed":
		a.embedCmd(arg)

	case "think":
		on := !config.Get().Thinking
		switch arg {
		case "on":
			on = true
		case "off":
			on = false
		}
		config.SetThinking(on)
		a.push(lines.Item{Kind: "sys", Text: "razonamiento " + map[bool]string{true: "activado: verás 💭 con lo que piensa el modelo (los turnos tardan más)", false: "desactivado"}[on]})

	case "pin", "unpin":
		n, _ := strconv.ParseInt(arg, 10, 64)
		if a.rag.Pin(n, cmd == "pin") {
			a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("#%s %s", arg, map[string]string{"pin": "fijada", "unpin": "suelta"}[cmd])})
		} else {
			a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("no existe #%s", arg)})
		}

	case "dedupe":
		found := a.rag.Dedupe(0, "", false)
		if len(found) == 0 {
			a.push(lines.Item{Kind: "sys", Text: "no hay notas duplicadas"})
			return
		}
		a.agent.Trash.BeginTurn("fusionar duplicados")
		for _, m := range found {
			if row := a.rag.Get(m.Drop); row != nil {
				a.agent.Trash.NoteMemory(trash.MemOp{Type: "delete", Row: &trash.MemRow{Title: row.Title, Content: row.Content, Tags: row.Tags, Kind: row.Kind}})
			}
		}
		a.rag.Dedupe(0, "", true)
		a.agent.Trash.CommitTurn()
		a.refreshStats()
		var rows []string
		for _, m := range found {
			rows = append(rows, fmt.Sprintf("  #%d → #%d  %s", m.Drop, m.Keep, m.Title))
		}
		a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("fusionadas %d:\n%s\n↩ /undo para deshacer", len(found), strings.Join(rows, "\n"))})

	case "prune":
		days := 30
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			days = n
		}
		n := a.rag.PruneChats(days)
		a.refreshStats()
		a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("%d turno(s) de chat de hace más de %d días eliminados", n, days)})

	case "bad":
		tr := a.agent.LastTrace()
		if tr != nil && trace.Mark(tr.ID, "bad", arg) {
			a.push(lines.Item{Kind: "sys", Text: "anotado: ese turno saldrá en from-trace"})
		} else {
			a.push(lines.Item{Kind: "sys", Text: "no hay un turno reciente que marcar (o las trazas están desactivadas)"})
		}

	case "auto":
		a.auto.Store(true)
		a.push(lines.Item{Kind: "sys", Text: "↯ modo AUTO: ejecuto sin preguntar (sobrescribir y los pasos tras un fallo siempre preguntan)"})

	case "ask":
		a.auto.Store(false)
		a.push(lines.Item{Kind: "sys", Text: "◆ modo ASK: confirmaré lo que modifica algo"})

	case "clear":
		a.agent.Reset()
		a.push(lines.Item{Kind: "sys", Text: "sesión limpia (la memoria y los resúmenes guardados se conservan)"})

	case "quit", "exit":
		a.quitNow()

	default:
		a.push(lines.Item{Kind: "sys", Text: "comando desconocido: /" + cmd})
	}
}

var dangerRe = regexp.MustCompile(`SOBRESCRIBIR|OLVIDAR|ELIMINAR|anterior falló`)

// modelCmd implementa /model: sin argumento lista (instalados y descargables);
// con argumento cambia el modelo de chat, bajándolo de Hugging Face si no está.
func (a *app) modelCmd(arg string) {
	const dir = "models"
	if config.Get().External() {
		a.externalModelCmd(arg)
		return
	}
	cur := backend.Current()
	if arg == "" {
		var out []string
		for i, e := range backend.Entries(dir) {
			mark := "  "
			switch {
			case e.File == cur:
				mark = "▶ "
			case !e.Installed:
				mark = "⇣ "
			}
			out = append(out, fmt.Sprintf("%s%d. %s", mark, i+1, e.File))
		}
		a.modelPick.Store(1)
		a.push(lines.Item{Kind: "sys", Text: strings.Join(out, "\n") + "\n▶ en uso · ⇣ se descarga de Hugging Face al elegirlo\nescribe el número para elegir  ·  /model <texto>  ·  /model hf:usuario/repo/archivo.gguf"})
		return
	}
	pick, err := backend.Pick(dir, arg)
	if err != nil {
		a.push(lines.Item{Kind: "sys", Text: err.Error()})
		return
	}
	if pick.File == cur {
		a.push(lines.Item{Kind: "sys", Text: pick.File + " ya está cargado"})
		return
	}
	if a.busy != nil {
		a.push(lines.Item{Kind: "sys", Text: "espera a que termine el turno en curso"})
		return
	}
	if !a.switching.CompareAndSwap(false, true) {
		a.push(lines.Item{Kind: "sys", Text: "ya hay un cambio de modelo en curso"})
		return
	}
	a.push(lines.Item{Kind: "sys", Text: "⟫ " + map[bool]string{true: "cargando ", false: "descargando "}[pick.Installed] + pick.File + "… (los turnos esperan hasta que termine)"})
	go func() {
		defer a.switching.Store(false)
		ctx := context.Background()
		if !pick.Installed {
			if err := a.fetch(ctx, dir, pick); err != nil {
				a.msg <- sysMsg{text: "no se pudo descargar: " + err.Error()}
				return
			}
			a.msg <- sysMsg{text: "  ⟫ cargando " + pick.File + "…"}
		}
		if err := backend.SwitchLLM(ctx, dir, pick.File); err != nil {
			a.msg <- sysMsg{text: "no se pudo cambiar de modelo: " + err.Error()}
			return
		}
		text := "✔ modelo activo: " + pick.File
		if d := config.ApplyPreset(pick.File); d != "" {
			text += "\n  " + d
		}
		a.msg <- sysMsg{text: text}
	}()
}

// externalModelCmd es /model con un proveedor externo activo: elige entre los modelos que ofrece su servidor.
func (a *app) externalModelCmd(arg string) {
	cfg := config.Get()
	models, err := backend.ProviderModels(context.Background(), cfg.LLMURL)
	if err != nil || len(models) == 0 {
		a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("el proveedor %s no ofrece modelos (%v). /provider internal vuelve a los modelos internos", cfg.LLMURL, err)})
		return
	}
	if arg == "" {
		var out []string
		for i, m := range models {
			mark := "  "
			if m == cfg.Model {
				mark = "▶ "
			}
			out = append(out, fmt.Sprintf("%s%d. %s", mark, i+1, m))
		}
		a.modelPick.Store(1)
		a.push(lines.Item{Kind: "sys", Text: "proveedor externo " + cfg.LLMURL + "\n" + strings.Join(out, "\n") + "\nescribe el número para elegir  ·  /provider internal vuelve a los modelos internos"})
		return
	}
	pick := ""
	if n, err := strconv.Atoi(arg); err == nil && n >= 1 && n <= len(models) {
		pick = models[n-1]
	} else {
		for _, m := range models {
			if strings.Contains(strings.ToLower(m), strings.ToLower(arg)) {
				if pick != "" {
					a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("%q es ambiguo", arg)})
					return
				}
				pick = m
			}
		}
	}
	if pick == "" {
		a.push(lines.Item{Kind: "sys", Text: fmt.Sprintf("ningún modelo coincide con %q", arg)})
		return
	}
	if err := config.ProviderModel(pick); err != nil {
		a.push(lines.Item{Kind: "sys", Text: err.Error()})
		return
	}
	text := "✔ modelo del proveedor: " + pick
	if d := config.ApplyPreset(pick); d != "" {
		text += "\n  " + d
	}
	a.push(lines.Item{Kind: "sys", Text: text})
}

// providerCmd implementa /provider. Por defecto Capri usa sus modelos internos
// (llama.cpp); un servidor externo (Ollama, vLLM, LM Studio…) solo se usa si lo configuras aquí.
func (a *app) providerCmd(arg string) {
	cfg := config.Get()
	f := strings.Fields(arg)
	sys := func(t string) { a.push(lines.Item{Kind: "sys", Text: t}) }
	if len(f) == 0 {
		if cfg.External() {
			sys(fmt.Sprintf("proveedor externo: %s · modelo %s · embeddings %s (%s)\n/provider internal vuelve a los modelos internos",
				cfg.LLMURL, map[bool]string{true: "(elige con /model)", false: cfg.Model}[cfg.Model == ""], cfg.EmbedModel, cfg.EmbedURL))
		} else {
			sys("modelos internos (llama.cpp, GGUF de models/)  ·  /model para cambiar\n" +
				"externo: /provider ollama [url]  ·  /provider openai <url>  ·  /provider embed <modelo>")
		}
		return
	}
	if a.busy != nil || a.switching.Load() {
		sys("espera a que termine el turno o el cambio de modelo en curso")
		return
	}
	switch f[0] {
	case "internal", "interno":
		if err := config.SetProvider(nil); err != nil {
			sys(err.Error())
			return
		}
		a.switching.Store(true)
		sys("⟫ volviendo a los modelos internos…")
		go func() {
			defer a.switching.Store(false)
			if _, err := backend.StartInternal(context.Background(), backend.Request{}); err != nil {
				a.msg <- sysMsg{text: "modelos internos: " + err.Error()}
				return
			}
			config.ApplyPreset(backend.Active())
			a.reindex()
			a.msg <- sysMsg{text: "✔ modelos internos (los servidores cargan en segundo plano)"}
		}()
	case "ollama", "openai":
		url := "http://localhost:11434/v1"
		if f[0] == "openai" {
			if len(f) < 2 {
				sys("uso: /provider openai <url>   (p. ej. http://localhost:8000/v1)")
				return
			}
			url = f[1]
		} else if len(f) > 1 {
			url = f[1]
		}
		p := &config.Provider{Backend: "openai", URL: url, EmbedModel: "nomic-embed-text"}
		if f[0] == "openai" {
			p.EmbedModel = ""
		}
		if err := config.SetProvider(p); err != nil {
			sys(err.Error())
			return
		}
		a.switching.Store(true)
		go func() {
			defer a.switching.Store(false)
			models, err := backend.ProviderModels(context.Background(), config.Get().LLMURL)
			if err != nil {
				a.msg <- sysMsg{text: fmt.Sprintf("proveedor guardado, pero no responde: %v\n/provider internal para volver", err)}
				return
			}
			a.reindex()
			a.modelPick.Store(1)
			var out []string
			for i, m := range models {
				out = append(out, fmt.Sprintf("  %d. %s", i+1, m))
			}
			a.msg <- sysMsg{text: "✔ proveedor externo " + url + "\nelige modelo (escribe el número):\n" + strings.Join(out, "\n")}
		}()
	case "embed":
		if len(f) < 2 || !cfg.External() {
			sys("uso: /provider embed <modelo>   (solo con proveedor externo)")
			return
		}
		p := &config.Provider{Backend: "openai", URL: cfg.LLMURL, EmbedURL: cfg.EmbedURL, Model: cfg.Model, EmbedModel: f[1]}
		if err := config.SetProvider(p); err != nil {
			sys(err.Error())
			return
		}
		go a.reindex()
		sys("embeddings: " + f[1] + " (se reindexa la memoria si cambió)")
	default:
		sys("uso: /provider [ollama [url] | openai <url> | embed <modelo> | internal]")
	}
}

// reindex recalcula los vectores de la memoria si cambió el modelo de embeddings.
func (a *app) reindex() {
	if n, err := a.rag.EnsureEmbeddings(context.Background()); err != nil {
		a.msg <- sysMsg{text: "reindexado de memoria: " + err.Error()}
	} else if n > 0 {
		a.msg <- sysMsg{text: fmt.Sprintf("%d recuerdos reindexados con %s", n, config.Get().EmbedModel)}
	}
}

// fetch descarga un modelo mostrando el avance cada 5 %.
func (a *app) fetch(ctx context.Context, dir string, pick backend.Entry) error {
	last := -1
	_, err := backend.Download(ctx, dir, pick, func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total / 5 * 5); pct != last {
			last = pct
			a.msg <- sysMsg{text: fmt.Sprintf("  ⇣ %s  %d%%  (%.1f/%.1f GB)", pick.File, pct, float64(done)/1e9, float64(total)/1e9)}
		}
	})
	return err
}

// embedCmd implementa /embed: elige el modelo de embeddings interno. Lo descarga si falta,
// reinicia solo el servidor de embeddings y reindexa la memoria si cambió el modelo.
func (a *app) embedCmd(arg string) {
	const dir = "models"
	cfg := config.Get()
	sys := func(t string) { a.push(lines.Item{Kind: "sys", Text: t}) }
	if cfg.External() {
		sys("con un proveedor externo usa /provider embed <modelo>  ·  /provider internal vuelve a los internos")
		return
	}
	cur := backend.CurrentEmbed()
	if arg == "" {
		var out []string
		for i, e := range backend.EmbedEntries(dir) {
			mark := "  "
			switch {
			case e.File == cur:
				mark = "▶ "
			case !e.Installed:
				mark = "⇣ "
			}
			out = append(out, fmt.Sprintf("%s%d. %s", mark, i+1, e.File))
		}
		a.modelPick.Store(2)
		sys("embeddings: " + cfg.EmbedModel + "\n" + strings.Join(out, "\n") + "\n▶ en uso · ⇣ se descarga al elegirlo · escribe el número  ·  /embed default  ·  /embed hf:usuario/repo/archivo.gguf")
		return
	}
	if a.busy != nil || !a.switching.CompareAndSwap(false, true) {
		sys("espera a que termine el turno o el cambio de modelo en curso")
		return
	}
	var pick backend.Entry
	if arg == "default" || arg == "auto" {
		pick = backend.Entry{Installed: true} // vacío: vuelve al predeterminado
	} else {
		var err error
		if pick, err = backend.PickEmbed(dir, arg); err != nil {
			a.switching.Store(false)
			sys(err.Error())
			return
		}
		if pick.File == cur {
			a.switching.Store(false)
			sys(pick.File + " ya está en uso")
			return
		}
	}
	sys("⟫ embeddings: " + map[bool]string{true: "cargando ", false: "descargando "}[pick.Installed] + pick.File + "…")
	go func() {
		defer a.switching.Store(false)
		ctx := context.Background()
		if !pick.Installed {
			if err := a.fetch(ctx, dir, pick); err != nil {
				a.msg <- sysMsg{text: "no se pudo descargar: " + err.Error()}
				return
			}
		}
		if err := config.SetEmbedGGUF(pick.File); err != nil {
			a.msg <- sysMsg{text: err.Error()}
			return
		}
		req := backend.Request{}
		if pick.File != "" {
			req.Embed = pick.File
		}
		if _, err := backend.StartInternal(ctx, req); err != nil {
			a.msg <- sysMsg{text: "no se pudo arrancar el servidor de embeddings: " + err.Error()}
			return
		}
		if !backend.WaitReady(ctx, 2*time.Minute, false) {
			a.msg <- sysMsg{text: "el servidor de embeddings no respondió (log: data/run/embed.log)"}
			return
		}
		a.msg <- sysMsg{text: "✔ embeddings: " + config.Get().EmbedModel}
		a.reindex()
	}()
}
