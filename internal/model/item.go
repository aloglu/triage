package model

import (
	"strings"
	"time"
)

type Stage string
type Type string
type SyncOperation string

const (
	StageIdea    Stage = "idea"
	StagePlanned Stage = "planned"
	StageActive  Stage = "active"
	StageBlocked Stage = "blocked"
	StageDone    Stage = "done"
)

var Stages = []Stage{
	StageIdea,
	StagePlanned,
	StageActive,
	StageBlocked,
	StageDone,
}

const (
	TypeFeature Type = "feature"
	TypeBug     Type = "bug"
	TypeChore   Type = "chore"
)

var Types = []Type{
	TypeFeature,
	TypeBug,
	TypeChore,
}

const (
	SyncNone    SyncOperation = ""
	SyncCreate  SyncOperation = "create"
	SyncUpdate  SyncOperation = "update"
	SyncDelete  SyncOperation = "delete"
	SyncRestore SyncOperation = "restore"
	SyncPurge   SyncOperation = "purge"
)

type Item struct {
	Title           string    `json:"title"`
	Project         string    `json:"project"`
	Type            Type      `json:"type"`
	Stage           Stage     `json:"stage"`
	Trashed         bool      `json:"trashed,omitempty"`
	Body            string    `json:"body"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	RemoteUpdatedAt time.Time `json:"remote_updated_at,omitempty"`
	IssueNumber     int       `json:"issue_number"`
	Repo            string    `json:"repo"`
	SyncedRepo      string    `json:"synced_repo,omitempty"`
	PendingSync     SyncOperation `json:"pending_sync,omitempty"`
	SyncConflict    bool      `json:"sync_conflict,omitempty"`
	SyncError       string    `json:"sync_error,omitempty"`
	State           string    `json:"state,omitempty"`
}

func (i Item) IsDone() bool {
	return i.Stage == StageDone
}

func (i Item) IsTrashed() bool {
	return i.Trashed
}

func (i Item) HasPendingSync() bool {
	return i.PendingSync != SyncNone
}

func (i Item) IsLocallyPurged() bool {
	return i.PendingSync == SyncPurge
}

func (i Item) RemoteRepo() string {
	if strings.TrimSpace(i.SyncedRepo) != "" {
		return strings.TrimSpace(i.SyncedRepo)
	}
	if i.IssueNumber == 0 {
		return ""
	}
	return strings.TrimSpace(i.Repo)
}

func (i Item) PendingSyncLabel() string {
	switch i.PendingSync {
	case SyncCreate:
		return "create"
	case SyncUpdate:
		return "update"
	case SyncDelete:
		return "trash"
	case SyncRestore:
		return "restore"
	case SyncPurge:
		return "purge"
	default:
		return ""
	}
}

func (i Item) Matches(query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}

	haystacks := []string{
		i.Title,
		i.Project,
		i.Repo,
		string(i.NormalizedType()),
		string(i.Stage),
		i.Body,
	}
	if i.Trashed {
		haystacks = append(haystacks, "trashed")
	}

	for _, haystack := range haystacks {
		if strings.Contains(strings.ToLower(haystack), query) {
			return true
		}
	}

	return false
}

func (i Item) Labels() []string {
	labels := []string{i.Project, string(i.NormalizedType()), string(i.Stage)}
	if i.Trashed {
		labels = append(labels, "trashed")
	}
	seen := make(map[string]struct{}, len(labels))
	deduped := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		deduped = append(deduped, label)
	}
	return deduped
}

func (i Item) NormalizedType() Type {
	if i.Type == "" {
		return TypeFeature
	}
	return i.Type
}
