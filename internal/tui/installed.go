package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/aloglu/triage/internal/app"
)

// PrintInstalled prints the message the install script shows once triage
// is in place: the wordmark, where it went, and what to do next, including
// how to fix PATH if the install directory isn't on it.
func PrintInstalled(env *app.Env, out io.Writer) error {
	th := newTheme(darkTerminal(), nil)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	var b strings.Builder
	b.WriteString("\n")
	for _, line := range strings.Split(th.bigWordmark(), "\n") {
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("\n  " + th.statusOK.Render("✓") + " " + th.bold.Render("triage "+env.Version) + th.subtle.Render(" is installed in "+tildePath(dir)) + "\n\n")

	if !onPath(dir, os.Getenv("PATH")) {
		b.WriteString("  " + th.statusWarn.Render("One more step: ") + tildePath(dir) + " isn't on your PATH yet. Run:\n\n")
		b.WriteString("      " + th.key.Render(pathFixCommand(dir, os.Getenv("SHELL"))) + "\n\n")
		b.WriteString("  " + th.subtle.Render("then open a new terminal.") + "\n\n")
	}

	row := func(label, command string) {
		b.WriteString("  " + th.dim.Render(fmt.Sprintf("%-14s", label)) + th.key.Render(command) + "\n")
	}
	row("Get started", "triage")
	row("All commands", "triage help")
	row("Update later", "triage update")
	b.WriteString("\n")
	_, err = lipgloss.Fprint(out, b.String())
	return err
}

// darkTerminal asks the terminal for its background. The install script's
// stdin is a pipe, so it asks through /dev/tty, assuming dark when it can't.
func darkTerminal() bool {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return true
	}
	defer tty.Close()
	return lipgloss.HasDarkBackground(tty, os.Stdout)
}

func onPath(dir, path string) bool {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for _, entry := range filepath.SplitList(path) {
		if entry == "" {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(entry); err == nil {
			entry = resolved
		}
		if filepath.Clean(entry) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

// pathFixCommand is the one-liner that adds dir to PATH for the user's
// shell.
func pathFixCommand(dir, shell string) string {
	home, _ := os.UserHomeDir()
	shown := dir
	if home != "" && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		shown = "$HOME" + strings.TrimPrefix(dir, home)
	}
	switch filepath.Base(shell) {
	case "fish":
		return fmt.Sprintf("fish_add_path %s", strings.Replace(shown, "$HOME", "~", 1))
	case "zsh":
		return fmt.Sprintf(`echo 'export PATH="%s:$PATH"' >> ~/.zshrc`, shown)
	default:
		return fmt.Sprintf(`echo 'export PATH="%s:$PATH"' >> ~/.bashrc`, shown)
	}
}

func tildePath(path string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
