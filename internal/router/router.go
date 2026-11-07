// Package router enruta la intención por embeddings (kNN con nomic).
package router

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"

	"capri/internal/embed"
)

// INTENTS son los ejemplos de cada intención.
var INTENTS = map[string][]string{
	"rag_add":    {"recuerda que mi perro se llama Rocky", "mi cumpleaños es el 3 de mayo", "me gusta el jazz", "anota que el servidor es 10.0.0.5", "guarda en tu memoria que la clave del wifi es casa123", "remember that I work at Acme", "soy vegetariano, tenlo en cuenta", "mi nombre es Ana y vivo en Temuco", "no olvides que tengo reunión los lunes"},
	"rag_search": {"qué sabes de mí", "cómo se llama mi gato", "qué te dije sobre mi proyecto", "busca en tu memoria algo sobre el servidor", "recuerdas cuál es mi comida favorita", "what do you know about my pets", "cuándo es mi cumpleaños", "dime lo que sabes de mis gustos"},
	"rag_list":   {"muestra todo lo que has guardado", "lista tus memorias", "qué hay en tu cerebro", "show everything you remember"},
	"rag_delete": {"olvida lo de mi perro", "borra esa memoria", "elimina la nota 3 de tu memoria", "forget what I said about my job"},
	"rag_update": {"corrige: mi perro ahora se llama Max", "actualiza la memoria sobre mi trabajo", "ya no vivo en Santiago, vivo en Valdivia", "update what you know about my address"},
	"fs_list":    {"lista los archivos", "qué archivos hay en la carpeta src", "ls", "muestra el contenido del directorio", "list the files here", "qué hay en esta carpeta", "muéstrame los archivos del proyecto"},
	"fs_read":    {"muestra el contenido de notas.txt", "lee el archivo config.json", "abre README.md y dime qué dice", "cat package.json", "show me the file main.py", "qué dice el archivo todo.md"},
	"fs_write":   {"crea un archivo hola.txt que diga hola mundo", "escribe en notas.txt: comprar pan", "guarda este texto en un archivo llamado a.md", "create a file todo.txt with my tasks", "genera un script run.sh que imprima la fecha", "crea el archivo index.html"},
	"fs_edit":    {"en config.json cambia el puerto 80 por 8080", "reemplaza foo por bar en main.js", "edita el archivo y cambia el título", "reemplaza gato por perro en historia.txt", "sustituye la palabra rojo por azul en estilos.css", "cambia la variable x por y en programa.js", "replace X with Y in app.py", "modifica la línea del puerto en el archivo"},
	"fs_delete":  {"borra el archivo viejo.txt", "elimina la carpeta tmp", "remove old.log", "delete the file a.txt", "quita el archivo temporal.txt"},
	"shell":      {"ejecuta el comando uname -a", "corre npm test", "ejecuta ls -la", "qué hora es, usa date", "run git status", "instala las dependencias con npm install", "cuánto espacio libre hay en disco", "haz un ping a google.com", "cuál es mi ip local", "crea una carpeta llamada fotos", "crea el directorio build", "mueve a.txt a la carpeta docs", "copia a.txt como b.txt", "renombra a.txt a b.txt", "cuánto pesa la carpeta node_modules", "qué versión de node tengo", "cuánta memoria ram libre hay", "qué procesos están corriendo"},
	"none":       {"qué modelo usas", "qué modelo de ia eres", "qué puedes hacer", "cuáles son tus herramientas", "sabes qué son los modelos heretic", "qué es un LLM", "qué es un modelo cuantizado", "conoces GSQ", "quién te creó", "hola capri", "hola, qué tal", "hola", "cómo estás", "cuéntame un chiste", "cuánto es 15 por 4", "qué es una API", "explícame qué es docker", "gracias", "traduce hello al español", "escribe un poema corto", "qué opinas del fútbol", "dame un consejo para estudiar", "resume brevemente qué es la fotosíntesis", "cuál es la capital de Francia", "quién escribió Don Quijote", "cuántos días tiene un año", "qué es la inteligencia artificial", "cómo se dice gato en inglés", "qué día fue ayer", "dame una receta de panqueques"},
}

// Rank es una intención con su puntuación.
type Rank struct {
	Intent string
	Score  float64
}

// Decision es el resultado del enrutado.
type Decision struct {
	Intent  string
	Score   float64
	Ranking []Rank
}

type indexItem struct {
	Intent string
	Vec    []float32
}

var (
	mu    sync.Mutex
	index []indexItem
)

func build(ctx context.Context) ([]indexItem, error) {
	var items []indexItem
	var texts []string
	for intent, examples := range INTENTS {
		for _, ex := range examples {
			items = append(items, indexItem{Intent: intent})
			texts = append(texts, ex)
		}
	}
	vecs, err := embed.Embed(ctx, texts, "query")
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Vec = vecs[i]
	}
	return items, nil
}

func dot(a, b []float32) float64 {
	var s float64
	for i := 0; i < len(a) && i < len(b); i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

var greetingHead = regexp.MustCompile(`(?i)^\s*(?:hola|buenas|buenos d[ií]as|buenas (?:tardes|noches)|hey|ey|oye)(?:[,!.]?\s*capri)?[,!.:\s]*`)

// Route enruta un texto a la intención más parecida (sin el saludo inicial).
func Route(ctx context.Context, txt string) (Decision, error) {
	if t := greetingHead.ReplaceAllString(txt, ""); strings.TrimSpace(t) != "" {
		txt = t
	}
	mu.Lock()
	if index == nil {
		idx, err := build(ctx)
		if err != nil {
			mu.Unlock()
			return Decision{}, err
		}
		index = idx
	}
	idx := index
	mu.Unlock()

	q, err := embed.Embed(ctx, []string{txt}, "query")
	if err != nil || len(q) == 0 {
		if err == nil {
			err = context.Canceled
		}
		return Decision{}, err
	}
	best := map[string]float64{}
	for _, it := range idx {
		score := dot(q[0], it.Vec)
		if old, ok := best[it.Intent]; !ok || score > old {
			best[it.Intent] = score
		}
	}
	ranking := make([]Rank, 0, len(best))
	for intent, score := range best {
		ranking = append(ranking, Rank{Intent: intent, Score: score})
	}
	sort.SliceStable(ranking, func(i, j int) bool { return ranking[i].Score > ranking[j].Score })
	if len(ranking) == 0 {
		return Decision{Intent: "none"}, nil
	}
	return Decision{Intent: ranking[0].Intent, Score: ranking[0].Score, Ranking: ranking}, nil
}
