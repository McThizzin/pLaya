package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"playa/internal/api"
)

func sample(id int) Entry {
	return Entry{
		ID:     id,
		At:     time.Date(2026, 11, 6, 12, 0, id, 0, time.UTC),
		Preset: "guard",
		Model:  "english",
		Request: api.DecideRequest{
			State: map[string]any{"prompt": "hello"},
			Questions: api.QuestionSet{{
				Name: "safe",
				Question: api.Question{
					Type:         api.TypeNoul,
					Instructions: "Safe?",
					Criteria:     json.RawMessage(`{"false":"no","true":"yes"}`),
				},
			}},
		},
		Response: &api.DecideResponse{
			Model:  "laya",
			Family: "english",
			Route:  "english words 1/1",
			Answers: map[string]api.Answer{
				"safe": {Type: api.TypeNoul, Noul: ptr(0.9), Confidence: 0.9},
			},
			Raw:  []byte(`{"answers":{"safe":{"type":"noul"}}}`),
			Wall: 12 * time.Millisecond,
		},
		Raw:    []byte(`{"answers":{"safe":{"type":"noul"}}}`),
		WallMs: 12,
	}
}

func ptr(v float64) *float64 { return &v }

func TestStoreAddListGetAndEvict(t *testing.T) {
	s := New(2)
	s.Add(sample(0))
	s.Add(sample(0))
	s.Add(sample(0))
	if s.Len() != 2 {
		t.Fatalf("len = %d", s.Len())
	}
	list := s.List()
	if list[0].ID != 3 || list[1].ID != 2 {
		t.Fatalf("IDs newest first = %d, %d", list[0].ID, list[1].ID)
	}
	if _, ok := s.Get(1); ok {
		t.Error("evicted entry still present")
	}
	if got, ok := s.Get(3); !ok || got.Preset != "guard" {
		t.Errorf("Get(3) = %+v, %v", got, ok)
	}
	if !s.Delete(3) || s.Len() != 1 {
		t.Error("Delete failed")
	}
}

func TestStoreCopiesAreIndependent(t *testing.T) {
	s := New(5)
	e := s.Add(sample(0))
	e.Request.State.(map[string]any)["prompt"] = "changed"
	e.Request.Questions[0].Criteria[0] = '['
	e.Response.Raw[0] = '['
	got, _ := s.Get(e.ID)
	if got.Request.State.(map[string]any)["prompt"] != "hello" {
		t.Error("stored state was mutated")
	}
	if string(got.Request.Questions[0].Criteria) != `{"false":"no","true":"yes"}` {
		t.Error("stored criteria was mutated")
	}
	if string(got.Response.Raw) != `{"answers":{"safe":{"type":"noul"}}}` {
		t.Error("stored raw response was mutated")
	}
	if got.Response.Wall != 12*time.Millisecond {
		t.Error("wall time was lost")
	}

	list := s.List()
	list[0].Request.State.(map[string]any)["prompt"] = "changed"
	got, _ = s.Get(e.ID)
	if got.Request.State.(map[string]any)["prompt"] != "hello" {
		t.Error("List returned a shared request")
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "history.json")
	s := New(5)
	s.Add(sample(0))
	s.Add(sample(0))
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o", info.Mode().Perm())
	}

	got, err := Load(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 2 {
		t.Fatalf("len = %d", got.Len())
	}
	list := got.List()
	if list[0].ID != 2 || list[0].ReplayOf != 0 {
		t.Errorf("newest = %+v", list[0])
	}
	if list[0].Request.Questions.Names()[0] != "safe" {
		t.Errorf("questions = %v", list[0].Request.Questions.Names())
	}
	if string(list[0].Request.Questions[0].Criteria) != `{"false":"no","true":"yes"}` {
		t.Errorf("criteria order = %s", list[0].Request.Questions[0].Criteria)
	}
	if string(list[0].Response.Raw) == "" {
		t.Error("raw response not saved")
	}
	added := got.Add(sample(0))
	if added.ID != 3 {
		t.Errorf("next ID = %d, want 3", added.ID)
	}
}

func TestLoadMissing(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "missing.json"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 0 {
		t.Errorf("len = %d", s.Len())
	}
}

func TestLoadRejectsBadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, 10); err == nil {
		t.Fatal("expected an error")
	}
}
