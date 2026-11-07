// Package text contiene utilidades de texto compartidas.
package text

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Trim recorta a n caracteres y añade «…» si se pasa.
func Trim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Block envuelve un texto en un bloque de código Markdown.
func Block(s string) string { return "```\n" + s + "\n```" }

var wordRe = regexp.MustCompile(`[a-z0-9]{2,}`)

// Words devuelve palabras en minúscula y sin tildes (2+ letras o dígitos).
func Words(s string) []string {
	t := norm.NFD.String(strings.ToLower(s))
	var b strings.Builder
	b.Grow(len(t))
	for _, r := range t {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return wordRe.FindAllString(b.String(), -1)
}

// Collapse reduce cualquier secuencia de espacios a uno y recorta.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// RuneLen cuenta caracteres (no bytes).
func RuneLen(s string) int { return len([]rune(s)) }
