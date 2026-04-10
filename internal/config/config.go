package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aloglu/triage/internal/fileutil"
)

const (
	ModeLocal  = "local"
	ModeGitHub = "github"

	ProjectLabelAlways = "always"
	ProjectLabelAuto   = "auto"
	ProjectLabelNever  = "never"
)

type AppConfig struct {
	StorageMode          string            `json:"storage_mode"`
	Repo                 string            `json:"repo,omitempty"`
	TrackedRepos         []string          `json:"tracked_repos,omitempty"`
	ProjectRepos         map[string]string `json:"project_repos,omitempty"`
	DataFile             string            `json:"data_file"`
	DraftsFolder         string            `json:"drafts_folder,omitempty"`
	Density              string            `json:"density,omitempty"`
	ProjectLabelSync     string            `json:"project_label_sync,omitempty"`
	LastSuccessfulSyncAt time.Time         `json:"last_successful_sync_at,omitempty"`
}

type Manager struct {
	path string
}

func NewManager() (*Manager, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}

	return &Manager{
		path: filepath.Join(configDir, "triage", "config.json"),
	}, nil
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Load() (AppConfig, bool, error) {
	var cfg AppConfig

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false, fmt.Errorf("decode config: %w", err)
	}

	if cfg.StorageMode == "" {
		return cfg, false, fmt.Errorf("config missing storage mode")
	}
	if cfg.DataFile == "" {
		return cfg, false, fmt.Errorf("config missing data file")
	}

	return Normalize(cfg), true, nil
}

func (m *Manager) Save(cfg AppConfig) error {
	cfg = Normalize(cfg)
	if cfg.StorageMode == "" {
		return fmt.Errorf("storage mode is required")
	}
	if cfg.DataFile == "" {
		return fmt.Errorf("data file is required")
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := fileutil.AtomicWriteFile(m.path, append(data, '\n'), 0o700, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func DefaultDataFile() (string, error) {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}

	return filepath.Join(dataDir, "triage", "items.json"), nil
}

func DefaultDraftsFolder() (string, error) {
	dataDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve drafts dir: %w", err)
	}

	return filepath.Join(dataDir, "triage", "drafts"), nil
}

func Normalize(cfg AppConfig) AppConfig {
	cfg.Repo = normalizeRepo(cfg.Repo)
	cfg.TrackedRepos = normalizeTrackedRepos(cfg.TrackedRepos, cfg.Repo)
	cfg.ProjectRepos = normalizeProjectRepos(cfg.ProjectRepos)
	cfg.DraftsFolder = normalizeDraftsFolder(cfg.DraftsFolder)
	if cfg.DraftsFolder == "" {
		if draftsDir, err := DefaultDraftsFolder(); err == nil {
			cfg.DraftsFolder = draftsDir
		}
	}
	if cfg.Density == "" {
		cfg.Density = "comfortable"
	}
	cfg.ProjectLabelSync = normalizeProjectLabelSync(cfg.ProjectLabelSync)
	if !cfg.LastSuccessfulSyncAt.IsZero() {
		cfg.LastSuccessfulSyncAt = cfg.LastSuccessfulSyncAt.UTC()
	}
	return cfg
}

func normalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if strings.EqualFold(repo, "local") {
		return ""
	}
	return repo
}

func normalizeTrackedRepos(repos []string, defaultRepo string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(repos)+1)
	add := func(repo string) {
		repo = normalizeRepo(repo)
		if !validRepo(repo) {
			return
		}
		if _, ok := seen[repo]; ok {
			return
		}
		seen[repo] = struct{}{}
		normalized = append(normalized, repo)
	}

	add(defaultRepo)
	for _, repo := range repos {
		add(repo)
	}
	return normalized
}

func validRepo(repo string) bool {
	if repo == "" {
		return false
	}
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func normalizeProjectRepos(projectRepos map[string]string) map[string]string {
	if len(projectRepos) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(projectRepos))
	for project, repo := range projectRepos {
		key := normalizeProjectKey(project)
		repo = normalizeRepo(repo)
		if key == "" || !validRepo(repo) {
			continue
		}
		normalized[key] = repo
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeProjectKey(project string) string {
	return strings.ToLower(strings.TrimSpace(project))
}

func normalizeDraftsFolder(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func normalizeProjectLabelSync(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProjectLabelAlways:
		return ProjectLabelAlways
	case ProjectLabelNever:
		return ProjectLabelNever
	default:
		return ProjectLabelAuto
	}
}
