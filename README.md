# Capri

Agente local con **segundo cerebro**: un LLM en llama.cpp (GPU) + RAG propio (SQLite: FTS5 + vectores nomic-embed) +
una TUI cyberpunk a pantalla completa. Sin webui.

```bash
make build              # compila ./capri (binario estático, sin CGO)
make models             # descarga los GGUF a models/
make backend ARGS=install   # baja llama.cpp nativo para tu OS/GPU
make backend ARGS=start     # chat en :7869 y embeddings en :8081
make run                # abre la TUI (sandbox automático si hay bwrap)
make test / make eval
```

Sin Docker, sin Podman y sin runtime de contenedores. Funciona en **Linux, macOS y Windows**
(`amd64`/`arm64`): el binario es el mismo y el runtime de llama.cpp se descarga según la plataforma.

## Instalación universal

```bash
go build -o capri ./cmd/capri     # o `go install ./cmd/capri`
./capri doctor                    # comprueba plataforma, backend, modelos, memoria y sandbox
./capri backend install           # detecta CUDA/ROCm/Metal/Vulkan/CPU y baja el asset correcto
./capri backend start
./capri
```

- **Backend `llamacpp`** (por defecto): Capri descarga el binario oficial de llama.cpp para tu
  sistema (`~/.cache/capri/llama/<build>-<acelerador>/`) y arranca los dos servidores. Si tu GPU
  no está en las builds oficiales (p. ej. **Tesla P4 / Pascal sm_61**), usa
  `capri backend install --accel vulkan` (probado en la P4) o `--build` con el toolkit de CUDA.
  `capri doctor` avisa si el binario instalado no ve ningún dispositivo.
- **Backend `openai`**: cualquier servidor OpenAI-compatible (Ollama, vLLM, LM Studio, uno remoto):

  ```bash
  CAPRI_BACKEND=openai CAPRI_URL=http://localhost:11434/v1 \
  EMBED_URL=http://localhost:11434/v1 CAPRI_MODEL=qwen2.5:7b EMBED_MODEL=nomic-embed-text ./capri
  ```

- **Modelos**: `LLM_GGUF` / `EMBED_GGUF` eligen archivo; si no, se usa el GGUF más grande de
  `models/` para chat y el primero con "embed"/"nomic" para embeddings.
- **`.env`**: si existe en el directorio de trabajo, sus claves se aplican como valores por defecto
  (sin pisar variables ya definidas). Útil para fijar `CAPRI_ACCEL`, `LLM_GGUF` o `CAPRI_MODEL`.

### Windows

- `capri backend install` elige entre `win-cpu-x64`, `win-cuda-12.4-x64` y `win-vulkan-x64`
  (también `arm64`), y arranca los servidores como procesos normales.
- Si hay **Git Bash** o **WSL**, los comandos del agente se ejecutan con `bash` y las skills POSIX
  funcionan tal cual; si no, se usa PowerShell. La elección es automática.
- No hay sandbox en Windows: la barra inferior muestra `HOST`. `capri doctor` lo indica.
- `scripts/get-models.sh` es un script bash: en Windows se ejecuta desde Git Bash o WSL.

## La TUI
Usa **toda la ventana** (pantalla alternativa, como `vim`/`htop`) y se adapta al tamaño real, también si lo cambias en caliente:
barra de título arriba, el **chat con scroll** (RePág/AvPág, barra lateral) ocupando todo el alto sobrante, y el dock, la
entrada y el pie abajo. Al salir imprime la conversación en tu terminal; `/export` la guarda en `data/transcripts/`.
Todo lo que se mueve es función de un único reloj (10 fps; `CAPRI_FPS`, `CAPRI_ANIM=0` para conexiones lentas).
- **Arranque** animado con *glitch* y comprobación real de cada sistema; cualquier tecla lo salta.
- **Dock de instrumentos**: *Sistema* (LLM, embeddings, `↯ t/s`, **temperatura de GPU** con medidor y alerta
  `THROTTLE`, VRAM, **contexto usado/máximo**), *Enlace neuronal* (pipeline RECALL→ROUTE→THINK→ACT→MEMORY que se
  enciende nodo a nodo, ecualizador, escáner, parámetros de muestreo) y *Memoria* (recuerdos activados con su similitud).
