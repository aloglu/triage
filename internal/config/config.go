// Package config loads and saves triage's settings and knows where triage
// keeps its files.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/aloglu/triage/internal/fileutil"
	"github.com/aloglu/triage/internal/gh"
	"github.com/aloglu/triage/internal/issue"
)

// View is a named saved filter.
type View struct {
	Name  string `toml:"name"`
	Query string `toml:"query"`
}

// Config is the contents of config.toml.
type Config struct {
	// Repos are the repositories triage tracks, in owner/name form.
	Repos []string `toml:"repos"`
	// DefaultRepo receives new issues when triage can't tell which repo you
	// mean from the current directory.
	DefaultRepo string `toml:"default_repo,omitempty"`
	// RefreshMinutes is how often the app checks GitHub for changes.
	RefreshMinutes int `toml:"refresh_minutes,omitempty"`
	// Labels names the labels that carry type and status.
	Labels issue.Convention `toml:"labels"`
	// Views are extra saved filters shown after the built-in ones.
	Views []View `toml:"views,omitempty"`
}

const defaultRefreshMinutes = 5

// Default returns the configuration used before the user sets anything.
func Default() Config {
	return Config{RefreshMinutes: defaultRefreshMinutes, Labels: issue.DefaultConvention()}
}

// RefreshInterval returns how often to poll GitHub.
func (c Config) RefreshInterval() time.Duration {
	return time.Duration(c.RefreshMinutes) * time.Minute
}

// HasRepo reports whether repo is tracked.
func (c Config) HasRepo(repo string) bool {
	for _, tracked := range c.Repos {
		if strings.EqualFold(tracked, repo) {
			return true
		}
	}
	return false
}

// AddRepo tracks repo; it reports whether repo was new.
func (c *Config) AddRepo(repo string) bool {
	if c.HasRepo(repo) {
		return false
	}
	c.Repos = append(c.Repos, repo)
	return true
}

// RemoveRepo stops tracking repo; it reports whether repo was tracked.
func (c *Config) RemoveRepo(repo string) bool {
	for i, tracked := range c.Repos {
		if strings.EqualFold(tracked, repo) {
			c.Repos = append(c.Repos[:i], c.Repos[i+1:]...)
			if strings.EqualFold(c.DefaultRepo, repo) {
				c.DefaultRepo = ""
			}
			return true
		}
	}
	return false
}

func (c Config) normalize() (Config, error) {
	var problems []string
	repos := make([]string, 0, len(c.Repos))
	seen := map[string]bool{}
	for _, repo := range c.Repos {
		normalized, err := gh.NormalizeRepo(repo)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		key := strings.ToLower(normalized)
		if !seen[key] {
			seen[key] = true
			repos = append(repos, normalized)
		}
	}
	c.Repos = repos
	if strings.TrimSpace(c.DefaultRepo) != "" {
		normalized, err := gh.NormalizeRepo(c.DefaultRepo)
		if err != nil {
			problems = append(problems, "default_repo: "+err.Error())
		}
		c.DefaultRepo = normalized
	}
	if c.RefreshMinutes <= 0 {
		c.RefreshMinutes = defaultRefreshMinutes
	}
	c.Labels = c.Labels.WithDefaults()
	views := c.Views[:0:0]
	for _, view := range c.Views {
		view.Name = strings.TrimSpace(view.Name)
		if view.Name == "" {
			problems = append(problems, "a view is missing its name")
			continue
		}
		views = append(views, view)
	}
	c.Views = views
	if len(problems) > 0 {
		return c, errors.New(strings.Join(problems, "; "))
	}
	return c, nil
}

// Load reads the config file. A missing file returns Default() and
// exists=false.
func Load(path string) (cfg Config, exists bool, err error) {
	cfg = Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("read config: %w", err)
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Default(), true, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err = cfg.normalize()
	if err != nil {
		return cfg, true, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, true, nil
}

const header = `# triage configuration. Edit freely; triage rewrites this file when you
# change settings in the app, keeping only the keys below.
#
# repos            repositories to track, in owner/name form
# default_repo     where new issues go when the current directory isn't a tracked repo
# refresh_minutes  how often to check GitHub for changes
# [labels]         label names that carry type and status, if your repos use different ones
# [[views]]        saved filters, e.g. name = "UI bugs", query = "is:open type:bug label:ui"

`

// Save writes cfg to path atomically.
func Save(path string, cfg Config) error {
	cfg, err := cfg.normalize()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, buf.Bytes(), 0o700, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Paths are the locations triage reads and writes.
type Paths struct {
	// ConfigDir holds config.toml and the outbox of unsent changes.
	ConfigDir string
	// CacheDir holds cached issues; deleting it loses nothing.
	CacheDir string
}

func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config.toml") }

func (p Paths) OutboxDir() string { return filepath.Join(p.ConfigDir, "outbox") }

// DefaultPaths returns the OS-specific locations, overridable with
// TRIAGE_CONFIG_DIR and TRIAGE_CACHE_DIR.
func DefaultPaths() (Paths, error) {
	configDir := os.Getenv("TRIAGE_CONFIG_DIR")
	if configDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("find config directory: %w", err)
		}
		configDir = filepath.Join(base, "triage")
	}
	cacheDir := os.Getenv("TRIAGE_CACHE_DIR")
	if cacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return Paths{}, fmt.Errorf("find cache directory: %w", err)
		}
		cacheDir = filepath.Join(base, "triage")
	}
	return Paths{ConfigDir: configDir, CacheDir: cacheDir}, nil
}
