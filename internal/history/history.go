// Package history keeps the recent decide runs for the history screen.
//
// Entries are stored in the order they were recorded (oldest first) and are
// deep-copied on the way in and out, so an in-flight request or a later edit
// cannot mutate a saved run.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"playa/internal/api"
)

// DefaultMax is the number of runs kept by the TUI.
const DefaultMax = 50

// Entry is one completed decide call, successful or not.
type Entry struct {
	ID       int                 `json:"id"`
	At       time.Time           `json:"at"`
	Preset   string              `json:"preset"`
	Model    string              `json:"model,omitempty"` // empty means auto
	Request  api.DecideRequest   `json:"request"`
	Response *api.DecideResponse `json:"response,omitempty"`
	Raw      json.RawMessage     `json:"raw,omitempty"`
	WallMs   float64             `json:"wall_ms,omitempty"`
	Err      string              `json:"error,omitempty"`
	Param    string              `json:"param,omitempty"`
	Cold     bool                `json:"cold,omitempty"`
	ReplayOf int                 `json:"replay_of,omitempty"`
}

// Store is a bounded, in-memory run history.
type Store struct {
	max     int
	nextID  int
	entries []Entry // oldest first
}

// New returns an empty store that keeps at most max entries.
func New(max int) *Store {
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{max: max, nextID: 1}
}

// Add records e, assigning an ID when it has none, and returns the stored copy.
// The oldest entries are evicted once the store is full.
func (s *Store) Add(e Entry) Entry {
	e = cloneEntry(e)
	if e.ID <= 0 {
		e.ID = s.nextID
	}
	if e.ID >= s.nextID {
		s.nextID = e.ID + 1
	}
	s.entries = append(s.entries, e)
	if len(s.entries) > s.max {
		drop := len(s.entries) - s.max
		s.entries = append([]Entry(nil), s.entries[drop:]...)
	}
	return cloneEntry(e)
}

// Len returns the number of stored runs.
func (s *Store) Len() int { return len(s.entries) }

// List returns copies of the entries, newest first.
func (s *Store) List() []Entry {
	out := make([]Entry, len(s.entries))
	for i := range s.entries {
		out[len(s.entries)-1-i] = cloneEntry(s.entries[i])
	}
	return out
}

// Get returns a copy of the entry with id.
func (s *Store) Get(id int) (Entry, bool) {
	for _, e := range s.entries {
		if e.ID == id {
			return cloneEntry(e), true
		}
	}
	return Entry{}, false
}

// Delete removes id and reports whether it existed.
func (s *Store) Delete(id int) bool {
	for i, e := range s.entries {
		if e.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes every entry.
func (s *Store) Clear() { s.entries = nil }

// CloneRequest returns a deep copy of a decide request.
func CloneRequest(req api.DecideRequest) api.DecideRequest {
	b, err := json.Marshal(req)
	if err != nil {
		return api.DecideRequest{Model: req.Model}
	}
	var out api.DecideRequest
	if err := json.Unmarshal(b, &out); err != nil {
		return api.DecideRequest{Model: req.Model}
	}
	return out
}

func cloneEntry(e Entry) Entry {
	e.Request = CloneRequest(e.Request)
	e.Response = cloneResponse(e.Response)
	if e.Raw != nil {
		e.Raw = append(json.RawMessage(nil), e.Raw...)
		if e.Response != nil && len(e.Response.Raw) == 0 {
			e.Response.Raw = append([]byte(nil), e.Raw...)
		}
	}
	return e
}

func cloneResponse(in *api.DecideResponse) *api.DecideResponse {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out api.DecideResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	out.Raw = append([]byte(nil), in.Raw...)
	out.Wall = in.Wall
	return &out
}

// DefaultPath is where the TUI persists history.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "playa", "history.json"), nil
}

type file struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Save writes the history to path atomically, creating the directory.
func (s *Store) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(file{Version: 1, Entries: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".history-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Load reads history from path. A missing file is an empty store.
func Load(path string, max int) (*Store, error) {
	s := New(max)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return s, fmt.Errorf("decode %s: %w", path, err)
	}
	for _, e := range f.Entries {
		s.Add(e)
	}
	return s, nil
}
