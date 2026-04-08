# triage

`triage` is a keyboard-first terminal UI for managing software project items.

It supports:

- local-only JSON storage
- optional GitHub Issues sync
- per-item GitHub repo targeting
- local JSON import/export
- inline editing
- command palette with autocomplete
- conflict handling for remote edits

![triage screenshot](img/screenshot.png)

## Install

> [!NOTE]
> Install commands place `triage` in your Go bin directory, usually `$(go env GOPATH)/bin`. Ensure that directory is on `PATH`:
>
> ```bash
> export PATH="$PATH:$(go env GOPATH)/bin"
> ```
>
> Then reload your shell and run `triage`.

### Go install

```bash
go install github.com/aloglu/triage/cmd/triage@latest
```

### From source

```bash
make install
```

Or install directly with Go:

```bash
go install ./cmd/triage
```

### Run without installing

```bash
make run
```

## Development

Run tests:

```bash
make test
```

Build a local binary:

```bash
make build
```

## First Run

On first launch, `triage` asks you to choose:

- local-only mode
- GitHub Issues sync

If you choose GitHub sync, enter the target repository in `owner/repo` form.

Current data/config locations are resolved through Go's user config directory:

- config: `triage/config.json`
- local cache/data: `triage/items.json`

On Linux, this is typically under `~/.config/triage/`.

## GitHub Sync

GitHub sync uses the `gh` CLI and expects you to already be authenticated.

Each item maps to one GitHub issue:

- issue title = item title
- issue body = YAML frontmatter + freeform markdown body
- labels are derived from `project`, `stage`, and trash state

`triage` keeps:

- one default GitHub repo in config
- an optional per-item `repo` override
- a tracked repo list for startup/manual sync

That means:

- general items can keep syncing to your default repo
- project-specific items can target a different repo item-by-item
- startup and manual sync fetch from every tracked repo, not just one

If you change an existing synced item's repo, `triage` treats that as a move:

- create the issue in the new repo
- update the local item to point at the new repo
- delete the old issue from the old repo

If deleting the old issue fails, the move still succeeds and `triage` shows a warning instead of risking data loss.

Example issue body:

```md
---
project: triage
stage: active
---

Freeform notes here.
```

### Workflow states

`stage` is fixed to:

- `idea`
- `planned`
- `active`
- `blocked`
- `done`

### Views

- `all`: non-archived, non-trashed items
- `archive`: completed items (`stage: done`)
- `trash`: deleted-but-recoverable items

### GitHub lifecycle behavior

- `done` closes the GitHub issue and moves the item to `archive`
- `delete` moves the item to `trash`, adds a `trashed` label, and closes the issue
- `restore` removes the `trashed` label and reopens the issue
- `purge` permanently deletes the item locally and deletes the GitHub issue

`purge` requires sufficient GitHub permissions to delete issues.

Autocomplete is built into the command palette:

- unique matches show inline ghost completion
- ambiguous matches open a suggestion list above the footer
- `tab`, `→`, or `enter` accept the highlighted suggestion

## License

Released under the [MIT License](https://github.com/aloglu/triage/blob/main/LICENSE).
