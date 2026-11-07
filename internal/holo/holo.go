// Package holo implementa la memoria holográfica (HRR, Plate 1995): un vector por recuerdo que
// superpone palabras y pares ordenados ligados por convolución circular.
package holo

import (
	"math"
	"strings"
	"sync"
	"unicode/utf16"

	"capri/internal/text"
)

// D es la dimensión de los vectores holográficos.
const D = 512

var stop = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("de la el los las un una unos unas que y o a al del en por con para es son mi mis tu tus su se lo le les me te no si ya como cual cuales hay") {
		m[w] = true
	}
	return m
}()

// hash FNV-1a sobre unidades UTF-16 (igual que charCodeAt en JS).
func hash(s string) uint32 {
	h := uint32(2166136261)
	for _, u := range utf16.Encode([]rune(s)) {
		h = (h ^ uint32(u)) * 16777619
	}
	return h
}

func lcg(seed uint32) func() float64 {
	return func() float64 {
		seed = seed*1664525 + 1013904223
		return float64(seed) / 4294967296
	}
}

func unit(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	if n == 0 {
		n = 1
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

var (
	tokensMu sync.Mutex
	tokens   = map[string][]float32{}
)

// Vector devuelve el vector fijo de una palabra (misma palabra → mismo vector en cualquier proceso).
func Vector(token string) []float32 {
	tokensMu.Lock()
	defer tokensMu.Unlock()
	if v, ok := tokens[token]; ok {
		return v
	}
	r := lcg(hash(token))
	v := make([]float32, D)
	for i := range v {
		v[i] = float32(r()*2 - 1)
	}
	v = unit(v)
	tokens[token] = v
	return v
}

var perm = func() []int {
	r := lcg(0x5eed)
	p := make([]int, D)
	for i := range p {
		p[i] = i
	}
	for i := D - 1; i > 0; i-- {
		j := int(math.Floor(r() * float64(i+1)))
		p[i], p[j] = p[j], p[i]
	}
	return p
}()

func permute(a []float32) []float32 {
	out := make([]float32, D)
	for i := 0; i < D; i++ {
		out[perm[i]] = a[i]
	}
	return out
}

// Bind liga dos vectores en uno casi ortogonal a ambos (convolución circular).
func Bind(a, b []float32) []float32 {
	c := make([]float32, D)
	for i := 0; i < D; i++ {
		ai := a[i]
		if ai == 0 {
			continue
		}
		for j := 0; j < D; j++ {
			c[(i+j)%D] += ai * b[j]
		}
	}
	return c
}

// Encode convierte un texto en su vector holográfico normalizado.
func Encode(s string) []float32 {
	ws := make([]string, 0)
	for _, w := range text.Words(s) {
		if !stop[w] {
			ws = append(ws, w)
		}
	}
	out := make([]float32, D)
	for i, w := range ws {
		u := Vector(w)
		for k := 0; k < D; k++ {
			out[k] += u[k]
		}
		if i+1 < len(ws) {
			big := Bind(u, permute(Vector(ws[i+1])))
			for k := 0; k < D; k++ {
				out[k] += big[k]
			}
		}
	}
	return unit(out)
}

// Sim es la similitud coseno de dos vectores ya normalizados.
func Sim(a, b []float32) float64 {
	var s float64
	for i := 0; i < D && i < len(a) && i < len(b); i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
