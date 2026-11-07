//go:build !vulkan || !(linux || windows) || !(amd64 || arm64)

package backend

// Sin runtime incrustado: compilación normal o plataforma sin build Vulkan (p. ej. macOS usa Metal).
var (
	embArchive []byte
	embMeta    string
)
