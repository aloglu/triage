package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/ghtest"
)

func testEnv(t *testing.T, repos ...string) (*app.Env, *ghtest.Server, *bytes.Buffer, *bytes.Buffer) {
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
	var out, errOut bytes.Buffer
	return app.NewTestEnv(paths, server.Client(), &out, &errOut), server, &out, &errOut
}

func TestResolveRepo(t *testing.T) {
	env, _, _, _ := testEnv(t, "aloglu/triage", "aloglu/bookshelf", "other/bookshelf")

	if got, err := env.ResolveRepo("triage"); err != nil || got != "aloglu/triage" {
		t.Errorf("by name = %q, %v", got, err)
	}
	if _, err := env.ResolveRepo("bookshelf"); err == nil || !strings.Contains(err.Error(), "matches") {
		t.Errorf("ambiguous name should fail, got %v", err)
	}
	if got, err := env.ResolveRepo("github.com/a/b"); err != nil || got != "a/b" {
		t.Errorf("full ref = %q, %v", got, err)
	}
	if _, err := env.ResolveRepo(""); err == nil || !strings.Contains(err.Error(), "which repository") {
		t.Errorf("no default should list choices, got %v", err)
	}

	env.Config.DefaultRepo = "aloglu/bookshelf"
	if got, _ := env.ResolveRepo(""); got != "aloglu/bookshelf" {
		t.Errorf("default = %q", got)
	}
	env.CurrentRepo = func() (string, error) { return "aloglu/triage", nil }
	if got, _ := env.ResolveRepo(""); got != "aloglu/triage" {
		t.Errorf("cwd should win over default, got %q", got)
	}

	repo, number, err := env.ParseIssueRef("bookshelf#12")
	if err == nil {
		t.Errorf("ambiguous ref should fail, got %s#%d", repo, number)
	}
	repo, number, err = env.ParseIssueRef("#7")
	if err != nil || repo != "aloglu/triage" || number != 7 {
		t.Errorf("#7 = %s#%d, %v", repo, number, err)
	}
	repo, number, err = env.ParseIssueRef("https://github.com/x/y/issues/3")
	if err != nil || repo != "x/y" || number != 3 {
		t.Errorf("URL = %s#%d, %v", repo, number, err)
	}
}

