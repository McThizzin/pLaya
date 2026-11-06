package tui

import (
	"encoding/json"
	"testing"

	"playa/internal/api"
)

func testPreset() api.Preset {
	return api.Preset{
		Name:     "email",
		StateKey: "body",
		State: map[string]any{
			"body":   "original",
			"locale": "en",
			"tier":   map[string]any{"name": "gold"},
		},
		Questions: api.QuestionSet{
			{Name: "category", Question: api.Question{Type: api.TypeChoice, Instructions: "Which?", Criteria: json.RawMessage(`{"billing":null,"sales":null}`)}},
		},
	}
}

func TestDraftIsIndependentOfPreset(t *testing.T) {
	p := testPreset()
	d := newDraft(p)
	d.setStateText("changed")
	d.questions[0].Question.Criteria = json.RawMessage(`{"other":null}`)
	d.state["tier"].(map[string]any)["name"] = "silver"

	if p.State["body"] != "original" {
		t.Errorf("preset state changed: %v", p.State["body"])
	}
	if string(p.Questions[0].Question.Criteria) != `{"billing":null,"sales":null}` {
		t.Errorf("preset criteria changed: %s", p.Questions[0].Question.Criteria)
	}
	if p.State["tier"].(map[string]any)["name"] != "gold" {
		t.Errorf("preset nested state changed: %v", p.State["tier"])
	}
	if !d.dirty {
		t.Error("editing should mark the draft dirty")
	}
}

func TestDraftExtrasAndRequest(t *testing.T) {
	d := newDraft(testPreset())
	if got := d.extraFields(); len(got) != 2 || got[0] != "locale" || got[1] != "tier" {
		t.Errorf("extraFields = %v", got)
	}
	d.deleteField("locale")
	if got := d.extraFields(); len(got) != 1 || got[0] != "tier" {
		t.Errorf("after delete = %v", got)
	}

	req := d.request("multilingual")
	if req.Model != "multilingual" {
		t.Errorf("model = %q", req.Model)
	}
	if req.State.(map[string]any)["body"] != "original" {
		t.Errorf("request state = %v", req.State)
	}
	// the request must get a copy, not the draft's question slice
	req.Questions[0].Question.Instructions = "mutated"
	if d.questions[0].Question.Instructions == "mutated" {
		t.Error("request shares questions with the draft")
	}

	auto := d.request("auto")
	if auto.Model != "" {
		t.Errorf("auto should leave model empty, got %q", auto.Model)
	}
}

func TestDraftHandlesMissingState(t *testing.T) {
	d := newDraft(api.Preset{Name: "x", StateKey: "body"})
	if d.stateText() != "" {
		t.Errorf("stateText = %q", d.stateText())
	}
	d.setStateText("hello")
	if d.stateText() != "hello" {
		t.Errorf("stateText = %q", d.stateText())
	}
}
