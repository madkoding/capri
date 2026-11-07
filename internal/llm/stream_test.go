package llm

import "testing"

// Las etiquetas <think> pueden llegar partidas entre trozos del stream.
func TestThinkSplitterAcrossChunks(t *testing.T) {
	var sp thinkSplitter
	var think, answer string
	for _, piece := range []string{"<th", "ink>pie", "nso</th", "ink>Ho", "la <", "3"} {
		th, an := sp.Feed(piece)
		think += th
		answer += an
	}
	th, an := sp.Flush()
	think += th
	answer += an
	if think != "pienso" || answer != "Hola <3" {
		t.Errorf("think=%q answer=%q", think, answer)
	}

	// sin cerrar: todo lo que sigue es razonamiento
	sp = thinkSplitter{}
	th, an = sp.Feed("<think>sin fin")
	th2, an2 := sp.Flush()
	if th+th2 != "sin fin" || an+an2 != "" {
		t.Errorf("abierto: %q %q", th+th2, an+an2)
	}

	// sin etiquetas: todo es respuesta
	sp = thinkSplitter{}
	th, an = sp.Feed("solo texto")
	if th != "" || an != "solo texto" {
		t.Errorf("plano: %q %q", th, an)
	}
}
