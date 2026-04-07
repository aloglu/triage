# Triage v0 Spec

## Overview

Triage is a terminal UI for managing software project items. It must stay visually polished but structurally simple, with a strong focus on keyboard-driven workflows, adaptive terminal layouts, and optional GitHub Issues sync.

The application code lives in its own repository. User data may be kept locally or synced to a user-selected GitHub repository via Issues.

## Product Goals

- Provide a fast, elegant TUI with restrained styling.
- Support dynamic layout changes based on terminal size.
- Keep the core workflow small enough to avoid UI and sync complexity.
- Treat GitHub Issues as a sync backend, not as a separate workflow the user must manually maintain.
- Support vim-like navigation and command patterns.

## Non-Goals For v0

- Full Kanban board behavior
- Multi-user collaboration semantics beyond GitHub being the remote source of truth
- Rich custom schemas beyond the fixed v0 item model
- Nested items, subtasks, or project metadata records

## Data Model

### Item

One GitHub issue represents one item.

Required editable fields:

- `title`
- `project`
- `stage`
- `body`

Read-only metadata:

- `created_at`
- `updated_at`
- `issue_number`
- `repo`

### Project

`project` is a required freeform string field on each item.

Projects are not first-class records in v0. They behave like grouping keys or folder names. The UI should support project-name autocomplete using names already seen in stored items.

## Workflow Field

v0 uses a single fixed workflow field:

- `stage`

Allowed values:

- `idea`
- `planned`
- `active`
- `blocked`
- `done`

These values are intentionally simple:

- `idea`: rough thought or capture
- `planned`: concrete plan exists
- `active`: currently being worked on
- `blocked`: cannot progress yet
- `done`: completed

Completed items should be hidden by default in list views unless the user toggles them on.

## Storage Modes

### Local-only

- Storage backend: local JSON
- The local JSON file is the source of truth

### Sync-enabled

- Storage backend: GitHub Issues in a user-selected repository
- GitHub is the source of truth
- A local cache may be kept for startup speed and offline display, but must not override remote truth after sync

The app must not hardcode `aloglu/triage-inbox`. That repository is the current user setup, but v0 must treat the sync repository as configurable.

## GitHub Issue Format

Issue title is the item title.

Issue body format:

1. YAML frontmatter
2. Blank line
3. Freeform Markdown body

Example:

```md
---
project: triage
stage: active
---

Freeform notes, links, and implementation details.
```

The app should treat everything after the frontmatter as one freeform Markdown body. v0 does not impose sections such as Notes or Links.

## Labels

GitHub labels are derived directly from frontmatter values with no prefixes.

Example:

- `project: triage` produces the label `triage`
- `stage: active` produces the label `active`

The app should manage labels it derives from current frontmatter values while leaving unrelated labels untouched.

## Sync Behavior

### First-run setup

On first run, the user should choose between:

- local-only mode
- GitHub sync mode

If GitHub sync is selected, the app should prompt for the target repository in `owner/repo` form.

Users who start in local-only mode must be able to enable sync later.

### Authentication

v0 should rely on existing terminal authentication, with `gh` as the preferred integration path if available.

### Sync triggers

- Auto-fetch on startup when sync is enabled
- Manual sync on demand
- Auto-push after saving item edits

### Conflict handling

If the same item has unsynced local edits and also changed remotely, the app must show a conflict UI instead of guessing.

If only the remote copy changed, the local cache should refresh automatically.

## TUI Behavior

The UI should adapt to terminal size changes at runtime.

The main screen should emphasize items and detail views over project navigation.

Recommended layout:

- narrow left rail for project and filter context
- main item list as the primary pane
- detail pane for the selected item

The detail pane should support inline editing. Pressing `e` should switch the detail view into edit mode, including title editing and other editable item fields.

## Main List Behavior

Primary columns:

- `title`
- `project`
- `stage`
- `updated_at`

Default sorting:

1. `updated_at` descending
2. `created_at` descending

Sorting should be user-changeable in the UI.

## Keybinding Direction

The interaction model should follow vim-inspired conventions similar to LazyVim.

Required directions for v0:

- `hjkl` navigation
- `/` search
- `:` command mode
- `n` new item
- `e` edit selected item
- `s` sync

Additional bindings can be defined during implementation, but the app should remain modal and keyboard-first.

## Framework Recommendation

Recommended stack:

- Go
- Bubble Tea for application state and terminal event handling
- Lip Gloss for styling

Reasoning:

- Bubble Tea is a strong fit for terminal apps that need resize handling, keyboard interaction, and composable views.
- Lip Gloss provides enough styling control to make the interface feel refined without pushing the implementation into a fragile custom rendering layer.
- This stack is mature, common in the Go TUI ecosystem, and aligned with the desired balance of elegance and restraint.

## Open Questions For Implementation

These are intentionally deferred to implementation planning rather than left undefined:

- exact local JSON file path and cache layout
- exact conflict-resolution screen behavior
- exact command palette and additional keymap
- responsive behavior for very narrow terminals
- exact `gh` command/API integration layer
