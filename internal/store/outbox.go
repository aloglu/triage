package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpKind is the kind of change an outbox entry makes.
type OpKind string

const (
	OpCreate  OpKind = "create"
	OpEdit    OpKind = "edit"
	OpLabels  OpKind = "labels"
	OpState   OpKind = "state"
	OpAssign  OpKind = "assign"
	OpComment OpKind = "comment"
)

// Op is one change waiting to be sent to GitHub. Ops target an issue by
// Number, or by LocalID when the issue was created in triage and hasn't
// reached GitHub yet.
type Op struct {
	ID        string    `json:"id"`
	Kind      OpKind    `json:"kind"`
	Repo      string    `json:"repo"`
	Number    int       `json:"number,omitempty"`
	LocalID   string    `json:"local_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// Create and edit. For edits, nil means unchanged and Base* hold the
	// values the user started from, used to detect conflicting remote edits.
	Title     *string  `json:"title,omitempty"`
	Body      *string  `json:"body,omitempty"`
	BaseTitle *string  `json:"base_title,omitempty"`
	BaseBody  *string  `json:"base_body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`

	// Labels and assign.
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`

	// State.
	State       string `json:"state,omitempty"`
	StateReason string `json:"state_reason,omitempty"`

	// Comment.
	Comment string `json:"comment,omitempty"`

	// Error is set when GitHub rejected the op; it's held until the user
	// retries or discards it.
	Error string `json:"error,omitempty"`
	// Conflict is set when the issue changed on GitHub in a way that clashes
	// with this edit.
	Conflict *Conflict `json:"conflict,omitempty"`
	Attempts int       `json:"attempts,omitempty"`

	claimed bool
}

// Conflict records the remote values that clash with an edit.
type Conflict struct {
	RemoteTitle *string `json:"remote_title,omitempty"`
	RemoteBody  *string `json:"remote_body,omitempty"`
}

// Held reports whether the op needs the user's attention before it can be
// sent.
func (op Op) Held() bool { return op.Error != "" || op.Conflict != nil }

// Sending reports whether some process is sending the op right now.
func (op Op) Sending() bool { return op.claimed }

// Target identifies the issue an op applies to, matching issue.Issue.Key.
func (op Op) Target() string {
	if op.Number == 0 {
		return op.Repo + "@" + op.LocalID
	}
	return fmt.Sprintf("%s#%d", op.Repo, op.Number)
}

const (
	opSuffix      = ".json"
	claimedSuffix = ".sending"
	// staleClaim is how long a claim may be held before it's assumed that
	// the process sending it died.
	staleClaim = 2 * time.Minute
)

// NewID returns a sortable unique ID.
func NewID() string {
	var random [4]byte
	_, _ = rand.Read(random[:])
	return fmt.Sprintf("%019d-%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
}

func (s *Store) opPath(id string) string {
	return filepath.Join(s.paths.OutboxDir(), id+opSuffix)
}

// Enqueue adds op to the outbox, assigning it an ID.
func (s *Store) Enqueue(op Op) (Op, error) {
	op.ID = NewID()
	if op.CreatedAt.IsZero() {
		op.CreatedAt = time.Now().UTC()
	}
	if err := writeJSON(s.opPath(op.ID), op); err != nil {
		return op, fmt.Errorf("save change: %w", err)
	}
	return op, nil
}

// Pending returns every op in the outbox, oldest first, including ops being
// sent by another process. Claims that have gone stale are released.
func (s *Store) Pending() ([]Op, error) {
	entries, err := os.ReadDir(s.paths.OutboxDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read outbox: %w", err)
	}
	var ops []Op
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(s.paths.OutboxDir(), name)
		claimed := strings.HasSuffix(name, opSuffix+claimedSuffix)
		if !claimed && !strings.HasSuffix(name, opSuffix) {
			continue
		}
		if claimed {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > staleClaim {
				if os.Rename(path, strings.TrimSuffix(path, claimedSuffix)) == nil {
					path = strings.TrimSuffix(path, claimedSuffix)
					claimed = false
				}
			}
		}
		var op Op
		if err := readJSON(path, &op); err != nil {
			continue
		}
		op.claimed = claimed
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })
	return ops, nil
}

// ClaimOp claims an op and returns its current contents, which may be newer
// than a copy read earlier by Pending.
func (s *Store) ClaimOp(id string) (Op, bool, error) {
	ok, err := s.Claim(id)
	if !ok || err != nil {
		return Op{}, ok, err
	}
	var op Op
	if err := readJSON(s.opPath(id)+claimedSuffix, &op); err != nil {
		_ = os.Rename(s.opPath(id)+claimedSuffix, s.opPath(id))
		return Op{}, false, err
	}
	op.claimed = true
	return op, true, nil
}

// Claim marks op as being sent by this process. It returns false when the op
// is already claimed or gone, in which case the caller must leave it alone.
func (s *Store) Claim(id string) (bool, error) {
	path := s.opPath(id)
	err := os.Rename(path, path+claimedSuffix)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := time.Now()
	_ = os.Chtimes(path+claimedSuffix, now, now)
	return true, nil
}

// Release puts a claimed op back, saving any changes made to it.
func (s *Store) Release(op Op) error {
	path := s.opPath(op.ID)
	if err := writeJSON(path+claimedSuffix, op); err != nil {
		return err
	}
	return os.Rename(path+claimedSuffix, path)
}

// Complete removes a claimed op from the outbox.
func (s *Store) Complete(id string) error {
	err := os.Remove(s.opPath(id) + claimedSuffix)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Update rewrites an op that isn't being sent. It returns false when the op
// was claimed by someone else or no longer exists.
func (s *Store) Update(op Op) (bool, error) {
	ok, err := s.Claim(op.ID)
	if !ok || err != nil {
		return false, err
	}
	return true, s.Release(op)
}

// Discard deletes an op that isn't being sent. It returns false when the op
// was claimed by someone else or no longer exists.
func (s *Store) Discard(id string) (bool, error) {
	ok, err := s.Claim(id)
	if !ok || err != nil {
		return false, err
	}
	return true, s.Complete(id)
}
