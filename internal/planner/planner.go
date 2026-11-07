// Package planner define el formato de paso y comprueba la cobertura del objetivo.
package planner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"
)

// PlanTools son las herramientas válidas en un plan.
var PlanTools = []string{"fs_list", "fs_read", "fs_write", "fs_edit", "fs_delete", "shell", "web_search"}

// MaxSteps es el máximo de pasos de un plan.
const MaxSteps = 8

// Step es un paso parseado.
type Step struct {
	Tool   string            `json:"tool"`
	Args   map[string]string `json:"args"`
	OnFail bool              `json:"onFail"`
	OK     *bool             `json:"ok,omitempty"`
	Line   string            `json:"line,omitempty"`
}

// Gap es un hueco de cobertura.
type Gap struct {
	Kind    string  `json:"kind"`
	Hint    string  `json:"hint"`
	Suggest *string `json:"suggest"`
}

// EnvLine es una instantánea mínima del entorno.
func EnvLine(files []string, last string) string {
	list := "(vacía)"
	if len(files) > 0 {
		if len(files) > 30 {
			files = files[:30]
		}
		list = strings.Join(files, ", ")
	}
	out := fmt.Sprintf("Archivos en la carpeta de trabajo: %s.", list)
	if last != "" {
		out += fmt.Sprintf(" Último archivo usado: %s.", last)
	}
	return out
}

var planLineRe = regexp.MustCompile(`^(si-falla )?(\w+): ([\s\S]+)$`)

func validTool(t string) bool {
	for _, x := range PlanTools {
		if x == t {
			return true
		}
	}
	return false
}

// ParsePlan extrae los pasos de un texto con el formato «herramienta: argumentos».
func ParsePlan(txt string) []Step {
	var steps []Step
	for _, raw := range strings.Split(txt, "\n") {
		m := planLineRe.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil || !validTool(m[2]) {
			continue
		}
		tool, rest := m[2], m[3]
		parts := strings.Split(rest, " | ")
		var args map[string]string
		switch tool {
		case "fs_list", "fs_read", "fs_delete":
			args = map[string]string{"path": rest}
		case "shell":
			args = map[string]string{"command": rest}
		case "web_search":
			args = map[string]string{"query": rest}
		case "fs_write":
			content := ""
			if len(parts) > 1 {
				content = strings.Join(parts[1:], " | ")
			}
			args = map[string]string{"path": parts[0], "content": strings.ReplaceAll(content, `\n`, "\n")}
		case "fs_edit":
			find := ""
			if len(parts) > 1 {
				find = parts[1]
			}
			replace := ""
			if len(parts) > 2 {
				replace = strings.Join(parts[2:], " | ")
			}
			args = map[string]string{"path": parts[0], "find": find, "replace": replace}
		}
		steps = append(steps, Step{Tool: tool, Args: args, OnFail: m[1] != ""})
		if len(steps) == MaxSteps {
			break
		}
	}
	return steps
}

// COND_CUE indica condiciones explícitas en el objetivo.
var COND_CUE = regexp.MustCompile(`(?i)\b(si no|sino|si falla|si existe|en caso|de lo contrario)\b|\bsi\b[^,.]*\b(no|falla|existe|hay)\b`)

