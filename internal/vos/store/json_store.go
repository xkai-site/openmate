package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"vos/internal/vos/domain"
)

type StateStore interface {
	Load() (domain.VfsState, error)
	Save(state domain.VfsState) error
}

type JSONStateStore struct {
	path string
}

func NewJSONStateStore(path string) *JSONStateStore {
	return &JSONStateStore{path: path}
}

func (store *JSONStateStore) Load() (domain.VfsState, error) {
	state := domain.NewVfsState()
	payload, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return domain.VfsState{}, fmt.Errorf("read state file: %w", err)
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return domain.VfsState{}, fmt.Errorf("invalid state file: %w", err)
	}
	state.Normalize()
	return state, nil
}

func (store *JSONStateStore) Save(state domain.VfsState) error {
	state.Normalize()

	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(store.path), filepath.Base(store.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(payload); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}

	if err := os.Rename(tmpPath, store.path); err == nil {
		return nil
	} else if runtime.GOOS == "windows" {
		if removeErr := os.Remove(store.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace state file: %w", err)
		}
		if retryErr := os.Rename(tmpPath, store.path); retryErr != nil {
			return fmt.Errorf("replace state file: %w", retryErr)
		}
		return nil
	} else {
		return fmt.Errorf("replace state file: %w", err)
	}
}
