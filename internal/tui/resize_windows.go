//go:build windows

package tui

import (
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// watchResize sondea el tamaño del terminal cada 250 ms, porque Windows no
// tiene SIGWINCH.
func watchResize() (<-chan struct{}, func()) {
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		fd := int(os.Stdout.Fd())
		cols, rows, _ := term.GetSize(fd)
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				c, r, err := term.GetSize(fd)
				if err == nil && (c != cols || r != rows) {
					cols, rows = c, r
					select {
					case out <- struct{}{}:
					default:
					}
				}
			case <-done:
				return
			}
		}
	}()
	return out, func() { close(done) }
}

var vtOnce atomic.Bool

// enableVT activa las secuencias ANSI en la consola de Windows.
func enableVT() {
	if vtOnce.Swap(true) {
		return
	}
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	const enableVirtualTerminalProcessing = 0x0004
	_ = windows.SetConsoleMode(h, mode|enableVirtualTerminalProcessing)
}
