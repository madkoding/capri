//go:build !windows

package shell

import (
	"context"
	"os"
	"os/exec"
)

// CommandContext construye un comando de shell portable (sh -c en Unix).
func CommandContext(ctx context.Context, line string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", line)
}

// ShellName es el intérprete usado por CommandContext.
func ShellName() string { return "sh" }

// HasExecutable indica si `name` existe en el PATH o en workdir.
func HasExecutable(workdir, name string) bool {
	cmd := exec.Command("sh", "-c", `command -v -- "$1"`, "_", name)
	if workdir != "" {
		if st, err := os.Stat(workdir); err == nil && st.IsDir() {
			cmd.Dir = workdir
		}
	}
	return cmd.Run() == nil
}
