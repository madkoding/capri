package calc_test

import (
	"testing"

	"capri/internal/calc"
)

func TestCalcSpanish(t *testing.T) {
	cases := map[string]string{
		"¿Cuánto es 15 por 4?":  "60",
		"cuánto es 100 entre 8": "12.5",
		"calcula 2 + 3 * 4":     "14",
		"cuánto es 7 más 5":     "12",
		"el 15% de 200":         "30",
		"3,5 por 2":             "7",
	}
	for in, want := range cases {
		got := calc.Calc(in)
		if got == nil {
			t.Fatalf("Calc(%q) = nil, quería %q", in, want)
		}
		if *got != want {
			t.Errorf("Calc(%q) = %q, quería %q", in, *got, want)
		}
	}
}

func TestCalcIgnoresNonMath(t *testing.T) {
	for _, in := range []string{"hola", "Me llamo Matías", "tengo 3 perros", "cuéntame un chiste", "2026", "crea a.txt con 15 por 4 líneas", "qué es 5"} {
		if got := calc.Calc(in); got != nil {
			t.Errorf("Calc(%q) = %q, quería nil", in, *got)
		}
	}
}

func TestCalcDoesNotExecuteCode(t *testing.T) {
	for _, in := range []string{"process.exit(1)", "1 + 1; rm -rf /"} {
		if got := calc.Calc(in); got != nil {
			t.Errorf("Calc(%q) = %q, quería nil", in, *got)
		}
	}
}
