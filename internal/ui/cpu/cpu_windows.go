//go:build windows

package cpu

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

func rawModel() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, err := k.GetStringValue("ProcessorNameString")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// readPackageTemp devuelve nil: Windows no expone la temperatura del paquete
// de forma portable sin WMI ni privilegios.
func readPackageTemp() *float64 { return nil }
