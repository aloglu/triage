package gh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListIssuesPagesSkipsPRsAndHonorsETag(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.URL.Query().Get("page") == "" {
			if r.URL.Query().Get("since") != "2026-01-02T03:04:05Z" || r.URL.Query().Get("state") != "all" {
				t.Errorf("unexpected query %s", r.URL.RawQuery)
			}
			w.Header().Set("Link", `<`+server.URL+`/repos/a/b/issues?page=2>; rel="next", <x>; rel="last"`)
			_, _ = w.Write([]byte(`[{"number":1,"title":"one"},{"number":2,"title":"pr","pull_request":{}}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"number":3,"title":"three"}]`))
	}))
	defer server.Close()
	c := NewWithHTTP(server.Client(), server.URL, "github.com")

	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	page, err := c.ListIssues(context.Background(), "a/b", since, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Issues) != 2 || page.Issues[0].Number != 1 || page.Issues[1].Number != 3 || page.ETag != `"v1"` {
		t.Fatalf("page = %+v", page)
	}
	page, err = c.ListIssues(context.Background(), "a/b", since, `"v1"`)
	if err != nil || !page.NotModified || len(page.Issues) != 0 {
		t.Fatalf("conditional page = %+v, %v", page, err)
	}
}

func TestErrorClassification(t *testing.T) {
	tests := []struct {
		status    int
		remaining string
		kind      ErrorKind
		message   string
	}{
		{401, "", ErrAuth, "gh auth login"},
		{403, "10", ErrPermission, "permission"},
		{403, "0", ErrRateLimited, "rate limit"},
		{404, "", ErrNotFound, "a/b"},
		{422, "", ErrValidation, "rejected"},
		{502, "", ErrOffline, "reach GitHub"},
	}
	for _, tt := range tests {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", tt.remaining)
			w.WriteHeader(tt.status)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		}))
		c := NewWithHTTP(server.Client(), server.URL, "github.com")
		_, err := c.GetIssue(context.Background(), "a/b", 1)
		server.Close()
		var ghErr *Error
		if !errors.As(err, &ghErr) || ghErr.Kind != tt.kind {
			t.Errorf("HTTP %d: err = %#v", tt.status, err)
			continue
		}
		if msg := UserMessage(err); !strings.Contains(msg, tt.message) {
			t.Errorf("HTTP %d: message %q missing %q", tt.status, msg, tt.message)
		}
	}
}

func TestNetworkErrorIsOffline(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	c := NewWithHTTP(http.DefaultClient, url, "github.com")
	_, err := c.GetIssue(context.Background(), "a/b", 1)
	if !IsOffline(err) {
		t.Fatalf("expected offline error, got %#v", err)
	}
}

func TestRemoveMissingLabelAndCreateExistingLabelAreNotErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.URL.EscapedPath() != "/repos/a/b/issues/1/labels/in%20progress" {
				t.Errorf("label path not escaped: %s", r.URL.EscapedPath())
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(422)
	}))
	defer server.Close()
	c := NewWithHTTP(server.Client(), server.URL, "github.com")
	if err := c.RemoveLabel(context.Background(), "a/b", 1, "in progress"); err != nil {
		t.Errorf("RemoveLabel = %v", err)
	}
	if err := c.CreateLabel(context.Background(), "a/b", Label{Name: "idea"}); err != nil {
		t.Errorf("CreateLabel = %v", err)
	}
}

func TestNormalizeRepo(t *testing.T) {
	for input, want := range map[string]string{
		"owner/repo":                          "owner/repo",
		" https://github.com/owner/repo.git ": "owner/repo",
		"github.com/owner/re.po_x/":           "owner/re.po_x",
	} {
		if got, err := NormalizeRepo(input); err != nil || got != want {
			t.Errorf("NormalizeRepo(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"owner", "owner/repo/extra", "../repo", "owner name/repo"} {
		if _, err := NormalizeRepo(input); err == nil {
			t.Errorf("NormalizeRepo(%q) should fail", input)
		}
	}
}
