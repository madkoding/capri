// Package web busca en internet, lee páginas y guarda lo encontrado en el cerebro.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"capri/internal/config"
	"capri/internal/rag"
	"capri/internal/text"
)

const (
	timeoutMs = 15000
	pageChars = 4000
	userAgent = "Mozilla/5.0 (X11; Linux x86_64) Capri/0.1"
)

func decode(s string) string {
	s = html.UnescapeString(s)
	numRe := regexp.MustCompile(`&#(\d+);`)
	s = numRe.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(m[2 : len(m)-1])
		return string(rune(n))
	})
	return s
}

func stripTags(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	s = decode(s)
	return text.Collapse(s)
}

// RealURL desenvuelve el redirect de DuckDuckGo.
func RealURL(href string) string {
	h := decode(href)
	m := regexp.MustCompile(`[?&]uddg=([^&]+)`).FindStringSubmatch(h)
	u := h
	if m != nil {
		if d, err := url.QueryUnescape(m[1]); err == nil {
			u = d
		} else {
			u = m[1]
		}
	}
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}

// Result es un resultado de búsqueda.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

var (
	anchorRe  = regexp.MustCompile(`(?s)<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRe = regexp.MustCompile(`(?s)class="result__snippet"[^>]*>(.*?)</a>|class="result__snippet"[^>]*>(.*?)</(?:div|td|span)>`)
	resultSep = regexp.MustCompile(`class="result\s`)
)

// ParseDuckResults convierte el HTML de DuckDuckGo en resultados.
func ParseDuckResults(page string) []Result {
	var out []Result
	loc := resultSep.FindAllStringIndex(page, -1)
	for i, l := range loc {
		start := l[1]
		end := len(page)
		if i+1 < len(loc) {
			end = loc[i+1][0]
		}
		b := page[start:end]
		a := anchorRe.FindStringSubmatch(b)
		if a == nil {
			continue
		}
		snip := ""
		if sn := snippetRe.FindStringSubmatch(b); sn != nil {
			if sn[1] != "" {
				snip = sn[1]
			} else {
				snip = sn[2]
			}
		}
		out = append(out, Result{Title: stripTags(a[2]), URL: RealURL(a[1]), Snippet: stripTags(snip)})
	}
	var filtered []Result
	for _, r := range out {
		if regexp.MustCompile(`^https?://`).MatchString(r.URL) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

var (
	uiTextRe  = regexp.MustCompile(`(?i)\b(skip to content|sign (in|out|up)|reload to refresh|dismiss alert|switched accounts|signed (in|out)|can.t perform that action|cookies?|accept( all)?|log ?in|menu|subscribe|copy (path|link)|go to file|latest commit|history|blame|raw|download|watchers?|forks?|stars?|releases|packages|contributors|languages|report repository|open more actions|file metadata and controls|edit and raw actions|files?|main|expand file tree|more file actions|share|comments?|reply|next|previous|back to top)\b`)
	sessionRe = regexp.MustCompile(`(?i)(you (signed|switched) (in|out|accounts)|reload to refresh your session|dismiss alert)`)
	uiExactRe = regexp.MustCompile(`(?i)^(read|file|files|view|edit|raw|history|blame|more|open|copy|expand|collapse)$`)
	locLineRe = regexp.MustCompile(`(?i)^\d+ lines? \(\d+ loc\)`)
)

// CleanLines quita líneas de interfaz: botones, menús y navegación repetida.
func CleanLines(lines []string) []string {
	count := map[string]int{}
	for _, l := range lines {
		count[l]++
	}
	var out []string
	for _, l := range lines {
		if uiExactRe.MatchString(l) || sessionRe.MatchString(l) {
			continue
		}
		words := len(strings.Split(l, " "))
		if words <= 8 && (uiTextRe.MatchString(l) || locLineRe.MatchString(l)) {
			continue
		}
		if words <= 6 && count[l] > 1 {
			continue
		}
		out = append(out, l)
	}
	return out
}

func mainBlock(page string) string {
	for _, tag := range []string{"main", "article"} {
		open := regexp.MustCompile(`(?is)<` + tag + `\b[^>]*>`).FindStringIndex(page)
		if open == nil {
			continue
		}
		rest := page[open[1]:]
		closeIdx := regexp.MustCompile(`(?is)</` + tag + `>`).FindStringIndex(rest)
		if closeIdx != nil {
			return rest[:closeIdx[0]]
		}
		return rest
	}
	return page
}

// HTMLToText convierte una página en texto legible.
func HTMLToText(page string) string {
	body := mainBlock(page)
	for _, tag := range []string{"script", "style", "noscript", "svg", "nav", "footer", "header", "form", "button", "select", "input"} {
		re := regexp.MustCompile(`(?is)<` + tag + `\b[\s\S]*?</` + tag + `>`)
		body = re.ReplaceAllString(body, " ")
	}
	body = regexp.MustCompile(`(?s)<!--[\s\S]*?-->`).ReplaceAllString(body, " ")
	body = regexp.MustCompile(`(?is)</(p|div|h[1-6]|li|tr|br|section|article)>|<br\s*/?>`).ReplaceAllString(body, "\n")
	body = decode(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(body, " "))
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		l = text.Collapse(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(CleanLines(lines), "\n")
}

func get(ctx context.Context, u, accept string) (string, string, error) {
	cfg := config.Get()
	_ = cfg
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("user-agent", userAgent)
	req.Header.Set("accept", accept)
	client := &http.Client{Timeout: timeoutMs * time.Millisecond}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", "", err
	}
	return string(b), res.Header.Get("content-type"), nil
}

// SearchWeb busca con SearXNG (CAPRI_SEARCH_URL) o, por defecto, con DuckDuckGo.
func SearchWeb(ctx context.Context, query string, k int) ([]Result, error) {
	if k == 0 {
		k = 3
	}
	cfg := config.Get()
	if cfg.SearchURL != "" {
		body, _, err := get(ctx, cfg.SearchURL+"?q="+url.QueryEscape(query)+"&format=json", "application/json")
		if err != nil {
			return nil, err
		}
		var data struct {
			Results []struct {
				Title   string `json:"title"`
				URL     string `json:"url"`
				Content string `json:"content"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			return nil, err
		}
		out := make([]Result, 0, len(data.Results))
		for _, r := range data.Results {
			out = append(out, Result{Title: r.Title, URL: r.URL, Snippet: r.Content})
		}
		if len(out) > k {
			out = out[:k]
		}
		return out, nil
	}
	body, _, err := get(ctx, "https://html.duckduckgo.com/html/?q="+url.QueryEscape(query), "text/html")
	if err != nil {
		return nil, err
	}
	out := ParseDuckResults(body)
	if len(out) > k {
		out = out[:k]
	}
	return out, nil
}

// FetchPage lee una página y devuelve su texto (vacío si no se pudo).
func FetchPage(ctx context.Context, u string) string {
	body, ctype, err := get(ctx, u, "text/html,text/plain")
	if err != nil {
		return ""
	}
	if !regexp.MustCompile(`text/(html|plain)`).MatchString(ctype) {
		return ""
	}
	if regexp.MustCompile(`(?i)<html|<body|<p[\s>]`).MatchString(body) {
		return text.Trim(HTMLToText(body), pageChars)
	}
	return text.Trim(strings.TrimSpace(body), pageChars)
}

var notTerms = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("Hola Yo Me Mi Mis Tu Tus Te El La Los Las Un Una Es Y O Pero Si No Que Qué Como Cómo Cuál Cuáles Cuándo Dónde Por Para Se Sé Eres Estás Gracias Oye Mira Entonces Ahora Hoy Ayer") {
		m[w] = true
	}
	return m
}()

var (
	memoryOrderRe = regexp.MustCompile(`(?i)\b(memoria|recuerd\w*|olvida\w*|an[oó]ta\w*|apunta\w*|cerebro)\b`)
	chitchatRe    = regexp.MustCompile(`(?i)\b(c[oó]mo est[aá]s|qu[eé] tal (est[aá]s|todo|tu d[ií]a)|qu[eé] haces|c[oó]mo te (va|llamas)|qui[eé]n eres|qu[eé] eres|c[oó]mo (te )?sientes|qu[eé] me recomiendas)\b`)
	fileCueRe     = regexp.MustCompile(`(?i)\b(archivo|carpeta|directorio|comando|terminal|shell|ejecut\w*|instal\w*|borra\w*|crea\w*|edita\w*)\b|\.\w{2,5}\b`)
	mixedRe       = regexp.MustCompile(`^[\p{L}\d.+#-]*(\p{Ll}\p{Lu}|\d\p{L}|\p{L}\d)[\p{L}\d.+#-]*$`)
	capitalRe     = regexp.MustCompile(`^\p{Lu}[\p{L}\d.+#-]*$`)
	termTrimHead  = regexp.MustCompile(`^[¿¡"«(]+`)
	termTrimTail  = regexp.MustCompile(`[?!.,;:"»)]+$`)
	asksRe        = regexp2.MustCompile(`(?i)^\W*(?:(?:oye|y|bueno|pues|mira|ok)\W+)*(?:(?:sabes|conoces|sabr[ií]as)\W+)?(?:qu[eé]|c[oó]mo|cu[aá]l(?:es)?|cu[aá]ndo|d[oó]nde|qui[eé]n|por qu[eé]|para qu[eé]|explica|define|resume|dime|háblame de|cu[eé]ntame de)(?!\p{L})`, 0)
)

// IsKnowledgeQuestion indica si es una pregunta de conocimiento sin archivos ni comandos.
func IsKnowledgeQuestion(txt string) bool {
	ok, _ := asksRe.MatchString(txt)
	return ok && !fileCueRe.MatchString(txt)
}

// UnknownTerms devuelve nombres propios o técnicos que parecen desconocidos.
func UnknownTerms(txt string) []string {
	var out []string
	for i, w := range strings.Fields(txt) {
		w = termTrimHead.ReplaceAllString(w, "")
		w = termTrimTail.ReplaceAllString(w, "")
		if len([]rune(w)) < 2 || notTerms[w] {
			continue
		}
		isName := i > 0 && capitalRe.MatchString(w)
		if isName || mixedRe.MatchString(w) {
			dup := false
			for _, x := range out {
				if x == w {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, w)
			}
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// LookupOptions controla si hace falta buscar en internet.
type LookupOptions struct {
	HasMemory  bool
	MemoryTalk bool
	Trivial    bool
	Known      map[string]bool
}

// LookupQuery devuelve la consulta a buscar o nil.
func LookupQuery(txt string, o LookupOptions) *string {
	t := strings.TrimSpace(txt)
	if o.MemoryTalk || memoryOrderRe.MatchString(t) || o.Trivial || len(strings.Fields(t)) < 3 || chitchatRe.MatchString(t) {
		return nil
	}
	question := strings.HasPrefix(strings.TrimSpace(t), "¿") || regexp.MustCompile(`\?\s*$`).MatchString(t)
	if question && o.HasMemory {
		return nil
	}
	var terms []string
	for _, x := range UnknownTerms(t) {
		if !o.Known[x] {
			terms = append(terms, x)
		}
	}
	ok, _ := asksRe.MatchString(t)
	if question && (ok || len(terms) > 0) {
		return &t
	}
	if len(terms) > 0 {
		s := strings.Join(terms, " ")
		return &s
	}
	return nil
}

// KnownTerms devuelve los términos del mensaje que la memoria ya conoce.
func KnownTerms(ctx context.Context, r *rag.Rag, txt string) map[string]bool {
	known := map[string]bool{}
	for _, term := range UnknownTerms(txt) {
		hits := r.Search(ctx, term, 3, 0.3)
		lt := strings.ToLower(term)
		for _, h := range hits {
			if strings.Contains(strings.ToLower(h.Title+" "+h.Content), lt) {
				known[term] = true
				break
			}
		}
	}
	return known
}

// Found es una página encontrada y guardada.
type Found struct {
	Title   string
	URL     string
	Snippet string
	IDs     []int64
	Known   bool
}

// Research busca, lee las páginas y guarda cada una en el cerebro.
func Research(ctx context.Context, query string, r *rag.Rag, k int) ([]Found, error) {
	results, err := SearchWeb(ctx, query, k)
	if err != nil {
		return nil, err
	}
	var found []Found
	for _, res := range results {
		u, err := url.Parse(res.URL)
		if err != nil {
			continue
		}
		host := u.Host
		known := r.HasSource(res.URL)
		body := ""
		if !known {
			body = FetchPage(ctx, res.URL)
			if body == "" {
				body = res.Snippet
			}
		}
		if !known && body == "" {
			continue
		}
		var ids []int64
		if !known {
			title := res.Title
			if title == "" {
				title = host
			}
			ids, _ = r.Add(ctx, rag.AddOptions{
				Title:   text.Trim(title, 60),
				Content: body + "\nFuente: " + res.URL,
				Tags:    "web," + host,
				Kind:    "web",
				Whole:   true,
			})
		}
		title := res.Title
		if title == "" {
			title = host
		}
		snip := res.Snippet
		if snip == "" {
			snip = body
		}
		found = append(found, Found{Title: title, URL: res.URL, Snippet: text.Trim(snip, 300), IDs: ids, Known: known})
	}
	return found, nil
}

var (
	searchCueRe = regexp.MustCompile(`(?i)\b(b[uú]sc(a|ar|arlo|arla|arlos|arlas|alo|ala|alos|alas|ame|arme)|investig(a|ar|alo|ala|arlo|arla)|googlea(r|lo|la)?|consult(a|ar|alo|ala|arlo|arla))\b.{0,40}\b(web|internet|online|en línea|google)\b`)
	// seguimientos: «busca mejor», «búscalo de verdad», «¿y la búsqueda?», «hazla»
	searchFollowRe = regexp.MustCompile(`(?i)^\W*(?:(?:y|pero|oye|bueno)\W+)*(?:b[uú]sca\w*\b.{0,20}\b(mejor|bien|de nuevo|otra vez|de verdad|ahora)|(?:la |esa |tu )?b[uú]squeda\W*$|haz(?:la)? la b[uú]squeda)`)
	searchLeadRe   = regexp.MustCompile(`(?i)^\s*(por favor,?\s*)?(b[uú]sc(a|ar|arlo|arla|arlos|arlas|alo|ala|alos|alas|ame|arme)|investig(a|ar|alo|ala|arlo|arla)|googlea(r|lo|la)?|consult(a|ar|alo|ala|arlo|arla))\s+(en\s+)?(la\s+)?(web|internet|google|línea|online)?\s*(mejor|bien|de nuevo|otra vez|de verdad|ahora)?\s*(sobre|acerca de|de)?\s*`)
	// mensajes sobre la propia búsqueda o la respuesta anterior: no son un tema que buscar
	metaRe = regexp.MustCompile(`(?i)\b(inventa\w*|buscaste|hiciste|b[uú]squeda|internet|respuesta|respondiste|mentira|de verdad|no hiciste|seguro)\b`)
)

// AsksToSearch indica si el usuario pide expresamente buscar en internet.
func AsksToSearch(txt string) bool {
	return searchCueRe.MatchString(txt) || searchFollowRe.MatchString(txt)
}

// StripSearchCue quita la orden ("busca en internet sobre…") y deja el tema.
func StripSearchCue(txt string) string {
	t := strings.TrimSpace(txt)
	if searchFollowRe.MatchString(t) && !searchCueRe.MatchString(t) {
		return "" // «y la búsqueda?» no trae tema
	}
	t = searchLeadRe.ReplaceAllString(t, "")
	return strings.TrimSpace(strings.Trim(t, "?¿!¡. "))
}

var (
	pretendLineRe = regexp.MustCompile(`(?im)^[ \t]*(?:\[[^\]\n]*\]|(?:ahora )?voy a buscar\w*[^\n]*|d[eé]jame (?:buscar|hacerlo|ver qu[eé] (?:sale|encuentro))[^\n]*|buscando (?:ahora|en internet)[^\n]*|realizo la b[uú]squeda[^\n]*|(?:ahora,? )?con la herramienta web_search:?[^\n]*)[ \t]*(?:\n|$)`)
	blankRunRe    = regexp.MustCompile(`\n{3,}`)
)

// StripPretend quita las líneas en que el modelo finge ejecutar la búsqueda
// («Voy a buscar…», «[Realizo la búsqueda]»): la búsqueda ya se hizo antes de redactar.
func StripPretend(answer string) string {
	out := pretendLineRe.ReplaceAllString(answer, "")
	out = blankRunRe.ReplaceAllString(out, "\n\n")
	return strings.TrimSpace(out)
}

// IsMeta indica si el mensaje habla de la búsqueda o de la respuesta anterior
// («lo estás inventando?», «y la búsqueda?») en vez de aportar un tema.
func IsMeta(txt string) bool { return metaRe.MatchString(txt) || AsksToSearch(txt) }

// Topic elige qué buscar cuando la orden no trae tema («busca en internet mejor»):
// la pregunta más reciente que no sea sobre la propia búsqueda. "" si no hay.
func Topic(previousUsers []string) string {
	for i := len(previousUsers) - 1; i >= 0; i-- {
		t := strings.TrimSpace(previousUsers[i])
		if len(strings.Fields(t)) >= 3 && !IsMeta(t) {
			return t
		}
	}
	return ""
}
