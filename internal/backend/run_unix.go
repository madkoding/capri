//go:build !windows

package backend

import (
	"os"
	"os/exec"
	"syscall"
)

// detach hace que el servidor sobreviva al cierre de la terminal.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// alive indica si el proceso sigue vivo.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
