// Package ghtest is an in-memory fake of the parts of the GitHub REST API
// triage uses, for tests.
package ghtest

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aloglu/triage/internal/gh"
)

// Server is a fake GitHub.
type Server struct {
	*httptest.Server
	Login string

	mu       sync.Mutex
	repos    map[string]*repo
	now      time.Time
	offline  bool
	Requests []string
}

type repo struct {
	issues   map[int]*gh.Issue
	labels   map[string]gh.Label
	comments map[int][]gh.Comment
	next     int
	readOnly bool
	hidden   map[int]bool
}

// New starts a fake GitHub that is shut down when the test ends.
func New(t testing.TB) *Server {
	s := &Server{Login: "octo", repos: map[string]*repo{}, now: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

// Client returns a gh.Client pointed at the fake.
func (s *Server) Client() *gh.Client {
	hc := &http.Client{Transport: offlineTransport{s}}
	return gh.NewWithHTTP(hc, s.URL+"/", "github.com")
}

type offlineTransport struct{ s *Server }

func (t offlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.s.mu.Lock()
	offline := t.s.offline
	t.s.mu.Unlock()
	if offline {
		return nil, errors.New("dial tcp: network is unreachable")
	}
	return http.DefaultTransport.RoundTrip(req)
}

// SetOffline makes every request fail with a network error.
func (s *Server) SetOffline(offline bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offline = offline
}

// SetReadOnly makes writes to repo fail with 403.
func (s *Server) SetReadOnly(name string, readOnly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repo(name).readOnly = readOnly
}

func (s *Server) repo(name string) *repo {
	name = strings.ToLower(name)
	r, ok := s.repos[name]
	if !ok {
		r = &repo{issues: map[int]*gh.Issue{}, labels: map[string]gh.Label{}, comments: map[int][]gh.Comment{}, next: 1}
		for _, label := range []string{"bug", "enhancement", "documentation"} {
			r.labels[label] = gh.Label{Name: label, Color: "ededed"}
		}
		s.repos[name] = r
	}
	return r
}

func (s *Server) tick() time.Time {
	s.now = s.now.Add(time.Minute)
	return s.now
}

// AddRepo creates an empty repository.
func (s *Server) AddRepo(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repo(name)
}

// HideFromListing makes an issue missing from issue listings, as GitHub's
// eventually consistent listing sometimes does for new issues.
func (s *Server) HideFromListing(repoName string, number int, hidden bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repo(repoName)
	if r.hidden == nil {
		r.hidden = map[int]bool{}
	}
	r.hidden[number] = hidden
}

// AddIssue creates an issue directly, as if someone used the website.
func (s *Server) AddIssue(repoName string, title, body string, labels ...string) gh.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repo(repoName)
	now := s.tick()
	issue := &gh.Issue{Number: r.next, Title: title, Body: body, State: "open", User: gh.User{Login: "someone"}, CreatedAt: now, UpdatedAt: now}
	issue.HTMLURL = fmt.Sprintf("https://github.com/%s/issues/%d", repoName, issue.Number)
	for _, label := range labels {
		issue.Labels = append(issue.Labels, s.ensureLabel(r, label))
	}
	r.issues[issue.Number] = issue
	r.next++
	return *issue
}

// EditIssue changes an issue directly, as if someone used the website.
func (s *Server) EditIssue(repoName string, number int, edit func(*gh.Issue)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	issue := s.repo(repoName).issues[number]
	edit(issue)
	issue.UpdatedAt = s.tick()
}

// Issue returns a copy of an issue.
func (s *Server) Issue(repoName string, number int) gh.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.repo(repoName).issues[number]
}

// LabelNames returns the names of an issue's labels.
func (s *Server) LabelNames(repoName string, number int) []string {
	issue := s.Issue(repoName, number)
	names := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		names = append(names, label.Name)
	}
	return names
}

// RepoLabel returns a label defined in a repo.
func (s *Server) RepoLabel(repoName, name string) (gh.Label, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	label, ok := s.repo(repoName).labels[strings.ToLower(name)]
	return label, ok
}

// Comments returns an issue's comments.
func (s *Server) Comments(repoName string, number int) []gh.Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gh.Comment(nil), s.repo(repoName).comments[number]...)
}

