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
	fields, body, err := parseMetadataBlock(raw)
	if err != nil {
		return "", "", "", "", err
	}

	project := strings.TrimSpace(fields["project"])
	itemType := parseFrontmatterType(fields["type"])
	stage := parseFrontmatterStage(fields["stage"])

	if project == "" {
		return "", "", "", "", fmt.Errorf("project missing from frontmatter")
	}
	if !validType(itemType) {
		return "", "", "", "", fmt.Errorf("invalid type %q", itemType)
	}
	if !validStage(stage) {
		return "", "", "", "", fmt.Errorf("invalid stage %q", stage)
	}

	return project, itemType, stage, body, nil
}

type DraftMetadata struct {
	Title   string
	Project string
	Repo    string
	Type    model.Type
	Stage   model.Stage
}

func ParseDraft(raw string) (DraftMetadata, string, error) {
	fields, body, err := parseMetadataBlock(raw)
	if err != nil {
		return DraftMetadata{}, "", err
	}

	title := strings.TrimSpace(fields["title"])
	project := strings.TrimSpace(fields["project"])
	repo := strings.TrimSpace(fields["repo"])
	itemType := parseFrontmatterType(fields["type"])
	stage := parseFrontmatterStage(fields["stage"])

	if title == "" {
		return DraftMetadata{}, "", fmt.Errorf("title missing from frontmatter")
	}
	if project == "" {
		return DraftMetadata{}, "", fmt.Errorf("project missing from frontmatter")
	}
	if repo != "" && !validRepo(repo) {
		return DraftMetadata{}, "", fmt.Errorf("invalid repo %q", repo)
	}
	if !validType(itemType) {
		return DraftMetadata{}, "", fmt.Errorf("invalid type %q", itemType)
	}
	if !validStage(stage) {
		return DraftMetadata{}, "", fmt.Errorf("invalid stage %q", stage)
	}

	return DraftMetadata{
		Title:   title,
		Project: project,
		Repo:    repo,
		Type:    itemType,
		Stage:   stage,
	}, body, nil
}

func parseMetadataBlock(raw string) (map[string]string, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	frontmatterLines, bodyStart, err := extractFrontmatter(lines)
	if err != nil {
		return nil, "", err
	}

	fields := map[string]string{}
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
		fields[key] = value
	}

	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	return fields, body, nil
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
	if value == "" {
		return model.TypeFeature
	}
	return model.Type(value)
}

func parseFrontmatterStage(value string) model.Stage {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return model.StageIdea
	case "planning":
		return model.StagePlanned
	default:
		return model.Stage(value)
	}
}

func validRepo(repo string) bool {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return false
	}
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
