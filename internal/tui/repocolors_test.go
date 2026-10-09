package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func colorKey(t theme, repo string) string {
	r, g, b, _ := t.repoColor(repo).RGBA()
	return fmt.Sprintf("%d-%d-%d", r, g, b)
}

func TestRepoColorsAreDistinctAndStable(t *testing.T) {
	var repos []string
	for i := 0; i < len(repoPalette); i++ {
		repos = append(repos, fmt.Sprintf("owner/repo%d", i))
	}
	th := newTheme(true, nil)
	if err := th.assignRepoColors(repos, nil); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, repo := range repos {
		key := colorKey(th, repo)
		if other, dup := seen[key]; dup {
			t.Fatalf("%s and %s share a color", repo, other)
		}
		seen[key] = repo
	}

	// Removing a repo never moves a repo that holds its natural color;
	// only repos that collided with others can shift.
	before := map[string]string{}
	for _, repo := range repos {
		before[repo] = colorKey(th, repo)
	}
	for drop := range repos {
		rest := append(append([]string{}, repos[:drop]...), repos[drop+1:]...)
		if err := th.assignRepoColors(rest, nil); err != nil {
			t.Fatal(err)
		}
		for _, repo := range rest {
			nat := th.paletteColor(naturalRepoColor(repo))
			r, g, b, _ := nat.RGBA()
			holdsNatural := before[repo] == fmt.Sprintf("%d-%d-%d", r, g, b)
			if holdsNatural && colorKey(th, repo) != before[repo] {
				t.Fatalf("removing %s moved %s off its natural color", repos[drop], repo)
			}
		}
	}
}

func TestRepoColorOverrides(t *testing.T) {
	th := newTheme(true, nil)
	err := th.assignRepoColors([]string{"a/one", "a/two", "a/three"}, map[string]string{
		"a/one": "orange", "a/two": "#123456", "a/three": "chartreuse",
	})
	if err == nil || !strings.Contains(err.Error(), "chartreuse") {
		t.Fatalf("unknown color should be reported, got %v", err)
	}
	orange := th.paletteColor(4)
	r1, g1, b1, _ := th.repoColor("A/One").RGBA()
	r2, g2, b2, _ := orange.RGBA()
	if r1 != r2 || g1 != g2 || b1 != b2 {
		t.Fatal("named override not applied (case-insensitively)")
	}
	r, g, b, _ := th.repoColor("a/two").RGBA()
	r3, g3, b3, _ := lipgloss.Color("#123456").RGBA()
	if r != r3 || g != g3 || b != b3 {
		t.Fatal("hex override not applied")
	}
	// The repo with a bad override still gets a palette color, and not
	// orange, which is reserved by a/one.
	if colorKey(th, "a/three") == colorKey(th, "a/one") {
		t.Fatal("a reserved color was handed out again")
	}
}
