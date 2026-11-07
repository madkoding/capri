//go:build vulkan

package backend

import _ "embed"

// Runtime Vulkan de llama.cpp incrustado; lo deja scripts/embed-vulkan.sh (make build-vulkan).

//go:embed embedded/runtime-linux-amd64.archive
var embArchive []byte

//go:embed embedded/runtime-linux-amd64.meta
var embMeta string
