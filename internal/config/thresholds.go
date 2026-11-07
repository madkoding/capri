package config

// Thresholds son los umbrales de similitud coseno que usan el enrutador, la
// memoria y las skills. Cada familia de embeddings distribuye sus similitudes
// de forma distinta (medido con pares de paráfrasis / sin relación y con el
// propio índice de intents y skills), así que se fijan por familia:
//
//	           sin relación   paráfrasis   hecho↔pregunta rel./no
//	nomic      0.52–0.75      0.70–0.95    0.59–0.81 / 0.59–0.64   (se solapan)
//	qwen3      0.14–0.29      0.63–0.98    0.54–0.86 / 0.25–0.34   (hueco limpio)
//
// Con nomic el kNN apenas discrimina: ahí mandan las reglas léxicas de intent, y
// las skills se seleccionan con ruido (0.67 deja pasar alguna ajena; más alto
// pierde las buenas). Qwen3-Embedding es el embedder por defecto por esto.
type Thresholds struct {
	Route       float64 // puntuación mínima para aceptar un intent del enrutador
	Destructive float64 // mínimo para intents que tocan la memoria (update/delete/list)
	Strong      float64 // un recuerdo entra al prompt por sí solo
	Fact        float64 // mínimo para un recuerdo (junto a Strong o a una pregunta personal)
	MinSim      float64 // suelo de la búsqueda vectorial del RAG
	Top         float64 // candidato a actualizar/olvidar en el camino rápido
	Skill       float64 // mínimo para usar una skill como ejemplo
	Dedupe      float64 // /dedupe: notas casi idénticas
	Dup         float64 // un hecho nuevo que ya estaba (no se vuelve a guardar)
}

var thresholds = map[string]Thresholds{
	"nomic": {Route: 0.6, Destructive: 0.75, Strong: 0.62, Fact: 0.45, MinSim: 0.45, Top: 0.5, Skill: 0.67, Dedupe: 0.92, Dup: 0.85},
	"qwen3": {Route: 0.45, Destructive: 0.6, Strong: 0.5, Fact: 0.42, MinSim: 0.4, Top: 0.45, Skill: 0.55, Dedupe: 0.9, Dup: 0.8},
}

// ThresholdsFor devuelve los umbrales de una familia (nomic si es desconocida).
func ThresholdsFor(family string) Thresholds {
	if t, ok := thresholds[family]; ok {
		return t
	}
	return thresholds["nomic"]
}