var (
	fileRe  = regexp.MustCompile(`[\w-]+\.[a-zA-Z]\w{1,7}\b`)
	extRe   = regexp.MustCompile(`(?:^|[\s(])(\.[a-zA-Z]\w{1,4})\b`)
	fileGRe = regexp.MustCompile(`[\w-]+(?:/[\w.-]+)*\.[a-zA-Z]\w{1,7}\b`)

	editVerb = regexp.MustCompile(`(?i)\b(cambia|reemplaza|sustituye|modifica|edita)\b`)
	createVb = regexp.MustCompile(`(?i)\b(crea|cr[eé]al[oa]|escribe|escr[ií]bel[oa]|genera|redacta|guarda|gu[aá]rdal[oa])\b`)
	copyVb   = regexp.MustCompile(`(?i)\b(copia\w*|con el contenido de)\b`)
	delVb    = regexp.MustCompile(`(?i)\b(borra|elimina|quita)\b`)
	listVb   = regexp.MustCompile(`(?i)\b(lista|listar)\b`)
	runVb    = regexp.MustCompile(`(?i)\b(ejec[uú]ta(?:lo|la)?|c[oó]rre(?:lo|la)?|corre|lanza(?:lo)?)\b`)
	wordsVb  = regexp.MustCompile(`(?i)\bcu[aá]ntas palabras\b`)
	sumVb    = regexp.MustCompile(`(?i)\b(suma|sumar|promedio|total de|m[aá]ximo|m[ií]nimo)\b`)
	linesVb  = regexp.MustCompile(`(?i)\bcu[aá]ntas l[ií]neas\b`)
	countVb  = regexp.MustCompile(`(?i)\bcu[aá]nt[oa]s\b`)
	timesVb  = regexp.MustCompile(`(?i)\bcu[aá]ntas veces\b`)
	rowsVb   = regexp.MustCompile(`(?i)\bcu[aá]nt[oa]s (filas|registros|nombres|elementos|items|entradas)\b`)
	bigVb    = regexp.MustCompile(`(?i)\bm[aá]s (grande|pesado)\b`)
	smallVb  = regexp.MustCompile(`(?i)\bm[aá]s peque[nñ]o\b`)
	recentVb = regexp.MustCompile(`(?i)\bm[aá]s reciente\b`)
	oldVb    = regexp.MustCompile(`(?i)\bm[aá]s (antiguo|viejo)\b`)
	showVb   = regexp.MustCompile(`(?i)\b(mu[eé]stra\w*|muestra\w*|ens[eé]ñ\w*|l[eé]e(?:lo|la)?|lee(?:lo|la)?|imprime\w*|cat)\b`)
	folderVb = regexp.MustCompile(`(?i)\b(carpetas?|directorios?)\b`)
	appendVb = regexp.MustCompile(`(?i)\b(a[ñn]ade|agrega|anexa|a[ñn]adir|agregar)\b.*\bal final\b`)
	mutating = regexp.MustCompile(`\b(rm|mv|cp|sed -i|mkdir|touch|tee)\b|>>?`)
	reading  = regexp.MustCompile(`\b(cat|head|tail|less|more|wc|grep|awk)\b`)
)

// folderNames extrae los nombres de carpeta pedidos («tres carpetas llamadas a, b y c»).
func folderNames(g string) []string {
	m := regexp.MustCompile(`(?i)(?:carpetas?|directorios?)(?: nuevas?| vac[ií]as?)?(?: llamad[ao]s?| con (?:los )?nombres?| de nombre)?[:\s]+([\w./-]+(?:\s*(?:,|\by\b|\be\b)\s*[\w./-]+)*)`).FindStringSubmatch(g)
	if m == nil {
		return nil
	}
	stop := map[string]bool{"que": true, "dentro": true, "con": true, "en": true, "luego": true, "después": true, "despues": true, "la": true, "el": true, "un": true, "una": true}
	var out []string
	for _, tok := range regexp.MustCompile(`[\w./-]+`).FindAllString(m[1], -1) {
		low := strings.ToLower(tok)
		if low == "y" || low == "e" {
			continue
		}
		if stop[low] {
			break
		}
		if fileRe.MatchString(tok) { // eso es un archivo, no una carpeta
			break
		}
		out = append(out, tok)
		if len(out) == 5 {
			break
		}
	}
	return out
}

// lastMutation devuelve el índice del último paso que modificó archivos (-1 si ninguno).
func lastMutation(steps []Step) int {
	last := -1
	for i, st := range steps {
		switch st.Tool {
		case "fs_write", "fs_edit", "fs_delete":
			last = i
		case "shell":
			if mutating.MatchString(st.Args["command"]) {
				last = i
			}
		}
	}
	return last
}

// shownAfter indica si tras el último cambio hubo una lectura (fs_read, fs_list o shell de lectura).
func shownAfter(steps []Step) bool {
	last := lastMutation(steps)
	for i := last + 1; i < len(steps); i++ {
		st := steps[i]
		if st.Tool == "fs_read" || st.Tool == "fs_list" || (st.Tool == "shell" && reading.MatchString(st.Args["command"]) && !mutating.MatchString(st.Args["command"])) {
			return true
		}
	}
	return false
}

