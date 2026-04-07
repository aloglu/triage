package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aloglu/triage/internal/model"
)

type JSONStore struct {
	path string
}

func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path}
}

func (s *JSONStore) Path() string {
	return s.path
}

func (s *JSONStore) LoadItems() ([]model.Item, bool, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read items: %w", err)
	}

	var items []model.Item
	if len(data) == 0 {
		return items, true, nil
	}

	if err := json.Unmarshal(data, &items); err != nil {
		return nil, false, fmt.Errorf("decode items: %w", err)
	}

	return items, true, nil
}

func (s *JSONStore) SaveItems(items []model.Item) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create items dir: %w", err)
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("encode items: %w", err)
	}

	if err := os.WriteFile(s.path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write items: %w", err)
	}

	return nil
}
