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

func (d *draft) request(model string) api.DecideRequest {
	req := api.DecideRequest{State: d.state, Questions: forge.Clone(d.questions)}
	if model != "" && model != "auto" {
		req.Model = model
	}
	return req
}