// mutationVerbIndex devuelve dónde aparece el primer verbo de cambio del objetivo (-1 si no hay).
func mutationVerbIndex(g string) int {
	idx := -1
	for _, re := range []*regexp.Regexp{editVerb, createVb, delVb, copyVb, appendVb} {
		if m := re.FindStringIndex(g); m != nil && (idx < 0 || m[0] < idx) {
			idx = m[0]
		}
	}
	return idx
}

// listedAfter indica si tras el último cambio hubo un listado (fs_list o ls).
func listedAfter(steps []Step) bool {
	last := lastMutation(steps)
	for i := last + 1; i < len(steps); i++ {
		st := steps[i]
		if st.OK != nil && !*st.OK {
			continue
		}
		if st.Tool == "fs_list" || (st.Tool == "shell" && regexp.MustCompile(`\bls\b|\bfind\b`).MatchString(st.Args["command"]) && !mutating.MatchString(st.Args["command"])) {
			return true
		}
	}
	return false
}

// readOnlyFile indica si, en un objetivo de crear, un archivo nombrado es solo
// fuente («crea b.txt con el contenido de a.txt», «lee a.txt y guarda … en b.txt»).
func readOnlyFile(g, f string) bool {
	i := strings.Index(g, f)
	if i < 0 {
		return false
	}
	before := strings.ToLower(g[:i])
	return regexp.MustCompile(`(?i)\b(lee|l[eé]e\w*|de|desde|contenido de|a partir de|seg[uú]n|usando)\s*(?:el |la |el archivo )?$`).MatchString(before)
}

// deletedFile indica si un archivo nombrado se borró (fs_delete o shell rm).
func deletedFile(steps []Step, f string) bool {
	for _, st := range steps {
		if st.Tool == "fs_delete" && strings.Contains(st.Args["path"], f) {
			return true
		}
		if st.Tool == "shell" && regexp.MustCompile(`\brm\b`).MatchString(st.Args["command"]) && strings.Contains(st.Args["command"], f) {
			return true
		}
	}
	return false
}

// writtenFile indica si un archivo nombrado se creó (fs_write o shell que lo nombra).
func writtenFile(steps []Step, f string) bool {
	for _, st := range steps {
		if st.Tool == "fs_write" && strings.Contains(st.Args["path"], f) {
			return true
		}
		if st.Tool == "shell" && mutating.MatchString(st.Args["command"]) && strings.Contains(st.Args["command"], f) {
			return true
		}
	}
	return false
}

var (
	contentAfterRe = regexp.MustCompile(`(?i)^\s*(?:con|que diga|que contenga|que tenga|cuyo contenido sea)\s+(?:la palabra|el texto|la frase|el contenido|la l[ií]nea|las palabras)?\s*["'«]?([^"'»]+?)["'»]?\s*(?:[,.;]|$|\s+(?:y|e|después|despu[eé]s|luego)\s)`)
	contentAnyRe   = regexp.MustCompile(`(?i)\bcr[eé]alo con\s+(?:la palabra|el texto|la frase|el contenido|la l[ií]nea)?\s*["'«]?([^"'»]+?)["'»]?\s*(?:[,.;]|$)`)
	dateWord       = regexp.MustCompile(`(?i)\b(fecha|d[ií]a de hoy)\b`)
	timeWord       = regexp.MustCompile(`(?i)\bhora\b`)
)

