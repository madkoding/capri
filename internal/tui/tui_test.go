package tui

import (
	"testing"
	"time"
)

// frame y widgets miden el tiempo en milisegundos; pasar segundos congela la animación.
func TestMsEsMilisegundos(t *testing.T) {
	if got := ms(1500 * time.Millisecond); got != 1500 {
		t.Errorf("ms(1,5 s)=%v, quiero 1500", got)
	}
	if got := ms(2 * time.Second); got != 2000 {
		t.Errorf("ms(2 s)=%v, quiero 2000", got)
	}
}
