package tui

import (
	"context"
	"os"
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
	"github.com/aloglu/triage/internal/uninstall"
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
	// No timers or animations: every command the harness runs is real
	// work, so it can wait for each one to finish however slow the machine.
	env.Config.ReduceMotion = true
	previous := after
	after = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	t.Cleanup(func() { after = previous })
	return &harness{t: t, server: server, env: env}
}

// start creates the model and runs its startup commands.
func (h *harness) start(width, height int) {
	h.m = New(h.env)
	h.send(tea.WindowSizeMsg{Width: width, Height: height})
	h.run(h.m.Init())
}

// run executes cmd and feeds resulting messages back into the model.
// Timers are off in tests, so every command finishes; the timeout only
// guards against a hung test.
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
		case <-time.After(10 * time.Second):
			h.t.Fatal("a command didn't finish within 10s")
		}
	}
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	if _, isMouse := msg.(tea.MouseMsg); isMouse {
		// The real program draws after every update, so click targets
		// always match the screen; do the same here.
		h.m.render()
	}
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

// at returns the screen position of the first occurrence of text.
func (h *harness) at(text string) (int, int) {
	h.t.Helper()
	for y, line := range strings.Split(h.screen(), "\n") {
		if idx := strings.Index(line, text); idx >= 0 {
			return ansi.StringWidth(line[:idx]), y
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", text, h.screen())
	return 0, 0
}

// clickAfter clicks text that appears to the right of label on the same
// line, for text that also appears elsewhere on screen.
func (h *harness) clickAfter(label, text string) {
	h.t.Helper()
	for y, line := range strings.Split(h.screen(), "\n") {
		if l := strings.Index(line, label); l >= 0 {
			if idx := strings.Index(line[l:], text); idx >= 0 {
				x := ansi.StringWidth(line[:l+idx])
				h.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
				return
			}
		}
	}
	h.t.Fatalf("%q after %q is not on screen:\n%s", text, label, h.screen())
}

// click clicks the first occurrence of text on screen.
func (h *harness) click(text string, mod tea.KeyMod) {
	h.t.Helper()
	x, y := h.at(text)
	h.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
}

// wheel scrolls at the first occurrence of text.
func (h *harness) wheel(text string, down bool) {
	h.t.Helper()
	x, y := h.at(text)
	button := tea.MouseWheelUp
	if down {
		button = tea.MouseWheelDown
	}
	h.send(tea.MouseWheelMsg{X: x, Y: y, Button: button})
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

func TestViewsScopeAndFilter(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.server.AddIssue("aloglu/triage", "Crash on start", "It panics.", "bug")
	h.server.AddIssue("aloglu/bookshelf", "Dark mode", "", "enhancement", "in progress")
	h.server.AddIssue("aloglu/bookshelf", "Old idea", "")
	h.server.EditIssue("aloglu/bookshelf", 2, func(i *gh.Issue) { i.State = "closed" })
	h.start(140, 40)

	h.expectScreen("VIEWS", "Open", "Mine", "Closed", "REPOS", "All repos", "triage", "bookshelf", "Dark mode", "Crash on start")
	if strings.Contains(h.screen(), "Old idea") {
		t.Fatal("closed issue shown in the open view")
	}
	if got := h.selectedTitle(); got != "Dark mode" {
		t.Fatalf("most recently updated issue should be selected first, got %q", got)
	}

	// Scope to one repo with the picker.
	h.press("R")
	h.typeText("triage")
	h.press("enter")
	if h.m.scope != "aloglu/triage" || len(h.m.visible) != 1 || h.selectedTitle() != "Crash on start" {
		t.Fatalf("scope %q shows %d issues", h.m.scope, len(h.m.visible))
	}

	// Views combine with the scope: closed issues in triage — none.
	h.press("tab", "tab")
	if h.m.views[h.m.viewIdx].name != "Closed" || len(h.m.visible) != 0 {
		t.Fatalf("view %s shows %d issues", h.m.views[h.m.viewIdx].name, len(h.m.visible))
	}

	// Moving through the sidebar only highlights; enter applies. Walking
	// down to the repos must not switch to the views on the way.
	h.press("tab") // Closed → Open
	h.press("h")
	if h.m.focus != focusSidebar {
		t.Fatal("h should focus the sidebar")
	}
	h.press("j", "j", "j", "j", "j") // Mine, Closed, All repos, triage, bookshelf
	if h.m.views[h.m.viewIdx].name != "Open" || h.m.scope != "aloglu/triage" {
		t.Fatalf("moving changed the selection: view %s, scope %q", h.m.views[h.m.viewIdx].name, h.m.scope)
	}
	h.press("enter")
	if h.m.focus != focusList || h.m.scope != "aloglu/bookshelf" || h.m.views[h.m.viewIdx].name != "Open" {
		t.Fatalf("enter should apply the repo and keep the view: focus %v, scope %q, view %s", h.m.focus, h.m.scope, h.m.views[h.m.viewIdx].name)
	}
	h.press("h", "k", "k", "space") // All repos, applied without leaving
	if h.m.scope != "" || h.m.focus != focusSidebar {
		t.Fatalf("space should apply in place: scope %q focus %v", h.m.scope, h.m.focus)
	}
	h.press("esc")

	h.press("/")
	h.typeText("type:bug")
	if len(h.m.visible) != 1 || h.selectedTitle() != "Crash on start" {
		t.Fatalf("filter should narrow to the bug, got %d", len(h.m.visible))
	}
	h.press("esc")
	if len(h.m.visible) != 2 {
		t.Fatalf("esc should clear the filter, got %d", len(h.m.visible))
	}
	h.press("j") // Crash on start

	h.press("enter")
	h.expectScreen("It panics.")
}

func TestNarrowTerminalHidesSidebar(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Crash on start", "", "bug")
	h.start(100, 30)
	if strings.Contains(h.screen(), "REPOS") {
		t.Fatal("sidebar should be hidden on a narrow terminal")
	}
	h.expectScreen("Open · all repos")
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
	h.press("R")
	h.typeText("bookshelf")
	h.press("enter")
	h.press("n")
	h.typeText("Add export")
	h.press("tab", "tab") // title → repo → type
	h.press("l", "l")
	h.press("tab", "tab") // status → description
	h.typeText("CSV please")
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
	if h.m.editor == nil {
		t.Fatal("first esc on a dirty form should ask before discarding")
	}
	h.expectScreen("Press esc again")
	h.press("esc")
	if h.m.editor != nil || h.server.IssueCount("aloglu/triage") != 0 {
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
	h.press("tab", "tab") // Closed view
	if h.selectedTitle() != "Title" {
		t.Fatalf("closed issue should be selected, got %q", h.selectedTitle())
	}
	h.server.SetOffline(true)
	h.press("e", "tab")
	h.typeText("mine ")
	h.server.EditIssue("aloglu/triage", 1, func(i *gh.Issue) { i.Body = "theirs" })
	h.press("ctrl+s")
	h.server.SetOffline(false)
	h.send(retryFlushMsg{})
	h.press("enter")
	h.expectScreen("changed on GitHub", "keep mine", "Their description:", "theirs")
	h.press("K")
	if got := h.server.Issue("aloglu/triage", 1).Body; got != "mine body" {
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
	h.expectScreen("Welcome!", "aloglu/one", "aloglu/two")

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

func TestRefreshRemovesDeletedIssues(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Doomed", "")
	h.server.Advance(time.Hour)
	h.server.AddIssue("aloglu/triage", "Survivor", "")
	h.start(120, 40)
	h.expectScreen("Doomed", "Survivor")

	h.server.DeleteIssue("aloglu/triage", 1)
	h.press("r")
	if strings.Contains(h.screen(), "Doomed") {
		t.Fatalf("deleted issue still shown after refresh:\n%s", h.screen())
	}
	h.expectScreen("Survivor")
}

func TestUninstallScreen(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "a"), filepath.Join(dir, "b")}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := newHarness(t)
	u := &uninstaller{env: h.env, plan: uninstall.TestPlan(files...), th: newTheme(true, nil), width: 80}
	u.targets = u.plan.Targets()

	view := ansi.Strip(u.View().Content)
	if !strings.Contains(view, "Remove triage?") || !strings.Contains(view, "GitHub stay exactly") {
		t.Fatalf("confirm screen:\n%s", view)
	}
	run := func(cmd tea.Cmd) {
		for cmd != nil {
			msg := cmd()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					if m := c(); m != nil {
						if r, ok := m.(targetRemovedMsg); ok {
							_, cmd = u.Update(r)
						}
					}
				}
				continue
			}
			if _, ok := msg.(tea.QuitMsg); ok || msg == nil {
				return
			}
			_, cmd = u.Update(msg)
		}
	}
	_, cmd := u.Update(keyMsg("y"))
	run(cmd)
	if u.phase != uninstallDone {
		t.Fatalf("phase = %v, err = %v", u.phase, u.err)
	}
	for _, f := range files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", f)
		}
	}
	if view := ansi.Strip(u.View().Content); !strings.Contains(view, "triage is uninstalled") {
		t.Fatalf("done screen:\n%s", view)
	}

	u2 := &uninstaller{env: h.env, plan: uninstall.TestPlan(filepath.Join(dir, "c")), th: newTheme(true, nil)}
	u2.Update(keyMsg("n"))
	if u2.phase != uninstallCancelled {
		t.Fatal("n should cancel")
	}
}

