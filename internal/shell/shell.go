// Package shell clasifica comandos (solo lectura / peligrosos) y valida ejecutables.
package shell

import (
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"

	"capri/internal/config"
)

var readOnly = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields("ls cat head tail wc grep egrep fgrep date df du uname whoami pwd stat sort uniq cut tr echo printf basename dirname file hostname id uptime free which find awk sed nproc ps env seq tree column paste expr bc") {
		m[w] = true
	}
	return m
}()

var (
	unsafeRe   = regexp2.MustCompile(">|\\$\\(|`|<\\(|\\btee\\b|-delete\\b|-exec\\b|-ok\\b|-fprint|\\bsystem\\s*\\(|\\bsed\\b[^|;&]*\\s-\\w*i|\\bsed\\b[^|;&]*\\bw\\s", 0)
	envRe      = regexp.MustCompile(`^(?:\w+=\S+\s+)*`)
	aliasRe    = regexp.MustCompile(`(^|[|;&]\s*)(python|pip)\b`)
	segmentSep = regexp.MustCompile(`\|\||&&|;|\|`)
)

func splitSegments(c string) []string {
	parts := segmentSep.Split(c, -1)
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstWord(seg string) string {
	stripped := envRe.ReplaceAllString(seg, "")
	fields := strings.Fields(stripped)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// IsReadOnlyCommand indica si un comando solo lee (no pide confirmación).
func IsReadOnlyCommand(cmd string) bool {
	if bad, _ := unsafeRe.MatchString(cmd); bad {
		return false
	}
	segs := splitSegments(cmd)
	if len(segs) == 0 {
		return false
	}
	for _, seg := range segs {
		if !readOnly[firstWord(seg)] {
			return false
		}
	}
	return true
}

// MissingExecutable devuelve el primer ejecutable que no existe en un pipeline.
func MissingExecutable(command string) string {
	cfg := config.Get()
	for _, seg := range splitSegments(command) {
		word := firstWord(seg)
		if word == "" || strings.ContainsRune("!({$", rune(word[0])) {
			continue
		}
		if !HasExecutable(cfg.Workdir, word) {
			return word
		}
	}
	return ""
}

var aliases = map[string]string{"python": "python3", "pip": "pip3"}

// WithAliases reescribe alias comunes si el original no existe (python → python3).
func WithAliases(command string) string {
	return aliasRe.ReplaceAllStringFunc(command, func(m string) string {
		sub := aliasRe.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		pre, w := sub[1], sub[2]
		alias := aliases[strings.ToLower(w)]
		if alias != "" && MissingExecutable(w) != "" && MissingExecutable(alias) == "" {
			return pre + alias
		}
		return m
	})
}
