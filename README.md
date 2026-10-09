# triage

A fast keyboard client for the issues in your GitHub repos.

triage shows every issue across the repos you care about in one inbox, opens instantly from a local cache, and turns the common moves (change status, label, assign, comment, close) into single keystrokes. Everything it does is an ordinary GitHub issue edit, so your issues look normal on GitHub and work with any other tool.

## Install

```bash
go install github.com/aloglu/triage/cmd/triage@latest
```

triage uses the [GitHub CLI](https://cli.github.com)'s login. If you haven't already:

```bash
gh auth login
```

Then run `triage`. On first launch it lists your repositories; pick the ones to track and you're done. If you start triage inside a repo's directory, that repo is preselected.

## Using it

| Key | Does |
|---|---|
| `j` `k` / arrows | move |
| `enter` | read the issue and its comments |
| `tab` | next view: Inbox, Mine, each repo, Closed |
| `/` | filter |
| `n` | new issue |
| `s` | set status |
| `>` `<` | move status forward / back |
| `t` `L` `a` | type, labels, assign yourself |
| `e` / `E` | edit here / in `$EDITOR` |
| `c` | comment |
| `x` | close or reopen |
| `u` | undo |
| `b` | board view |
| `#` `R` | jump to an issue / a repo |
| `:` | every command, searchable |
| `?` | all shortcuts |

Changes show up immediately and are sent to GitHub in the background. If you're offline they wait and go out when you're back; the header shows anything still queued. If someone else edits the same issue's text at the same time, triage shows both versions and lets you keep yours or theirs.

### Filters

Filters use GitHub-style qualifiers, and combine with the current view:

```text
type:bug status:"in progress" assignee:@me
repo:bookshelf label:ui -label:wontfix crash
no:assignee is:closed
```

## Types and statuses

triage stores everything as plain labels and GitHub's own open/closed state, reusing the labels GitHub creates in every repo:

| Type | Label | | Status | On GitHub |
|---|---|---|---|---|
| Bug | `bug` | | Idea | open + `idea` |
| Feature | `enhancement` | | Todo | open |
| Docs | `documentation` | | In progress | open + `in progress` |
| Task | none | | Blocked | open + `blocked` |
| | | | Done | closed as completed |
| | | | Won't do | closed as not planned |

Only `idea`, `in progress`, and `blocked` may need creating, and triage does that the first time you use them. If your repos already use other names, map them in the config (below).

## Command line

```bash
triage add "Fix the top bar" -t bug -s idea      # in a repo's directory, it goes there
triage add -r bookshelf                           # no title: write it in $EDITOR
git log -1 --format=%B | triage add "Follow-up" -b -
triage ls type:bug assignee:@me                   # --json for scripts
triage open 12                                    # or repo#12, owner/repo#12
triage sync                                       # send queued changes, fetch updates
triage repos add owner/name
triage repos default owner/name
```

Run `triage help` for everything.

## Configuration

Most settings live in the app (`:` → *Track repos…*, *Make … the default*, *Edit config file*). The file itself is `config.toml` in your config directory (`triage paths` shows where):

```toml
repos = ["aloglu/triage", "aloglu/bookshelf"]
default_repo = "aloglu/triage"   # where `triage add` goes outside a repo directory
refresh_minutes = 5

[labels]                         # only if your repos use different label names
in_progress = "wip"
feature = "feature"

[[views]]                        # extra tabs
name = "UI bugs"
query = "type:bug label:ui"
```

## Uninstall

```bash
triage uninstall --dry-run   # see what would be removed
triage uninstall
```

This removes the binary, config, and cache. Your issues and labels on GitHub are never touched. `--keep-data` removes only the binary.

## Development

```bash
make run     # start the app
make check   # vet, race-enabled tests, gofmt
make build   # bin/triage
```

The code is organized as `gh` (GitHub REST client), `issue` (types, statuses, filters), `store` (cache and outbox), `engine` (sync), `tui` (the app), and `cli` (commands). Tests run against an in-memory fake GitHub in `ghtest`.

## License

Released under the [MIT License](LICENSE).
