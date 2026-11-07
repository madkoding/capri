// Package intent contiene reglas léxicas que acompañan al router por embeddings y las guardas de seguridad.
package intent

import (
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"

	"capri/internal/config"
	"capri/internal/learn"
)

// Los umbrales viven en config.Thresholds (por familia de embeddings).
func thr() config.Thresholds { return config.Get().Thr }

var memoryWrites = map[string]bool{"rag_add": true, "rag_update": true, "rag_delete": true}

var (
	questionRe = regexp.MustCompile(`^\s*¿|\?\s*$`)

	cleanTail = regexp.MustCompile(`(?i)[,.;:]?\s*\b(?:an[oó]ta(?:lo|me)?|apunta(?:lo)?|recu[eé]rda(?:lo|me)?|no olvides(?: eso)?|ten en cuenta(?: eso)?)\b[.!\s]*$`)
	cleanHead = regexp.MustCompile(`(?i)^\s*(?:recuerda|anota|apunta|no olvides|ten en cuenta)(?: que)?[:,]?\s+`)

	correctionRe = regexp.MustCompile(`(?i)\b(corrige|corrijo|actualiza|actualizar|ya no|en realidad|me equivoqu[eé]|no es .+ sino|cambia(?:r)? (?:lo de|mi)|update|correction)\b`)
	forgetRe     = regexp.MustCompile(`(?i)\b(olvida|olv[ií]date|borra(?:r)?|elimina(?:r)?|quita(?:r)?|forget|delete)\b`)
	editVerbRe   = regexp.MustCompile(`(?i)\b(cambia|reemplaza|sustituye|modifica|edita)\b`)
	createVerbRe = regexp.MustCompile(`(?i)\b(crea|escribe|genera|redacta)\b`)
	hasFileRe    = regexp.MustCompile(`[\w-]+\.\w{1,8}\b`)
	rememberRe   = regexp.MustCompile(`(?i)\b(an[oó]ta(?:lo|me|lo por favor)?|apunta(?:lo)?|recu[eé]rda(?:lo|me)?|acu[eé]rdate|no olvides|ten en cuenta|gu[aá]rda(?:lo|me)? (?:en (?:tu )?memoria|esto|eso|que)|remember)\b`)
	compoundRe   = regexp.MustCompile(`(?i)\b(y luego|y despu[eé]s|luego|despu[eé]s|dentro|si no|sino|y (?:dime|d[eé]cime|mu[eé]stra\w*|l[eé]e\w*|lista\w*|crea\w*|borra\w*|b[oó]rra\w*|elimina\w*|elim[ií]na\w*|guarda\w*|gu[aá]rda\w*|copia\w*|c[oó]pia\w*|ejec[uú]ta\w*|escribe\w*|cambia\w*|edita\w*|ed[ií]ta\w*|abre\w*|[aá]bre\w*|c[oó]rre\w*|corre\w*|renombra\w*|mueve\w*|cuenta\w*|compara\w*))\b`)
	envQuestion  = regexp.MustCompile(`(?i)^\W*(cu[aá]nt[oa]s?|cu[aá]l(?:es)?|qu[eé]|d[oó]nde|hay|dime)\b`)
	envThing     = regexp.MustCompile(`(?i)\b(archivos?|carpetas?|directorios?|disco|procesos?|espacio|comando|\w+\.\w{2,5})\b`)
	computeRe    = regexp.MustCompile(`(?i)\b(suma|sumar|promedio|calcula|cuenta|total de)\b`)
	memoryTalkRe = regexp.MustCompile(`(?i)\b(memoria|recuerd\w*|olvida\w*|an[oó]ta\w*|apunta\w*|cerebro)\b`)
	answerCueRe  = regexp.MustCompile(`(?i)\b(dime|dec[ií]me|qué|cu[aá]l(?:es)?|cu[aá]nt[oa]s?|d[oó]nde|cu[aá]ndo|qui[eé]n|hay|resume|explica|suma|promedio)\b|\bcómo\b|\?`)
	// pistas de que el mensaje pide actuar sobre el equipo (sin ellas, nunca se usan herramientas)
	actionVerbRe = regexp.MustCompile(`(?i)\b(crea\w*|escrib\w*|genera\w*|redacta\w*|guarda\w*|gu[aá]rda\w*|lista\w*|muestra\w*|mu[eé]stra\w*|ens[eé]ñ\w*|lee|l[eé]e\w*|abre|[aá]bre\w*|borra\w*|b[oó]rra\w*|elimina\w*|elim[ií]na\w*|quita\w*|ejecuta\w*|ejec[uú]ta\w*|corre|c[oó]rre\w*|lanza\w*|copia\w*|c[oó]pia\w*|mueve\w*|renombra\w*|cambia\w*|reemplaza\w*|sustituye\w*|modifica\w*|edita\w*|ed[ií]ta\w*|instala\w*|compila\w*|a[ñn]ade\w*|agrega\w*|anexa\w*|descomprime\w*|comprime\w*|haz un ping)\b`)
	sysInfoRe    = regexp.MustCompile(`(?i)\b(qu[eé] hora es|la hora actual|qu[eé] fecha es|la fecha de hoy|fecha actual|espacio (libre|en disco)|memoria ram|cu[aá]nta memoria|qu[eé] versi[oó]n|mi ip\b|procesos (corriendo|activos|en ejecuci[oó]n)|uptime|cu[aá]nto pesa|cu[aá]nto ocupa)\b`)
	cmdTokenRe   = regexp.MustCompile(`(?i)^\s*(ls|cat|grep|git|npm|pip3?|python3?|node|date|uname|df|du|ps|ping|curl|wget|wc|find|mkdir|rm|cp|mv|touch|sed|awk|head|tail|chmod|tar|unzip|make|cargo|docker)(\s|$)`)
	objectRe     = regexp.MustCompile(`(?i)["'\x60«]|\*\.\w{1,5}|\.\w{2,5}\b|\b(todos?|todas?) (los|las)\b|\b(archivos?|carpetas?|directorios?|ficheros?)\b`)
	trivialRe    = regexp.MustCompile(`(?i)^\W*(hola|buenas|buenos d[ií]as|buenas (tardes|noches)|gracias|ok|vale|listo|perfecto|genial|chao|adi[oó]s|s[ií]|no)\W*$`)

	personalRe = regexp2.MustCompile(`(?i)(?<![\p{L}])(m[ií]|mis?|m[ií]os?|m[ií]as?|me|yo|soy|estoy|tengo|vivo|trabajo|llamo|gusta|gustan|prefiero|sabes de m[ií]|recuerdas|te (dije|cont[eé])|conmigo|hablamos|dijimos|hicimos|hiciste|antes|ayer|resumen|conversaci[oó]n)(?![\p{L}])`, 0)
)

