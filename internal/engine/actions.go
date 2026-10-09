package engine

import (
	"fmt"
	"strings"

	"github.com/aloglu/triage/internal/issue"
	"github.com/aloglu/triage/internal/store"
)

// Draft describes a new issue.
type Draft struct {
	Repo      string
	Title     string
	Body      string
	Type      issue.Type
	Status    issue.Status
	Labels    []string
	Assignees []string
}

// Create queues a new issue. Statuses that need a closed issue are queued as
// a follow-up change.
func (e *Engine) Create(d Draft) ([]store.Op, error) {
	title := strings.TrimSpace(d.Title)
	if title == "" {
		return nil, fmt.Errorf("a title is required")
	}
	if d.Repo == "" {
		return nil, fmt.Errorf("a repository is required")
	}
	body := strings.TrimSpace(d.Body)
	blank := issue.Issue{State: issue.StateOpen}
	labels := issue.WithLabels(d.Labels, e.conv.SetType(blank, d.Type).AddLabels, nil)
	status := e.conv.SetStatus(blank, d.Status)
	labels = issue.WithLabels(labels, status.AddLabels, nil)

	op, err := e.Submit(store.Op{Kind: store.OpCreate, Repo: d.Repo, Title: &title, Body: &body, Labels: labels, Assignees: d.Assignees})
	if err != nil {
		return nil, err
	}
	ops := []store.Op{op}
	if status.State != "" {
		follow, err := e.Submit(store.Op{Kind: store.OpState, Repo: d.Repo, LocalID: op.LocalID, State: status.State, StateReason: status.StateReason})
		if err != nil {
			return ops, err
		}
		ops = append(ops, follow)
	}
	return ops, nil
}

func target(i issue.Issue) store.Op {
	return store.Op{Repo: i.Repo, Number: i.Number, LocalID: i.LocalID}
}

// Edit queues a title and/or body change. Unchanged fields are left alone.
func (e *Engine) Edit(i issue.Issue, title, body string) ([]store.Op, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("a title is required")
	}
	op := target(i)
	op.Kind = store.OpEdit
	if title != i.Title {
		op.Title = &title
	}
	if normalizeBody(body) != normalizeBody(i.Body) {
		body = strings.TrimSpace(body)
		op.Body = &body
	}
	if op.Title == nil && op.Body == nil {
		return nil, nil
	}

	// Fold into an earlier edit that hasn't been sent, so the base values
	// still describe what GitHub had when editing started.
	if pending, ok := e.unsentEdit(i); ok {
		if op.Title != nil {
			pending.Title = op.Title
		}
		if op.Body != nil {
			pending.Body = op.Body
		}
		if updated, err := e.store.Update(pending); err != nil {
			return nil, err
		} else if updated {
			return []store.Op{pending}, nil
		}
	}

	if op.Title != nil {
		base := i.Title
		op.BaseTitle = &base
	}
	if op.Body != nil {
		base := i.Body
		op.BaseBody = &base
	}
	queued, err := e.Submit(op)
	if err != nil {
		return nil, err
	}
	return []store.Op{queued}, nil
}

func (e *Engine) unsentEdit(i issue.Issue) (store.Op, bool) {
	ops, err := e.store.Pending()
	if err != nil {
		return store.Op{}, false
	}
	for idx := len(ops) - 1; idx >= 0; idx-- {
		op := ops[idx]
		if op.Kind == store.OpEdit && op.Target() == target(i).Target() && !op.Sending() && !op.Held() {
			return op, true
		}
	}
	return store.Op{}, false
}

// Apply queues the label and state edits in ch.
func (e *Engine) Apply(i issue.Issue, ch issue.Change) ([]store.Op, error) {
	var ops []store.Op
	if len(ch.AddLabels) > 0 || len(ch.RemoveLabels) > 0 {
		op := target(i)
		op.Kind, op.Add, op.Remove = store.OpLabels, ch.AddLabels, ch.RemoveLabels
		queued, err := e.Submit(op)
		if err != nil {
			return ops, err
		}
		ops = append(ops, queued)
	}
	if ch.State != "" {
		op := target(i)
		op.Kind, op.State, op.StateReason = store.OpState, ch.State, ch.StateReason
		queued, err := e.Submit(op)
		if err != nil {
			return ops, err
		}
		ops = append(ops, queued)
	}
	return ops, nil
}

