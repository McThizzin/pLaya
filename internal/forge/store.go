package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"playa/internal/api"
)

// Set is a saved question set. Questions keeps the author's order.
type Set struct {
	Preset    string          `json:"preset,omitempty"`
	Questions api.QuestionSet `json:"questions"`
}

// ConfigDir returns the directory where playa stores question sets.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "playa", "questions"), nil
}

// Path returns the file path for a set name.
func Path(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Slug(name)+".json"), nil
}

// Save writes a set to the config directory and returns the path written.
func Save(name string, set Set) (string, error) {
	path, err := Path(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Load reads a set by name.
func Load(name string) (Set, error) {
	path, err := Path(name)
	if err != nil {
		return Set{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Set{}, err
	}
	var set Set
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err != nil {
		return Set{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if len(set.Questions) == 0 {
		return Set{}, errors.New("saved set has no questions")
	}
	return set, nil
}

// List returns the names of saved sets, sorted.
func List() ([]string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
}
