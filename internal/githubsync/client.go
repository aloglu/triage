package githubsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aloglu/triage/internal/model"
)

const requestTimeout = 30 * time.Second

type apiRunner func(ctx context.Context, method, endpoint string, payload any, target any) error

type Client struct {
	run        apiRunner
	runGraphQL graphQLRunner
}

func NewClient() *Client {
	return &Client{
		run:        runAPIJSON,
		runGraphQL: runGraphQLJSON,
	}
}

type graphQLRunner func(ctx context.Context, query string, variables map[string]string, target any) error

type ConflictError struct {
	Local  model.Item
	Remote model.Item
}

func (e *ConflictError) Error() string {
	if e.Remote.IssueNumber > 0 {
		return fmt.Sprintf("issue #%d changed on GitHub since last sync", e.Remote.IssueNumber)
	}
	return "item changed on GitHub since last sync"
}

type issuePayload struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	State  string   `json:"state,omitempty"`
}

type issueResponse struct {
	Number      int       `json:"number"`
	NodeID      string    `json:"node_id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Labels      []label   `json:"labels"`
	PullRequest any       `json:"pull_request"`
}

type label struct {
	Name string `json:"name"`
}

func (c *Client) SyncRepo(repo string) ([]model.Item, error) {
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	var responses []issueResponse
	if err := c.run(context.Background(), "GET", fmt.Sprintf("repos/%s/issues?state=all&per_page=100", repo), nil, &responses); err != nil {
		return nil, err
	}

	items := make([]model.Item, 0, len(responses))
	for _, response := range responses {
		if response.PullRequest != nil {
			continue
		}
		item, err := issueToItem(repo, response)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return items, nil
}

func (c *Client) UpsertItem(repo string, item model.Item) (model.Item, error) {
	return c.upsertItem(repo, item, false)
}

func (c *Client) ForceUpsertItem(repo string, item model.Item) (model.Item, error) {
	return c.upsertItem(repo, item, true)
}

func (c *Client) DeleteIssue(repo string, issueNumber int) error {
	if repo == "" {
		return fmt.Errorf("repo is required")
	}
	if issueNumber == 0 {
		return nil
	}

	current, err := c.fetchIssue(repo, issueNumber)
	if err != nil {
		if strings.Contains(err.Error(), "Not Found") {
			return nil
		}
		return err
	}
	if current.NodeID == "" {
		return fmt.Errorf("issue #%d is missing a node ID", issueNumber)
	}

	runner := c.runGraphQL
	if runner == nil {
		runner = runGraphQLJSON
	}

	var response struct {
		Data struct {
			DeleteIssue struct {
				ClientMutationID string `json:"clientMutationId"`
			} `json:"deleteIssue"`
		} `json:"data"`
	}
	return runner(context.Background(), `
mutation($issueId: ID!) {
  deleteIssue(input: {issueId: $issueId}) {
    clientMutationId
  }
}
`, map[string]string{"issueId": current.NodeID}, &response)
}

func (c *Client) upsertItem(repo string, item model.Item, force bool) (model.Item, error) {
	if repo == "" {
		return item, fmt.Errorf("repo is required")
	}

	if item.IssueNumber == 0 {
		return c.createItem(repo, item)
	}

	return c.updateItem(repo, item, force)
}

func (c *Client) createItem(repo string, item model.Item) (model.Item, error) {
	payload := issuePayload{
		Title:  item.Title,
		Body:   SerializeBody(item),
		Labels: item.Labels(),
	}
	if item.Stage == model.StageDone {
		payload.State = "closed"
	}

	var response issueResponse
	if err := c.run(context.Background(), "POST", fmt.Sprintf("repos/%s/issues", repo), payload, &response); err != nil {
		return item, err
	}
	return issueToItem(repo, response)
}

func (c *Client) updateItem(repo string, item model.Item, force bool) (model.Item, error) {
	current, err := c.fetchIssue(repo, item.IssueNumber)
	if err != nil {
		return item, err
	}

	oldItem, err := issueToItem(repo, current)
	if err != nil {
		return item, err
	}

	if !force && !item.RemoteUpdatedAt.IsZero() && !current.UpdatedAt.Equal(item.RemoteUpdatedAt) {
		return item, &ConflictError{
			Local:  item,
			Remote: oldItem,
		}
	}

	payload := issuePayload{
		Title:  item.Title,
		Body:   SerializeBody(item),
		Labels: mergeLabels(current.labelNames(), oldItem, item),
		State:  desiredIssueState(item),
	}

	var response issueResponse
	if err := c.run(context.Background(), "PATCH", fmt.Sprintf("repos/%s/issues/%d", repo, item.IssueNumber), payload, &response); err != nil {
		return item, err
	}
	return issueToItem(repo, response)
}

func (c *Client) fetchIssue(repo string, issueNumber int) (issueResponse, error) {
	var response issueResponse
	err := c.run(context.Background(), "GET", fmt.Sprintf("repos/%s/issues/%d", repo, issueNumber), nil, &response)
	return response, err
}

func runAPIJSON(ctx context.Context, method, endpoint string, payload any, target any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	args := []string{"api", endpoint, "--method", method}
	var stdin []byte
	if payload != nil {
		args = append(args, "--input", "-")
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
		stdin = data
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("gh api %s %s: %s", method, endpoint, message)
	}

	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode gh response: %w", err)
	}

	return nil
}

func runGraphQLJSON(ctx context.Context, query string, variables map[string]string, target any) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	args := []string{"api", "graphql", "-f", "query=" + query}
	for key, value := range variables {
		args = append(args, "-f", key+"="+value)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("gh api graphql: %s", message)
	}

	if target == nil {
		return nil
	}

	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode gh graphql response: %w", err)
	}
	return nil
}

func issueToItem(repo string, response issueResponse) (model.Item, error) {
	project, stage, body, err := ParseBody(response.Body)
	if err != nil {
		return model.Item{}, fmt.Errorf("issue #%d: %w", response.Number, err)
	}

	return model.Item{
		Title:           response.Title,
		Project:         project,
		Stage:           stage,
		Trashed:         hasLabel(response.labelNames(), "trashed"),
		Body:            body,
		CreatedAt:       response.CreatedAt,
		UpdatedAt:       response.UpdatedAt,
		RemoteUpdatedAt: response.UpdatedAt,
		IssueNumber:     response.Number,
		Repo:            repo,
		State:           response.State,
	}, nil
}

func desiredIssueState(item model.Item) string {
	if item.Trashed || item.Stage == model.StageDone {
		return "closed"
	}
	return "open"
}

func mergeLabels(existing []string, oldItem, newItem model.Item) []string {
	managed := map[string]struct{}{
		oldItem.Project:       {},
		newItem.Project:       {},
		string(oldItem.Stage): {},
		string(newItem.Stage): {},
		"trashed":             {},
	}
	for _, stage := range model.Stages {
		managed[string(stage)] = struct{}{}
	}

	labels := make([]string, 0, len(existing)+2)
	seen := map[string]struct{}{}

	for _, label := range existing {
		if _, ok := managed[label]; ok {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}

	for _, label := range newItem.Labels() {
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}

	sort.Strings(labels)
	return labels
}

func (r issueResponse) labelNames() []string {
	names := make([]string, 0, len(r.Labels))
	for _, label := range r.Labels {
		names = append(names, label.Name)
	}
	return names
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
