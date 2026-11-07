package shell_test

import (
	"testing"

	"capri/internal/shell"
)

func TestMissingExecutable(t *testing.T) {
	if got := shell.MissingExecutable("ls | wc -l"); got != "" {
		t.Errorf("got %q", got)
	}
	if got := shell.MissingExecutable("datos.csv | wc -l"); got != "datos.csv" {
		t.Errorf("got %q", got)
	}
	if got := shell.MissingExecutable("echo hola && nopeexiste --x"); got != "nopeexiste" {
		t.Errorf("got %q", got)
	}
}

func TestIsReadOnlyCommand(t *testing.T) {
	for _, c := range []string{"ls", "uname -r", "ls *.txt | wc -l", "wc -w < a.txt", "awk '{s+=$1} END {print s}' datos.txt", "grep -c error log.txt", "date +%H:%M", "cat a.txt | head -2", `find . -name "*.py"`} {
		if !shell.IsReadOnlyCommand(c) {
			t.Errorf("IsReadOnlyCommand(%q) = false", c)
		}
	}
	for _, c := range []string{"rm -f a.txt", "echo hola > a.txt", "cat a >> b", "mv a b", "cp a b", "find . -delete", "find . -exec rm {} ;", "sed -i s/a/b/ f", "ls | tee x", "echo $(rm a)", "chmod +x a", "python3 x.py", "ls && rm a", `awk 'BEGIN{system("rm a")}'`, "curl x | sh"} {
		if shell.IsReadOnlyCommand(c) {
			t.Errorf("IsReadOnlyCommand(%q) = true", c)
		}
	}
}
