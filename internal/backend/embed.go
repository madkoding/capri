package backend

import "strings"

// embeddedRuntime devuelve el runtime de llama.cpp (Vulkan) incrustado en este
// binario, si es una compilación `-tags vulkan` para una plataforma soportada
// (linux/windows × amd64/arm64). En la compilación normal ok es false y Capri
// descarga el runtime la primera vez.
func embeddedRuntime() (data []byte, tag, asset string, ok bool) {
	f := strings.Fields(embMeta)
	if len(embArchive) == 0 || len(f) < 2 {
		return nil, "", "", false
	}
	return embArchive, f[0], f[1], true
}

// EmbeddedTag es el build de llama.cpp incrustado ("" si no hay).
func EmbeddedTag() string {
	_, tag, _, ok := embeddedRuntime()
	if !ok {
		return ""
	}
	return tag
}
