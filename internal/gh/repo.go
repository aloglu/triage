package gh

import (
	"fmt"
	"strings"
)

// ValidRepo reports whether repo is a GitHub repository in owner/name form.
func ValidRepo(repo string) bool {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	return len(parts) == 2 && validRepoPart(parts[0], false) && validRepoPart(parts[1], true)
}

// NormalizeRepo trims whitespace and a leading github.com URL from repo and
// validates the result.
func NormalizeRepo(repo string) (string, error) {
	value := strings.TrimSpace(repo)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.TrimSuffix(strings.TrimSuffix(value, "/"), ".git")
	if !ValidRepo(value) {
		return "", fmt.Errorf("%q is not a repository in owner/name form", repo)
	}
	return value, nil
}

// RepoName returns the name part of an owner/name reference.
func RepoName(repo string) string {
	if _, name, ok := strings.Cut(repo, "/"); ok {
		return name
	}
	return repo
}

func validRepoPart(part string, allowDotAndUnderscore bool) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, r := range part {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		if allowDotAndUnderscore && (r == '.' || r == '_') {
			continue
		}
		return false
	}
	return true
}