- La **caja de entrada** tiene una luz que recorre el borde; las respuestas se "transmiten" con efecto máquina de escribir.
- Autorizaciones con borde parpadeante; el explorador de memoria (`/mem`) se maneja con ↑↓ y `d`.
- El dock se adapta al tamaño (completo / compacto / oculto). `scripts/tui-shot.py` captura la pantalla real para verificarlo.
- **Render propio:** la pantalla la construye `internal/ui/frame` como cadenas ANSI (función pura de estado y tiempo) y
  `internal/tui` escribe únicamente las filas que cambiaron. El reloj anima a 10 fps; `CAPRI_ANIM=0` lo baja a 1 fps y
  `CAPRI_FPS` ajusta la fluidez. Como la pantalla es una función pura, `internal/ui/frame/frame_test.go` comprueba sin
  terminal que **cada fotograma mide exactamente el alto×ancho de la ventana** en 9 tamaños × 11 estados.

Comandos: `/model [n|texto|hf:usuario/repo/archivo.gguf]` (lista los GGUF instalados y los descargables; si eliges uno que falta lo **baja de Hugging Face** con reanudación y progreso — `HF_TOKEN` para repos restringidos —, y cambia el modelo de chat en caliente: reinicia solo ese servidor, deja los embeddings, aplica el preset de muestreo del modelo y recuerda la elección en `.capri/model`). Presets propios: `.capri/presets.json` (`{"trozo-del-nombre": {"temperature":…, "top_k":…, "min_p":…, "repeat_penalty":…}}`); `TEMPERATURE`/`TOP_K`/`MIN_P`/`REPEAT_PENALTY` en el entorno siempre ganan, `/pin /unpin`, `/undo [list] /mem /search /forget /dedupe /prune /bad /compact /context /export /auto /ask /clear /help /quit`.

## Cómo funciona un turno
1. **Recordar**: búsqueda híbrida (BM25 + coseno, fusión RRF); entran ≤3000 caracteres de hechos al prompt.
2. **Aprender solo**: las frases que son datos personales se guardan sin pedirlo (`internal/learn`, evita duplicados).
3. **¿Acción con herramientas?** → **bucle ReAct** (`internal/loop`): el modelo decide **un paso cada vez, viendo el resultado
   real del anterior** (gramática GBNF: una llamada válida o `DONE`). Si no, conversación o calculadora determinista.
4. El arnés, no el modelo, controla: **cobertura** al terminar (¿se hizo lo pedido, con los nombres exactos?, recetas
   deterministas si el modelo se atasca), **seguridad** y **presupuesto** (≤8 pasos, sin repetir, 2 fallos detienen).
5. **Memorizar** el turno como episodio: lo que dijiste + acciones + resultado *determinista*. Las respuestas libres del
   modelo **no** se guardan (a veces son basura y contaminan la memoria); `STORE_ANSWERS=1` las guarda igual.

## Internet (`internal/web`, `skills/buscar-en-internet.md`)
Si el cerebro no tiene nada sobre una pregunta factual, Capri busca (DuckDuckGo, o SearXNG con `CAPRI_SEARCH_URL`), lee las
3 páginas más relevantes y **guarda cada una como memoria** (`kind: web`, con su URL), así que la próxima vez responde desde
el RAG. También puede usar la herramienta `web_search` en el bucle ReAct cuando se lo pides ("busca en internet…").
`CAPRI_WEB=0` lo desactiva (sin red). Solo sale a internet para preguntas de al menos tres palabras que el cerebro no cubre.

