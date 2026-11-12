package tui

import (
	"encoding/json"
	"sort"

	"playa/internal/api"
	"playa/internal/forge"
)

// draft is the editable copy of one preset. Runs use the draft, so the user can
// change the state or questions without changing the preset on the server.
type draft struct {
	preset    string
	stateKey  string
	state     map[string]any
	questions api.QuestionSet
	dirty     bool
}

func newDraft(p api.Preset) *draft {
	return &draft{
		preset:    p.Name,
		stateKey:  p.StateKey,
		state:     cloneState(p.State),
		questions: forge.Clone(p.Questions),
	}
}

func cloneState(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		out = make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
	}
	return out
}

// stateText returns the main text field of the state.
func (d *draft) stateText() string {
	if s, ok := d.state[d.stateKey].(string); ok {
		return s
	}
	return ""
}

func (d *draft) setStateText(s string) {
	if d.state == nil {
		d.state = map[string]any{}
	}
	d.state[d.stateKey] = s
	d.dirty = true
}

// extraFields returns the state keys other than the main text field, sorted.
func (d *draft) extraFields() []string {
	out := make([]string, 0, len(d.state))
	for k := range d.state {
		if k != d.stateKey {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (d *draft) deleteField(key string) {
	delete(d.state, key)
	d.dirty = true
}

// revertState restores the state half from the server preset while keeping
// local question edits. The dirty flag still reflects the other half.
func (d *draft) revertState(p api.Preset) {
	d.state = cloneState(p.State)
	d.stateKey = p.StateKey
	d.dirty = !d.matches(p)
}

// revertQuestions restores the questions half from the server preset while
// keeping local state edits.
func (d *draft) revertQuestions(p api.Preset) {
	d.questions = forge.Clone(p.Questions)
	d.dirty = !d.matches(p)
}

func (d *draft) matches(p api.Preset) bool {
	if d.stateKey != p.StateKey {
		return false
	}
	a, errA := json.Marshal(d.state)
	b, errB := json.Marshal(p.State)
	if errA != nil || errB != nil || string(a) != string(b) {
		return false
	}
	aq, errAQ := json.Marshal(d.questions)
	bq, errBQ := json.Marshal(p.Questions)
	return errAQ == nil && errBQ == nil && string(aq) == string(bq)
}

func (d *draft) request(model string) api.DecideRequest {
	req := api.DecideRequest{State: cloneState(d.state), Questions: forge.Clone(d.questions)}
	if model != "" && model != "auto" {
		req.Model = model
	}
	return req
}
