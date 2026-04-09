# triage

`triage` is a terminal workspace for managing software project work.

It is built for fast capture, editing, filtering, and review from the keyboard. You can use it as a local tool or sync items to GitHub Issues.

![triage screenshot](img/screenshot.png)

## Getting Started

Install with Go:

```bash
go install github.com/aloglu/triage/cmd/triage@latest
```

Or from source:

```bash
make install
triage
```

`triage` installs into your Go bin directory, usually `$(go env GOPATH)/bin`. If the command is not found after install, add that directory to your `PATH`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

On first launch, choose where your items should live. If you enable GitHub sync, `gh` must already be installed and authenticated, and `triage` will ask for a default repository.

## Working Model

Each item has five core parts:

- title
- project
- type (`feature`, `bug`, `chore`)
- stage (`idea`, `planned`, `active`, `blocked`, `done`)
- body

The main views are `all`, `archive`, and `trash`.

In GitHub mode, edits are kept locally until you sync, so capture and editing stay quick even when GitHub is involved.

## GitHub Sync

`triage` can sync to:

- a default repo
- a project-level repo default
- a per-item repo override

That makes it practical to keep a general inbox while routing project-specific work to dedicated repositories.

## Development

```bash
make run
make test
make build
```

## License

Released under the [MIT License](https://github.com/aloglu/triage/blob/main/LICENSE).
