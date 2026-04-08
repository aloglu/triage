# triage

`triage` is a keyboard-first terminal UI for managing software project items.

It supports local JSON storage or GitHub Issues sync, inline editing, per-item repo targeting, archive/trash flows, and conflict handling for remote edits.

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

## First Run

On first launch, `triage` asks you to choose:

- local-only mode
- GitHub Issues sync

If you choose GitHub sync, enter the default repository in `owner/repo` form.

Current data/config locations are resolved through Go's user config directory:

- config: `triage/config.json`
- local cache/data: `triage/items.json`

On Linux, this is typically under `~/.config/triage/`.

## Item Model

Each item has:

- `title`
- `project`
- `stage`
- `body`

`stage` is fixed to:

- `idea`
- `planned`
- `active`
- `blocked`
- `done`

The main views are:

- `all`
- `archive`
- `trash`

## GitHub Sync

GitHub sync uses the `gh` CLI and expects you to already be authenticated.

Each item maps to one GitHub issue:

- issue title = item title
- issue body = YAML frontmatter + freeform markdown body
- labels are derived from `project`, `stage`, and trash state

Example issue body:

```md
---
project: triage
stage: active
---

Freeform notes here.
```

`triage` keeps:

- one default GitHub repo in config
- an optional per-item `repo` override
- a tracked repo list for startup and manual sync

That lets you keep a default inbox repo while sending specific items to other repositories.

If you change an existing synced item's repo, `triage` treats that as a move:

- create the issue in the new repo
- update the local item to point at the new repo
- delete the old issue from the old repo

If deleting the old issue fails, the move still succeeds and `triage` shows a warning instead of risking data loss.

Lifecycle behavior:

- `done` closes the GitHub issue and moves the item to `archive`
- `delete` moves the item to `trash`, adds a `trashed` label, and closes the issue
- `restore` removes the `trashed` label and reopens the issue
- `purge` permanently deletes the item locally and deletes the GitHub issue

`purge` requires sufficient GitHub permissions to delete issues.

## Local Mode

In local mode, items live only in the local JSON store.

Import and export are local-mode only:

- `:export json /path/to/file.json`
- `:import json /path/to/file.json`

Import replaces the current local item set after confirmation. Import and export do not automatically write to GitHub.

## Development

Run tests:

```bash
make test
```

Build a local binary:

```bash
make build
```

## License

Released under the [MIT License](https://github.com/aloglu/triage/blob/main/LICENSE).
