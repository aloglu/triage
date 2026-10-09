// Package gh is a small GitHub REST client built on the user's existing
// GitHub CLI authentication.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

const requestTimeout = 30 * time.Second

// Client talks to the GitHub REST API.
type Client struct {
	http *http.Client
	base string
	host string

	mu    sync.Mutex
	login string
}

// New returns a client authenticated with the token `gh` uses (or
// GITHUB_TOKEN / GH_TOKEN). It fails with an ErrAuth error when no token is
// available.
func New() (*Client, error) {
	host, _ := auth.DefaultHost()
	if token, _ := auth.TokenForHost(host); token == "" {
		return nil, &Error{Kind: ErrAuth, Message: "not logged in to GitHub; run `gh auth login`"}
	}
	hc, err := api.NewHTTPClient(api.ClientOptions{Host: host, LogIgnoreEnv: true})
	if err != nil {
		return nil, err
	}
	return NewWithHTTP(hc, apiBase(host), host), nil
}

// NewWithHTTP returns a client that sends requests with hc to base, which
// must end with a slash. Tests use it to point the client at a fake server.
func NewWithHTTP(hc *http.Client, base, host string) *Client {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return &Client{http: hc, base: base, host: host}
}

// Host returns the GitHub host the client is authenticated against.
func (c *Client) Host() string { return c.host }

func apiBase(host string) string {
	if host == "" || host == "github.com" {
		return "https://api.github.com/"
	}
	if auth.IsTenancy(host) {
		return "https://api." + host + "/"
	}
	return "https://" + host + "/api/v3/"
}

// Label is a GitHub issue label.
type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// User is a GitHub account reference.
type User struct {
	Login string `json:"login"`
}

// Issue is a GitHub issue as returned by the REST API.
type Issue struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	State       string     `json:"state"`
	StateReason string     `json:"state_reason"`
	Labels      []Label    `json:"labels"`
	Assignees   []User     `json:"assignees"`
	User        User       `json:"user"`
	Comments    int        `json:"comments"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	HTMLURL     string     `json:"html_url"`
	PullRequest *struct{}  `json:"pull_request,omitempty"`
}

// Comment is an issue comment.
type Comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      User      `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}

// RepoSummary describes a repository the user can access.
type RepoSummary struct {
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Private     bool      `json:"private"`
	Archived    bool      `json:"archived"`
	HasIssues   bool      `json:"has_issues"`
	OpenIssues  int       `json:"open_issues_count"`
	PushedAt    time.Time `json:"pushed_at"`
}

// IssuePage is the result of an incremental issue listing.
type IssuePage struct {
	Issues      []Issue
	ETag        string
	NotModified bool
}

// ListIssues returns every issue (not pull request) in repo updated at or
// after since. When etag matches GitHub's current listing, NotModified is set
// and no issues are returned; such requests don't count against the rate limit.
func (c *Client) ListIssues(ctx context.Context, repo string, since time.Time, etag string) (IssuePage, error) {
	query := url.Values{"state": {"all"}, "per_page": {"100"}, "sort": {"updated"}, "direction": {"asc"}}
	if !since.IsZero() {
		query.Set("since", since.UTC().Format(time.RFC3339))
	}
	next := fmt.Sprintf("repos/%s/issues?%s", repo, query.Encode())

	var page IssuePage
	first := true
	for next != "" {
		header := http.Header{}
		if first && etag != "" {
			header.Set("If-None-Match", etag)
		}
		var batch []Issue
		resp, err := c.do(ctx, http.MethodGet, next, header, nil, &batch, repo)
		if err != nil {
			return IssuePage{}, err
		}
		if first {
			page.ETag = resp.Header.Get("ETag")
			if resp.StatusCode == http.StatusNotModified {
				page.NotModified = true
				return page, nil
			}
		}
		for _, issue := range batch {
			if issue.PullRequest == nil {
				page.Issues = append(page.Issues, issue)
			}
		}
		next = nextLink(resp.Header.Get("Link"))
		first = false
	}
	return page, nil
}

// GetIssue returns a single issue.
func (c *Client) GetIssue(ctx context.Context, repo string, number int) (Issue, error) {
	var issue Issue
	_, err := c.do(ctx, http.MethodGet, fmt.Sprintf("repos/%s/issues/%d", repo, number), nil, nil, &issue, repo)
	return issue, err
}

