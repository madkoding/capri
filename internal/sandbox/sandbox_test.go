package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestActive(t *testing.T) {
	if Active(None) {
		t.Error("el modo none nunca debe estar activo")
	}
	avail := Available()
	if Active(Auto) != (avail != "") {
		t.Errorf("Active(auto)=%v con Available()=%q", Active(Auto), avail)
	}
	if avail == "" && Active(Bwrap) {
		t.Error("bwrap no está disponible pero Active(bwrap)=true")
	}
	if avail != "" && Name(Auto) != avail {
		t.Errorf("Name(auto)=%q, quiero %q", Name(Auto), avail)
	}
}

func TestWrapFallback(t *testing.T) {
	if Available() != "" {
		t.Skip("hay sandbox disponible; se prueba en TestWrapBwrap")
	}
	base := exec.Command("echo", "hola")
	if got := Wrap(context.Background(), base, "", nil); got != base {
		t.Error("sin sandbox, Wrap debe devolver el mismo comando")
	}
}

func TestWrapBwrap(t *testing.T) {
	if runtime.GOOS != "linux" || Available() != Bwrap {
		t.Skip("bwrap no disponible")
	}
	work := t.TempDir()
	base := exec.Command("sh", "-c", "echo hola > dentro.txt && cat dentro.txt")
	base.Dir = work
	cmd := Wrap(context.Background(), base, work, []string{work})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bwrap: %v (%s)", err, out)
	}
	if string(out) != "hola\n" {
		t.Errorf("salida=%q", out)
	}
	if _, err := os.Stat(filepath.Join(work, "dentro.txt")); err != nil {
		t.Error("no se escribió dentro del workspace:", err)
	}

	// Fuera del workspace debe ser de solo lectura (/tmp sí es escribible
	// porque el sandbox le da un tmpfs).
	base2 := exec.Command("sh", "-c", "echo x > /etc/capri-sandbox-test")
	base2.Dir = work
	if err := Wrap(context.Background(), base2, work, []string{work}).Run(); err == nil {
		t.Error("se pudo escribir fuera del workspace")
	}
}
