// Package lengthguard warns before the server silently truncates.
//
// The server never errors or warns on over-long input: it cuts the END of the
// state at the family's cap (512 english, 1024 multilingual, 2048 Kev), and
// accuracy degrades well before the cap. Each question is its own sequence,
// so the cap applies per question, and usage.input_tokens is the sum.
//
// Token counts here are ESTIMATES. The constants were fitted to a handful of
// measured requests (see lengthguard_test.go); recalibrate from
// usage.input_tokens as more data comes in.
package lengthguard

import (
	"encoding/json"
	"math"
	"strings"

	"playa/internal/api"
)

const (
	// charsPerToken is fitted so summed estimates land within about -6%/+13%
	// of measured input_tokens across all ten built-in presets.
	charsPerToken = 4.0
	// Fixed template overhead per sequence. Noul sequences measured heavier
	// than choice/score ones, so they get their own constant.
	overheadNoul  = 28
	overheadOther = 14

	warnFraction = 0.70 // warn at 70% of the cap
)

// Level is how close an estimate is to a cap.
type Level int

const (
	OK Level = iota
	Warn
	Over
)

func (l Level) String() string {
	switch l {
	case Warn:
		return "warn"
	case Over:
		return "over"
	}
	return "ok"
}

// Cap returns the per-sequence token cap (max_len) of a family. Unknown
// families get the smallest known cap so warnings err on the safe side.
// typed-decisions is assumed to match english; check with `laya info`.
func Cap(family string) int {
	switch family {
	case "multilingual":
		return 1024
	case "kev-0.8b":
		return 2048
	default: // english, typed-decisions, anything unknown
		return 512
	}
}

// HeadCap returns head_max_len, which looks like the budget for the question
// part (instructions plus options) of a sequence. 0 means unknown.
func HeadCap(family string) int {
	switch family {
	case "english":
		return 192
	case "kev-0.8b":
		return 2048
	}
	return 0
}

// EstimateTokens estimates the tokens of free text. CJK and other wide
// scripts are counted at about one token per character.
func EstimateTokens(s string) int {
	narrow, wide := 0, 0
	for _, r := range s {
		if r >= 0x2E80 {
			wide++
		} else {
			narrow++
		}
	}
	return int(math.Ceil(float64(narrow)/charsPerToken)) + wide
}

// StateTokens estimates the tokens the state contributes to every sequence.
func StateTokens(state any) int {
	if s, ok := state.(string); ok {
		return EstimateTokens(s)
	}
	b, err := json.Marshal(state)
	if err != nil {
		return 0
	}
	return EstimateTokens(string(b))
}

// HeadTokens estimates the question part: instructions plus criteria.
func HeadTokens(q api.Question) int {
	var b strings.Builder
	b.WriteString(q.Instructions)
	for _, o := range q.Options() {
		b.WriteByte(' ')
		b.WriteString(o.Key)
		b.WriteByte(' ')
		b.WriteString(o.Description)
	}
	for _, l := range q.Levels() {
		b.WriteByte(' ')
		b.WriteString(l)
	}
	return EstimateTokens(b.String())
}

// SequenceTokens estimates the length of one question's sequence.
func SequenceTokens(state any, q api.Question) int {
	ov := overheadOther
	if q.Type == api.TypeNoul {
		ov = overheadNoul
	}
	return ov + HeadTokens(q) + StateTokens(state)
}

// LevelFor classifies an estimate against a cap.
func LevelFor(est, limit int) Level {
	switch {
	case limit <= 0:
		return OK
	case est >= limit:
		return Over
	case float64(est) >= warnFraction*float64(limit):
		return Warn
	}
	return OK
}

// Report is the result of checking a request before sending it.
type Report struct {
	Family    string
	Cap       int   // per-sequence cap for the family
	Longest   int   // estimated tokens of the longest sequence
	Level     Level // Longest against Cap
	Head      int   // estimated tokens of the longest question part
	HeadCap   int   // 0 when unknown
	HeadLevel Level
}

// Check estimates the longest sequence in a request for a family. Pass the
// family the request will land on ("english" is a reasonable default in auto
// mode; the server reports the real one in DecideResponse.Family).
func Check(family string, state any, qs api.QuestionSet) Report {
	r := Report{Family: family, Cap: Cap(family), HeadCap: HeadCap(family)}
	for _, nq := range qs {
		if n := SequenceTokens(state, nq.Question); n > r.Longest {
			r.Longest = n
		}
		if n := HeadTokens(nq.Question); n > r.Head {
			r.Head = n
		}
	}
	r.Level = LevelFor(r.Longest, r.Cap)
	r.HeadLevel = LevelFor(r.Head, r.HeadCap)
	return r
}

// Truncated reports whether a response shows the state hit the cap. Because
// input_tokens is summed over questions, an overflowing state makes every
// sequence exactly cap long, so the sum reaches n x cap. Partial truncation of
// a long question set will not show up here; rely on Check for that.
func Truncated(inputTokens, nQuestions, limit int) bool {
	return nQuestions > 0 && limit > 0 && inputTokens >= nQuestions*limit
}
