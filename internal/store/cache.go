// Package store persists cached GitHub data and the outbox of changes that
// haven't reached GitHub yet.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aloglu/triage/internal/config"
	"github.com/aloglu/triage/internal/fileutil"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
)

// Store reads and writes triage's local files.
type Store struct {
	paths config.Paths
}

func New(paths config.Paths) *Store { return &Store{paths: paths} }

func (s *Store) Paths() config.Paths { return s.paths }

// RepoCache is everything triage remembers about one repository.
type RepoCache struct {
	Repo string `json:"repo"`
	// ETag and Watermark identify the last issue listing so the next refresh
	// only asks for issues updated since then.
	ETag      string    `json:"etag,omitempty"`
	Watermark time.Time `json:"watermark,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	// FullETag and FullCheckedAt describe the last complete listing, which
	// is how triage notices issues that were deleted or transferred.
	FullETag      string    `json:"full_etag,omitempty"`
	FullCheckedAt time.Time `json:"full_checked_at,omitempty"`
	// FullListed are the issue numbers in that listing, so a 304 answer
	// still tells triage which cached issues are missing.
	FullListed []int         `json:"full_listed,omitempty"`
	Issues     []issue.Issue `json:"issues"`
	Labels     []gh.Label    `json:"labels,omitempty"`
}

// Merge folds freshly fetched issues into the cache, replacing older copies
// and advancing the watermark.
func (c *RepoCache) Merge(fetched []issue.Issue) {
	index := make(map[int]int, len(c.Issues))
	for i, existing := range c.Issues {
		index[existing.Number] = i
	}
	for _, fresh := range fetched {
		if at, ok := index[fresh.Number]; ok {
			fresh.LocalID = c.Issues[at].LocalID
			c.Issues[at] = fresh
		} else {
			index[fresh.Number] = len(c.Issues)
			c.Issues = append(c.Issues, fresh)
		}
		if fresh.UpdatedAt.After(c.Watermark) {
			c.Watermark = fresh.UpdatedAt
		}
	}
	sort.SliceStable(c.Issues, func(i, j int) bool { return c.Issues[i].Number > c.Issues[j].Number })
}

// Put stores a single issue returned by a write, without moving the
// watermark: the next listing still has to catch up with other changes.
func (c *RepoCache) Put(fresh issue.Issue) {
	for i, existing := range c.Issues {
		if existing.Number == fresh.Number {
			if fresh.LocalID == "" {
				fresh.LocalID = existing.LocalID
			}
			c.Issues[i] = fresh
			return
		}
	}
	c.Issues = append([]issue.Issue{fresh}, c.Issues...)
}

func (s *Store) repoPath(repo string) string {
	return filepath.Join(s.paths.CacheDir, "repos", strings.ToLower(strings.ReplaceAll(repo, "/", "__"))+".json")
}

// LoadRepo returns the cached data for repo, or an empty cache.
func (s *Store) LoadRepo(repo string) (RepoCache, error) {
	cache := RepoCache{Repo: repo}
	if err := readJSON(s.repoPath(repo), &cache); err != nil {
		// A missing or corrupt cache just means fetching from scratch.
		return RepoCache{Repo: repo}, nil
	}
	cache.Repo = repo
	return cache, nil
}

func (s *Store) SaveRepo(cache RepoCache) error {
	return writeJSON(s.repoPath(cache.Repo), cache)
}

// ForgetRepo deletes everything cached for repo.
func (s *Store) ForgetRepo(repo string) error {
	err := os.Remove(s.repoPath(repo))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Meta is small cached state that isn't tied to a repository.
type Meta struct {
	Login string `json:"login,omitempty"`
}

func (s *Store) LoadMeta() Meta {
	var meta Meta
	_ = readJSON(filepath.Join(s.paths.CacheDir, "meta.json"), &meta)
	return meta
}

func (s *Store) SaveMeta(meta Meta) error {
	return writeJSON(filepath.Join(s.paths.CacheDir, "meta.json"), meta)
}

// Comment is a cached issue comment.
type Comment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	URL       string    `json:"url,omitempty"`
}

// CommentCache holds an issue's comments as of the issue's UpdatedAt.
type CommentCache struct {
	IssueUpdatedAt time.Time `json:"issue_updated_at"`
	Comments       []Comment `json:"comments"`
}

func (s *Store) commentsPath(repo string, number int) string {
	name := fmt.Sprintf("%s__%d.json", strings.ToLower(strings.ReplaceAll(repo, "/", "__")), number)
	return filepath.Join(s.paths.CacheDir, "comments", name)
}

// LoadComments returns cached comments; ok is false when nothing is cached.
func (s *Store) LoadComments(repo string, number int) (CommentCache, bool) {
	var cache CommentCache
	if err := readJSON(s.commentsPath(repo, number), &cache); err != nil {
		return CommentCache{}, false
	}
	return cache, true
}

// ForgetComments deletes an issue's cached comments.
func (s *Store) ForgetComments(repo string, number int) {
	_ = os.Remove(s.commentsPath(repo, number))
}

func (s *Store) SaveComments(repo string, number int, cache CommentCache) error {
	return writeJSON(s.commentsPath(repo, number), cache)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(data, '\n'), 0o700, 0o600)
}