// IssueCount returns how many issues a repo has.
func (s *Server) IssueCount(repoName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.repo(repoName).issues)
}

func (s *Server) ensureLabel(r *repo, name string) gh.Label {
	key := strings.ToLower(name)
	if label, ok := r.labels[key]; ok {
		return label
	}
	label := gh.Label{Name: name, Color: "ededed"}
	r.labels[key] = label
	return label
}

func (s *Server) handle(w http.ResponseWriter, req *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, req.Method+" "+req.URL.RequestURI())

	path := strings.Trim(req.URL.Path, "/")
	parts := strings.Split(path, "/")

	if path == "user" {
		writeJSON(w, 200, gh.User{Login: s.Login})
		return
	}
	if path == "user/repos" {
		var out []gh.RepoSummary
		names := make([]string, 0, len(s.repos))
		for name := range s.repos {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			out = append(out, gh.RepoSummary{FullName: name, HasIssues: true, OpenIssues: len(s.repos[name].issues)})
		}
		writeJSON(w, 200, out)
		return
	}
	if len(parts) < 4 || parts[0] != "repos" {
		writeError(w, 404, "Not Found")
		return
	}
	repoName := parts[1] + "/" + parts[2]
	r, ok := s.repos[strings.ToLower(repoName)]
	if !ok {
		writeError(w, 404, "Not Found")
		return
	}
	if req.Method != http.MethodGet && r.readOnly {
		writeError(w, 403, "Must have admin rights to Repository.")
		return
	}
	rest := parts[3:]

	switch {
	case rest[0] == "labels" && len(rest) == 1 && req.Method == http.MethodGet:
		labels := make([]gh.Label, 0, len(r.labels))
		for _, label := range r.labels {
			labels = append(labels, label)
		}
		sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
		writeJSON(w, 200, labels)
	case rest[0] == "labels" && len(rest) == 1 && req.Method == http.MethodPost:
		var label gh.Label
		_ = json.NewDecoder(req.Body).Decode(&label)
		if _, exists := r.labels[strings.ToLower(label.Name)]; exists {
			writeError(w, 422, "Validation Failed")
			return
		}
		r.labels[strings.ToLower(label.Name)] = label
		writeJSON(w, 201, label)
	case rest[0] == "issues" && len(rest) == 1 && req.Method == http.MethodGet:
		s.listIssues(w, req, r)
	case rest[0] == "issues" && len(rest) == 1 && req.Method == http.MethodPost:
		var payload gh.NewIssue
		_ = json.NewDecoder(req.Body).Decode(&payload)
		if strings.TrimSpace(payload.Title) == "" {
			writeError(w, 422, "Validation Failed")
			return
		}
		now := s.tick()
		issue := &gh.Issue{Number: r.next, Title: payload.Title, Body: payload.Body, State: "open", User: gh.User{Login: s.Login}, CreatedAt: now, UpdatedAt: now}
		issue.HTMLURL = fmt.Sprintf("https://github.com/%s/issues/%d", repoName, issue.Number)
		for _, label := range payload.Labels {
			issue.Labels = append(issue.Labels, s.ensureLabel(r, label))
		}
		for _, login := range payload.Assignees {
			issue.Assignees = append(issue.Assignees, gh.User{Login: login})
		}
		r.issues[issue.Number] = issue
		r.next++
		writeJSON(w, 201, issue)
	default:
		s.handleIssue(w, req, r, rest)
	}
}

