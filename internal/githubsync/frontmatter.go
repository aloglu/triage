package githubsync

import (
	"fmt"
	"strings"

	"github.com/aloglu/triage/internal/model"
)

func SerializeBody(item model.Item) string {
	body := strings.TrimSpace(item.Body)
	itemType := item.Type
	if !validType(itemType) {
		itemType = model.TypeFeature
	}
	frontmatter := fmt.Sprintf("```yaml\nproject: %s\ntype: %s\nstage: %s\n```", item.Project, itemType, item.Stage)
	if body == "" {
		return frontmatter + "\n"
	}

	return fmt.Sprintf("%s\n\n%s\n", frontmatter, body)
}

func ParseBody(raw string) (string, model.Type, model.Stage, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	frontmatterLines, bodyStart, err := extractFrontmatter(lines)
	if err != nil {
		return "", "", "", "", err
	}

	var project string
	itemType := model.TypeFeature
	var stage model.Stage
	for _, rawLine := range frontmatterLines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		switch key {
		case "project":
			project = value
		case "type":
			itemType = parseFrontmatterType(value)
		case "stage":
			stage = parseFrontmatterStage(value)
		}
	}

	if project == "" {
		return "", "", "", "", fmt.Errorf("project missing from frontmatter")
	}
	if !validType(itemType) {
		return "", "", "", "", fmt.Errorf("invalid type %q", itemType)
	}
	if !validStage(stage) {
		return "", "", "", "", fmt.Errorf("invalid stage %q", stage)
	}

	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	return project, itemType, stage, body, nil
}

func extractFrontmatter(lines []string) ([]string, int, error) {
	if len(lines) == 0 {
		return nil, 0, fmt.Errorf("missing frontmatter opening")
	}

	first := strings.TrimSpace(lines[0])
	switch {
	case first == "---":
		for idx := 1; idx < len(lines); idx++ {
			if strings.TrimSpace(lines[idx]) == "---" {
				return lines[1:idx], idx + 1, nil
			}
		}
		return nil, 0, fmt.Errorf("missing frontmatter closing")
	case strings.HasPrefix(first, "```"):
		for idx := 1; idx < len(lines); idx++ {
			if strings.TrimSpace(lines[idx]) == "```" {
				return lines[1:idx], idx + 1, nil
			}
		}
		return nil, 0, fmt.Errorf("missing frontmatter closing")
	default:
		return nil, 0, fmt.Errorf("missing frontmatter opening")
	}
}

func validType(itemType model.Type) bool {
	for _, candidate := range model.Types {
		if candidate == itemType {
			return true
		}
	}
	return false
}

func validStage(stage model.Stage) bool {
	for _, candidate := range model.Stages {
		if candidate == stage {
			return true
		}
	}
	return false
}

func parseFrontmatterType(value string) model.Type {
	value = strings.ToLower(strings.TrimSpace(value))
	return model.Type(value)
}

func parseFrontmatterStage(value string) model.Stage {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "planning":
		return model.StagePlanned
	default:
		return model.Stage(value)
	}
}
