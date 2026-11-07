package holo_test

import (
	"math"
	"testing"

	"capri/internal/holo"
)

func TestFixedVectorsNormalized(t *testing.T) {
	if holo.D != 512 {
		t.Fatalf("D = %d, quería 512", holo.D)
	}
	a, b := holo.Vector("perro"), holo.Vector("perro")
	if len(a) != len(b) {
		t.Fatal("vector('perro') no es determinista")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("vector('perro') no es determinista")
		}
	}
	if s := holo.Sim(a, a); math.Abs(s-1) >= 1e-4 {
		t.Errorf("sim(v,v) = %f", s)
	}
}

func TestBindNearlyOrthogonal(t *testing.T) {
	a := holo.Vector("perro")
	b := holo.Vector("rocky")
	c := holo.Bind(a, b)
	if s := holo.Sim(c, a); math.Abs(s) >= 0.2 {
		t.Errorf("sim(bind,a) = %f", s)
	}
	if s := holo.Sim(c, b); math.Abs(s) >= 0.2 {
		t.Errorf("sim(bind,b) = %f", s)
	}
}

func TestOrderMatters(t *testing.T) {
	if s := holo.Sim(holo.Encode("perro llama"), holo.Encode("llama perro")); s >= 0.9 {
		t.Errorf("sim(perro llama, llama perro) = %f", s)
	}
	if s := holo.Sim(holo.Encode("perro llama rocky"), holo.Encode("rocky llama perro")); s >= 0.95 {
		t.Errorf("sim(orden distinto) = %f", s)
	}
	if s := holo.Sim(holo.Encode("perro llama"), holo.Encode("perro llama")); s <= 0.99 {
		t.Errorf("sim(igual) = %f", s)
	}
}

func TestRelatedMemoriesScoreHigher(t *testing.T) {
	memo := holo.Encode("El usuario trabaja como ingeniero de software en Santiago de Chile")
	cerca := holo.Encode("dónde trabaja el usuario")
	lejos := holo.Encode("receta de pastel de choclo con albahaca")
	if s := holo.Sim(memo, cerca); s <= 0.15 {
		t.Errorf("cerca = %f", s)
	}
	if s := holo.Sim(memo, lejos); math.Abs(s) >= 0.15 {
		t.Errorf("lejos = %f", s)
	}
}

func TestEmptyTextNullVector(t *testing.T) {
	v := holo.Encode("de la el que")
	if len(v) != holo.D {
		t.Fatalf("len = %d", len(v))
	}
	if s := holo.Sim(v, holo.Encode("perro")); math.IsNaN(s) {
		t.Fatal("sim devolvió NaN")
	}
}