func (s *Server) handleIssue(w http.ResponseWriter, req *http.Request, r *repo, rest []string) {
	if rest[0] != "issues" || len(rest) < 2 {
		writeError(w, 404, "Not Found")
		return
	}
	number, err := strconv.Atoi(rest[1])
	issue, ok := r.issues[number]
	if err != nil || !ok {
		writeError(w, 404, "Not Found")
		return
	}
	sub := rest[2:]
	switch {
	case len(sub) == 0 && req.Method == http.MethodGet:
		writeJSON(w, 200, issue)
	case len(sub) == 0 && req.Method == http.MethodPatch:
		var patch gh.IssuePatch
		_ = json.NewDecoder(req.Body).Decode(&patch)
		if patch.Title != nil {
			issue.Title = *patch.Title
		}
		if patch.Body != nil {
			issue.Body = *patch.Body
		}
		if patch.State != nil {
			issue.State = *patch.State
			issue.StateReason = ""
			if patch.StateReason != nil {
				issue.StateReason = *patch.StateReason
			}
		}
		issue.UpdatedAt = s.tick()
		writeJSON(w, 200, issue)
	case len(sub) == 1 && sub[0] == "labels" && req.Method == http.MethodPost:
		var payload struct{ Labels []string }
		_ = json.NewDecoder(req.Body).Decode(&payload)
		for _, name := range payload.Labels {
			if !hasLabel(issue.Labels, name) {
				issue.Labels = append(issue.Labels, s.ensureLabel(r, name))
			}
		}
		issue.UpdatedAt = s.tick()
		writeJSON(w, 200, issue.Labels)
	case len(sub) == 2 && sub[0] == "labels" && req.Method == http.MethodDelete:
		kept := issue.Labels[:0]
		found := false
		for _, label := range issue.Labels {
			if strings.EqualFold(label.Name, sub[1]) {
				found = true
				continue
			}
			kept = append(kept, label)
		}
		issue.Labels = kept
		if !found {
			writeError(w, 404, "Label does not exist")
			return
		}
		issue.UpdatedAt = s.tick()
		writeJSON(w, 200, issue.Labels)
	case len(sub) == 1 && sub[0] == "assignees":
		var payload struct{ Assignees []string }
		_ = json.NewDecoder(req.Body).Decode(&payload)
		for _, login := range payload.Assignees {
			kept := issue.Assignees[:0]
			for _, user := range issue.Assignees {
				if !strings.EqualFold(user.Login, login) {
					kept = append(kept, user)
				}
			}
			issue.Assignees = kept
			if req.Method == http.MethodPost {
				issue.Assignees = append(issue.Assignees, gh.User{Login: login})
			}
		}
		issue.UpdatedAt = s.tick()
		writeJSON(w, 201, issue)
	case len(sub) == 1 && sub[0] == "comments" && req.Method == http.MethodGet:
		comments := r.comments[number]
		if comments == nil {
			comments = []gh.Comment{}
		}
		writeJSON(w, 200, comments)
	case len(sub) == 1 && sub[0] == "comments" && req.Method == http.MethodPost:
		var payload struct{ Body string }
		_ = json.NewDecoder(req.Body).Decode(&payload)
		now := s.tick()
		comment := gh.Comment{ID: int64(len(r.comments[number]) + 1), Body: payload.Body, User: gh.User{Login: s.Login}, CreatedAt: now, UpdatedAt: now}
		r.comments[number] = append(r.comments[number], comment)
		issue.Comments++
		issue.UpdatedAt = now
		writeJSON(w, 201, comment)
	default:
		writeError(w, 404, "Not Found")
	}
}

func (s *Server) listIssues(w http.ResponseWriter, req *http.Request, r *repo) {
	var since time.Time
	if value := req.URL.Query().Get("since"); value != "" {
		since, _ = time.Parse(time.RFC3339, value)
	}
	var issues []*gh.Issue
	for _, issue := range r.issues {
		if !issue.UpdatedAt.Before(since) && !r.hidden[issue.Number] {
			issues = append(issues, issue)
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].UpdatedAt.Before(issues[j].UpdatedAt) })

	data, _ := json.Marshal(issues)
	sum := sha1.Sum(append([]byte(req.URL.RawQuery), data...))
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	if req.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	perPage, _ := strconv.Atoi(req.URL.Query().Get("per_page"))
	if perPage <= 0 {
		perPage = 30
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start > len(issues) {
		start = len(issues)
	}
	if end > len(issues) {
		end = len(issues)
	}
	if end < len(issues) {
		query := req.URL.Query()
		query.Set("page", strconv.Itoa(page+1))
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next"`, s.URL, req.URL.Path, query.Encode()))
	}
	w.Header().Set("ETag", etag)
	out := issues[start:end]
	if out == nil {
		out = []*gh.Issue{}
	}
	writeJSON(w, 200, out)
}

func hasLabel(labels []gh.Label, name string) bool {
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}
