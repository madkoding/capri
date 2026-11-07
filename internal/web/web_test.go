package web_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"capri/internal/embed"
	"capri/internal/rag"
	"capri/internal/skills"
	"capri/internal/web"
)

func TestRealURL(t *testing.T) {
	if got := web.RealURL("//duckduckgo.com/l/?uddg=https%3A%2F%2Fes.wikipedia.org%2Fwiki%2FQUIC&amp;rut=abc"); got != "https://es.wikipedia.org/wiki/QUIC" {
		t.Errorf("RealURL = %q", got)
	}
	if got := web.RealURL("https://example.com/a"); got != "https://example.com/a" {
		t.Errorf("RealURL = %q", got)
	}
}

func TestParseDuckResults(t *testing.T) {
	html := `
<div class="result results_links results_links_deep web-result ">
  <h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fquic&amp;rut=1">QUIC &amp; HTTP/3</a></h2>
  <a class="result__snippet" href="x">QUIC es un <b>protocolo</b> de transporte.</a>
</div>
<div class="result results_links">
  <a class="result__a" href="https://example.net/otro">Otro</a>
  <div class="result__snippet">Segundo fragmento.</div>
</div>`
	r := web.ParseDuckResults(html)
	if len(r) != 2 {
		t.Fatalf("resultados = %#v", r)
	}
	if r[0].Title != "QUIC & HTTP/3" || r[0].URL != "https://example.org/quic" || r[0].Snippet != "QUIC es un protocolo de transporte." {
		t.Errorf("r[0] = %+v", r[0])
	}
	if r[1].URL != "https://example.net/otro" || r[1].Snippet != "Segundo fragmento." {
		t.Errorf("r[1] = %+v", r[1])
	}
}

func TestHTMLToText(t *testing.T) {
	got := web.HTMLToText("<html><script>var x=1</script><style>p{}</style><h1>Título</h1><p>Uno &amp; dos</p><p>Tres</p></html>")
	if got != "Título\nUno & dos\nTres" {
		t.Errorf("HTMLToText = %q", got)
	}
}

func TestWebSkillLoaded(t *testing.T) {
	var found *skills.Skill
	for _, s := range skills.LoadSkills(skills.SkillsDir) {
		if s.Name == "buscar-en-internet" {
			s := s
			found = &s
			break
		}
	}
	if found == nil {
		t.Fatal("la skill buscar-en-internet no existe")
	}
	ok := false
	for _, turn := range found.Turns {
		if turn.Role == "assistant" && strings.HasPrefix(turn.Content, "web_search: ") {
			ok = true
		}
	}
	if !ok {
		t.Errorf("la skill no trae ejemplos web_search: %#v", found.Turns)
	}
	if s := skills.ParseSkill("---\nname: x\ndescription: y\n---\n"); s == nil || len(s.Turns) != 0 {
		t.Errorf("ParseSkill vacío = %+v", s)
	}
}

func TestAddWholeAndHasSource(t *testing.T) {
	if !embed.Health(context.Background()) {
		t.Skip("servicio embed no disponible")
	}
	r, err := rag.Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	long := strings.Repeat("palabra ", 600) + "\nFuente: https://example.org/a"
	ids, err := r.Add(context.Background(), rag.AddOptions{Title: "Página", Content: long, Kind: "web", Whole: true})
	if err != nil || len(ids) != 1 {
		t.Fatalf("Add = %#v, %v", ids, err)
	}
	if row := r.Get(ids[0]); row == nil || row.Content != strings.TrimSpace(long) {
		t.Errorf("fila = %+v", row)
	}
	if !r.HasSource("https://example.org/a") {
		t.Error("HasSource no encontró la fuente")
	}
	if r.HasSource("https://example.org/otra") {
		t.Error("HasSource encontró una fuente inexistente")
	}
}

func TestUnknownTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"uso Hyprland con Waybar en la P4", []string{"Hyprland", "Waybar", "P4"}},
		{"Hola, ¿qué tal?", nil},
		{"Tengo una GPT4 y un iPhone", []string{"GPT4", "iPhone"}},
	}
	for _, c := range cases {
		got := web.UnknownTerms(c.in)
		if !same(got, c.want) {
			t.Errorf("UnknownTerms(%q) = %#v, quería %#v", c.in, got, c.want)
		}
	}
}

func TestLookupQuery(t *testing.T) {
	eq := func(txt string, o web.LookupOptions, want string) {
		t.Helper()
		got := web.LookupQuery(txt, o)
		if want == "" {
			if got != nil {
				t.Errorf("LookupQuery(%q) = %q, quería nil", txt, *got)
			}
			return
		}
		if got == nil || *got != want {
			t.Errorf("LookupQuery(%q) = %v, quería %q", txt, got, want)
		}
	}
	eq("¿qué es el kintsugi en cerámica?", web.LookupOptions{}, "¿qué es el kintsugi en cerámica?")
	eq("uso Hyprland con Waybar en mi equipo", web.LookupOptions{}, "Hyprland Waybar")
	eq("¿cómo estás hoy amigo?", web.LookupOptions{}, "")
	eq("¿qué es el kintsugi en cerámica?", web.LookupOptions{HasMemory: true}, "")
	eq("busca en tu memoria lo de Hyprland", web.LookupOptions{}, "")
	eq("el día está bonito y tranquilo", web.LookupOptions{}, "")
	eq("Estoy montando Proxmox y TrueNAS", web.LookupOptions{HasMemory: true, Known: map[string]bool{"Proxmox": true}}, "TrueNAS")
	eq("Estoy montando Proxmox y TrueNAS", web.LookupOptions{Known: map[string]bool{"Proxmox": true, "TrueNAS": true}}, "")
	eq("Estoy montando Proxmox y TrueNAS", web.LookupOptions{HasMemory: true}, "Proxmox TrueNAS")
}

func TestIsKnowledgeQuestion(t *testing.T) {
	cases := map[string]bool{
		"Oye, ¿sabes qué es Hyprland y cómo se configura?": true,
		"¿Y qué tal va el kintsugi de cerámica en Japón?":  true,
		"¿qué hay en la carpeta src?":                      false,
		"¿cuál es el tamaño de config.json?":               false,
	}
	for in, want := range cases {
		if got := web.IsKnowledgeQuestion(in); got != want {
			t.Errorf("IsKnowledgeQuestion(%q) = %v, quería %v", in, got, want)
		}
	}
}

func TestHTMLToTextSkipsMenus(t *testing.T) {
	page := `<html><body><header>Skip to content</header>
<div>You signed in with another tab or window.</div>
<div>Files</div><div>Go to file</div><div>Blame</div><div>Latest commit</div>
<main><h1>Guía de QUIC</h1><p>QUIC corre sobre UDP y cifra el transporte con TLS 1.3 de forma integrada.</p>
<p>Blame</p><p>Copy path</p></main>
<footer>© 2026 Organization</footer></body></html>`
	want := "Guía de QUIC\nQUIC corre sobre UDP y cifra el transporte con TLS 1.3 de forma integrada."
	if got := web.HTMLToText(page); got != want {
		t.Errorf("HTMLToText = %q", got)
	}
}

func TestCleanLines(t *testing.T) {
	got := web.CleanLines([]string{"Read", "File", "Files Expand file tree", "Lee el manual del archivo de configuración con calma."})
	want := []string{"Lee el manual del archivo de configuración con calma."}
	if !same(got, want) {
		t.Errorf("CleanLines = %#v", got)
	}
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
