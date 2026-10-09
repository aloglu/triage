package tui

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/ghtest"
	"github.com/aloglu/triage/internal/issue"
)

type harness struct {
	t      *testing.T
	m      *Model
	server *ghtest.Server
	env    *app.Env
}

func newHarness(t *testing.T, repos ...string) *harness {
	t.Helper()
	server := ghtest.New(t)
	dir := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(dir, "config"), CacheDir: filepath.Join(dir, "cache")}
	if len(repos) > 0 {
		cfg := config.Default()
		cfg.Repos = repos
		if err := config.Save(paths.ConfigFile(), cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, repo := range repos {
		server.AddRepo(repo)
	}
	env := app.NewTestEnv(paths, server.Client(), nil, nil)
	return &harness{t: t, server: server, env: env}
}

// start creates the model and runs its startup commands.
func (h *harness) start(width, height int) {
	h.m = New(h.env)
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
	h.run(h.m.Init())
}

// run executes cmd and feeds resulting messages back into the model.
// Commands that don't finish quickly (timers) are dropped.
func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		select {
		case msg := <-ch:
			switch msg := msg.(type) {
			case nil:
			case tea.BatchMsg:
				queue = append(queue, msg...)
			case tea.QuitMsg:
			default:
				_, next := h.m.Update(msg)
				queue = append(queue, next)
			}
		case <-time.After(80 * time.Millisecond):
		}
	}
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	_, cmd := h.m.Update(msg)
	h.run(cmd)
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// press sends keys one at a time.
func (h *harness) press(keys ...string) {
	h.t.Helper()
	for _, k := range keys {
		h.send(keyMsg(k))
	}
}