## Compactación de contexto
La **sesión** acumula turnos (lo que dijiste, lo que respondió Capri y las acciones). Antes de cada turno se estima cuánto
ocupará el prompt; si la sesión + lo que va a añadir llega al **75 % de la ventana de trabajo**
(`CAPRI_CTX_WINDOW`, 8192 por defecto, o la del servidor si es menor; `CAPRI_CTX_THRESHOLD`) se **compacta**:
1. los turnos antiguos se **resumen con el LLM** (si falla, resumen determinista: nunca se pierde el contexto ni se bloquea el turno);
2. el resumen se **guarda en el RAG** (`kind: summary`, ⇣): reaparece como memoria activada en turnos futuros;
3. la sesión queda como *resumen + últimos `CAPRI_CTX_KEEP` turnos literales* (2).

Hay un segundo nivel **dentro de una tarea** del bucle ReAct: si sus pasos llenan la ventana, los pasos viejos pasan a un
resumen dentro del objetivo (y, si aún no cabe, se descartan los ejemplos); ese resumen también se guarda en el RAG.
La TUI muestra `⟫ COMPACTANDO CONTEXTO`, un bloque `⇣ CONTEXTO COMPACTADO` (tokens antes → después, turnos, memoria #id y
vista previa) y el contador `⇣N`. `/compact` compacta ya, `/context` muestra ventana, umbral, uso y compactaciones;
`CAPRI_SESSION=0` desactiva la memoria de sesión (cada turno independiente). El medidor `CTX` usa la ventana de trabajo y
pasa a naranja al acercarse al umbral.

**Recuperación:** un recuerdo con similitud media (0,45–0,62) solo entra al prompt si la pregunta trata del usuario o de la
conversación (mi/me/vivo/«de qué hablamos»); antes se colaban datos personales en preguntas de cultura general.

## Robustez con modelos pequeños
Los modelos cuantizados (3B, Q2, IQ3…) enrutan mal, copian plantillas del prompt y ensucian argumentos. El arnés lo compensa de
forma determinista (sin consultar al modelo):
- **Pista de acción obligatoria** (`intent.ActionCue`): un intent de herramienta (`shell`, `fs_*`) solo se acepta si el mensaje
  trae archivo, palabra de entorno (archivos, carpeta, disco…), verbo imperativo, pregunta de sistema (hora, espacio, ip…) o un
  comando al inicio. Un saludo o una pregunta de conocimiento nunca acaba en `echo "hola"`. El saludo inicial («hola capri, …»)
  se descarta antes de enrutar, y `rag_list` exige hablar de memoria.
- **Umbrales por familia de embeddings** (`config.Thresholds`, medidos): nomic solapa pares sin relación (0.52–0.75) con
  paráfrasis (0.70–0.95) y su kNN apenas discrimina; Qwen3-Embedding separa limpio (≤0.29 vs ≥0.63). Por eso Qwen3 es el
  embedder por defecto y nomic funciona a base de reglas léxicas. `MIN_SIM` / `CAPRI_SKILL_MIN` siguen mandando.
- **Sin embeddings** el enrutado cae a las reglas léxicas (pista de acción + no pregunta → bucle de herramientas).
- **Gate**: una «PREGUNTA: ¿qué archivo?» se ignora si el mensaje ya nombra el objeto (`.tmp`, `a.txt`, «los archivos»); y una
  pregunta que copia la plantilla (`<…>`, `→`) se descarta.
- **Bucle ReAct**: rutas sucias se limpian (`frutas.txt. (el nombre incluye el punto)` → `frutas.txt`); `rm` solo puede borrar lo
  que nombra el objetivo (comodines solo con «todos» o la extensión pedida); rechazar una **sobrescritura** no cancela la tarea: el
  modelo recibe la pista de usar `>>` o `fs_edit`; `web_search` es un paso válido del plan (antes la gramática lo permitía pero el
  parser lo descartaba y el turno quedaba vacío).
