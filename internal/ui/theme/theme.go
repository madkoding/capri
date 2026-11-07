// Package theme contiene la paleta neón y utilidades de color/animación (funciones puras del tiempo).
package theme

import (
	"fmt"
	"math"
	"strconv"
	"sync"
)

// C es la paleta de colores.
var C = map[string]string{
	"primary":   "#39ff14",
	"secondary": "#ff6ec7",
	"violet":    "#b967ff",
	"yellow":    "#f9f002",
	"green":     "#39ff14",
	"red":       "#ff3131",
	"orange":    "#ff9f1c",
	"blue":      "#2b6cff",
	"ink":       "#d6dbe8",
	"mute":      "#8d8fa8",
	"dim":       "#4a4e69",
	"dark":      "#0b0b10",
}

// NEON son los colores del degradado principal.
var NEON = []string{C["primary"], C["violet"], C["secondary"]}

// LEVELS son los glifos del ecualizador.
var LEVELS = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// GLITCH son los caracteres de corrupción.
const GLITCH = "▓▒░█▀▄#@%&$/\\|<>=+*?"

var (
	hexMu    sync.Mutex
	hexCache = map[string][3]int{}
)

func hex(h string) [3]int {
	hexMu.Lock()
	defer hexMu.Unlock()
	if v, ok := hexCache[h]; ok {
		return v
	}
	var v [3]int
	for i := 0; i < 3; i++ {
		n, _ := strconv.ParseInt(h[1+i*2:3+i*2], 16, 32)
		v[i] = int(n)
	}
	hexCache[h] = v
	return v
}

func toHex(c [3]int) string {
	var out [3]int
	for i, x := range c {
		if x < 0 {
			x = 0
		}
		if x > 255 {
			x = 255
		}
		out[i] = int(math.Round(float64(x)))
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

// Mix mezcla dos colores (k=0 → a, k=1 → b).
func Mix(a, b string, k float64) string {
	A, B := hex(a), hex(b)
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(float64(A[i]) + (float64(B[i])-float64(A[i]))*k))
	}
	return toHex(out)
}

// GradientAt devuelve el color en x∈[0,1] a lo largo de varias paradas.
func GradientAt(stops []string, x float64) string {
	if len(stops) == 0 {
		return C["ink"]
	}
	if len(stops) == 1 {
		return stops[0]
	}
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	p := x * float64(len(stops)-1)
	i := int(math.Floor(p))
	if i > len(stops)-2 {
		i = len(stops) - 2
	}
	if i < 0 {
		i = 0
	}
	return Mix(stops[i], stops[i+1], p-float64(i))
}

// Sin01 es una onda 0..1 con periodo en ms.
func Sin01(t, period, phase float64) float64 {
	return 0.5 + 0.5*math.Sin((2*math.Pi*t)/period+phase)
}

// Glow es un color que respira entre oscuro y brillante.
func Glow(color string, t, period, lo float64) string {
	if period == 0 {
		period = 1800
	}
	if lo == 0 {
		lo = 0.4
	}
	return Mix(C["dark"], color, lo+(1-lo)*Sin01(t, period, 0))
}

// Cycle es un degradado animado: las paradas se desplazan con el tiempo.
func Cycle(stops []string, x, t, speed float64) string {
	if speed == 0 {
		speed = 0.00018
	}
	p := math.Mod(x+t*speed, 1)
	if p < 0 {
		p++
	}
	if p < 0.5 {
		return GradientAt(stops, p*2)
	}
	return GradientAt(stops, (1-p)*2)
}

// Heat es el color de un semáforo continuo: 0 = frío/bien … 1 = crítico.
func Heat(v float64) string {
	return GradientAt([]string{C["primary"], C["green"], C["yellow"], C["orange"], C["red"]}, v)
}

// Rnd es un hash determinista (para «azar» que no cambia entre renders del mismo frame).
func Rnd(a, b int64) float64 {
	const (
		c1 int32 = -1640531527 // 0x9e3779b9
		c2 int32 = -2048144789 // 0x85ebca6b
		c3 int32 = 2135587861  // 0x7f4a7c15
		c4 int32 = -1028477379 // 0xc2b2ae35
		c5 int32 = 740162413   // 0x2c1b3c6d
	)
	imul := func(x, y int32) int32 { return int32(uint32(x) * uint32(y)) }
	x := imul(int32(a)^c1, c2) ^ imul(int32(b)+c3, c4)
	x ^= int32(uint32(x) >> 15)
	x = imul(x, c5)
	x ^= int32(uint32(x) >> 12)
	return float64(uint32(x)%10000) / 10000
}
