package llm

import "testing"

func TestCleanStripsRealThinkBlocks(t *testing.T) {
	if got := clean("<think>pienso mucho</think>Hola"); got != "Hola" {
		t.Errorf("clean = %q", got)
	}
	if got := clean("Estoy thinking en voz alta, ¿ok?"); got != "Estoy thinking en voz alta, ¿ok?" {
		t.Errorf("no debe recortar texto normal: %q", got)
	}
}

func TestCleanStripsLeakedToolTags(t *testing.T) {
	in := "Voy a sacar la hora.\n\n<execute_command>\ndate\n</execute_command>\nListo."
	if got := clean(in); got != "Voy a sacar la hora.\n\n\nListo." && got != "Voy a sacar la hora.\n\nListo." {
		t.Errorf("clean = %q", got)
	}
}
