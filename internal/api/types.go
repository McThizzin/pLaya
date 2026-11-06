// Package api is a small, stdlib-only client for the `laya serve` HTTP API.
//
// The shapes here come from real responses captured against a local server
// (see testdata/), not from documentation. Anything not yet observed is noted.
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Question types understood by the server.
const (
	TypeNoul   = "noul"
	TypeChoice = "choice"
	TypeScore  = "score"
)

// Health is the response of GET /health.
type Health struct {
	Status   string   `json:"status"`
	Device   string   `json:"device"`   // e.g. "vulkan:0"
	Families []string `json:"families"` // catalogued families (not necessarily loaded yet)
	Family   string   `json:"family"`   // "auto" unless the server was pinned
	Model    string   `json:"model"`
	// LastRoute and LastReason describe the most recent request server-wide,
	// not any particular request. Use DecideResponse.Route for per-request data.
	LastRoute  string `json:"last_route"`
	LastReason string `json:"last_reason"`
}

// ModelInfo is one entry of GET /v1/models. The list mixes real families with
// aliases (laya, jev-latest, ...), so prefer Health.Families for a picker.
type ModelInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// Question is one typed question.
//
// Criteria has three shapes depending on Type, so it is kept raw and decoded
// on demand to preserve order:
//
//	choice: object  {"key": "description" | null, ...}   (required, max 16)
//	score:  array   ["level 0", "level 1", ...]          (required, min 2; index = score)
//	noul:   object  {"false": "...", "true": "..."}      (optional)
type Question struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

// Option is one ordered entry of an object-shaped criteria value.
type Option struct {
	Key         string
	Description string // empty when the server value was null
}

// Options returns the entries of an object-shaped criteria in their original
// order (choice and noul). It returns nil for score or when criteria is absent.
func (q Question) Options() []Option {
	raw := bytes.TrimSpace(q.Criteria)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening brace
		return nil
	}
	var out []Option
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return out
		}
		key, _ := keyTok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return out
		}
		var desc string
		_ = json.Unmarshal(v, &desc) // null or non-string leaves desc empty
		out = append(out, Option{Key: key, Description: desc})
	}
	return out
}

// Levels returns the ordered level descriptions of a score question, or nil.
func (q Question) Levels() []string {
	raw := bytes.TrimSpace(q.Criteria)
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// NamedQuestion pairs a question with the key it is sent under.
type NamedQuestion struct {
	Name string
	Question
}

// QuestionSet is an ordered question map. Go maps lose order, and presets
// present their questions in a meaningful order, so this type preserves it
// through both JSON decoding and encoding.
type QuestionSet []NamedQuestion

// Names returns the question names in order.
func (qs QuestionSet) Names() []string {
	out := make([]string, len(qs))
	for i, nq := range qs {
		out[i] = nq.Name
	}
	return out
}

// UnmarshalJSON decodes a JSON object, keeping key order.
func (qs *QuestionSet) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("questions must be a JSON object")
	}
	var out QuestionSet
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		name, _ := keyTok.(string)
		var q Question
		if err := dec.Decode(&q); err != nil {
			return fmt.Errorf("question %q: %w", name, err)
		}
		out = append(out, NamedQuestion{Name: name, Question: q})
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return err
	}
	*qs = out
	return nil
}

// MarshalJSON encodes the set as a JSON object in order.
func (qs QuestionSet) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, nq := range qs {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(nq.Name)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(nq.Question)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Preset is one entry of GET /v1/presets: a ready-to-send payload plus labels.
type Preset struct {
	Name     string         `json:"name"`
	Title    string         `json:"title"`
	Blurb    string         `json:"blurb"`
	StateKey string         `json:"state_key"` // the main text field inside State
	State    map[string]any `json:"state"`
	// Questions keeps the preset's own order.
	Questions QuestionSet `json:"questions"`
}

// StateText returns the main text of the preset's state (State[StateKey]).
func (p Preset) StateText() string {
	if s, ok := p.State[p.StateKey].(string); ok {
		return s
	}
	return ""
}

// Request builds a decide request from the preset as published.
func (p Preset) Request() DecideRequest {
	return DecideRequest{State: p.State, Questions: p.Questions}
}

// DecideRequest is the body of POST /v1/systemone (alias /v1/decide).
type DecideRequest struct {
	Model     string      `json:"model,omitempty"` // pin a family; empty = auto routing
	State     any         `json:"state"`           // a string or an object
	Questions QuestionSet `json:"questions"`
}

// Action is returned with every answer. act_probability is 1 in the built-in
// presets but was 0 in one Kev answer, so its meaning is still unknown; keep
// it out of the UI.
type Action struct {
	ActProbability float64 `json:"act_probability"`
}

// Answer is one answer. Which fields are set depends on Type:
//
//	noul:   Noul (P(yes)); Confidence = max(p, 1-p)
//	choice: Choice + Probabilities keyed by criterion name;
//	        Confidence = 1 - normalized Shannon entropy
//	score:  Score (expected level index) + Legend + Probabilities keyed by
//	        level index ("0", "1", ...); Confidence as for choice
type Answer struct {
	Type          string             `json:"type"`
	Confidence    float64            `json:"confidence"`
	Action        *Action            `json:"action,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Usage reports cost. InputTokens is the SUM across questions (each question
// is its own sequence), and the server truncates silently at the family's cap.
type Usage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	LatencyMs    float64 `json:"latency_ms"`
}

// DecideResponse is the response of a decide call.
type DecideResponse struct {
	Model   string            `json:"model"`
	Family  string            `json:"family"`
	Route   string            `json:"route"` // e.g. "english words 5/8", "model multilingual"
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`

	// Filled in by the client, not part of the wire format.
	Raw  []byte        `json:"-"` // response body as received
	Wall time.Duration `json:"-"` // client-side wall time
}

// PrettyRaw returns Raw re-indented for display.
func (r *DecideResponse) PrettyRaw() string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, r.Raw, "", "  "); err != nil {
		return strings.TrimSpace(string(r.Raw))
	}
	return buf.String()
}