func TestStatusPlacement(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "One", "")
	h.start(140, 30)
	h.m.flash("3 issues updated.", flashOK)
	lines := strings.Split(h.screen(), "\n")
	if !strings.Contains(lines[0], "3 issues updated.") {
		t.Fatalf("messages belong on the top line:\n%s", lines[0])
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "synced") || !strings.Contains(last, "new issue") {
		t.Fatalf("hints and sync status belong on the bottom line:\n%s", last)
	}
	// On a narrow window the bottom line still fits.
	h.send(tea.WindowSizeMsg{Width: 60, Height: 20})
	for _, line := range strings.Split(h.screen(), "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Fatalf("line is %d wide: %q", w, line)
		}
	}
}

func TestManualRefreshReportsResultAndMessagesClear(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "One", "")
	h.start(120, 40)
	h.press("r")
	if h.m.flashState.text != "" {
		t.Fatalf("a refresh with no changes shouldn't show a message, got %q", h.m.flashState.text)
	}
	h.expectScreen("synced")

	h.server.AddIssue("aloglu/triage", "Two", "")
	h.press("r")
	h.expectScreen("1 issue updated.")

	// Every message queues its own clearing timer, even from code paths
	// that ignore the returned command.
	h.m.flash("hello", flashInfo)
	if len(h.m.pending) == 0 {
		t.Fatal("flash should queue a clearing timer")
	}
	h.send(clearFlashMsg{id: h.m.flashState.id})
	if h.m.flashState.text != "" {
		t.Fatal("message not cleared")
	}
}

