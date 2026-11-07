package doctor

import "testing"

func TestFailed(t *testing.T) {
	ok := Report{Checks: []Check{{Name: "a", Status: OK}, {Name: "b", Status: Warn}}}
	if ok.Failed() {
		t.Error("avisos no deben contar como fallo")
	}
	bad := Report{Checks: []Check{{Name: "a", Status: OK}, {Name: "b", Status: Fail}}}
	if !bad.Failed() {
		t.Error("un Fail debe marcar el informe como fallido")
	}
}

func TestSummary(t *testing.T) {
	r := Report{Checks: []Check{
		{Status: OK}, {Status: OK}, {Status: Warn}, {Status: Fail},
	}}
	if got, want := r.Summary(), "2 ok · 1 avisos · 1 fallos"; got != want {
		t.Errorf("Summary()=%q, quiero %q", got, want)
	}
}
