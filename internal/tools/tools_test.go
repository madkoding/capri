package tools_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"capri/internal/config"
	"capri/internal/tools"
)

func TestFsWriteRejectsCommandContent(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("WORKDIR", dir)
	config.Reload()
	r := tools.RunTool(context.Background(), "fs_write", map[string]string{"path": "zz-test-nunca.txt", "content": "$(cat a.txt)"}, &tools.Context{
		Confirm: func(string, string, tools.ConfirmOptions) bool { return true },
	})
	if r.OK {
		t.Fatalf("fs_write aceptó un comando: %+v", r)
	}
	if !regexp.MustCompile(`cp origen destino`).MatchString(r.Text) {
		t.Errorf("texto = %q", r.Text)
	}
}