// typeText sends each rune of s as a key press.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		if r == ' ' {
			h.send(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// serverComment adds a comment on GitHub as someone else.
func (h *harness) serverComment(repo string, number int, body string) {
	h.t.Helper()
	client := h.server.Client()
	if _, err := client.CreateComment(context.Background(), repo, number, body); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) screen() string { return ansi.Strip(h.m.render()) }

func (h *harness) expectScreen(parts ...string) {
	h.t.Helper()
	screen := h.screen()
	for _, part := range parts {
		if !strings.Contains(screen, part) {
			h.t.Fatalf("screen is missing %q:\n%s", part, screen)
		}
	}
}

func (h *harness) selectedTitle() string {
	i, ok := h.m.selected()
	if !ok {
		return ""
	}
	return i.Title
}

func TestInboxViewsAndFilter(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.server.AddIssue("aloglu/triage", "Crash on start", "It panics.", "bug")
	h.server.AddIssue("aloglu/bookshelf", "Dark mode", "", "enhancement", "in progress")
	h.server.AddIssue("aloglu/bookshelf", "Old idea", "")
	h.server.EditIssue("aloglu/bookshelf", 2, func(i *gh.Issue) { i.State = "closed" })
	h.start(140, 40)

	h.expectScreen("Inbox", "Mine", "triage", "bookshelf", "Closed", "Dark mode", "Crash on start")
	if strings.Contains(h.screen(), "Old idea") {
		t.Fatal("closed issue shown in inbox")
	}
	if got := h.selectedTitle(); got != "Dark mode" {
		t.Fatalf("most recently updated issue should be selected first, got %q", got)
	}

	h.press("tab", "tab")
	if h.m.views[h.m.viewIdx].name != "triage" {
		t.Fatalf("view = %s", h.m.views[h.m.viewIdx].name)
	}
	if len(h.m.visible) != 1 || h.selectedTitle() != "Crash on start" {
		t.Fatalf("triage view shows %d issues", len(h.m.visible))
	}

	h.press("shift+tab", "shift+tab", "/")
	h.typeText("type:bug")
	if len(h.m.visible) != 1 || h.selectedTitle() != "Crash on start" {
		t.Fatalf("filter should narrow to the bug, got %d", len(h.m.visible))
	}
	h.press("esc")
	if len(h.m.visible) != 2 {
		t.Fatalf("esc should clear the filter, got %d", len(h.m.visible))
	}

	h.press("enter")
	h.expectScreen("It panics.")
}

func TestStatusChangeSyncsAndUndoes(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Crash on start", "", "idea")
	h.start(120, 40)

	h.press("s", "p")
	if got := h.server.LabelNames("aloglu/triage", 1); !reflect.DeepEqual(got, []string{"in progress"}) {
		t.Fatalf("labels on GitHub = %v", got)
	}
	h.expectScreen("In progress", "u to undo")

	h.press(">")
	if h.server.Issue("aloglu/triage", 1).State != "closed" {
		t.Fatal("> from in progress should close the issue as done")
	}
	h.press("u", "u")
	got := h.server.Issue("aloglu/triage", 1)
	if got.State != "open" || !reflect.DeepEqual(h.server.LabelNames("aloglu/triage", 1), []string{"idea"}) {
		t.Fatalf("after two undos: state %s labels %v", got.State, h.server.LabelNames("aloglu/triage", 1))
	}
}

func TestCreateIssueFromForm(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.start(120, 40)
	h.press("tab", "tab", "tab") // bookshelf view
	h.press("n")
	h.typeText("Add export")
	h.press("tab")
	h.typeText("CSV please")
	h.press("tab", "tab") // repo → type
	h.press("l", "l")
	h.press("ctrl+s")

	if h.server.IssueCount("aloglu/bookshelf") != 1 {
		h.expectScreen("nothing") // dump screen
	}
	created := h.server.Issue("aloglu/bookshelf", 1)
	if created.Title != "Add export" || created.Body != "CSV please" {
		t.Fatalf("created = %+v", created)
	}
	if got := h.server.LabelNames("aloglu/bookshelf", 1); !reflect.DeepEqual(got, []string{"enhancement"}) {
		t.Fatalf("labels = %v", got)
	}
	if h.selectedTitle() != "Add export" {
		t.Fatalf("new issue should be selected, got %q", h.selectedTitle())
	}
}

func TestFormEscConfirmsDiscard(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.start(120, 40)
	h.press("n")
	h.typeText("Half a thought")
	h.press("esc")
	if len(h.m.overlays) != 1 {
		t.Fatal("first esc on a dirty form should ask before discarding")
	}
	h.expectScreen("Press esc again")
	h.press("esc")
	if len(h.m.overlays) != 0 || h.server.IssueCount("aloglu/triage") != 0 {
		t.Fatal("second esc should discard")
	}
}

func TestCommentAndLabels(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Crash", "", "ui")
	h.start(120, 40)

	h.press("c")
	h.typeText("Looking into it")
	h.press("ctrl+s")
	if comments := h.server.Comments("aloglu/triage", 1); len(comments) != 1 || comments[0].Body != "Looking into it" {
		t.Fatalf("comments = %+v", comments)
	}

	h.press("L")
	h.press("space") // deselect "ui"
	h.typeText("mobile")
	h.press("down", "enter")
	if got := h.server.LabelNames("aloglu/triage", 1); !reflect.DeepEqual(got, []string{"mobile"}) {
		t.Fatalf("labels = %v", got)
	}
}

func TestOfflineQueueAndConflict(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Title", "body")
	h.start(120, 40)

	h.server.SetOffline(true)
	h.press("a")
	h.expectScreen("↑ unsent")
	h.press("x")
	h.expectScreen("↑2 queued", "offline")
	h.server.SetOffline(false)
	h.send(retryFlushMsg{})
	if got := h.server.Issue("aloglu/triage", 1); got.State != "closed" || len(got.Assignees) != 1 {
		t.Fatalf("queued changes not sent: %+v", got)
	}
	h.expectScreen("synced")

	// Someone edits the body on GitHub while we edit it here.
	h.press("tab", "tab", "tab") // Closed view
	if h.selectedTitle() != "Title" {
		t.Fatalf("closed issue should be selected, got %q", h.selectedTitle())
	}
	h.server.SetOffline(true)
	h.press("e", "tab")
	h.typeText(" mine")
	h.server.EditIssue("aloglu/triage", 1, func(i *gh.Issue) { i.Body = "theirs" })
	h.press("ctrl+s")
	h.server.SetOffline(false)
	h.send(retryFlushMsg{})
	h.press("enter")
	h.expectScreen("changed on GitHub", "keep mine", "Their description:", "theirs")
	h.press("K")
	if got := h.server.Issue("aloglu/triage", 1).Body; got != "body mine" {
		t.Fatalf("body = %q", got)
	}
}

func TestBoardMovesCards(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Card", "", "idea")
	h.start(150, 40)
	h.press("b")
	h.expectScreen("Idea", "Todo", "In progress", "Blocked", "Done", "Card")
	if h.m.boardCol != 0 {
		t.Fatalf("selection should start on the card's column, got %d", h.m.boardCol)
	}
	h.press(">", ">")
	if got := h.server.LabelNames("aloglu/triage", 1); !reflect.DeepEqual(got, []string{"in progress"}) {
		t.Fatalf("labels = %v", got)
	}
	if h.m.boardCol != 2 {
		t.Fatalf("selection should follow the card, col = %d", h.m.boardCol)
	}
	if conv := h.m.eng.Convention(); conv.StatusOf(h.m.snap.Issues[0]) != issue.StatusInProgress {
		t.Fatal("snapshot not updated")
	}
}

func TestOnboardingTracksPickedRepos(t *testing.T) {
	h := newHarness(t)
	h.server.AddRepo("aloglu/one")
	h.server.AddRepo("aloglu/two")
	h.server.AddIssue("aloglu/two", "Hello", "")
	h.start(120, 40)
	h.expectScreen("Welcome to triage", "aloglu/one", "aloglu/two")

	h.press("down", "space", "enter")
	if !reflect.DeepEqual(h.env.Config.Repos, []string{"aloglu/two"}) {
		t.Fatalf("repos = %v", h.env.Config.Repos)
	}
	if h.m.onboarding != nil {
		t.Fatal("onboarding should finish after the first fetch")
	}
	h.expectScreen("Hello")
	saved, _, _ := config.Load(h.env.Paths.ConfigFile())
	if !saved.HasRepo("aloglu/two") {
		t.Fatal("chosen repos should be saved")
	}
}

func TestPaletteAndJump(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "First", "")
	h.server.AddIssue("aloglu/triage", "Second", "")
	h.server.EditIssue("aloglu/triage", 1, func(i *gh.Issue) { i.State = "closed" })
	h.start(120, 40)

	h.press("#")
	h.typeText("First")
	h.press("enter")
	if h.selectedTitle() != "First" || h.m.views[h.m.viewIdx].name != "Closed" {
		t.Fatalf("jump should switch to the closed view, selected %q in %s", h.selectedTitle(), h.m.views[h.m.viewIdx].name)
	}

	h.press(":")
	h.typeText("board")
	h.press("enter")
	if !h.m.board {
		t.Fatal("palette should toggle the board")
	}
}

