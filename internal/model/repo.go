package model

import "strings"

// ValidRepoRef reports whether repo is a GitHub repository in owner/name form.
func ValidRepoRef(repo string) bool {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	return len(parts) == 2 && validRepoPart(parts[0], false) && validRepoPart(parts[1], true)
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
