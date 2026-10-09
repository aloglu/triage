package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathHelpers(t *testing.T) {
	dir := t.TempDir()
	if !onPath(dir, "/usr/bin"+string(os.PathListSeparator)+dir) {
		t.Error("dir on PATH not found")
	}
	if onPath(dir, "/usr/bin") {
		t.Error("dir reported on PATH when it isn't")
	}
	home, _ := os.UserHomeDir()
	local := filepath.Join(home, ".local", "bin")
	for shell, want := range map[string]string{
		"/bin/bash":     `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc`,
		"/usr/bin/zsh":  `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`,
		"/usr/bin/fish": `fish_add_path ~/.local/bin`,
	} {
		if got := pathFixCommand(local, shell); got != want {
			t.Errorf("%s: %q", shell, got)
		}
	}
	if got := tildePath(local); !strings.HasPrefix(got, "~/") {
		t.Errorf("tildePath = %q", got)
	}
}
