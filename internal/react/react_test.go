package react_test

import (
	"regexp"
	"strings"
	"testing"

	"capri/internal/react"
)

func TestCleanCommand(t *testing.T) {
	cases := map[string]string{
		"date +%H:%M | fs_write: hora.txt": "date +%H:%M",
		"wc -w a.txt | answer":             "wc -w a.txt",
		`rm b.tmp\nrm c.tmp`:               "rm b.tmp",
		"ls | wc -l":                       "ls | wc -l",
		// prosa pegada tras el comando (modelos pequeños «piensan» en el argumento)
		"date +%H:%M para obtener la hora, luego fs_write para guardar": "date +%H:%M",
		"ls -la - lista los archivos":                                   "ls -la",
		"date +%F porque el objetivo pide la fecha":                     "date +%F",
		`echo "hola para ti" > a.txt`:                                   `echo "hola para ti" > a.txt`,
		"grep -c 'y luego' a.txt":                                       "grep -c 'y luego' a.txt",
	}
	for in, want := range cases {
		if got := react.CleanCommand(in); got != want {
			t.Errorf("CleanCommand(%q) = %q, quería %q", in, got, want)
		}
	}
}

func TestNoWebSearchWithoutWeb(t *testing.T) {
	if strings.Contains(react.GrammarFor(false), "web_search") || strings.Contains(react.SystemFor(false), "web_search") {
		t.Error("sin web no debe ofrecerse web_search")
	}
	if !strings.Contains(react.GrammarFor(true), "web_search") || !strings.Contains(react.SystemFor(true), "web_search") {
		t.Error("con web debe ofrecerse web_search")
	}
}

func TestErrorHint(t *testing.T) {
	if got := react.ErrorHint("fs_edit", "ENOENT: x"); !regexp.MustCompile(`fs_write`).MatchString(got) {
		t.Errorf("ErrorHint = %q", got)
	}
	if got := react.ErrorHint("fs_delete", "no admite comodines"); !regexp.MustCompile(`rm -f`).MatchString(got) {
		t.Errorf("ErrorHint = %q", got)
	}
}
