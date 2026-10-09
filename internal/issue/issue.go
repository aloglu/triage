// Package issue holds triage's view of a GitHub issue and the label
// conventions that give issues a type and a status.
package issue

import (
	"fmt"
	"strings"
	"time"

	"github.com/aloglu/triage/internal/gh"
)

// Issue is a GitHub issue as triage displays it.
type Issue struct {
	Repo        string    `json:"repo"`
	Number      int       `json:"number,omitempty"`
	LocalID     string    `json:"local_id,omitempty"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	StateReason string    `json:"state_reason,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	Assignees   []string  `json:"assignees,omitempty"`
	Author      string    `json:"author,omitempty"`
	Comments    int       `json:"comments,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ClosedAt    time.Time `json:"closed_at,omitempty"`
	URL         string    `json:"url,omitempty"`
}

const (
	StateOpen   = "open"
	StateClosed = "closed"

	ReasonCompleted  = "completed"
	ReasonNotPlanned = "not_planned"
)

// FromGitHub converts a REST API issue.
func FromGitHub(repo string, src gh.Issue) Issue {
	out := Issue{
		Repo:        repo,
		Number:      src.Number,
		Title:       src.Title,
		Body:        src.Body,
		State:       src.State,
		StateReason: src.StateReason,
		Author:      src.User.Login,
		Comments:    src.Comments,
		CreatedAt:   src.CreatedAt,
		UpdatedAt:   src.UpdatedAt,
		URL:         src.HTMLURL,
	}
	if src.ClosedAt != nil {
		out.ClosedAt = *src.ClosedAt
	}
	for _, label := range src.Labels {
		out.Labels = append(out.Labels, label.Name)
	}
	for _, user := range src.Assignees {
		out.Assignees = append(out.Assignees, user.Login)
	}
	return out
}

// Key identifies an issue across repos, including issues that haven't been
// created on GitHub yet.
func (i Issue) Key() string {
	if i.Number == 0 {
		return i.Repo + "@" + i.LocalID
	}
	return fmt.Sprintf("%s#%d", i.Repo, i.Number)
}

// Ref is the short human reference, e.g. "triage#12".
func (i Issue) Ref() string {
	if i.Number == 0 {
		return gh.RepoName(i.Repo) + "#new"
	}
	return fmt.Sprintf("%s#%d", gh.RepoName(i.Repo), i.Number)
}

func (i Issue) IsOpen() bool { return i.State != StateClosed }

func (i Issue) HasLabel(name string) bool { return containsFold(i.Labels, name) }

func (i Issue) IsAssigned(login string) bool { return containsFold(i.Assignees, login) }

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

// WithLabels returns labels with add appended and remove dropped, compared
// case-insensitively and without duplicates.
func WithLabels(labels, add, remove []string) []string {
	out := make([]string, 0, len(labels)+len(add))
	for _, label := range labels {
		if !containsFold(remove, label) && !containsFold(out, label) {
			out = append(out, label)
		}
	}
	for _, label := range add {
		if !containsFold(out, label) {
			out = append(out, label)
		}
	}
	return out
}
