.PHONY: build-vulkan qwen3 qwen3-models build run test eval from-trace doctor backend models gpu cross

build:            ## compila el binario estático (sin CGO)
	CGO_ENABLED=0 go build -trimpath -o capri ./cmd/capri

run:              ## abre la TUI (sandbox automático si hay bwrap/sandbox-exec)
	go run ./cmd/capri

test:
	go test ./...

eval:             ## evaluación del bucle sobre tareas con verificación real
	go run ./cmd/capri eval --blind2

from-trace:       ## convierte trazas reales en tareas de evaluación (--stats para un resumen)
	go run ./cmd/capri from-trace --stats

doctor:           ## comprueba plataforma, backend, modelos, memoria y sandbox
	go run ./cmd/capri doctor

backend:          ## runtime nativo de llama.cpp: make backend ARGS="install|start|stop|status"
	go run ./cmd/capri backend $(ARGS)

gpu:              ## publica la temperatura de la GPU en telemetry/ (para la TUI)
	go run ./cmd/capri gpu-telemetry

models:           ## descarga los GGUF a models/ (scripts/get-models.sh nomic qwen3b)
	scripts/get-models.sh nomic qwen3b

cross:            ## comprueba que compila en linux/darwin/windows (amd64 y arm64)
	GOOS=linux   GOARCH=amd64 go build ./...
	GOOS=linux   GOARCH=arm64 go build ./...
	GOOS=darwin  GOARCH=arm64 go build ./...
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=windows GOARCH=arm64 go build ./...

qwen3-models:     ## descarga Qwen3-8B GSQ Q2_K + Qwen3-Embedding-0.6B (perfil manual)
	scripts/get-models.sh qwen3-gsq qwen3-embed

qwen3:            ## arranca el backend con el perfil qwen3 y abre la TUI (no es el modo por defecto)
	go run ./cmd/capri backend stop
	go run ./cmd/capri --profile qwen3 backend start
	go run ./cmd/capri --profile qwen3

build-vulkan:     ## binario único con llama.cpp Vulkan incluido (make build-vulkan [GOOS=linux GOARCH=amd64])
	scripts/embed-vulkan.sh $(or $(GOOS),$(shell go env GOOS)) $(or $(GOARCH),$(shell go env GOARCH))
	CGO_ENABLED=0 go build -tags vulkan -trimpath -o capri-vulkan ./cmd/capri