// MEMORY_TALK detecta órdenes sobre la memoria.
var MEMORY_TALK = memoryTalkRe

// ANSWER_CUE indica que el objetivo pide un dato.
var ANSWER_CUE = answerCueRe

// TRIVIAL detecta saludos y cortesías.
var TRIVIAL = trivialRe

// ActionCue indica si el mensaje tiene alguna pista de acción sobre el equipo:
// un nombre de archivo, una palabra de entorno (archivos, carpeta, disco…), un
// verbo imperativo, una pregunta de sistema (hora, espacio, ip…) o un comando.
// Sin pista, un intent de herramienta se descarta aunque el kNN lo proponga.
func ActionCue(txt string) bool {
	t := StripGreeting(txt)
	return hasFileRe.MatchString(t) || envThing.MatchString(t) || actionVerbRe.MatchString(t) ||
		sysInfoRe.MatchString(t) || cmdTokenRe.MatchString(t)
}

// HasObject indica si el mensaje nombra sobre qué actuar (archivo, extensión,
// texto entre comillas, «todos los…», archivos/carpetas). Si lo nombra, una
// PREGUNTA del gate pidiendo «¿qué archivo?» es un falso positivo.
func HasObject(txt string) bool { return hasFileRe.MatchString(txt) || objectRe.MatchString(txt) }

var greetingHead = regexp.MustCompile(`(?i)^\s*(?:hola|buenas|buenos d[ií]as|buenas (?:tardes|noches)|hey|ey|oye)(?:[,!.]?\s*capri)?[,!.:\s]*`)

