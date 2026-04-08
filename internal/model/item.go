package model

import (
	"strings"
	"time"
)

type Stage string

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

type Item struct {
	Title           string    `json:"title"`
	Project         string    `json:"project"`
	Stage           Stage     `json:"stage"`
	Trashed         bool      `json:"trashed,omitempty"`
	Body            string    `json:"body"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	RemoteUpdatedAt time.Time `json:"remote_updated_at,omitempty"`
	IssueNumber     int       `json:"issue_number"`
	Repo            string    `json:"repo"`
	State           string    `json:"state,omitempty"`
}

func (i Item) IsDone() bool {
	return i.Stage == StageDone
}

func (i Item) IsTrashed() bool {
	return i.Trashed
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
	labels := []string{i.Project, string(i.Stage)}
	if i.Trashed {
		labels = append(labels, "trashed")
	}
	return labels
}