func TestMouse(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.server.AddIssue("aloglu/triage", "Crash on start", "See https://example.com/crash-report for details.\n\n"+strings.Repeat("More text.\n\n", 40), "bug")
	h.server.AddIssue("aloglu/bookshelf", "Dark mode", "", "enhancement")
	h.start(140, 40)
	var opened []string
	h.env.OpenURL = func(url string) error { opened = append(opened, url); return nil }

	// Sidebar items highlight the sidebar and apply.
	h.click("● bookshelf", 0)
	if h.m.focus != focusSidebar || h.m.scope != "aloglu/bookshelf" {
		t.Fatalf("sidebar click: focus %v scope %q", h.m.focus, h.m.scope)
	}
	h.click("Closed", 0)
	if h.m.views[h.m.viewIdx].name != "Closed" {
		t.Fatal("clicking a view should apply it")
	}
	h.click("Open ", 0)
	h.click("All repos", 0)

	// Anywhere in the list pane focuses it, not just an issue row.
	x, _ := h.at("Open · all repos")
	h.send(tea.MouseClickMsg{X: x + 5, Y: 30, Button: tea.MouseLeft})
	if h.m.focus != focusList {
		t.Fatalf("clicking empty list space should focus the list, focus %v", h.m.focus)
	}

	// Rows select; a double click opens.
	h.click("Crash on start", 0)
	if h.selectedTitle() != "Crash on start" || h.m.focus != focusList {
		t.Fatalf("row click: selected %q focus %v", h.selectedTitle(), h.m.focus)
	}
	h.click("Crash on start", 0)
	if h.m.focus != focusDetail {
		t.Fatal("double click should open the issue")
	}

	// Links: a plain click explains, ctrl+click opens.
	h.click("https://example.com/crash-report", 0)
	if len(opened) != 0 {
		t.Fatal("a plain click shouldn't open links")
	}
	h.expectScreen("Ctrl+click to open the link.")
	h.click("https://example.com/crash-report", tea.ModCtrl)
	if len(opened) != 1 || opened[0] != "https://example.com/crash-report" {
		t.Fatalf("opened %v", opened)
	}

	// The wheel scrolls the pane under the pointer.
	h.wheel("More text.", true)
	if h.m.detail.YOffset() == 0 {
		t.Fatal("wheel over the detail pane should scroll it")
	}

	// Footer hints act like their keys (once the message above has cleared).
	h.send(clearFlashMsg{id: h.m.flashState.id})
	h.click("n new issue", 0)
	if h.m.editor == nil {
		t.Fatal("clicking the 'n' hint should open the new-issue editor")
	}
}

