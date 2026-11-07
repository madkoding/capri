package summary_test

import (
	"regexp"
	"testing"

	"capri/internal/summary"
)

func TestSummarize(t *testing.T) {
	out := summary.Summarize([]summary.Obs{
		{Tool: "fs_write", Args: map[string]string{"path": "a"}, OK: true},
		{Tool: "fs_read", Args: map[string]string{"path": "z"}, OK: false, Text: "error: ENOENT"},
	}, "error: ENOENT")
	for _, re := range []string{`✔ escribí ` + "`a`", `✗ fs_read z`, `Me detuve`} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("Summarize no contiene %q:\n%s", re, out)
		}
	}
}