- **Cobertura** (`planner`): «…y muéstralo/léelo» exige una lectura **posterior** al último cambio (una lectura inicial «lee a.txt y
  guarda…» es la fuente, no el resultado); «…y lista los archivos» exige un listado posterior al último cambio; «borra a.txt»
  sugiere `fs_delete: a.txt`; «crea x.txt con hola» sugiere `fs_write: x.txt | hola` (también «créalo con la palabra nuevo»);
  «crea carpetas a, b y c» sugiere `mkdir -p a b c`; «añade X al final de f» sugiere `echo 'X' >> f`; la hora y la fecha solo
  salen de `shell: date`.
- **Pasos forzados**: si el modelo responde `DONE` sin haber hecho nada, el paso sugerido se ejecuta directamente (y si el objetivo
  nombra un archivo que existe, se empieza leyéndolo); si repite un paso, en vez de parar se ejecuta lo que falta. Cada
  sugerencia distinta se fuerza una sola vez.
- **Argumentos con razonamiento**: `date +%F para obtener la fecha, luego…` se corta en la prosa (fuera de comillas);
  `fs_list: x.txt` lista la carpeta del archivo; «reemplaza X por Y» sin acotar («la primera», «solo una vez») reemplaza
  **todas** las apariciones en vez de pedir más contexto.
- Con la web apagada (`CAPRI_WEB=0`), `web_search` desaparece de la gramática y del prompt del bucle: un modelo pequeño la
  elegía para «la fecha de hoy».
- **Camino rápido**: si el mensaje pide un dato («dime la última línea de x.txt») la respuesta se redacta sobre lo observado en
  lugar de volcar el archivo.
- **Búsqueda web pedida a mano**: «busca en internet mejor», «¿y la búsqueda?» o «búscalo de verdad» se reconocen como orden;
  si no traen tema se busca la última pregunta real de la sesión (no «¿lo estás inventando?»). La búsqueda se ejecuta ANTES de
  redactar y se le dice al modelo el resultado real (hallazgos, nada, o error); las líneas en que aun así finge lanzarla
  («Voy a buscar…», «[Realizo la búsqueda]») se eliminan. Con `CAPRI_WEB=0` se contesta en seco que está desactivada.
- **Memoria**: `rag_add` solo con orden explícita («anota…», «guarda esto…») o un dato personal; una queja como «parece que
  no hiciste nada» ya no se guarda como recuerdo.
- Las etiquetas de herramienta que algunos modelos filtran al texto (`<execute_command>…`) se eliminan de la respuesta; el
  razonamiento del modelo se muestra como `💭` (`/think on|off`).
- `capri eval --chat` comprueba que la charla se resuelve con **cero** herramientas y sin plantillas.

## Seguridad
- Lo que modifica pide confirmación; los comandos de **solo lectura** (`ls`, `wc`, `grep`, `date`…) no.
- **Sobrescribir** un archivo existente pregunta siempre (también en `/auto`); tras fallar una lectura, un paso destructivo
  sobre **otra** ruta se reconfirma.
- **`/undo`**: antes de la primera modificación de cada turno se copia la carpeta de trabajo (y se anota el valor anterior
  de la memoria); `/undo` revierte el último turno entero. La copia vive en `data/trash/`, fuera de la carpeta de trabajo.
- **Aislamiento sin contenedores** (`internal/sandbox`): si hay `bwrap` (Linux) o `sandbox-exec`
  (macOS), los comandos del agente corren con el resto del sistema de solo lectura y solo pueden
  escribir en `workspace/` y `data/`. En Windows no hay sandbox: la barra inferior muestra
  `SANDBOX` solo cuando está activo de verdad, y `HOST` cuando no. `CAPRI_SANDBOX=none` lo desactiva.