// StripGreeting quita el saludo inicial («hola capri, …») para enrutar lo que importa.
func StripGreeting(txt string) string {
	t := greetingHead.ReplaceAllString(txt, "")
	if strings.TrimSpace(t) == "" {
		return txt
	}
	return t
}

// IsQuestion indica si el texto es una pregunta.
func IsQuestion(t string) bool { return questionRe.MatchString(t) }

// CleanFact quita la orden («…, anótalo») y deja el hecho.
func CleanFact(txt string) string {
	t := cleanTail.ReplaceAllString(txt, "")
	t = cleanHead.ReplaceAllString(t, "")
	t = strings.TrimSpace(t)
	if len([]rune(t)) >= 4 {
		return t
	}
	return strings.TrimSpace(txt)
}

// SafeIntent aplica las guardas: una pregunta nunca modifica el cerebro y los intents destructivos
// necesitan más seguridad.
func SafeIntent(intentName string, score float64, txt string) string {
	if rememberRe.MatchString(txt) && !IsQuestion(txt) && !strings.HasPrefix(intentName, "fs_") && !strings.HasPrefix(intentName, "shell") {
		return "rag_add"
	}
	if editVerbRe.MatchString(txt) && hasFileRe.MatchString(txt) && regexp.MustCompile(`(?i) por `).MatchString(txt) && !IsQuestion(txt) {
		return "fs_edit"
	}
	if createVerbRe.MatchString(txt) && hasFileRe.MatchString(txt) && !IsQuestion(txt) &&
		!regexp.MustCompile(`(?i)carpeta|directorio`).MatchString(txt) &&
		(intentName == "shell" || intentName == "none" || intentName == "fs_write" || intentName == "fs_read" || intentName == "fs_list" ||
			(intentName == "fs_edit" && !editVerbRe.MatchString(txt))) {
		return "fs_write"
	}
	if intentName == "rag_update" && !correctionRe.MatchString(txt) {
		return "none"
	}
	if intentName == "rag_delete" && !forgetRe.MatchString(txt) {
		return "none"
	}
	if score < thr().Route {
		return "none"
	}
	if memoryWrites[intentName] && IsQuestion(txt) {
		return "none"
	}
	// guardar en memoria solo con orden explícita o un dato personal: una queja
	// («parece que no hiciste nada») no es un recuerdo
	if intentName == "rag_add" && !rememberRe.MatchString(txt) && !learn.LooksLikeFact(txt) {
		return "none"
	}
	if (intentName == "rag_update" || intentName == "rag_delete" || intentName == "rag_list") && score < thr().Destructive {
		return "none"
	}
	// listar la memoria solo si se habla de ella (el kNN confunde «lee x.txt» con «lista tus memorias»)
	if intentName == "rag_list" && !MEMORY_TALK.MatchString(txt) {
		return "none"
	}
	// herramientas solo con una pista de acción real: un saludo o una pregunta de
	// conocimiento nunca deben acabar en shell/fs_* por cercanía de embeddings
	if (intentName == "shell" || strings.HasPrefix(intentName, "fs_")) && !ActionCue(txt) {
		return "none"
	}
	return intentName
}

// NeedsPlan dispara el bucle ReAct: objetivos compuestos, preguntas sobre el entorno o cálculos sobre archivos.
func NeedsPlan(txt string) bool {
	if MEMORY_TALK.MatchString(txt) || learn.LooksLikeFact(txt) {
		return false
	}
	return (compoundRe.MatchString(txt) && ActionCue(txt)) ||
		(envQuestion.MatchString(txt) && envThing.MatchString(txt)) ||
		(computeRe.MatchString(txt) && envThing.MatchString(txt))
}

// Personal indica si la pregunta trata del propio usuario o de la conversación.
func Personal(txt string) bool {
	ok, _ := personalRe.MatchString(txt)
	return ok
}

// RelevantFor decide si un recuerdo es lo bastante relevante para el prompt.
func RelevantFor(sim float64, query string) bool {
	t := thr()
	return sim >= t.Strong || (sim >= t.Fact && Personal(query))
}
