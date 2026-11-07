// Package sandbox detecta y aplica aislamiento del agente sin contenedores:
// bwrap en Linux, sandbox-exec en macOS y nada en Windows.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Modos aceptados por CAPRI_SANDBOX.
const (
	Auto        = "auto"
	Bwrap       = "bwrap"
	SandboxExec = "sandbox-exec"
	None        = "none"
)

var (
	once     sync.Once
	detected string // mecanismo disponible, "" si ninguno
)

func detect() {
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("bwrap"); err == nil {
			detected = Bwrap
		}
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err == nil {
			detected = SandboxExec
		}
	}
}

// Available devuelve el mecanismo de aislamiento disponible en este equipo.
func Available() string {
	once.Do(detect)
	return detected
}

// Active indica si el modo pedido se puede aplicar de verdad.
func Active(mode string) bool {
	switch mode {
	case "", Auto:
		return Available() != ""
	case None:
		return false
	default:
		return Available() == mode
	}
}

// Name devuelve el nombre legible del mecanismo activo ("" si no hay).
func Name(mode string) string {
	if !Active(mode) {
		return ""
	}
	if mode == "" || mode == Auto {
		return Available()
	}
	return mode
}

// Wrap envuelve un comando para ejecutarlo aislado. writable son los únicos
// directorios donde se permite escribir; el resto del sistema queda de solo
// lectura. Si no hay mecanismo disponible devuelve el comando tal cual.
func Wrap(ctx context.Context, base *exec.Cmd, workdir string, writable []string) *exec.Cmd {
	switch Available() {
	case Bwrap:
		return wrapBwrap(ctx, base, workdir, writable)
	case SandboxExec:
		return wrapSandboxExec(ctx, base, workdir, writable)
	default:
		return base
	}
}

func wrapBwrap(ctx context.Context, base *exec.Cmd, workdir string, writable []string) *exec.Cmd {
	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--unshare-pid",
		"--die-with-parent",
	}
	for _, d := range writable {
		if d == "" {
			continue
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			args = append(args, "--bind", d, d)
		}
	}
	if workdir != "" {
		args = append(args, "--chdir", workdir)
	}
	args = append(args, "--")
	args = append(args, base.Args...)
	return clone(ctx, base, "bwrap", args)
}

func wrapSandboxExec(ctx context.Context, base *exec.Cmd, workdir string, writable []string) *exec.Cmd {
	var rules strings.Builder
	rules.WriteString("(version 1)\n(allow default)\n(deny file-write*)\n")
	for _, d := range writable {
		if d == "" {
			continue
		}
		fmt.Fprintf(&rules, "(allow file-write* (subpath %q))\n", d)
	}
	args := []string{"-p", rules.String()}
	args = append(args, base.Args...)
	return clone(ctx, base, "sandbox-exec", args)
}

func clone(ctx context.Context, base *exec.Cmd, name string, args []string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = base.Dir
	c.Env = base.Env
	c.Stdin = base.Stdin
	c.Stdout = base.Stdout
	c.Stderr = base.Stderr
	return c
}
