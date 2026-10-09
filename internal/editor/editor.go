// Package editor edits issue text in the user's $EDITOR.
package editor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const helpStart = "<!-- triage:"

// Template renders title and body as an editable document: the first line is
// the title and everything after the blank line is the body, like a commit
// message. help is appended as an HTML comment, which is stripped on parse.
func Template(title, body, help string) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	if body = strings.TrimSpace(body); body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	b.WriteString(helpStart + "\n")
	b.WriteString("The first line is the title; everything below it is the body (Markdown).\n")
	if help != "" {
		b.WriteString(help + "\n")
	}
	b.WriteString("Save and close to continue. Delete everything to cancel.\n")
	b.WriteString("-->\n")
	return b.String()
}

// Parse splits an edited document into title and body. ok is false when the
// document is empty, meaning the user cancelled.
func Parse(doc string) (title, body string, ok bool) {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	if at := strings.Index(doc, helpStart); at >= 0 {
		rest := doc[at:]
		if end := strings.Index(rest, "-->"); end >= 0 {
			doc = doc[:at] + rest[end+3:]
		} else {
			doc = doc[:at]
		}
	}
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return "", "", false
	}
	title, body, _ = strings.Cut(doc, "\n")
	return strings.TrimSpace(strings.TrimLeft(title, "# ")), strings.TrimSpace(body), true
}

// Command returns the user's editor command: $VISUAL, $EDITOR, or the
// first of nano, vim, and vi that is installed.
func Command() []string {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return strings.Fields(value)
		}
	}
	for _, candidate := range []string{"nano", "vim", "vi"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return []string{candidate}
		}
	}
	return []string{"vi"}
}

// TempFile writes doc to a new markdown file and returns its path.
func TempFile(doc string) (string, error) {
	dir, err := os.MkdirTemp("", "triage-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "ISSUE.md")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Cmd returns an exec.Cmd that opens path in the user's editor.
func Cmd(path string) *exec.Cmd {
	command := Command()
	args := append(command[1:], path)
	return exec.Command(command[0], args...)
}

// ReadAndRemove reads the edited file and deletes its temp directory.
func ReadAndRemove(path string) (string, error) {
	data, err := os.ReadFile(path)
	_ = os.RemoveAll(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}
	return string(data), nil
}

// Run edits doc in the user's editor, attached to the terminal, and returns
// the result.
func Run(doc string) (string, error) {
	path, err := TempFile(doc)
	if err != nil {
		return "", err
	}
	cmd := Cmd(path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(filepath.Dir(path))
		return "", fmt.Errorf("run editor %q: %w", strings.Join(Command(), " "), err)
	}
	return ReadAndRemove(path)
}