func (e *Engine) SetStatus(i issue.Issue, s issue.Status) ([]store.Op, error) {
	return e.Apply(i, e.conv.SetStatus(i, s))
}

func (e *Engine) SetType(i issue.Issue, t issue.Type) ([]store.Op, error) {
	return e.Apply(i, e.conv.SetType(i, t))
}

// Assign adds and removes assignees.
func (e *Engine) Assign(i issue.Issue, add, remove []string) ([]store.Op, error) {
	if len(add) == 0 && len(remove) == 0 {
		return nil, nil
	}
	op := target(i)
	op.Kind, op.Add, op.Remove = store.OpAssign, add, remove
	queued, err := e.Submit(op)
	if err != nil {
		return nil, err
	}
	return []store.Op{queued}, nil
}

// Comment queues a new comment.
func (e *Engine) Comment(i issue.Issue, body string) ([]store.Op, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("the comment is empty")
	}
	op := target(i)
	op.Kind, op.Comment = store.OpComment, body
	queued, err := e.Submit(op)
	if err != nil {
		return nil, err
	}
	return []store.Op{queued}, nil
}

// Restore undoes earlier changes to an issue: changes still in the outbox
// are discarded, and anything already sent is reverted by queueing the
// opposite change. before is the issue as it was, current as it is now.
func (e *Engine) Restore(opIDs []string, before, current issue.Issue) ([]store.Op, error) {
	allDiscarded := true
	for _, id := range opIDs {
		discarded, err := e.store.Discard(id)
		if err != nil {
			return nil, err
		}
		if !discarded {
			allDiscarded = false
		}
	}
	if allDiscarded {
		return nil, nil
	}
	if current.Number == 0 && current.LocalID == "" {
		return nil, nil
	}
	// The new issue was already created; the closest undo is closing it.
	if before.Title == "" && before.Number == 0 && before.LocalID == "" {
		return e.Apply(current, issue.Change{State: issue.StateClosed, StateReason: issue.ReasonNotPlanned})
	}

	var ops []store.Op
	if current.Title != before.Title || normalizeBody(current.Body) != normalizeBody(before.Body) {
		edit, err := e.Edit(current, before.Title, before.Body)
		if err != nil {
			return ops, err
		}
		ops = append(ops, edit...)
	}
	ch := issue.Change{
		AddLabels:    missing(before.Labels, current.Labels),
		RemoveLabels: missing(current.Labels, before.Labels),
	}
	if current.State != before.State || (before.State == issue.StateClosed && current.StateReason != before.StateReason) {
		ch.State, ch.StateReason = before.State, before.StateReason
		if before.State == issue.StateOpen {
			ch.StateReason = "reopened"
		}
	}
	applied, err := e.Apply(current, ch)
	ops = append(ops, applied...)
	if err != nil {
		return ops, err
	}
	assigned, err := e.Assign(current, missing(before.Assignees, current.Assignees), missing(current.Assignees, before.Assignees))
	return append(ops, assigned...), err
}

// missing returns the values in want that aren't in have.
func missing(want, have []string) []string {
	var out []string
	for _, value := range want {
		found := false
		for _, candidate := range have {
			if strings.EqualFold(candidate, value) {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}

// Retry clears a held op's error so the next flush tries it again.
func (e *Engine) Retry(op store.Op) error {
	op.Error = ""
	op.Conflict = nil
	_, err := e.store.Update(op)
	return err
}

// Discard drops a queued op.
func (e *Engine) Discard(op store.Op) error {
	_, err := e.store.Discard(op.ID)
	return err
}

// KeepMine resolves a conflict by overwriting GitHub's version with the
// user's edit.
func (e *Engine) KeepMine(op store.Op) error {
	if op.Conflict == nil {
		return nil
	}
	if op.Conflict.RemoteTitle != nil {
		op.BaseTitle = op.Conflict.RemoteTitle
	}
	if op.Conflict.RemoteBody != nil {
		op.BaseBody = op.Conflict.RemoteBody
	}
	op.Conflict = nil
	_, err := e.store.Update(op)
	return err
}

// TakeTheirs resolves a conflict by dropping the user's edit.
func (e *Engine) TakeTheirs(op store.Op) error { return e.Discard(op) }