func TestAddCreatesIssue(t *testing.T) {
	env, server, out, _ := testEnv(t, "aloglu/triage")
	err := Run(env, []string{"add", "Fix", "the", "-t", "bug", "top", "bar", "-s", "in-progress", "-b", "details", "-l", "ui"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Created triage#1: Fix the top bar") {
		t.Fatalf("output = %q", out.String())
	}
	issue := server.Issue("aloglu/triage", 1)
	if issue.Title != "Fix the top bar" || issue.Body != "details" {
		t.Fatalf("issue = %+v", issue)
	}
	if got := server.LabelNames("aloglu/triage", 1); !reflect.DeepEqual(got, []string{"ui", "bug", "in progress"}) {
		t.Fatalf("labels = %v", got)
	}
}

func TestAddBodyFromStdinAndAutoTrack(t *testing.T) {
	env, server, _, errOut := testEnv(t)
	server.AddRepo("aloglu/new")
	env.In = strings.NewReader("from stdin\n")
	if err := Run(env, []string{"add", "-r", "aloglu/new", "-b", "-", "Title"}); err != nil {
		t.Fatal(err)
	}
	if got := server.Issue("aloglu/new", 1).Body; got != "from stdin" {
		t.Fatalf("body = %q", got)
	}
	if !env.Config.HasRepo("aloglu/new") || !strings.Contains(errOut.String(), "Now tracking aloglu/new") {
		t.Fatalf("repo should be tracked automatically: %v / %q", env.Config.Repos, errOut.String())
	}
	reloaded, _, _ := config.Load(env.Paths.ConfigFile())
	if !reloaded.HasRepo("aloglu/new") {
		t.Fatal("tracking should be saved")
	}
}

func TestAddOfflineQueues(t *testing.T) {
	env, server, out, _ := testEnv(t, "aloglu/triage")
	server.SetOffline(true)
	if err := Run(env, []string{"add", "Offline idea", "-s", "idea"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Saved offline") {
		t.Fatalf("output = %q", out.String())
	}
	server.SetOffline(false)
	out.Reset()
	if err := Run(env, []string{"sync"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Sent 1 change") || server.IssueCount("aloglu/triage") != 1 {
		t.Fatalf("sync output = %q", out.String())
	}
}

func TestAddRejectedIsNotLeftQueued(t *testing.T) {
	env, server, _, _ := testEnv(t, "aloglu/triage")
	server.SetReadOnly("aloglu/triage", true)
	err := Run(env, []string{"add", "Nope"})
	if err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("err = %v", err)
	}
	if ops, _ := env.Engine.Store().Pending(); len(ops) != 0 {
		t.Fatalf("rejected issue left in outbox: %+v", ops)
	}
}

func TestAddValidation(t *testing.T) {
	env, _, _, _ := testEnv(t, "aloglu/triage")
	var usageErr UsageError
	if err := Run(env, []string{"add"}); !errors.As(err, &usageErr) {
		t.Errorf("missing title outside a terminal should be a usage error, got %v", err)
	}
	if err := Run(env, []string{"add", "x", "-t", "nope"}); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("bad type: %v", err)
	}
}

func TestList(t *testing.T) {
	env, server, out, _ := testEnv(t, "aloglu/triage", "aloglu/bookshelf")
	server.AddIssue("aloglu/triage", "Crash on start", "", "bug")
	server.AddIssue("aloglu/bookshelf", "Dark mode", "", "enhancement", "in progress")
	server.AddIssue("aloglu/bookshelf", "Old thing", "")
	server.EditIssue("aloglu/bookshelf", 2, func(i *gh.Issue) { i.State = "closed" })

	if err := Run(env, []string{"ls"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "bookshelf#1") || !strings.Contains(lines[0], "In progress") || !strings.Contains(lines[1], "Bug") {
		t.Fatalf("ls output:\n%s", out.String())
	}

	out.Reset()
	if err := Run(env, []string{"ls", "--cached", "--json", "type:bug"}); err != nil {
		t.Fatal(err)
	}
	var listed []listedIssue
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].Title != "Crash on start" {
		t.Fatalf("json = %s (%v)", out.String(), err)
	}

	out.Reset()
	if err := Run(env, []string{"ls", "--cached", "is:closed"}); err != nil || !strings.Contains(out.String(), "Old thing") {
		t.Fatalf("closed list = %q, %v", out.String(), err)
	}
}

func TestReposCommands(t *testing.T) {
	env, server, out, _ := testEnv(t)
	server.AddRepo("aloglu/triage")
	if err := Run(env, []string{"repos", "add", "aloglu/triage"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(env, []string{"repos", "add", "aloglu/missing"}); err == nil || !strings.Contains(err.Error(), "aloglu/missing") {
		t.Fatalf("adding an inaccessible repo should fail, got %v", err)
	}
	if err := Run(env, []string{"repos", "default", "triage"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(env, []string{"repos"}); err != nil || strings.TrimSpace(out.String()) != "aloglu/triage (default)" {
		t.Fatalf("repos = %q, %v", out.String(), err)
	}
	if err := Run(env, []string{"repos", "rm", "triage"}); err != nil {
		t.Fatal(err)
	}
	if len(env.Config.Repos) != 0 || env.Config.DefaultRepo != "" {
		t.Fatalf("config after rm = %+v", env.Config)
	}
}

func TestOpen(t *testing.T) {
	env, _, _, _ := testEnv(t, "aloglu/triage")
	var opened []string
	env.OpenURL = func(url string) error { opened = append(opened, url); return nil }
	for _, ref := range []string{"3", "triage#4", "a/b"} {
		if err := Run(env, []string{"open", ref}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"https://github.com/aloglu/triage/issues/3", "https://github.com/aloglu/triage/issues/4", "https://github.com/a/b/issues"}
	if !reflect.DeepEqual(opened, want) {
		t.Fatalf("opened %v", opened)
	}
}
