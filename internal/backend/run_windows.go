//go:build windows

package backend

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detach hace que el servidor sobreviva al cierre de la terminal.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008} // CREATE_NEW_PROCESS_GROUP
}

// alive indica si el proceso sigue vivo.
func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}