// contentFor extrae del objetivo el contenido pedido para un archivo
// («crea x.txt con hola», «créalo con la palabra nuevo»). "" si no está claro.
func contentFor(g, file string) string {
	if i := strings.Index(g, file); i >= 0 {
		if m := contentAfterRe.FindStringSubmatch(g[i+len(file):]); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	if m := contentAnyRe.FindStringSubmatch(g); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// shownFile elige el archivo que hay que mostrar: el que sigue al verbo, o el último del objetivo.
func shownFile(g string) string {
	if m := regexp.MustCompile(`(?i)(?:mu[eé]stra\w*|muestra\w*|ens[eé]ñ\w*|lee|l[eé]e|imprime\w*)\s+(?:el\s+|la\s+|el archivo\s+|el contenido de\s+)?([\w./-]+\.[a-zA-Z]\w{1,7})`).FindStringSubmatch(g); m != nil {
		return m[1]
	}
	f := filesOf(g)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

func filesOf(g string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range fileGRe.FindAllString(g, -1) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func extOf(g string) string {
	m := extRe.FindStringSubmatch(g)
	if m == nil {
		return ""
	}
	return m[1]
}

func hasAny(steps []Step, tools ...string) bool {
	for _, st := range steps {
		for _, t := range tools {
			if st.Tool == t {
				return true
			}
		}
	}
	return false
}

func shellHas(rx *regexp.Regexp) func([]Step, string) bool {
	return func(steps []Step, _ string) bool {
		for _, st := range steps {
			if st.Tool == "shell" && rx.MatchString(st.Args["command"]) {
				return true
			}
		}
		return false
	}
}

func argsJSON(st Step) string {
	b, _ := json.Marshal(st.Args)
	return string(b)
}

func strptr(s string) *string { return &s }

type runnerRule struct{}

var runners = map[string]string{"py": "python3", "sh": "bash", "js": "node", "rb": "ruby", "pl": "perl"}

func runnerFor(f string) string {
	parts := strings.Split(f, ".")
	return runners[parts[len(parts)-1]]
}

var runnerShell = regexp.MustCompile(`(?i)\b(python3?|bash|sh|node|ruby|perl)\b|\./`)

var (
	lsBigRe    = regexp2.MustCompile(`\bls\b[^|]*-\w*S\w*(?<!r)\b(?!r)`, 0)
	lsSmallRe  = regexp2.MustCompile(`\bls\b.*-\w*S\w*r|-\w*rS`, 0)
	lsRecentRe = regexp2.MustCompile(`\bls\b[^|]*-\w*t\w*(?<!r)\b`, 0)
	lsOldRe    = regexp2.MustCompile(`\bls\b.*-\w*tr|-\w*rt`, 0)
)

func shellMatch(re *regexp2.Regexp) func([]Step, string) bool {
	return func(steps []Step, _ string) bool {
		for _, st := range steps {
			if st.Tool == "shell" {
				if ok, _ := re.MatchString(st.Args["command"]); ok {
					return true
				}
			}
		}
		return false
	}
}

type coverageRule struct {
	kind     string
	when     func(string) bool
	any      []string
	allSteps bool
	ok       func([]Step, string) bool
	hint     func(string) string
	suggest  func(string) *string
	// suggestNext decide el paso mirando lo ya hecho (p. ej. el primer archivo que falta)
	suggestNext func(string, []Step) *string
}

func coverage() []coverageRule {
	return []coverageRule{
		{
			kind: "fidelity",
			when: func(g string) bool { return len(filesOf(g)) > 0 },
			any:  PlanTools, allSteps: true,
			ok: func(steps []Step, g string) bool {
				for _, f := range filesOf(g) {
					found := false
					for _, st := range steps {
						if strings.Contains(argsJSON(st), f) {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
				return true
			},
			hint: func(g string) string {
				return fmt.Sprintf("usa EXACTAMENTE estos nombres de archivo del usuario, completos y sin cambiarlos: %s.", strings.Join(filesOf(g), ", "))
			},
		},
		{
			when: func(g string) bool {
				return editVerb.MatchString(g) && fileRe.MatchString(g) && regexp.MustCompile(`(?i) por `).MatchString(g)
			},
			any: []string{"fs_edit"},
			hint: func(string) string {
				return "el objetivo pide CAMBIAR texto dentro de un archivo: incluye un paso fs_edit (archivo | texto a buscar | texto nuevo)."
			},
		},
		{
			when: func(g string) bool {
				return createVb.MatchString(g) && fileRe.MatchString(g) &&
					!regexp.MustCompile(`(?i)\b(memoria|recuerd)`).MatchString(g) &&
					!copyVb.MatchString(g)
			},
			any: []string{"fs_write", "shell"},
			ok: func(steps []Step, g string) bool {
				for _, f := range filesOf(g) {
					if !writtenFile(steps, f) && !readOnlyFile(g, f) {
						return false
					}
				}
				return true
			},
			hint: func(string) string {
				return "el objetivo pide CREAR o GUARDAR un archivo: incluye un paso fs_write (archivo | contenido)."
			},
			suggestNext: func(g string, steps []Step) *string {
				for _, f := range filesOf(g) {
					if c := contentFor(g, f); c != "" && !writtenFile(steps, f) {
						return strptr("fs_write: " + f + " | " + c)
					}
				}
				return nil
			},
		},
		{
			when: func(g string) bool { return copyVb.MatchString(g) && len(filesOf(g)) >= 2 },
			any:  []string{"shell", "fs_write"},
			ok: func(steps []Step, _ string) bool {
				for _, st := range steps {
					if st.Tool == "shell" && regexp.MustCompile(`\bcp\b`).MatchString(st.Args["command"]) {
						return true
					}
				}
				ri, wi := -1, -1
				for i, st := range steps {
					if st.Tool == "fs_read" && ri < 0 {
						ri = i
					}
					if st.Tool == "fs_write" && wi < 0 {
						wi = i
					}
				}
				return ri >= 0 && wi >= 0 && ri < wi
			},
			hint: func(string) string { return "para COPIAR un archivo usa shell: cp origen destino." },
			suggest: func(g string) *string {
				f := filesOf(g)
				return strptr(fmt.Sprintf("shell: cp %s %s", f[0], f[len(f)-1]))
			},
		},
		{
			when: func(g string) bool {
				// borrar archivos, no «borra la línea 3 de a.txt» ni recuerdos
				return delVb.MatchString(g) && !regexp.MustCompile(`(?i)\b(memoria|recuerd\w*|l[ií]neas?|palabras?|texto|contenido|parte|car[aá]cter\w*)\b`).MatchString(g)
			},
			any: []string{"fs_delete", "shell"},
			ok: func(steps []Step, g string) bool {
				if files := filesOf(g); len(files) > 0 {
					for _, f := range files {
						if !deletedFile(steps, f) {
							return false
						}
					}
					return true
				}
				if !regexp.MustCompile(`(?i)\btodos?\b|\blos \.\w+`).MatchString(g) {
					return true
				}
				for _, st := range steps {
					if st.Tool == "shell" && regexp.MustCompile(`\brm\b|\bfind\b.*-delete`).MatchString(st.Args["command"]) {
						return true
					}
				}
				return false
			},
			hint: func(g string) string {
				if f := filesOf(g); len(f) > 0 {
					return fmt.Sprintf("el objetivo pide BORRAR %s: incluye fs_delete: archivo.", strings.Join(f, ", "))
				}
				return "el objetivo pide BORRAR varios archivos: usa shell con rm -f patrón (fs_delete no admite comodines)."
			},
			suggestNext: func(g string, steps []Step) *string {
				for _, f := range filesOf(g) {
					if !deletedFile(steps, f) {
						return strptr("fs_delete: " + f)
					}
				}
				return nil
			},
			suggest: func(g string) *string {
				if len(filesOf(g)) > 0 {
					return nil
				}
				if m := regexp.MustCompile(`\.\w{2,5}\b`).FindString(g); m != "" {
					return strptr("shell: rm -f *" + m)
				}
				if regexp.MustCompile(`(?i)\btodos\b`).MatchString(g) {
					return strptr("shell: find . -maxdepth 1 -type f -delete")
				}
				return nil
			},
		},
		{
			// «…y muéstralo» / «léelo»: la lectura tiene que ser POSTERIOR al último cambio,
			// si no el usuario ve el contenido viejo
			when: func(g string) bool {
				if !showVb.MatchString(g) || !(fileRe.MatchString(g) || regexp.MustCompile(`(?i)\b(mu[eé]stralo|l[eé]elo|muéstrala|léela)\b`).MatchString(g)) {
					return false
				}
				// «lee a.txt y guarda…»: esa lectura es la fuente, no mostrar el resultado
				show := showVb.FindStringIndex(g)
				mut := mutationVerbIndex(g)
				return mut < 0 || show[0] > mut
			},
			any: PlanTools, allSteps: true,
			ok: func(steps []Step, _ string) bool { return shownAfter(steps) }, // sin cambios, cualquier lectura vale
			hint: func(g string) string {
				return "el objetivo pide MOSTRAR o LEER el resultado: incluye fs_read del archivo DESPUÉS del último cambio."
			},
			suggest: func(g string) *string {
				if f := shownFile(g); f != "" {
					return strptr("fs_read: " + f)
				}
				return nil
			},
		},
		{
			// crear carpetas: mkdir -p con los nombres pedidos
			when: func(g string) bool {
				return createVb.MatchString(g) && folderVb.MatchString(g) && len(folderNames(g)) > 0
			},
			any: []string{"shell", "fs_write"},
			ok: func(steps []Step, g string) bool {
				for _, name := range folderNames(g) {
					found := false
					for _, st := range steps {
						if (st.Tool == "shell" && regexp.MustCompile(`\bmkdir\b`).MatchString(st.Args["command"]) && strings.Contains(st.Args["command"], name)) ||
							(st.Tool == "fs_write" && strings.HasPrefix(st.Args["path"], name+"/")) {
							found = true
							break
						}
					}
					if !found {
						return false
					}
				}
				return true
			},
			hint: func(g string) string {
				return fmt.Sprintf("el objetivo pide CREAR CARPETAS (%s): usa shell: mkdir -p nombres.", strings.Join(folderNames(g), ", "))
			},
			suggest: func(g string) *string { return strptr("shell: mkdir -p " + strings.Join(folderNames(g), " ")) },
		},
		{
			// añadir al final: nunca fs_write (sobrescribe); shell >> o fs_edit
			when: func(g string) bool { return appendVb.MatchString(g) && fileRe.MatchString(g) },
			any:  []string{"shell", "fs_edit"},
			ok: func(steps []Step, _ string) bool {
				for _, st := range steps {
					if st.Tool == "fs_edit" || (st.Tool == "shell" && strings.Contains(st.Args["command"], ">>")) {
						return true
					}
				}
				return false
			},
			hint: func(string) string {
				return "para AÑADIR al final de un archivo sin perder lo que tiene usa shell: echo 'texto' >> archivo (fs_write lo sobrescribiría)."
			},
			suggest: func(g string) *string {
				m := regexp.MustCompile(`(?i)(?:palabra|texto|l[ií]nea|frase)\s+["'«]?([^"'»]+?)["'»]?\s+al final`).FindStringSubmatch(g)
				if m == nil {
					return nil
				}
				return strptr(fmt.Sprintf("shell: echo '%s' >> %s", strings.ReplaceAll(strings.TrimSpace(m[1]), "'", ""), filesOf(g)[0]))
			},
		},
		{
			// «…y lista los archivos»: el listado tiene que ser POSTERIOR al último cambio
			when: func(g string) bool {
				return listVb.MatchString(g) && !regexp.MustCompile(`(?i)\b(memoria|recuerd|notas?)\b`).MatchString(g)
			},
			any:      PlanTools,
			allSteps: true,
			ok:       func(steps []Step, _ string) bool { return listedAfter(steps) },
			hint: func(string) string {
				return "el objetivo pide LISTAR archivos: incluye fs_list DESPUÉS del último cambio."
			},
			suggest: func(string) *string { return strptr("fs_list: .") },
		},
		{
			// la hora o la fecha solo salen de shell: date
			when: func(g string) bool {
				t := fileRe.ReplaceAllString(g, "")
				return (dateWord.MatchString(t) || timeWord.MatchString(t)) && regexp.MustCompile(`(?i)\b(dime|qu[eé]|cu[aá]l|guarda\w*|gu[aá]rda\w*|escribe\w*|escr[ií]be\w*|anota\w*|crea\w*|pon\w*)\b`).MatchString(t)
			},
			any:  []string{"shell"},
			ok:   shellHas(regexp.MustCompile(`\bdate\b`)),
			hint: func(string) string { return "la hora y la fecha reales salen de shell: date (no las inventes)." },
			suggest: func(g string) *string {
				t := fileRe.ReplaceAllString(g, "")
				if timeWord.MatchString(t) && !dateWord.MatchString(t) {
					return strptr("shell: date +%H:%M")
				}
				return strptr("shell: date +%F")
			},
		},
		{
			when: func(g string) bool { return runVb.MatchString(g) },
			any:  []string{"shell"},
			ok: func(steps []Step, g string) bool {
				for _, f := range filesOf(g) {
					if runnerFor(f) != "" {
						return runnerShell.MatchString(commandOf(steps))
					}
				}
				return true
			},
			hint: func(string) string {
				return "el objetivo pide EJECUTAR algo: incluye un paso shell con el comando (por ejemplo python3 hola.py)."
			},
			suggest: func(g string) *string {
				for _, f := range filesOf(g) {
					if r := runnerFor(f); r != "" {
						return strptr(fmt.Sprintf("shell: %s %s", r, f))
					}
				}
				return nil
			},
		},
		{
			when: func(g string) bool { return wordsVb.MatchString(g) && fileRe.MatchString(g) },
			any:  []string{"shell"},
			ok:   shellHas(regexp.MustCompile(`\bwc\s+-w\b`)),
			hint: func(string) string { return "para contar PALABRAS de un archivo usa wc -w < archivo." },
			suggest: func(g string) *string {
				return strptr(fmt.Sprintf("shell: wc -w < %s", filesOf(g)[0]))
			},
		},
		{
			when: func(g string) bool {
				return sumVb.MatchString(g) && regexp.MustCompile(`(?i)n[uú]meros|valores|columna`).MatchString(g) && fileRe.MatchString(g)
			},
			any: []string{"shell"},
			ok:  shellHas(regexp.MustCompile(`\bawk\b|\bsort\b|\bpaste\b|\bbc\b`)),
			hint: func(string) string {
				return "el modelo no sabe calcular: usa shell con awk sobre el archivo (por ejemplo awk '{s+=$1} END {print s}' archivo)."
			},
			suggest: func(g string) *string {
				f := filesOf(g)[0]
				if regexp.MustCompile(`(?i)promedio`).MatchString(g) {
					return strptr(fmt.Sprintf("shell: awk '{s+=$1} END {print s/NR}' %s", f))
				}
				return strptr(fmt.Sprintf("shell: awk '{s+=$1} END {print s}' %s", f))
			},
		},
		{
			when: func(g string) bool { return linesVb.MatchString(g) && fileRe.MatchString(g) },
			any:  []string{"shell"},
			ok: func(steps []Step, _ string) bool {
				rx := regexp.MustCompile(`\bwc\s+-l\s+\S+|\bgrep\s+-c\b|<\s*\S+`)
				bad := regexp.MustCompile(`\bls\b[^|]*\|\s*wc`)
				for _, st := range steps {
					if st.Tool == "shell" && rx.MatchString(st.Args["command"]) && !bad.MatchString(st.Args["command"]) {
						return true
					}
				}
				return false
			},
			hint: func(string) string { return "para contar LÍNEAS de un archivo usa wc -l < archivo (no ls | wc)." },
			suggest: func(g string) *string {
				return strptr(fmt.Sprintf("shell: wc -l < %s", filesOf(g)[0]))
			},
		},
		{
			when: func(g string) bool {
				return countVb.MatchString(g) && extOf(g) != "" && !fileRe.MatchString(g) &&
					!regexp.MustCompile(`(?i)palabras|l[ií]neas|veces`).MatchString(g)
			},
			any: []string{"shell"},
			ok: func(steps []Step, g string) bool {
				for _, st := range steps {
					if st.Tool == "shell" && strings.Contains(st.Args["command"], extOf(g)) {
						return true
					}
				}
				return false
			},
			hint: func(string) string {
				return "el comando de conteo debe FILTRAR por la extensión pedida, por ejemplo ls *.txt | wc -l."
			},
			suggest: func(g string) *string {
				return strptr(fmt.Sprintf("shell: ls *%s 2>/dev/null | wc -l", extOf(g)))
			},
		},
		{
			when: func(g string) bool { return timesVb.MatchString(g) && fileRe.MatchString(g) },
			any:  []string{"shell"},
			ok:   shellHas(regexp.MustCompile(`\bgrep\b.*-\w*[oc]`)),
			hint: func(string) string {
				return "para contar APARICIONES de una palabra usa shell: grep -o palabra archivo | wc -l."
			},
			suggest: func(g string) *string {
				w := regexp.MustCompile(`(?i)(?:palabra|texto|cadena)\s+["']?([\w-]+)|aparece\s+["']?([\w-]+)`).FindStringSubmatch(g)
				if w == nil {
					return nil
				}
				word := w[1]
				if word == "" {
					word = w[2]
				}
				if word == "" {
					return nil
				}
				return strptr(fmt.Sprintf("shell: grep -o %s %s | wc -l", word, filesOf(g)[0]))
			},
		},
		{
			when: func(g string) bool { return rowsVb.MatchString(g) && fileRe.MatchString(g) },
			any:  []string{"shell"},
			ok:   shellHas(regexp.MustCompile(`\bwc\s+-l\b|\bgrep\s+-c\b|\bawk\b`)),
			hint: func(string) string {
				return "para contar filas/nombres de un archivo usa shell: wc -l < archivo (resta 1 si hay encabezado: tail -n +2 archivo | wc -l)."
			},
			suggest: func(g string) *string {
				f := filesOf(g)[0]
				if regexp.MustCompile(`(?i)sin (contar )?(el )?(encabezado|cabecera)`).MatchString(g) {
					return strptr(fmt.Sprintf("shell: tail -n +2 %s | wc -l", f))
				}
				return strptr(fmt.Sprintf("shell: wc -l < %s", f))
			},
		},
		{
			when: func(g string) bool {
				return countVb.MatchString(g) && !regexp.MustCompile(`(?i)palabras|l[ií]neas`).MatchString(g)
			},
			any: []string{"shell"},
			hint: func(string) string {
				return "el objetivo pide CONTAR: usa un comando shell con wc -l (el modelo no sabe contar)."
			},
		},
		{
			when:    func(g string) bool { return bigVb.MatchString(g) },
			any:     []string{"shell"},
			ok:      shellMatch(lsBigRe),
			hint:    func(string) string { return "el MÁS grande: shell con ls -S | head -1." },
			suggest: func(string) *string { return strptr("shell: ls -S | head -1") },
		},
		{
			when:    func(g string) bool { return smallVb.MatchString(g) },
			any:     []string{"shell"},
			ok:      shellMatch(lsSmallRe),
			hint:    func(string) string { return "el MÁS pequeño: shell con ls -Sr | head -1." },
			suggest: func(string) *string { return strptr("shell: ls -Sr | head -1") },
		},
		{
			when:    func(g string) bool { return recentVb.MatchString(g) },
			any:     []string{"shell"},
			ok:      shellMatch(lsRecentRe),
			hint:    func(string) string { return "el MÁS reciente: shell con ls -t | head -1." },
			suggest: func(string) *string { return strptr("shell: ls -t | head -1") },
		},
		{
			when:    func(g string) bool { return oldVb.MatchString(g) },
			any:     []string{"shell"},
			ok:      shellMatch(lsOldRe),
			hint:    func(string) string { return "el MÁS antiguo: shell con ls -tr | head -1." },
			suggest: func(string) *string { return strptr("shell: ls -tr | head -1") },
		},
	}
}

func commandOf(steps []Step) string {
	for _, st := range steps {
		if st.Tool == "shell" {
			return st.Args["command"]
		}
	}
	return ""
}

// GapsOf devuelve los huecos de cobertura con su pista y el paso sugerido.
func GapsOf(goal string, steps []Step) []Gap {
	var good []Step
	for _, st := range steps {
		if st.OK == nil || *st.OK {
			good = append(good, st)
		}
	}
	var out []Gap
	for _, c := range coverage() {
		if !c.when(goal) {
			continue
		}
		base := good
		if c.allSteps {
			base = steps
		}
		satisfied := hasAny(base, c.any...)
		if satisfied && c.ok != nil {
			satisfied = c.ok(base, goal)
		}
		if satisfied {
			continue
		}
		g := Gap{Kind: c.kind, Hint: c.hint(goal)}
		if g.Kind == "" {
			g.Kind = "action"
		}
		if c.suggestNext != nil {
			g.Suggest = c.suggestNext(goal, base)
		}
		if g.Suggest == nil && c.suggest != nil {
			g.Suggest = c.suggest(goal)
		}
		out = append(out, g)
	}
	return out
}

// CoverageGaps devuelve solo las pistas de lo que falta.
func CoverageGaps(goal string, steps []Step) []string {
	var out []string
	for _, g := range GapsOf(goal, steps) {
		out = append(out, g.Hint)
	}
	return out
}
