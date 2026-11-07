//go:build windows

package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	bashOnce sync.Once
	bashPath string
)

// findBash busca un bash utilizable (Git Bash, MSYS2 o WSL) para que las
// skills POSIX sigan funcionando en Windows. Si no hay, se usa PowerShell.
func findBash() string {
	bashOnce.Do(func() {
		for _, name := range []string{"bash", "bash.exe"} {
			if p, err := exec.LookPath(name); err == nil {
				// El bash de System32 es el lanzador de WSL: sirve, pero puede
				// no tener distro instalada. Se prefiere Git Bash si aparece.
				if strings.Contains(strings.ToLower(p), "system32") {
					continue
				}
				bashPath = p
				return
			}
		}
		if p, err := exec.LookPath("bash.exe"); err == nil {
			bashPath = p
		}
	})
	return bashPath
}

// CommandContext construye un comando de shell portable: bash si existe
// (Git Bash/WSL) y si no PowerShell.
func CommandContext(ctx context.Context, line string) *exec.Cmd {
	if b := findBash(); b != "" {
		return exec.CommandContext(ctx, b, "-c", line)
	}
	return exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", line)
}

// ShellName es el intérprete usado por CommandContext.
func ShellName() string {
	if findBash() != "" {
		return "bash"
	}
	return "powershell"
}

// HasExecutable indica si `name` existe en el PATH o en workdir.
func HasExecutable(workdir, name string) bool {
	if name == "" {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		p := name
		if !filepath.IsAbs(p) && workdir != "" {
			p = filepath.Join(workdir, p)
		}
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	candidates := []string{name}
	if filepath.Ext(name) == "" {
		candidates = append(candidates, name+".exe", name+".cmd", name+".bat")
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return true
		}
	}
	return false
}
