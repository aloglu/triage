package githubsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aloglu/triage/internal/model"
)

const requestTimeout = 30 * time.Second

type apiRunner func(ctx context.Context, method, endpoint string, payload any, target any) error

type Client struct {
	run         apiRunner
	runGraphQL  graphQLRunner
	viewerLogin string
}

func NewClient() *Client {
	return &Client{
		run:        runAPIJSON,
		runGraphQL: runGraphQLJSON,
	}
}

type graphQLRunner func(ctx context.Context, query string, variables map[string]string, target any) error

type ErrorKind string

const (
	ErrorCLIUnavailable   ErrorKind = "cli_unavailable"
	ErrorAuthRequired     ErrorKind = "auth_required"
	ErrorNotFound         ErrorKind = "not_found"
	ErrorPermissionDenied ErrorKind = "permission_denied"
)

type Error struct {
	Kind     ErrorKind
	Method   string
	Endpoint string
	Repo     string
	Resource string
	Message  string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "github sync error"
}

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

type labelPayload struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type assigneesPayload struct {
	Assignees []string `json:"assignees"`
}

type viewerResponse struct {
	Login string `json:"login"`
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
	Name  string `json:"name"`
	Color string `json:"color"`
}

var projectLabelPalette = []string{
	"slate-blue",
	"teal",
	"purple",
	"magenta",
	"ochre",
	"green",
	"blue",
	"sea",
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

func (c *Client) UpsertItem(repo string, item model.Item) (model.Item, string, error) {
	return c.upsertItem(repo, item, false)
}

func (c *Client) ForceUpsertItem(repo string, item model.Item) (model.Item, string, error) {
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
		if IsNotFound(err) {
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

func (c *Client) upsertItem(repo string, item model.Item, force bool) (model.Item, string, error) {
	if repo == "" {
		return item, "", fmt.Errorf("repo is required")
	}

	if item.IssueNumber == 0 {
		saved, err := c.createItem(repo, item)
		if err != nil {
			return item, "", err
		}
		return saved, c.assignViewerWarning(repo, saved.IssueNumber), nil
	}

	saved, err := c.updateItem(repo, item, force)
	if err != nil {
		return item, "", err
	}
	return saved, c.assignViewerWarning(repo, saved.IssueNumber), nil
}

func (c *Client) createItem(repo string, item model.Item) (model.Item, error) {
	if err := c.ensureManagedLabels(repo, item.Labels()); err != nil {
		return item, err
	}

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

	if err := c.ensureManagedLabels(repo, item.Labels()); err != nil {
		return item, err
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
		return classifyAPIError(method, endpoint, message, err)
	}

	if target == nil {
		return nil
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
		return classifyGraphQLError(query, message, err)
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
	project, itemType, stage, body, err := ParseBody(response.Body)
	if err != nil {
		return model.Item{}, fmt.Errorf("issue #%d: %w", response.Number, err)
	}

	return model.Item{
		Title:           response.Title,
		Project:         project,
		Type:            itemType,
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
		oldItem.Project:                  {},
		newItem.Project:                  {},
		string(oldItem.NormalizedType()): {},
		string(newItem.NormalizedType()): {},
		string(oldItem.Stage):            {},
		string(newItem.Stage):            {},
		"trashed":                        {},
	}
	for _, itemType := range model.Types {
		managed[string(itemType)] = struct{}{}
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

func (c *Client) ensureManagedLabels(repo string, labels []string) error {
	seen := make(map[string]struct{}, len(labels))
	for _, name := range labels {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := c.ensureLabel(repo, name, managedLabelColor(name)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ensureLabel(repo, name, color string) error {
	if repo == "" || name == "" || color == "" {
		return nil
	}

	endpoint := fmt.Sprintf("repos/%s/labels/%s", repo, url.PathEscape(name))
	var current label
	err := c.run(context.Background(), "GET", endpoint, nil, &current)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return c.run(context.Background(), "POST", fmt.Sprintf("repos/%s/labels", repo), labelPayload{
			Name:  name,
			Color: color,
		}, nil)
	}

	if strings.EqualFold(current.Color, color) {
		return nil
	}

	return c.run(context.Background(), "PATCH", endpoint, labelPayload{
		Name:  name,
		Color: color,
	}, nil)
}

func (c *Client) assignViewerWarning(repo string, issueNumber int) string {
	if repo == "" || issueNumber == 0 {
		return ""
	}

	login, err := c.viewer()
	if err != nil || login == "" {
		return "Saved item, but could not assign it to your GitHub user."
	}

	err = c.run(context.Background(), "POST", fmt.Sprintf("repos/%s/issues/%d/assignees", repo, issueNumber), assigneesPayload{
		Assignees: []string{login},
	}, nil)
	if err != nil {
		return fmt.Sprintf("Saved item, but could not assign it to %s on GitHub.", login)
	}

	return ""
}

func (c *Client) viewer() (string, error) {
	if c.viewerLogin != "" {
		return c.viewerLogin, nil
	}

	var response viewerResponse
	if err := c.run(context.Background(), "GET", "user", nil, &response); err != nil {
		return "", err
	}
	c.viewerLogin = strings.TrimSpace(response.Login)
	if c.viewerLogin == "" {
		return "", fmt.Errorf("github viewer login is empty")
	}
	return c.viewerLogin, nil
}

func managedLabelColor(name string) string {
	switch name {
	case string(model.TypeFeature):
		return "0969da"
	case string(model.TypeBug):
		return "cf222e"
	case string(model.TypeChore):
		return "6e7781"
	case string(model.StageIdea):
		return "8250df"
	case string(model.StagePlanned):
		return "9ecb5d"
	case string(model.StageActive):
		return "1f6feb"
	case string(model.StageBlocked):
		return "db6d28"
	case string(model.StageDone):
		return "2da44e"
	case "trashed":
		return "6e7781"
	default:
		return projectLabelColor(name)
	}
}

func projectLabelColor(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return "6e7781"
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strings.ToLower(project)))
	hash := hasher.Sum32()
	band := projectLabelPalette[hash%uint32(len(projectLabelPalette))]
	hueMin, hueMax := hueBandRange(band)
	bandWidth := hueMax - hueMin + 1
	if bandWidth < 1 {
		bandWidth = 1
	}
	hue := hueMin + int((hash>>8)%uint32(bandWidth))
	saturation := 38 + int((hash>>16)%10)
	lightness := 44 + int((hash>>24)%10)
	return hslToHex(float64(hue), float64(saturation)/100, float64(lightness)/100)
}

func ProjectLabelColor(project string) string {
	return projectLabelColor(project)
}

func hueBandRange(name string) (int, int) {
	switch name {
	case "slate-blue":
		return 215, 235
	case "teal":
		return 175, 195
	case "purple":
		return 255, 285
	case "magenta":
		return 305, 330
	case "ochre":
		return 40, 60
	case "green":
		return 120, 145
	case "blue":
		return 195, 215
	case "sea":
		return 155, 175
	default:
		return 210, 230
	}
}

func hslToHex(h, s, l float64) string {
	h = math.Mod(h, 360) / 360
	var r, g, b float64
	if s == 0 {
		r, g, b = l, l, l
	} else {
		var q float64
		if l < 0.5 {
			q = l * (1 + s)
		} else {
			q = l + s - l*s
		}
		p := 2*l - q
		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}
	return fmt.Sprintf("%02x%02x%02x", int(math.Round(r*255)), int(math.Round(g*255)), int(math.Round(b*255)))
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func IsNotFound(err error) bool {
	var githubErr *Error
	return errors.As(err, &githubErr) && githubErr.Kind == ErrorNotFound
}

func UserMessage(err error) string {
	var githubErr *Error
	if !errors.As(err, &githubErr) {
		return ""
	}

	switch githubErr.Kind {
	case ErrorCLIUnavailable:
		return "GitHub CLI (`gh`) is not installed."
	case ErrorAuthRequired:
		return "GitHub authentication required. Run `gh auth login`."
	case ErrorPermissionDenied:
		if githubErr.Resource == "issue-delete" {
			return "GitHub denied this action. Deleting issues requires admin permission."
		}
		return "GitHub denied this action. Check your repository permissions."
	case ErrorNotFound:
		switch githubErr.Resource {
		case "repo":
			if githubErr.Repo != "" {
				return fmt.Sprintf("GitHub repository not found or inaccessible: %s.", githubErr.Repo)
			}
			return "GitHub repository not found or inaccessible."
		case "issue", "issue-delete":
			return "GitHub issue not found or inaccessible."
		default:
			return "GitHub resource not found or inaccessible."
		}
	default:
		return ""
	}
}

func classifyAPIError(method, endpoint, message string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return &Error{
			Kind:     ErrorCLIUnavailable,
			Method:   method,
			Endpoint: endpoint,
			Repo:     repoFromEndpoint(endpoint),
			Resource: resourceFromEndpoint(endpoint),
			Message:  message,
		}
	}

	lower := strings.ToLower(message)
	resource := resourceFromEndpoint(endpoint)
	repo := repoFromEndpoint(endpoint)

	switch {
	case isAuthError(lower):
		return &Error{
			Kind:     ErrorAuthRequired,
			Method:   method,
			Endpoint: endpoint,
			Repo:     repo,
			Resource: resource,
			Message:  message,
		}
	case isPermissionError(lower):
		return &Error{
			Kind:     ErrorPermissionDenied,
			Method:   method,
			Endpoint: endpoint,
			Repo:     repo,
			Resource: resource,
			Message:  message,
		}
	case isNotFoundError(lower):
		return &Error{
			Kind:     ErrorNotFound,
			Method:   method,
			Endpoint: endpoint,
			Repo:     repo,
			Resource: resource,
			Message:  message,
		}
	default:
		return fmt.Errorf("gh api %s %s: %s", method, endpoint, message)
	}
}

func classifyGraphQLError(query, message string, err error) error {
	resource := "graphql"
	if strings.Contains(query, "deleteIssue") {
		resource = "issue-delete"
	}

	if errors.Is(err, exec.ErrNotFound) {
		return &Error{
			Kind:     ErrorCLIUnavailable,
			Method:   "POST",
			Endpoint: "graphql",
			Resource: resource,
			Message:  message,
		}
	}

	lower := strings.ToLower(message)
	switch {
	case isAuthError(lower):
		return &Error{
			Kind:     ErrorAuthRequired,
			Method:   "POST",
			Endpoint: "graphql",
			Resource: resource,
			Message:  message,
		}
	case isPermissionError(lower):
		return &Error{
			Kind:     ErrorPermissionDenied,
			Method:   "POST",
			Endpoint: "graphql",
			Resource: resource,
			Message:  message,
		}
	case isNotFoundError(lower):
		return &Error{
			Kind:     ErrorNotFound,
			Method:   "POST",
			Endpoint: "graphql",
			Resource: resource,
			Message:  message,
		}
	default:
		return fmt.Errorf("gh api graphql: %s", message)
	}
}

func isAuthError(message string) bool {
	return strings.Contains(message, "gh auth login") ||
		strings.Contains(message, "not logged into any github hosts") ||
		strings.Contains(message, "authentication required") ||
		strings.Contains(message, "token is invalid") ||
		strings.Contains(message, "try authenticating with")
}

func isPermissionError(message string) bool {
	return strings.Contains(message, "must have admin rights to repository") ||
		strings.Contains(message, "resource not accessible by integration") ||
		strings.Contains(message, "forbidden") ||
		strings.Contains(message, "\"message\":\"forbidden\"") ||
		strings.Contains(message, "permission denied")
}

func isNotFoundError(message string) bool {
	return strings.Contains(message, "\"message\":\"not found\"") ||
		strings.Contains(message, "not found") ||
		strings.Contains(message, "repository not found")
}

func repoFromEndpoint(endpoint string) string {
	if !strings.HasPrefix(endpoint, "repos/") {
		return ""
	}
	parts := strings.Split(endpoint, "/")
	if len(parts) < 3 {
		return ""
	}
	return parts[1] + "/" + parts[2]
}

func resourceFromEndpoint(endpoint string) string {
	if !strings.HasPrefix(endpoint, "repos/") {
		return "resource"
	}
	if strings.Contains(endpoint, "/issues/") {
		return "issue"
	}
	return "repo"
}