func TestRendersAtManySizes(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", strings.Repeat("Very long title ", 12), strings.Repeat("Body text. ", 200), "bug")
	h.start(120, 40)
	for _, size := range [][2]int{{40, 12}, {60, 20}, {89, 30}, {200, 60}, {30, 8}} {
		h.send(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, keys := range [][]string{nil, {"enter"}, {"esc"}, {"b"}, {"b"}, {"?"}, {"esc"}, {"n"}, {"esc"}} {
			h.press(keys...)
			screen := h.screen()
			for idx, line := range strings.Split(screen, "\n") {
				if w := lipglossWidth(line); w > size[0] {
					t.Fatalf("%dx%d after %v: line %d is %d wide", size[0], size[1], keys, idx, w)
				}
			}
		}
	}
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func TestCommentsLoadAndRefreshAfterPosting(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Discussed", "")
	h.start(120, 40)
	h.press("c")
	h.typeText("First thoughts")
	h.press("ctrl+s")
	h.expectScreen("Comments (1)", "First thoughts", "@octo")

	// Someone else replies on GitHub; a refresh brings it in.
	h.server.EditIssue("aloglu/triage", 1, func(i *gh.Issue) {})
	h.serverComment("aloglu/triage", 1, "A reply")
	h.press("r")
	h.expectScreen("Comments (2)", "A reply")
}