## Memoria (RAG)
`data/brain.db`. `/mem` (explorador), `/dedupe` (funde notas casi idénticas, deshacible), `/prune [días]` (borra turnos viejos).
- **Búsqueda híbrida**: embeddings nomic (semántica) + FTS5 (palabras clave) + **memoria holográfica** (`internal/holo`): un vector HRR de 512 dimensiones por recuerdo que suma palabras y pares ordenados ligados por convolución circular. No necesita servicio extra, distingue el orden ("perro llama" ≠ "llama perro") y aporta un tercer ranking a la fusión RRF. `CAPRI_HOLO=0` la desactiva; `CAPRI_HOLO_MIN` (0,15) es el coseno mínimo para contar.

## Skills (`skills/`)
Biblioteca de recetas en Markdown, una por archivo: `name`, `description` (cuándo sirve) y bloques `Objetivo:` con sus pasos de ejemplo. El prompt **no** las carga todas: por objetivo se buscan las que encajan (`internal/skills`, por similitud con la descripción y con los objetivos de ejemplo) y solo esas entran como ejemplos. Para añadir una, crea un `.md` nuevo en `skills/`. `CAPRI_SKILL_MIN` (0,67) fija la similitud mínima.

**Skills aprendidas**: tras una tarea que sale limpia (ningún paso fallido ni rechazado, y sin skill parecida, similitud < 0,85), el agente guarda la receta en `skills/generated/`. El modelo solo escribe el nombre y la descripción; los pasos son los que se ejecutaron de verdad. `CAPRI_SKILL_LEARN=0` lo desactiva y `CAPRI_SKILL_MAX_GENERATED` (50) pone el tope. Son archivos normales: puedes revisarlos, editarlos o borrarlos.

## Trazas y evaluación
- Cada turno deja una traza local en `data/traces.jsonl` (`CAPRI_TRACE=0` la desactiva). `/bad` marca un turno mal
  resuelto; `capri from-trace` convierte fallos reales en tareas de evaluación (`--stats` para un resumen).
- `capri eval [--heldout|--fresh|--blind|--blind2] [--trace]` ejecuta tareas multi-paso y **verifica el
  resultado sobre el sistema de archivos**. `go test ./...` incluye el bucle, la compactación y la pantalla, con un **LLM falso** determinista.
- `capri ask "..."` responde sin TUI (scripts), `capri undo` deshace el último turno y `capri gpu-telemetry` publica
  `telemetry/gpu.json` para la TUI aislada.
- Las cifras de dev/reservado/fresco están **infladas** (guiaron las reglas); lo representativo para tareas nuevas son
  los conjuntos ciegos (`--blind`, `--blind2`).

## Perfil `qwen3` (modo manual, no es el predeterminado)
Qwen3-8B cuantizado con GSQ (Q2_K) + **Qwen3-Embedding-0.6B**. Solo se activa si lo pides:

```bash
make qwen3-models        # baja qwen3-8b-gsq-Q2_K.gguf y Qwen3-Embedding-0.6B-Q8_0.gguf a models/
make qwen3               # backend con el perfil + TUI   (o: capri --profile qwen3 backend start; capri --profile qwen3)
```

- **Contexto:** Qwen3-8B tiene 32 768 tokens nativos (40 960 posiciones en su config; 131 072 con YaRN). El perfil arranca el
  servidor con 16 384 y compacta al 75 % de esa ventana (8 GB de VRAM: pesos ≈3,3 GB + KV). Para 32 768:
  `CAPRI_SERVER_CTX=32768 CAPRI_KV_TYPE=q8_0 CAPRI_CTX_WINDOW=32768`.
- **Embeddings:** Qwen3-Embedding usa pooling `last`, 1024 dimensiones y una instrucción en la consulta (sin prefijo en los
  documentos). Al cambiar de embedder, Capri **reindexa solo** los recuerdos al arrancar (el sello vive en la tabla `meta`) y mientras
  tanto ignora los vectores de otra dimensión.
- Los valores del entorno real (`CAPRI_CTX_WINDOW`, `LLM_GGUF`…) ganan sobre el perfil, y el perfil gana sobre `.env`.

