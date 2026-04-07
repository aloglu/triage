package githubsync

import (
	"fmt"
	"strings"

	"github.com/aloglu/triage/internal/model"
)

func SerializeBody(item model.Item) string {
	body := strings.TrimSpace(item.Body)
	if body == "" {
		return fmt.Sprintf("---\nproject: %s\nstage: %s\n---\n", item.Project, item.Stage)
	}

	return fmt.Sprintf("---\nproject: %s\nstage: %s\n---\n\n%s\n", item.Project, item.Stage, body)
}

func ParseBody(raw string) (string, model.Stage, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", "", fmt.Errorf("missing frontmatter opening")
	}

	var project string
	var stage model.Stage
	end := -1
	for idx := 1; idx < len(lines); idx++ {
		line := strings.TrimSpace(lines[idx])
		if line == "---" {
			end = idx
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "project":
			project = value
		case "stage":
			stage = model.Stage(value)
		}
	}

	if end == -1 {
		return "", "", "", fmt.Errorf("missing frontmatter closing")
	}
	if project == "" {
		return "", "", "", fmt.Errorf("project missing from frontmatter")
	}
	if !validStage(stage) {
		return "", "", "", fmt.Errorf("invalid stage %q", stage)
	}

	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return project, stage, body, nil
}

func validStage(stage model.Stage) bool {
	for _, candidate := range model.Stages {
		if candidate == stage {
			return true
		}
	}
	return false
}
