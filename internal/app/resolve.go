package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aloglu/triage/internal/gh"
)

// ResolveRepo turns user input into an owner/name repo. Input may be a full
// reference or just a name that matches one tracked repo. Empty input falls
// back to the current directory's repo, then default_repo, then the only
// tracked repo.
func (env *Env) ResolveRepo(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input != "" {
		if strings.Contains(input, "/") {
			return gh.NormalizeRepo(input)
		}
		var matches []string
		for _, repo := range env.Config.Repos {
			if strings.EqualFold(gh.RepoName(repo), input) {
				matches = append(matches, repo)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return "", fmt.Errorf("no tracked repo is named %q; use owner/name", input)
		default:
			return "", fmt.Errorf("%q matches %s; use owner/name", input, strings.Join(matches, " and "))
		}
	}
	if env.CurrentRepo != nil {
		if repo, err := env.CurrentRepo(); err == nil && repo != "" {
			return repo, nil
		}
	}
	if env.Config.DefaultRepo != "" {
		return env.Config.DefaultRepo, nil
	}
	if len(env.Config.Repos) == 1 {
		return env.Config.Repos[0], nil
	}
	if len(env.Config.Repos) == 0 {
		return "", fmt.Errorf("no repository given and none tracked yet; pass -r owner/name or run `triage` to set up")
	}
	return "", fmt.Errorf("which repository? pass -r with one of: %s (or set one with `triage repos default`)", strings.Join(env.Config.Repos, ", "))
}

// ParseIssueRef parses "12", "#12", "repo#12", or "owner/repo#12".
func (env *Env) ParseIssueRef(ref string) (string, int, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "https://") {
		// https://github.com/owner/repo/issues/12
		parts := strings.Split(strings.TrimPrefix(ref, "https://"), "/")
		if len(parts) >= 5 && (parts[3] == "issues" || parts[3] == "pull") {
			number, err := strconv.Atoi(parts[4])
			if err == nil {
				return parts[1] + "/" + parts[2], number, nil
			}
		}
		return "", 0, fmt.Errorf("unrecognized issue URL %q", ref)
	}
	repoPart, numberPart, hasHash := strings.Cut(ref, "#")
	if !hasHash {
		repoPart, numberPart = "", ref
	}
	number, err := strconv.Atoi(numberPart)
	if err != nil || number <= 0 {
		return "", 0, fmt.Errorf("%q is not an issue reference like 12, repo#12, or owner/repo#12", ref)
	}
	repo, err := env.ResolveRepo(repoPart)
	if err != nil {
		return "", 0, err
	}
	return repo, number, nil
}
