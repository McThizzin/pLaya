// Package snippet stores reusable state snapshots for the State Canvas.
//
// A snippet keeps the whole state object, the key of its main text field and
// the preset it came from, so it can be loaded into a draft without losing
// structured fields. Files live under the playa config directory.
package snippet

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snippet is a saved state. StateKey names the main text field in State.
type Snippet struct {
	Preset   string         `json:"preset,omitempty"`
	StateKey string         `json:"state_key"`
	State    map[string]any `json:"state"`
}

// ConfigDir returns the directory where playa stores state snippets.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "playa", "snippets"), nil
}

// Path returns the file path for a snippet name.
func Path(name string) (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	slug := Slug(name)
	if slug == "" {
		return "", errors.New("a snippet name is required")
	}
	return filepath.Join(dir, slug+".json"), nil
}

// Save writes a snippet to the config directory and returns the path written.
func Save(name string, s Snippet) (string, error) {
	if strings.TrimSpace(s.StateKey) == "" {
		return "", errors.New("a snippet needs a state key")
	}
	if s.State == nil {
		return "", errors.New("a snippet needs a state")
	}
	path, err := Path(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Load reads a snippet by name.
func Load(name string) (Snippet, error) {
	path, err := Path(name)
	if err != nil {
		return Snippet{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Snippet{}, err
	}
	var s Snippet
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Snippet{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if strings.TrimSpace(s.StateKey) == "" {
		return Snippet{}, errors.New("saved snippet has no state key")
	}
	if s.State == nil {
		return Snippet{}, errors.New("saved snippet has no state")
	}
	return s, nil
}

// All returns the names of saved snippets, sorted.
func All() ([]string, error) {
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

// Delete removes a saved snippet.
func Delete(name string) error {
	path, err := Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Slug turns a display name into a file name. It matches forge.Slug so names
// look the same across the two libraries.
func Slug(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-' || r == ' ' || r == '\t':
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}
