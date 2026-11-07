#!/bin/bash
# Descarga el runtime Vulkan de llama.cpp para incrustarlo en el binario (go build -tags vulkan).
# uso: scripts/embed-vulkan.sh <linux|windows> <amd64|arm64>      (CAPRI_LLAMA_TAG fija el build)
set -e
cd "$(dirname "$0")/../internal/backend/embedded"
os=${1:?goos}; arch=${2:?goarch}
case "$os-$arch" in
  linux-amd64)   suffix=ubuntu-vulkan-x64.tar.gz ;;
  linux-arm64)   suffix=ubuntu-vulkan-arm64.tar.gz ;;
  windows-amd64) suffix=win-vulkan-x64.zip ;;
  windows-arm64) suffix=win-vulkan-arm64.zip ;;
  *) echo "no hay build Vulkan oficial de llama.cpp para $os/$arch (macOS usa Metal: compila sin -tags vulkan)" >&2; exit 1 ;;
esac
out="runtime-$os-$arch"
tag=${CAPRI_LLAMA_TAG:-}
if [ -z "$tag" ]; then
  tag=$(curl -fsSL "https://api.github.com/repos/ggml-org/llama.cpp/releases?per_page=10" | python3 -c "
import sys,json
for r in json.load(sys.stdin):
    if any(a['name'].endswith('$suffix') for a in r['assets']): print(r['tag_name']); break")
fi
[ -n "$tag" ] || { echo "no se encontró un build de llama.cpp con $suffix" >&2; exit 1; }
asset="llama-$tag-bin-$suffix"
if [ -s "$out.archive" ] && [ "$(cat "$out.meta" 2>/dev/null)" = "$tag $asset" ]; then echo "ya está: $asset"; exit 0; fi
echo "descargando $asset…"
curl -fL --retry 3 -o "$out.archive.tmp" "https://github.com/ggml-org/llama.cpp/releases/download/$tag/$asset"
mv "$out.archive.tmp" "$out.archive"
echo "$tag $asset" > "$out.meta"
