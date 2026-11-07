#!/bin/bash
# Descarga los GGUF a models/.   uso: scripts/get-models.sh [nomic] [qwen3b] [qwen3b-q2] [qwen7b] [qwen7b-q2] [ornith] [qwen3-gsq] [qwen3-embed] [ornith-heretic]
set -e
cd "$(dirname "$0")/.."
mkdir -p models
HF=https://huggingface.co
get() { [ -s "models/$1" ] && { echo "ya está: $1"; return; }; echo "descargando $1…"; curl -fL --retry 3 -o "models/$1" "$2"; }
ollama_blob() { # ollama_blob <modelo> <archivo>  (baja el blob GGUF del registro de Ollama)
  local d; d=$(curl -s -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' "https://registry.ollama.ai/v2/$1/manifests/latest" | python3 -c "import sys,json;print([l['digest'] for l in json.load(sys.stdin)['layers'] if l['mediaType'].endswith('.model')][0])")
  get "$2" "https://registry.ollama.ai/v2/$1/blobs/$d"
}
for m in "${@:-nomic qwen3b}"; do
  case $m in
    nomic)     get nomic-embed-text-v1.5.Q8_0.gguf $HF/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf ;;
    qwen3b)    get qwen2.5-3b-instruct-q4_k_m.gguf $HF/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q4_k_m.gguf ;;
    qwen3b-q2) get qwen2.5-3b-instruct-q2_k.gguf $HF/Qwen/Qwen2.5-3B-Instruct-GGUF/resolve/main/qwen2.5-3b-instruct-q2_k.gguf ;;
    qwen7b)    get qwen2.5-7b-instruct-q4_k_m.gguf $HF/bartowski/Qwen2.5-7B-Instruct-GGUF/resolve/main/Qwen2.5-7B-Instruct-Q4_K_M.gguf ;;
    qwen7b-q2) get qwen2.5-7b-instruct-q2_k.gguf $HF/Qwen/Qwen2.5-7B-Instruct-GGUF/resolve/main/qwen2.5-7b-instruct-q2_k.gguf ;;
    ornith)    ollama_blob madkoding/ornith-1.5-9b-iq2m ornith-1.5-9b-iq2m.gguf ;;
    qwen3-gsq)   get qwen3-8b-gsq-Q2_K.gguf $HF/ISTA-DASLab/Qwen3-8B-GGUF-GSQ/resolve/main/qwen3-8b-Q2_K.gguf ;;
    qwen3-embed) get Qwen3-Embedding-0.6B-Q8_0.gguf $HF/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf ;;
    ornith-heretic) get ornith-1.5-9b-heretic-gsq-rco-iq3_m.gguf $HF/motchii709/Ornith-1.5-9B-heretic-GSQ-RCO-IQ3_M-GGUF/resolve/main/Ornith-1.5-9B-heretic-GSQ-RCO-IQ3_M.gguf ;;
    *) echo "modelo desconocido: $m" >&2; exit 1 ;;
  esac
done