// NewIssue is the payload for CreateIssue.
type NewIssue struct {
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// CreateIssue opens a new issue.
func (c *Client) CreateIssue(ctx context.Context, repo string, payload NewIssue) (Issue, error) {
	var issue Issue
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/issues", repo), nil, payload, &issue, repo)
	return issue, err
}

// IssuePatch is the payload for UpdateIssue; nil fields are left unchanged.
type IssuePatch struct {
	Title       *string `json:"title,omitempty"`
	Body        *string `json:"body,omitempty"`
	State       *string `json:"state,omitempty"`
	StateReason *string `json:"state_reason,omitempty"`
}

// UpdateIssue edits an issue's title, body, or state.
func (c *Client) UpdateIssue(ctx context.Context, repo string, number int, patch IssuePatch) (Issue, error) {
	var issue Issue
	_, err := c.do(ctx, http.MethodPatch, fmt.Sprintf("repos/%s/issues/%d", repo, number), nil, patch, &issue, repo)
	return issue, err
}

// AddLabels adds labels to an issue. GitHub creates labels that don't exist yet.
func (c *Client) AddLabels(ctx context.Context, repo string, number int, labels []string) error {
	payload := map[string][]string{"labels": labels}
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/issues/%d/labels", repo, number), nil, payload, nil, repo)
	return err
}

// RemoveLabel removes a label from an issue. Removing a label the issue
// doesn't have is not an error.
func (c *Client) RemoveLabel(ctx context.Context, repo string, number int, label string) error {
	endpoint := fmt.Sprintf("repos/%s/issues/%d/labels/%s", repo, number, url.PathEscape(label))
	_, err := c.do(ctx, http.MethodDelete, endpoint, nil, nil, nil, repo)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// AddAssignees assigns users to an issue.
func (c *Client) AddAssignees(ctx context.Context, repo string, number int, logins []string) error {
	payload := map[string][]string{"assignees": logins}
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/issues/%d/assignees", repo, number), nil, payload, nil, repo)
	return err
}

// RemoveAssignees unassigns users from an issue.
func (c *Client) RemoveAssignees(ctx context.Context, repo string, number int, logins []string) error {
	payload := map[string][]string{"assignees": logins}
	_, err := c.do(ctx, http.MethodDelete, fmt.Sprintf("repos/%s/issues/%d/assignees", repo, number), nil, payload, nil, repo)
	return err
}

// ListComments returns all comments on an issue, oldest first.
func (c *Client) ListComments(ctx context.Context, repo string, number int) ([]Comment, error) {
	var comments []Comment
	next := fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", repo, number)
	for next != "" {
		var batch []Comment
		resp, err := c.do(ctx, http.MethodGet, next, nil, nil, &batch, repo)
		if err != nil {
			return nil, err
		}
		comments = append(comments, batch...)
		next = nextLink(resp.Header.Get("Link"))
	}
	return comments, nil
}

// CreateComment adds a comment to an issue.
func (c *Client) CreateComment(ctx context.Context, repo string, number int, body string) (Comment, error) {
	var comment Comment
	payload := map[string]string{"body": body}
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/issues/%d/comments", repo, number), nil, payload, &comment, repo)
	return comment, err
}

// ListLabels returns every label defined in repo.
func (c *Client) ListLabels(ctx context.Context, repo string) ([]Label, error) {
	var labels []Label
	next := fmt.Sprintf("repos/%s/labels?per_page=100", repo)
	for next != "" {
		var batch []Label
		resp, err := c.do(ctx, http.MethodGet, next, nil, nil, &batch, repo)
		if err != nil {
			return nil, err
		}
		labels = append(labels, batch...)
		next = nextLink(resp.Header.Get("Link"))
	}
	return labels, nil
}

// CreateLabel defines a label in repo. A label that already exists is left
// unchanged and is not an error.
func (c *Client) CreateLabel(ctx context.Context, repo string, label Label) error {
	_, err := c.do(ctx, http.MethodPost, fmt.Sprintf("repos/%s/labels", repo), nil, label, nil, repo)
	if IsValidation(err) {
		return nil
	}
	return err
}

// Viewer returns the login of the authenticated user.
func (c *Client) Viewer(ctx context.Context) (string, error) {
	c.mu.Lock()
	login := c.login
	c.mu.Unlock()
	if login != "" {
		return login, nil
	}
	var user User
	if _, err := c.do(ctx, http.MethodGet, "user", nil, nil, &user, ""); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.login = user.Login
	c.mu.Unlock()
	return user.Login, nil
}

// ListUserRepos returns repositories the user owns, collaborates on, or can
// access through an organization, most recently pushed first. Archived
// repositories and repositories with issues disabled are skipped.
func (c *Client) ListUserRepos(ctx context.Context, limit int) ([]RepoSummary, error) {
	var repos []RepoSummary
	next := "user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member"
	for next != "" && len(repos) < limit {
		var batch []RepoSummary
		resp, err := c.do(ctx, http.MethodGet, next, nil, nil, &batch, "")
		if err != nil {
			return nil, err
		}
		for _, repo := range batch {
			if !repo.Archived && repo.HasIssues {
				repos = append(repos, repo)
			}
		}
		next = nextLink(resp.Header.Get("Link"))
	}
	if len(repos) > limit {
		repos = repos[:limit]
	}
	return repos, nil
}

// Release is a published GitHub release.
type Release struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
}

// LatestRelease returns the release GitHub marks as latest in repo.
func (c *Client) LatestRelease(ctx context.Context, repo string) (Release, error) {
	var release Release
	_, err := c.do(ctx, http.MethodGet, fmt.Sprintf("repos/%s/releases/latest", repo), nil, nil, &release, repo)
	return release, err
}

// IssueURL returns the web URL of an issue.
func (c *Client) IssueURL(repo string, number int) string {
	host := c.host
	if host == "" {
		host = "github.com"
	}
	return fmt.Sprintf("https://%s/%s/issues/%d", host, repo, number)
}

func (c *Client) do(ctx context.Context, method, endpoint string, header http.Header, payload, target any, repo string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	targetURL := endpoint
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		targetURL = c.base + endpoint
	}

	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, classifyTransportError(err, repo)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, classifyTransportError(err, repo)
	}

	if resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(data, &apiErr)
		message := apiErr.Message
		for _, item := range apiErr.Errors {
			if item.Message != "" {
				message += " (" + item.Message + ")"
			}
		}
		return resp, classifyStatus(resp.StatusCode, message, resp.Header.Get("X-RateLimit-Remaining"), repo)
	}
	if target != nil && len(data) > 0 {
		if err := json.Unmarshal(data, target); err != nil {
			return resp, fmt.Errorf("decode GitHub response: %w", err)
		}
	}
	return resp, nil
}

var linkNextPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func nextLink(header string) string {
	match := linkNextPattern.FindStringSubmatch(header)
	if match == nil {
		return ""
	}
	return match[1]
}