func TestMousePopupsAndEditor(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Crash", "")
	h.start(140, 40)

	// Picker items are clickable; clicking outside closes a pop-up.
	h.press("s")
	h.click("In progress", 0)
	if got := h.server.LabelNames("aloglu/triage", 1); !reflect.DeepEqual(got, []string{"in progress"}) {
		t.Fatalf("labels = %v", got)
	}
	h.press("s")
	h.send(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	if len(h.m.overlays) != 0 {
		t.Fatal("clicking outside should close the pop-up")
	}

	// The editor lives in the right pane, with clickable fields.
	h.press("n")
	h.expectScreen("New issue", "Crash") // list stays visible
	h.typeText("Mouse made")
	h.clickAfter("Type", "bug")
	h.clickAfter("Status", "In progress")
	h.press("ctrl+s")
	created := h.server.Issue("aloglu/triage", 2)
	if created.Title != "Mouse made" {
		t.Fatalf("created %+v", created)
	}
	if got := h.server.LabelNames("aloglu/triage", 2); !reflect.DeepEqual(got, []string{"bug", "in progress"}) {
		t.Fatalf("labels = %v", got)
	}
}

func TestMouseOnboarding(t *testing.T) {
	h := newHarness(t)
	h.server.AddRepo("aloglu/one")
	h.server.AddRepo("aloglu/two")
	h.start(120, 40)
	h.click("aloglu/two", 0)
	h.press("enter")
	if !reflect.DeepEqual(h.env.Config.Repos, []string{"aloglu/two"}) {
		t.Fatalf("repos = %v", h.env.Config.Repos)
	}
}

func TestClicksFollowResize(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.server.AddIssue("aloglu/triage", "Crash on start", "", "bug")
	h.server.AddIssue("aloglu/bookshelf", "Dark mode", "", "enhancement")
	// Small window: no sidebar.
	h.start(100, 30)
	if strings.Contains(h.screen(), "REPOS") {
		t.Fatal("expected no sidebar at this size")
	}
	// Maximize: the sidebar appears and everything moves right.
	h.send(tea.WindowSizeMsg{Width: 200, Height: 50})
	h.click("● bookshelf", 0)
	if h.m.focus != focusSidebar || h.m.scope != "aloglu/bookshelf" {
		t.Fatalf("after growing: focus %v scope %q", h.m.focus, h.m.scope)
	}
	// The list row (the detail pane's title also says "Dark mode").
	h.click("▌ Dark mode", 0)
	if h.selectedTitle() != "Dark mode" || h.m.focus != focusList {
		t.Fatalf("after growing: selected %q focus %v", h.selectedTitle(), h.m.focus)
	}
	// Shrink again: the sidebar goes away and the list starts at the edge.
	h.send(tea.WindowSizeMsg{Width: 90, Height: 30})
	h.press("R")
	h.typeText("all")
	h.press("enter")
	h.click("Crash on start", 0)
	if h.selectedTitle() != "Crash on start" {
		t.Fatalf("after shrinking: selected %q", h.selectedTitle())
	}
}

func TestPickerTypingHighlightsBestMatch(t *testing.T) {
	h := newHarness(t, "aloglu/triage", "aloglu/bookshelf")
	h.start(140, 40)
	h.press("R")
	h.typeText("bookshelf")
	h.press("enter")
	h.press("R") // opens with bookshelf highlighted
	h.typeText("triage")
	h.press("enter")
	if h.m.scope != "aloglu/triage" {
		t.Fatalf("typing should highlight the best match, got scope %q", h.m.scope)
	}
}

func TestEditorPreviewAndCommentContext(t *testing.T) {
	h := newHarness(t, "aloglu/triage")
	h.server.AddIssue("aloglu/triage", "Crash", "Original body")
	h.start(140, 40)
	h.serverComment("aloglu/triage", 1, "Earlier reply")
	h.press("r")

	// Commenting shows what you're replying to.
	h.press("c")
	h.expectScreen("Commenting on", "Earlier reply", "Write", "Preview")
	h.typeText("**Bold** and `code`")
	// Writing is highlighted: the markers stay visible as you type.
	h.expectScreen("**Bold** and `code`")
	// Preview renders the Markdown.
	h.press("ctrl+p")
	if !h.m.editor.preview {
		t.Fatal("ctrl+p should switch to preview")
	}
	h.expectScreen("Bold and", "code")
	if strings.Contains(h.screen(), "**Bold**") {
		t.Fatal("preview should render, not show raw Markdown")
	}
	// Typing goes back to writing.
	h.typeText("!")
	if h.m.editor.preview || !strings.HasSuffix(h.m.editor.body.Value(), "!") {
		t.Fatalf("typing in preview should return to writing; value %q", h.m.editor.body.Value())
	}
	h.press("ctrl+s")
	comments := h.server.Comments("aloglu/triage", 1)
	if len(comments) != 2 || comments[1].Body != "**Bold** and `code`!" {
		t.Fatalf("comments = %+v", comments)
	}
}
