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

On first launch, choose where your items should live. If you enable GitHub sync, `gh` must already be installed and authenticated, and `triage` will ask for a default repository. A short Getting Started guide explains the core workflow; reopen it anytime with `:welcome`.

## Working Model

Each item has five core parts:

- title
- project
- type (`feature`, `bug`, `chore`)
- stage (`idea`, `planned`, `active`, `blocked`, `done`)
- body

The main views are `all`, `archive`, and `trash`.

In GitHub mode, edits are kept locally until you sync, so capture and editing stay quick even when GitHub is involved.

You can also drop draft files into a configurable drafts folder and let `triage` import them on startup or with `:drafts`.

## GitHub Sync

`triage` can sync to:

- a default repo
- a project-level repo default
- a per-item repo override

That makes it practical to keep a general inbox while routing project-specific work to dedicated repositories.

Manage repository routing from inside the app:

```text
:repo show
:repo default owner/repo
:repo project <project> owner/repo
:repo clear <project>
```

Edits remain local until you press `S` and confirm the sync. Use `:repos` to inspect the default repo, project mappings, and all currently tracked repositories.

### GitHub labels

`triage/managed` marks issues whose metadata labels are managed by the app. Issues without that marker keep their labels unchanged. On marked issues, triage uses familiar labels such as `bug`, `planned`, and the project name while preserving unrelated labels. Existing namespaced labels such as `triage/type/bug` are migrated only when you review and confirm a sync.

Metadata labels are optional. To stop triage from creating, updating, or removing conventional metadata labels—and have new issues receive only the ownership marker—run:

```text
:metadata-labels off
```

Existing conventional labels are left untouched when this setting is off; project, type, stage, and trash state remain in the issue body. Restore GitHub-facing metadata labels with `:metadata-labels on`. Project-label routing remains configurable with `:project-label always`, `:project-label auto`, or `:project-label never`.

## Uninstall

Before removing triage, inspect `config.json` and note the `data_file` and `drafts_folder` values if you customized them. By default, the configuration, local item database, drafts, and processed drafts all live in the same `triage` directory:

- Linux: `${XDG_CONFIG_HOME:-$HOME/.config}/triage`
- macOS: `$HOME/Library/Application Support/triage`
- Windows: `%AppData%\triage`

Back up anything you want to keep, close the app, and then remove the Go-installed binary and the default application directory.

On Linux:

```bash
triage_bin_dir="$(go env GOBIN)"
if [ -z "$triage_bin_dir" ]; then
  triage_bin_dir="$(go env GOPATH)/bin"
fi
rm "$triage_bin_dir/triage"
rm -r "${XDG_CONFIG_HOME:-$HOME/.config}/triage"
```

On macOS:

```bash
triage_bin_dir="$(go env GOBIN)"
if [ -z "$triage_bin_dir" ]; then
  triage_bin_dir="$(go env GOPATH)/bin"
fi
rm "$triage_bin_dir/triage"
rm -r "$HOME/Library/Application Support/triage"
```

On Windows PowerShell:

```powershell
$triageBinDir = go env GOBIN
if (-not $triageBinDir) {
    $triageBinDir = Join-Path (go env GOPATH) "bin"
}
Remove-Item (Join-Path $triageBinDir "triage.exe")
Remove-Item -Recurse (Join-Path $env:APPDATA "triage")
```

If `data_file` or `drafts_folder` points outside the default application directory, remove those paths separately only if they are dedicated to triage. A custom drafts folder may contain files you want to keep.

Uninstalling removes only the local application and its local data. It does not delete synced GitHub issues or repository labels. You normally should not remove your Go bin directory from `PATH`, because other Go-installed tools may use it.

## Development

```bash
make run
make test
make build
```

## License

Released under the [MIT License](https://github.com/aloglu/triage/blob/main/LICENSE).
