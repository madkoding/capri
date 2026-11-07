// Package calc es una calculadora determinista para expresiones aritméticas en español.
package calc

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	pctRe     = regexp.MustCompile(`(?:^|\s)(\d+(?:[.,]\d+)?)\s*%\s*(?:de|of)\s*(\d+(?:[.,]\d+)?)\s*$`)
	noiseRe   = regexp.MustCompile(`(?i)\b(?:cu[aá]nto|cu[aá]l|es|son|da|resultado|de|calcula|calcular|el|la|resuelve|dime|what is|how much is|equals?)\b`)
	commaRe   = regexp.MustCompile(`(\d),(\d)`)
	validRe   = regexp.MustCompile(`^[\d.+\-*/()]+$`)
	hasOpRe   = regexp.MustCompile(`\d[+\-*/]+\(?-?\d`)
	spaceRe   = regexp.MustCompile(`\s+`)
	wordRules = []struct {
		re *regexp.Regexp
		op string
	}{
		{regexp.MustCompile(`(?i)\bmás\b|\bmas\b|\bplus\b`), "+"},
		{regexp.MustCompile(`(?i)\bmenos\b|\bminus\b`), "-"},
		{regexp.MustCompile(`(?i)\bpor\b|\bmultiplicado por\b|\bveces\b|\btimes\b|[x×]`), "*"},
		{regexp.MustCompile(`(?i)\bentre\b|\bdividido (?:por|entre)\b|\bdivided by\b|÷`), "/"},
		{regexp.MustCompile(`(?i)\bal cuadrado\b`), "**2"},
		{regexp.MustCompile(`(?i)\belevado a\b|\bpotencia\b`), "**"},
	}
)

// Calc evalúa una cuenta y devuelve el resultado, o nil si el texto no es una cuenta.
func Calc(txt string) *string {
	t := strings.ToLower(txt)
	t = strings.NewReplacer("¿", " ", "?", " ", "¡", " ", "!", " ").Replace(t)
	if m := pctRe.FindStringSubmatch(t); m != nil {
		a, _ := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		b, _ := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", "."), 64)
		s := fmtNum(a * b / 100)
		return &s
	}
	t = noiseRe.ReplaceAllString(t, " ")
	for _, r := range wordRules {
		t = r.re.ReplaceAllString(t, " "+r.op+" ")
	}
	t = commaRe.ReplaceAllString(t, "$1.$2")
	t = spaceRe.ReplaceAllString(t, "")
	if !validRe.MatchString(t) || !hasOpRe.MatchString(t) || len(t) > 60 {
		return nil
	}
	v, ok := eval(t)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	s := fmtNum(v)
	return &s
}

func fmtNum(v float64) string {
	v = math.Round(v*1e9) / 1e9
	return strconv.FormatFloat(v, 'f', -1, 64)
}

type parser struct {
	s   string
	pos int
}

func eval(s string) (float64, bool) {
	p := &parser{s: s}
	v, ok := p.expr()
	if !ok || p.pos != len(p.s) {
		return 0, false
	}
	return v, true
}

func (p *parser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *parser) expr() (float64, bool) {
	v, ok := p.term()
	if !ok {
		return 0, false
	}
	for {
		switch p.peek() {
		case '+', '-':
			op := p.peek()
			p.pos++
			r, ok := p.term()
			if !ok {
				return 0, false
			}
			if op == '+' {
				v += r
			} else {
				v -= r
			}
		default:
			return v, true
		}
	}
}

func (p *parser) term() (float64, bool) {
	v, ok := p.power()
	if !ok {
		return 0, false
	}
	for {
		switch p.peek() {
		case '*':
			// ** ya lo maneja power; aquí solo queda la multiplicación simple
			if p.pos+1 < len(p.s) && p.s[p.pos+1] == '*' {
				return v, true
			}
			p.pos++
			r, ok := p.power()
			if !ok {
				return 0, false
			}
			v *= r
		case '/':
			p.pos++
			r, ok := p.power()
			if !ok {
				return 0, false
			}
			v /= r
		default:
			return v, true
		}
	}
}

func (p *parser) power() (float64, bool) {
	v, ok := p.unary()
	if !ok {
		return 0, false
	}
	if p.peek() == '*' && p.pos+1 < len(p.s) && p.s[p.pos+1] == '*' {
		p.pos += 2
		r, ok := p.power() // asociativa por la derecha
		if !ok {
			return 0, false
		}
		return math.Pow(v, r), true
	}
	return v, true
}

func (p *parser) unary() (float64, bool) {
	if p.peek() == '+' {
		p.pos++
		return p.unary()
	}
	if p.peek() == '-' {
		p.pos++
		v, ok := p.unary()
		return -v, ok
	}
	return p.atom()
}

func (p *parser) atom() (float64, bool) {
	if p.peek() == '(' {
		p.pos++
		v, ok := p.expr()
		if !ok || p.peek() != ')' {
			return 0, false
		}
		p.pos++
		return v, true
	}
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, false
	}
	v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
