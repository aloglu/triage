// Package cli implements triage's command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aloglu/triage/internal/app"
	"github.com/aloglu/triage/internal/editor"
	"github.com/aloglu/triage/internal/engine"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/tui"
	"github.com/aloglu/triage/internal/uninstall"
	"github.com/aloglu/triage/internal/update"
)

// UsageError is an error that should be followed by usage help.
type UsageError struct{ msg string }

func (e UsageError) Error() string { return e.msg }

const usage = `triage: a fast keyboard client for your GitHub issues.

Usage:
  triage                        open the app
  triage add [flags] [title]    create an issue (opens $EDITOR without a title)
  triage ls [query]             list issues matching a filter (default: is:open)
  triage open <issue|repo>      open an issue or repo in the browser
  triage sync                   send queued changes and fetch updates
  triage repos                  list tracked repos
  triage repos add <repo>...    track repos (owner/name)
  triage repos rm <repo>...     stop tracking repos
  triage repos default <repo>   set where new issues go by default
  triage repos discover         list repos you can access
  triage update                 install the latest version
  triage paths                  show where triage keeps its files
  triage uninstall              remove triage from this computer
  triage version                print the version

Filters use GitHub-like syntax, e.g.
  triage ls repo:bookshelf type:bug status:"in progress" assignee:@me

Run 'triage <command> -h' for a command's flags.
`

// Run executes a command-line invocation other than launching the app.
func Run(env *app.Env, args []string) error {
	if len(args) == 0 {
		return UsageError{"no command"}
	}
	command, rest := args[0], args[1:]
	switch command {
	case "add", "new":
		return cmdAdd(env, rest)
	case "ls", "list":
		return cmdList(env, rest)
	case "open":
		return cmdOpen(env, rest)
	case "sync":
		return cmdSync(env, rest)
	case "repos", "repo":
		return cmdRepos(env, rest)
	case "update", "upgrade":
		return tui.RunUpdate(env)
	case "installed":
		// Shown by the install script; not listed in the help.
		return tui.PrintInstalled(env, env.Out)
	case "paths":
		return uninstall.PrintPaths(env.Out)
	case "uninstall":
		var interactive uninstall.Interactive
		if env.IsTerminal {
			interactive = func(plan uninstall.Plan) error { return tui.RunUninstall(env, plan) }
		}
		return uninstall.Run(rest, env.In, env.Out, env.Err, interactive)
	case "version", "--version", "-v":
		fmt.Fprintln(env.Out, "triage", env.Version)
		return nil
	case "help", "--help", "-h":
		fmt.Fprint(env.Out, usage)
		return nil
	default:
		return UsageError{fmt.Sprintf("unknown command %q", command)}
	}
}

// quietCommands never print the update notice.
var quietCommands = map[string]bool{
	"update": true, "upgrade": true, "version": true, "--version": true, "-v": true,
	"help": true, "--help": true, "-h": true, "uninstall": true, "paths": true, "installed": true,
}

// Usage returns the top-level help text.
func Usage() string { return usage }

