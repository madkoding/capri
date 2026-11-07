// Package learn detecta datos personales duraderos y separa un mensaje en hechos y resto.
package learn

import (
	"regexp"
	"strings"
)

var firstPerson = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bme llamo\b`),
	regexp.MustCompile(`(?i)\bmi nombre es\b`),
	regexp.MustCompile(`(?i)\bsoy \p{L}+`),
	regexp.MustCompile(`(?i)\bvivo (en|cerca)\b`),
	regexp.MustCompile(`(?i)\bnac[ií] (en|el)\b`),
	regexp.MustCompile(`(?i)\btrabajo (en|como|para|de)\b`),
	regexp.MustCompile(`(?i)\bestudio\b`),
	regexp.MustCompile(`(?i)\btengo \d+ a[nñ]os\b`),
	regexp.MustCompile(`(?i)\btengo (un|una|dos|tres|\d+)\b`),
	regexp.MustCompile(`(?i)\bmi (?:\p{L}+ ){0,2}(?:es|son|se llama|se llaman|era|favorit[oa]s?)\b`),
	regexp.MustCompile(`(?i)\bmis (?:\p{L}+ ){0,2}(?:son|se llaman)\b`),
	regexp.MustCompile(`(?i)\bme (gusta|gustan|encanta|encantan|fascina|molesta)\b`),
	regexp.MustCompile(`(?i)\bno me (gusta|gustan)\b`),
	regexp.MustCompile(`(?i)\b(odio|amo|prefiero|adoro)\b`),
	regexp.MustCompile(`(?i)\b(uso|programo en|juego a|hablo|toco)\b`),
	regexp.MustCompile(`(?i)\bmi cumplea[nñ]os\b`),
	regexp.MustCompile(`(?i)\b(soy alérgic|soy vegetarian|soy vegan)`),
	regexp.MustCompile(`(?i)\bmi(?:s)? (?:\p{L}+ ){1,3}(?:vive|viven|trabaja|trabajan|estudia|tiene|tienen|naci[oó]|cumple|usa|juega|se llama|se llaman)\b`),
	regexp.MustCompile(`(?i)\b(?:estoy|estamos) (?:construyendo|desarrollando|aprendiendo|escribiendo|armando|montando)\b`),
	regexp.MustCompile(`(?i)\bmi servidor\b`),
	regexp.MustCompile(`(?i)\b(?:called|named)\b.*\b(?:my|is)\b|\bmy \w+ (?:is|are)\b`),
	regexp.MustCompile(`(?i)\bi (am|live|work|like|love|hate|prefer|have)\b`),
	regexp.MustCompile(`(?i)\bmy (name|dog|cat|favorite)\b`),
}

var (
	questionRe = regexp.MustCompile(`^\s*¿|\?\s*$`)
	commandRe  = regexp.MustCompile(`(?i)^\s*(crea|escribe|borra|elimina|lista|muestra|lee|ejecuta|corre|abre|dame|dime|cuéntame|explica|traduce|resume|genera|haz|busca|calcula|qué|quién|cómo|cuándo|dónde|cuál|por qué)\b`)
	sentenceRe = regexp.MustCompile(`[^.!?\n;]+[.!?;]*`)
	greetingRe = regexp.MustCompile(`(?i)^\s*(?:hola|buenas|buenos d[ií]as|buenas tardes|hey|ey|oye|ok|vale)\b[,!.\s]*`)
)

// LooksLikeFact indica si un texto es un dato personal duradero.
func LooksLikeFact(txt string) bool {
	t := strings.TrimSpace(txt)
	n := len([]rune(t))
	if n < 8 || n > 400 {
		return false
	}
	if questionRe.MatchString(t) || commandRe.MatchString(t) {
		return false
	}
	for _, r := range firstPerson {
		if r.MatchString(t) {
			return true
		}
	}
	return false
}

// SplitFacts separa las frases-dato (se guardan) del resto (se responde).
func SplitFacts(txt string) (facts []string, rest string) {
	var restParts []string
	for _, chunk := range splitBeforeQuestion(txt) {
		for _, raw := range sentenceRe.FindAllString(chunk, -1) {
			sent := strings.TrimSpace(raw)
			if sent == "" {
				continue
			}
			clean := greetingRe.ReplaceAllString(sent, "")
			if LooksLikeFact(clean) {
				facts = append(facts, sent)
			} else {
				restParts = append(restParts, sent)
			}
		}
	}
	for i, f := range facts {
		f = greetingRe.ReplaceAllString(f, "")
		f = strings.TrimRight(f, ".;, \t\r\n")
		facts[i] = strings.TrimSpace(f)
	}
	remainder := strings.TrimSpace(greetingRe.ReplaceAllString(strings.Join(restParts, " "), ""))
	if len([]rune(remainder)) >= 6 {
		rest = remainder
	}
	return facts, rest
}

// splitBeforeQuestion parte el texto antes de cada «¿» (equivalente al lookahead de JS).
func splitBeforeQuestion(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if r == '¿' && cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