## Binario único con Vulkan (`capri-vulkan`)
`make build-vulkan` (o la release `capri-vulkan_*`) compila con el `llama-server` Vulkan de llama.cpp **dentro del binario**
(`-tags vulkan`; ≈55 MB, linux/windows × amd64/arm64): la primera vez se extrae a `~/.cache/capri/llama/` sin descargar nada y el
acelerador es Vulkan por defecto. Sin CPU ni CUDA incluidos; macOS usa la versión normal (Metal). `scripts/embed-vulkan.sh` baja el
runtime (`CAPRI_LLAMA_TAG` fija el build).

## Embeddings internos
Los embeddings también son internos: **Qwen3-Embedding-0.6B** por defecto (1024 dimensiones, pooling `last`). Si no está en
`models/`, Capri lo descarga la primera vez (≈640 MB) y reindexa la memoria sola si venías de nomic. `EMBED_MODEL=nomic`
vuelve a nomic-embed-text. Si el servidor de embeddings en marcha usa otro modelo, Capri lo reinicia con el configurado.

## Modelos internos por defecto y `/provider`
Capri usa **sus modelos internos** y **su propio llama.cpp**: si no hay runtime instalado, la primera vez descarga el `llama-server` oficial de tu plataforma a `~/.cache/capri/llama/` (sin pasos manuales; `CAPRI_LLAMA_BIN` usa uno tuyo). Al abrir la TUI (o `capri ask`) levanta solo llama.cpp con los GGUF de `models/` si no están
en marcha, sin mirar qué tengas en Ollama. Un servidor externo solo se usa si lo configuras con `/provider`:
`/provider ollama [url]`, `/provider openai <url>`, `/provider embed <modelo>` y `/provider internal` para volver. Se guarda en
`.capri/provider.json` y se aplica en caliente (con un proveedor externo, `/model` elige entre los modelos de su servidor y la
memoria se reindexa si cambia el embedder). Sin clave de API: sirve para Ollama/vLLM/LM Studio locales.

## Carpeta `.capri/`
Todo el estado de Capri vive en `.capri/` (cambia con `CAPRI_HOME`): `brain.db` (memoria), `traces.jsonl`, `trash/` (copias de
`/undo`) y `transcripts/`. Si existe un `data/brain.db` antiguo, se **mueve** ahí la primera vez. `RAG_DB` sigue mandando si lo defines.

## Buenas prácticas de memoria (implementadas)
- **Espejo Markdown**: cada recuerdo es `.capri/memory/<tipo>/<id>-<slug>.md` (cabecera con id, tipo, etiquetas, fijado, fechas) y un
  `INDEX.md` con los fijados primero. La base sigue siendo la fuente de verdad; al arrancar, Capri **importa lo que editaste a mano** y
  da de alta como nota cualquier `.md` nuevo sin cabecera. `CAPRI_MIRROR=0` lo desactiva.
- **Importancia**: `/pin <id>` fija un recuerdo (no lo borra `/prune`, no lo funde `/dedupe`, puntúa ×1,5); `/unpin <id>` lo suelta.
- **Antigüedad**: chat, web y resúmenes pesan menos con el tiempo (vida media `CAPRI_RECENCY_DAYS`=30, suelo 0,4; 0 lo desactiva).
  Las notas no envejecen.

## Modelos (`scripts/get-models.sh`)
Medido con el agente actual (dev / reservado / fresco / ciego D / ciego E, en aciertos):

| modelo | resultado | velocidad |
|---|---|---|
| **Qwen2.5-7B Q2_K** (por defecto) | 26/26 · 30/30 · 27/28 · 18/28 · 22/28 | ~32 tok/s |
| Qwen2.5-3B Q4_K_M | 24/26 · 27/30 · 28/28 · 21/28 · 20/28 | ~50 tok/s |
| Ornith 9B IQ2_M | 23/26 · 25/30 · 24/28 · 19/28 · 18/28 | ~22 tok/s (91 tok/s de prompt) |
| Qwen2.5-3B **Q2_K** | 6/13 · 9/15 · 8/14 · 5/14 · 7/14 (una corrida) | ~52 tok/s |