// parseInterspersed parses flags that may appear before or after
// positional arguments, returning the positional ones.
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) == 0 {
			return positional, nil
		}
		if args[0] == "--" {
			return append(positional, args[1:]...), nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func newFlags(env *app.Env, name, usageLine string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(env.Err)
	flags.Usage = func() {
		fmt.Fprintln(env.Err, "Usage: "+usageLine)
		flags.PrintDefaults()
	}
	return flags
}

func cmdAdd(env *app.Env, args []string) error {
	flags := newFlags(env, "triage add", "triage add [-r repo] [-t type] [-s status] [-l label] [-b body | -b -] [title]")
	repoFlag := flags.String("r", "", "repository (owner/name, or a tracked repo's name); defaults to the current directory's repo")
	typeFlag := flags.String("t", "", "type: bug, feature, docs, or task")
	statusFlag := flags.String("s", "", "status: idea, todo, in-progress, blocked, done, or wontdo")
	bodyFlag := flags.String("b", "", "body text, or - to read it from stdin")
	assignMe := flags.Bool("me", false, "assign the issue to yourself")
	var labels stringList
	flags.Var(&labels, "l", "extra label (repeatable)")
	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return quietHelp(err)
	}

	conv := env.Config.Labels
	draft := engine.Draft{Title: strings.Join(positional, " "), Labels: labels}
	if *typeFlag != "" {
		if draft.Type, err = conv.ParseType(*typeFlag); err != nil {
			return err
		}
	}
	draft.Status = issue.StatusTodo
	if *statusFlag != "" {
		if draft.Status, err = conv.ParseStatus(*statusFlag); err != nil {
			return err
		}
	}
	if draft.Repo, err = env.ResolveRepo(*repoFlag); err != nil {
		return err
	}
	draft.Body = *bodyFlag
	if *bodyFlag == "-" {
		data, err := io.ReadAll(env.In)
		if err != nil {
			return fmt.Errorf("read body from stdin: %w", err)
		}
		draft.Body = string(data)
	}
	if strings.TrimSpace(draft.Title) == "" {
		if !env.IsTerminal {
			return UsageError{"a title is required"}
		}
		help := fmt.Sprintf("Repo: %s · Type: %s · Status: %s", draft.Repo, draft.Type, draft.Status)
		doc, err := editor.Run(editor.Template("", draft.Body, help))
		if err != nil {
			return err
		}
		title, body, ok := editor.Parse(doc)
		if !ok {
			fmt.Fprintln(env.Err, "Nothing written; issue not created.")
			return nil
		}
		draft.Title, draft.Body = title, body
	}
	if *assignMe {
		me := env.Engine.Me(context.Background())
		if me == "" {
			return errors.New("can't tell who you are; are you logged in with `gh auth login`?")
		}
		draft.Assignees = []string{me}
	}

	if !env.Config.HasRepo(draft.Repo) {
		env.Config.AddRepo(draft.Repo)
		if err := env.SaveConfig(); err != nil {
			return err
		}
		fmt.Fprintf(env.Err, "Now tracking %s.\n", draft.Repo)
	}

	ops, err := env.Engine.Create(draft)
	if err != nil {
		return err
	}
	ids := make([]string, len(ops))
	for i, op := range ops {
		ids[i] = op.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := env.Engine.FlushOnly(ctx, ids...)
	if err != nil {
		return err
	}
	number := result.Created[ops[0].LocalID]
	switch {
	case number != 0:
		ref := fmt.Sprintf("%s#%d", gh.RepoName(draft.Repo), number)
		fmt.Fprintf(env.Out, "Created %s: %s\n", ref, strings.TrimSpace(draft.Title))
		if env.Client != nil {
			fmt.Fprintln(env.Out, env.Client.IssueURL(draft.Repo, number))
		}
		if result.Held > 0 && result.Err != nil {
			fmt.Fprintln(env.Err, "Couldn't set the status:", gh.UserMessage(result.Err))
		}
		return nil
	case result.Offline:
		fmt.Fprintln(env.Out, "Saved offline. It will be sent the next time triage runs online (or run `triage sync`).")
		return nil
	default:
		for _, op := range ops {
			_ = env.Engine.Discard(op)
		}
		if result.Err != nil {
			return errors.New(gh.UserMessage(result.Err))
		}
		return errors.New("the issue could not be created")
	}
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func quietHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return UsageError{err.Error()}
}

type listedIssue struct {
	Repo      string   `json:"repo"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
	Updated   string   `json:"updated_at"`
	URL       string   `json:"url"`
	Pending   bool     `json:"pending"`
}

func cmdList(env *app.Env, args []string) error {
	flags := newFlags(env, "triage ls", "triage ls [--cached] [--json] [-n limit] [query]")
	cached := flags.Bool("cached", false, "don't contact GitHub; use cached data")
	asJSON := flags.Bool("json", false, "print JSON")
	limit := flags.Int("n", 0, "show at most this many issues")
	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return quietHelp(err)
	}
	if len(env.Config.Repos) == 0 {
		return errors.New("no repos tracked yet; run `triage` or `triage repos add owner/name`")
	}
	conv := env.Config.Labels
	query := issue.ParseQuery(strings.Join(positional, " "))
	if err := query.Validate(conv); err != nil {
		return err
	}
	if !query.Has("is") && !query.Has("status") {
		query = issue.ParseQuery("is:open " + query.String())
	}

	if !*cached {
		if err := env.RequireClient(); err != nil {
			fmt.Fprintln(env.Err, "Showing cached issues:", err)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			_, _ = env.Engine.Flush(ctx)
			if _, err := env.Engine.RefreshAll(ctx, env.Config.Repos, true); err != nil {
				fmt.Fprintln(env.Err, "Showing cached issues:", gh.UserMessage(err))
			}
			cancel()
		}
	}

	snap, err := env.Engine.Snapshot(env.Config.Repos)
	if err != nil {
		return err
	}
	engine.SortIssues(snap.Issues)
	ctx := issue.MatchContext{Me: env.Engine.CachedMe(), Convention: conv}
	var matched []issue.Issue
	for _, i := range snap.Issues {
		if query.Match(i, ctx) {
			matched = append(matched, i)
			if *limit > 0 && len(matched) == *limit {
				break
			}
		}
	}

	if *asJSON {
		out := make([]listedIssue, 0, len(matched))
		for _, i := range matched {
			out = append(out, listedIssue{
				Repo: i.Repo, Number: i.Number, Title: i.Title, Type: conv.TypeOf(i).String(), Status: conv.StatusOf(i).String(),
				Labels: nonNil(i.Labels), Assignees: nonNil(i.Assignees), Updated: i.UpdatedAt.Format(time.RFC3339), URL: i.URL,
				Pending: len(snap.Pending[i.Key()]) > 0,
			})
		}
		enc := json.NewEncoder(env.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	if len(matched) == 0 {
		fmt.Fprintln(env.Err, "No issues match.")
		return nil
	}
	tw := tabwriter.NewWriter(env.Out, 0, 0, 2, ' ', 0)
	for _, i := range matched {
		marker := ""
		if len(snap.Pending[i.Key()]) > 0 {
			marker = " ↑"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s%s\n", i.Ref(), conv.StatusOf(i), conv.TypeOf(i), i.Title, marker)
	}
	return tw.Flush()
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func cmdOpen(env *app.Env, args []string) error {
	if len(args) != 1 {
		return UsageError{"usage: triage open <12 | repo#12 | owner/repo#12 | owner/repo>"}
	}
	ref := args[0]
	host := "github.com"
	if env.Client != nil {
		host = env.Client.Host()
	}
	if !strings.Contains(ref, "#") && strings.Contains(ref, "/") && !strings.HasPrefix(ref, "https://") {
		repo, err := gh.NormalizeRepo(ref)
		if err != nil {
			return err
		}
		return env.OpenURL(fmt.Sprintf("https://%s/%s/issues", host, repo))
	}
	repo, number, err := env.ParseIssueRef(ref)
	if err != nil {
		return err
	}
	return env.OpenURL(fmt.Sprintf("https://%s/%s/issues/%d", host, repo, number))
}

func cmdSync(env *app.Env, args []string) error {
	if len(args) != 0 {
		return UsageError{"usage: triage sync"}
	}
	if err := env.RequireClient(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := env.Engine.Flush(ctx)
	if err != nil {
		return err
	}
	if result.Offline {
		return errors.New(gh.UserMessage(result.Err))
	}
	if result.Sent > 0 {
		fmt.Fprintf(env.Out, "Sent %d change(s).\n", result.Sent)
	}
	if result.Held > 0 {
		fmt.Fprintf(env.Err, "%d change(s) need attention; open triage to review them.\n", result.Held)
	}
	results, err := env.Engine.RefreshAll(ctx, env.Config.Repos, true)
	changed := 0
	for _, r := range results {
		changed += r.Changed
	}
	if err != nil {
		return errors.New(gh.UserMessage(err))
	}
	fmt.Fprintf(env.Out, "Up to date (%d issue(s) updated).\n", changed)
	return nil
}

func cmdRepos(env *app.Env, args []string) error {
	if len(args) == 0 {
		if len(env.Config.Repos) == 0 {
			fmt.Fprintln(env.Err, "No repos tracked yet. Add one with `triage repos add owner/name`.")
			return nil
		}
		for _, repo := range env.Config.Repos {
			suffix := ""
			if strings.EqualFold(repo, env.Config.DefaultRepo) {
				suffix = " (default)"
			}
			fmt.Fprintln(env.Out, repo+suffix)
		}
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		if len(rest) == 0 {
			return UsageError{"usage: triage repos add <owner/name>..."}
		}
		var added []string
		for _, value := range rest {
			repo, err := gh.NormalizeRepo(value)
			if err != nil {
				return err
			}
			if env.Client != nil {
				if _, err := env.Client.ListLabels(context.Background(), repo); err != nil && !gh.IsOffline(err) {
					return errors.New(gh.UserMessage(err))
				}
			}
			if env.Config.AddRepo(repo) {
				added = append(added, repo)
			}
		}
		if err := env.SaveConfig(); err != nil {
			return err
		}
		for _, repo := range added {
			fmt.Fprintln(env.Out, "Tracking", repo)
		}
		return nil
	case "rm", "remove":
		if len(rest) == 0 {
			return UsageError{"usage: triage repos rm <owner/name>..."}
		}
		for _, value := range rest {
			repo, err := env.ResolveRepo(value)
			if err != nil {
				return err
			}
			if !env.Config.RemoveRepo(repo) {
				return fmt.Errorf("%s isn't tracked", repo)
			}
			_ = env.Engine.Store().ForgetRepo(repo)
			fmt.Fprintln(env.Out, "Stopped tracking", repo)
		}
		return env.SaveConfig()
	case "default":
		if len(rest) != 1 {
			return UsageError{"usage: triage repos default <owner/name>"}
		}
		repo, err := env.ResolveRepo(rest[0])
		if err != nil {
			return err
		}
		env.Config.AddRepo(repo)
		env.Config.DefaultRepo = repo
		if err := env.SaveConfig(); err != nil {
			return err
		}
		fmt.Fprintln(env.Out, "New issues go to", repo, "unless you're in another repo's directory.")
		return nil
	case "discover":
		if err := env.RequireClient(); err != nil {
			return err
		}
		repos, err := env.Client.ListUserRepos(context.Background(), 200)
		if err != nil {
			return errors.New(gh.UserMessage(err))
		}
		tw := tabwriter.NewWriter(env.Out, 0, 0, 2, ' ', 0)
		for _, repo := range repos {
			mark := " "
			if env.Config.HasRepo(repo.FullName) {
				mark = "✓"
			}
			fmt.Fprintf(tw, "%s %s\t%d open\t%s\n", mark, repo.FullName, repo.OpenIssues, truncate(repo.Description, 60))
		}
		return tw.Flush()
	default:
		return UsageError{fmt.Sprintf("unknown repos command %q", sub)}
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

// Main runs triage with args and returns the process exit code. launchApp
// starts the interactive app.
func Main(args []string, version, channel string, launchApp func(*app.Env) error) int {
	env, err := app.NewEnv(version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "triage:", err)
		return 1
	}
	env.FromRelease = channel == "release"
	if len(args) == 0 {
		err = launchApp(env)
	} else {
		if env.ConfigErr != nil && args[0] != "uninstall" && args[0] != "paths" {
			fmt.Fprintln(env.Err, "Warning:", env.ConfigErr)
		}
		err = Run(env, args)
		if err == nil && env.IsTerminal && !quietCommands[args[0]] {
			if release, ok := update.ShouldNotify(env.Engine.Store(), env.Version); ok {
				fmt.Fprintf(env.Err, "\ntriage %s is out (you have %s). Run `triage update` to get it.\n", release.Version, env.Version)
			}
		}
	}
	if err == nil {
		return 0
	}
	var usageErr UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(env.Err, "triage: %s\n\n%s", usageErr.msg, usage)
		return 2
	}
	fmt.Fprintln(env.Err, "triage:", err)
	return 1
}
