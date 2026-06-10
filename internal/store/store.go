// Package store owns the tool's per-user data directory and the small amount of
// run state persisted there: whether now-playing is paused and the last
// signature written to Feishu. The state file lets one-shot `update` runs detect
// changes (so Feishu is written only when needed) across separate processes.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// dirName is the per-user data directory under $HOME.
const dirName = ".larktune"

// Dir returns the data directory (~/.larktune). It does not create it.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("store: locate home: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// State is the persisted run state. The zero value (not paused, no signature) is
// the correct default when the file is absent.
type State struct {
	Paused    bool   `json:"paused"`
	Signature string `json:"signature"`
}

// Load reads the state file. A missing file yields the zero State and no error,
// so a first run starts clean.
func Load() (State, error) {
	path, err := statePath()
	if err != nil {
		return State{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("store: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("store: parse %s: %w", path, err)
	}
	return s, nil
}

// Save writes the state file (0600), creating the data directory if needed.
func (s State) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("store: create %s: %w", dir, err)
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), b, 0o600); err != nil {
		return fmt.Errorf("store: write state: %w", err)
	}
	return nil
}

// SetPaused loads the state, flips the paused flag, saves, and returns the result.
func SetPaused(paused bool) (State, error) {
	s, err := Load()
	if err != nil {
		return State{}, err
	}
	s.Paused = paused
	if err := s.Save(); err != nil {
		return State{}, err
	}
	return s, nil
}

func statePath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}
