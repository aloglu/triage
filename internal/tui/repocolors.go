package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// repoPaletteColor is one repo color, readable on light and dark
// terminals.
type repoPaletteColor struct {
	name        string
	light, dark string
}

// repoPalette is the set of repo colors, chosen to stay distinguishable
// from each other. Users can name them in [repo_colors].
var repoPalette = []repoPaletteColor{
	{"green", "#1a7f37", "#7ee787"},
	{"purple", "#8250df", "#d2a8ff"},
	{"yellow", "#9a6700", "#e3b341"},
	{"blue", "#0969da", "#79c0ff"},
	{"orange", "#bc4c00", "#ffa657"},
	{"teal", "#1b7c83", "#56d4dd"},
	{"pink", "#bf3989", "#ff9bce"},
	{"red", "#cf222e", "#ff7b72"},
	{"lime", "#4d7c0f", "#b5e550"},
	{"indigo", "#4338ca", "#a5a5ff"},
	{"sand", "#8a5a2b", "#d9b38c"},
	{"gray", "#59636e", "#9198a1"},
}

// RepoColorNames lists the palette's names, for help text.
func RepoColorNames() []string {
	names := make([]string, len(repoPalette))
	for i, c := range repoPalette {
		names[i] = c.name
	}
	return names
}

func (t theme) paletteColor(idx int) color.Color {
	c := repoPalette[idx]
	return lipgloss.LightDark(t.isDark)(lipgloss.Color(c.light), lipgloss.Color(c.dark))
}

// naturalRepoColor is the palette slot a repo prefers, from its name, so a
// repo keeps its color across machines and regardless of what else is
// tracked.
func naturalRepoColor(repo string) int {
	var hash uint32 = 2166136261
	for _, b := range []byte(strings.ToLower(repo)) {
		hash ^= uint32(b)
		hash *= 16777619
	}
	return int(hash % uint32(len(repoPalette)))
}

var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// assignRepoColors gives every tracked repo its own palette color.
// Overrides from config come first, then every repo whose natural color is
// still free, then the rest, which take the next free color. Colors only
// repeat once all of them are in use. It returns a problem with an
// override, if any.
func (t *theme) assignRepoColors(repos []string, overrides map[string]string) error {
	t.repoColors = map[string]color.Color{}
	taken := make([]bool, len(repoPalette))
	var problems []string

	keys := make([]string, 0, len(overrides))
	for repo := range overrides {
		keys = append(keys, repo)
	}
	sort.Strings(keys)
	for _, repo := range keys {
		value := strings.TrimSpace(overrides[repo])
		if hexColor.MatchString(value) {
			t.repoColors[strings.ToLower(repo)] = lipgloss.Color(value)
			continue
		}
		found := false
		for idx, c := range repoPalette {
			if strings.EqualFold(c.name, value) {
				t.repoColors[strings.ToLower(repo)] = t.paletteColor(idx)
				taken[idx] = true
				found = true
			}
		}
		if !found {
			problems = append(problems, fmt.Sprintf("%s: %q isn't a color (use #rrggbb or one of %s)", repo, value, strings.Join(RepoColorNames(), ", ")))
		}
	}

	// First pass: every repo whose natural color is free gets it, so a
	// repo only changes color if it collided with another one.
	var displaced []string
	for _, repo := range repos {
		key := strings.ToLower(repo)
		if _, ok := t.repoColors[key]; ok {
			continue
		}
		if idx := naturalRepoColor(repo); !taken[idx] {
			taken[idx] = true
			t.repoColors[key] = t.paletteColor(idx)
		} else {
			displaced = append(displaced, repo)
		}
	}
	// Second pass: the rest take the next free color after their natural
	// one; once every color is in use, they share their natural color.
	for _, repo := range displaced {
		idx := naturalRepoColor(repo)
		for step := 0; step < len(repoPalette); step++ {
			candidate := (idx + step) % len(repoPalette)
			if !taken[candidate] {
				idx = candidate
				break
			}
		}
		taken[idx] = true
		t.repoColors[strings.ToLower(repo)] = t.paletteColor(idx)
	}
	if len(problems) > 0 {
		return fmt.Errorf("repo_colors: %s", strings.Join(problems, "; "))
	}
	return nil
}

// repoColor returns a repo's color: its assigned one when tracked, its
// natural one otherwise (e.g. in the onboarding list).
func (t theme) repoColor(repo string) color.Color {
	if c, ok := t.repoColors[strings.ToLower(repo)]; ok {
		return c
	}
	return t.paletteColor(naturalRepoColor(repo))
}
