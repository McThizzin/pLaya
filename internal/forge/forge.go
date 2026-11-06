// Package forge contains the editing logic behind the Question Forge.
//
// It owns the conversions between the form-friendly text shapes and the wire
// shapes used by the server. In particular, choice criteria are encoded by hand
// so their order is preserved: encoding a Go map would sort the keys.
package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"playa/internal/api"
)

const (
	// MaxOptions is the server's max_opts.
	MaxOptions = 16
	// MinLevels is the server's minimum criteria length for score questions.
	MinLevels = 2
)

var (
	// ErrNoOptions is returned when a choice question has no options.
	ErrNoOptions = errors.New("choice needs at least one option")
	// ErrNoLevels is returned when a score question has too few levels.
	ErrNoLevels = fmt.Errorf("score needs at least %d levels", MinLevels)
)

// ParseChoice reads one option per line. A line is either "key" or
// "key: description". Blank lines and lines starting with '#' are ignored.
func ParseChoice(text string) ([]api.Option, error) {
	var out []api.Option
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, desc := line, ""
		if j := strings.IndexByte(line, ':'); j >= 0 {
			key, desc = strings.TrimSpace(line[:j]), strings.TrimSpace(line[j+1:])
		}
		if key == "" {
			return nil, fmt.Errorf("line %d: empty option key", i+1)
		}
		if seen[key] {
			return nil, fmt.Errorf("line %d: duplicate option %q", i+1, key)
		}
		seen[key] = true
		out = append(out, api.Option{Key: key, Description: desc})
	}
	if len(out) == 0 {
		return nil, ErrNoOptions
	}
	if len(out) > MaxOptions {
		return nil, fmt.Errorf("%d options, max is %d", len(out), MaxOptions)
	}
	return out, nil
}

// FormatChoice renders options as the text accepted by ParseChoice.
func FormatChoice(opts []api.Option) string {
	var b strings.Builder
	for i, o := range opts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(o.Key)
		if o.Description != "" {
			b.WriteString(": ")
			b.WriteString(o.Description)
		}
	}
	return b.String()
}

// ParseLevels reads one level per line, in order. Blank lines are ignored.
func ParseLevels(text string) ([]string, error) {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) < MinLevels {
		return nil, ErrNoLevels
	}
	return out, nil
}

// FormatLevels renders levels as the text accepted by ParseLevels.
func FormatLevels(levels []string) string {
	return strings.Join(levels, "\n")
}

// EncodeCriteria builds a Question.Criteria value from its parts. It returns
// nil for a noul question with no true/false descriptions.
func EncodeCriteria(typ string, opts []api.Option, levels []string) (json.RawMessage, error) {
	switch typ {
	case api.TypeNoul:
		if len(opts) == 0 {
			return nil, nil
		}
		return encodeObject(opts)
	case api.TypeChoice:
		if len(opts) == 0 {
			return nil, ErrNoOptions
		}
		if len(opts) > MaxOptions {
			return nil, fmt.Errorf("%d options, max is %d", len(opts), MaxOptions)
		}
		return encodeObject(opts)
	case api.TypeScore:
		if len(levels) < MinLevels {
			return nil, ErrNoLevels
		}
		b, err := json.Marshal(levels)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(b), nil
	default:
		return nil, fmt.Errorf("unknown question type %q", typ)
	}
}

// encodeObject writes {"key": "value", ...} in slice order.
func encodeObject(opts []api.Option) (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, o := range opts {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(o.Key)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		if o.Description == "" {
			b.WriteString("null")
			continue
		}
		v, err := json.Marshal(o.Description)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return json.RawMessage(b.Bytes()), nil
}

// Question builds a validated question from its parts.
func Question(typ, instructions string, opts []api.Option, levels []string) (api.Question, error) {
	crit, err := EncodeCriteria(typ, opts, levels)
	if err != nil {
		return api.Question{}, err
	}
	return api.Question{Type: typ, Instructions: instructions, Criteria: crit}, nil
}

// Validate checks the parts of one question without changing them.
func Validate(typ, instructions string, opts []api.Option, levels []string) error {
	if strings.TrimSpace(instructions) == "" {
		return errors.New("instructions are required")
	}
	_, err := EncodeCriteria(typ, opts, levels)
	return err
}

// Clone returns a deep copy of a question set, including raw criteria.
func Clone(qs api.QuestionSet) api.QuestionSet {
	out := make(api.QuestionSet, len(qs))
	for i, nq := range qs {
		out[i] = api.NamedQuestion{
			Name: nq.Name,
			Question: api.Question{
				Type:         nq.Question.Type,
				Instructions: nq.Question.Instructions,
				Criteria:     append(json.RawMessage(nil), nq.Question.Criteria...),
			},
		}
	}
	return out
}

// NextName returns base, or base_2, base_3, ... so the name is not already in
// use. Question names are JSON object keys and must be unique.
func NextName(qs api.QuestionSet, base string) string {
	base = Slug(base)
	if base == "" {
		base = "question"
	}
	used := map[string]bool{}
	for _, nq := range qs {
		used[nq.Name] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s_%d", base, i)
		if !used[name] {
			return name
		}
	}
}

// Slug turns a human label into a question key.
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