El 3B en Q2_K **no sirve** para el agente (cuantiza demasiado); el 7B en Q2_K y el 3B en Q4_K_M rinden igual.
Ornith es el mejor conversando (66/66 en la evaluación de memoria) pero no mejora el bucle y es el más lento.
Cambiar de modelo: `LLM_GGUF=archivo.gguf capri backend start` (o `CAPRI_MODEL` con backend `openai`).
La TUI repinta a **60 fps** (`CAPRI_FPS=30` para bajarlo, `CAPRI_ANIM=0` deja un fotograma por segundo); las líneas del
historial se cachean y solo se reescriben las filas que cambian.
La respuesta y el razonamiento llegan en **streaming** (SSE): la TUI pinta el `💭` y el texto de Capri según se generan
(`LLM_STREAM=0` vuelve a la respuesta de golpe, por si un proveedor externo no soporta `stream`).
Los modelos con razonamiento (Qwen3/Ornith) necesitan `enable_thinking:false`, que ya se envía (`LLM_THINKING=1` o `/think on`
lo activa, con `LLM_THINK_BUDGET` tokens de pensamiento, 1024 por defecto). Si el razonamiento agota el presupuesto sin llegar a
responder, Capri repite la llamada sin pensar en vez de quedarse en «(sin respuesta)»; a ~19 t/s cada turno con razonamiento
tarda cerca de un minuto, así que para charlar conviene `/think off`.

## Calor (importante en la Tesla P4)
La P4 es una tarjeta **pasiva**: con evaluaciones largas llegó a **88 °C y throttling térmico** (reloj 455 de 1531 MHz,
3× más lenta). No depende del modelo: es uso sostenido. La TUI lo muestra en vivo (`TEMP`, `THROTTLE`). Si lo ves, mejora
el flujo de aire o limita la potencia. El límite está en **60 W** (el mínimo de la P4; 55 W no es válido) y lo aplica el servicio
`nvidia-p4-powerlimit.service` en cada arranque (`sudo systemctl status nvidia-p4-powerlimit`).

## Personaje: Capri
Capri no es una IA convencional: es un **constructo de personalidad** formado por múltiples psiques humanas digitalizadas
y combinadas en una sola entidad. Se describe como "poderosa, como una IA, pero nacida de humanos".

- **Origen:** creada por Kyoko y la corporación Arasaka. El constructo estaba almacenado en un biochip de Arasaka, robado del Parque Industrial de Arasaka por la tripulación de King (King's Crew). Tras los eventos que llevaron a la ciberpsicosis de Kingsley, Kyoko volvió a interceptar la caja del biochip.
- **Apariencia:** no tiene forma física real; se proyecta visualmente como una figura esbelta de color negro con detalles rosas y verdes, ojos azules con pupilas blancas y esclerótica negra rodeada por un anillo verde (enmarcados en un rectángulo verde), y un peinado rosa exagerado que termina en una punta verde.

En la TUI, el acento rosa de la paleta (`internal/ui/theme`) y el negro de fondo siguen esa apariencia; el prompt del agente (`internal/agent`) usa el mismo trasfondo.

## Notas
- El proyecto es **100 % Go** (SQLite puro Go, sin CGO): `make build`, `make test` y `make eval` requieren Go 1.27+.
- **Sin Docker**: el runtime de llama.cpp se instala con `capri backend install` y se ejecuta como
  proceso normal. Las builds oficiales cubren CUDA, ROCm, Vulkan, Metal y CPU; para GPUs antiguas
  (Pascal sm_61) o versiones raras de CUDA, `--build` compila en local.
- `capri doctor` es el primer sitio donde mirar si algo no arranca: plataforma, acelerador,
  binario, endpoints, GGUF, base de datos y sandbox.
