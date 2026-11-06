// Package lens turns raw answers into what the Decision Lens displays.
//
// Design notes, from real responses:
//   - The server's "confidence" means different things per type (noul:
//     max(p, 1-p); choice/score: 1 - normalized entropy), so it is not
//     comparable across questions. The UI colors by the probability of the
//     winning class and shows our own uniform Certainty next to it.
//   - Probabilities can saturate to exactly 0 or 1 (4-decimal rounding).
//   - Temperatures are baked into the models and vary with option count, so
//     certainty is only comparable between questions of similar shape.
package lens

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"playa/internal/api"
)

// DefaultContestedGap marks an answer as contested when the winner leads the
// runner-up by less than this much probability.
const DefaultContestedGap = 0.20

// saturatedAt is the winning probability at or above which an answer counts as
// saturated (the server rounds to 4 decimals, so 0.9999+ is effectively 1).
const saturatedAt = 0.9999

// Level is one class of a distribution.
type Level struct {
	Key   string
	Label string
	Prob  float64
}

// Summary is the display-ready view of one answer.
type Summary struct {
	Name string
	Type string

	Label    string  // headline: YES / NO, the choice key, or the score level's label
	Prob     float64 // probability of the headline class
	RunnerUp Level   // zero value when there is no other class
	Gap      float64 // Prob minus the runner-up's probability

	Certainty        float64 // 1 - H/ln(n), uniform across types
	ServerConfidence float64 // as reported; type-dependent meaning

	Saturated bool // winner probability is effectively 1
	Contested bool // Gap is below the threshold

	Expected *float64 // score only: probability-weighted level index
	Levels   []Level  // noul: YES, NO; choice: by probability desc; score: by level index
}

// Options tunes Summarize. The zero value uses the defaults.
type Options struct {
	ContestedGap float64
}

// Certainty returns 1 - H(p)/ln(n): 1 for a one-hot distribution, 0 for a
// uniform one. Fewer than two classes returns 1.
func Certainty(probs []float64) float64 {
	n := len(probs)
	if n < 2 {
		return 1
	}
	h := 0.0
	for _, p := range probs {
		if p > 0 {
			h -= p * math.Log(p)
		}
	}
	c := 1 - h/math.Log(float64(n))
	return math.Min(1, math.Max(0, c))
}

// Summarize builds the display summary of one answer.
func Summarize(name string, a api.Answer, opt Options) Summary {
	gapLimit := opt.ContestedGap
	if gapLimit <= 0 {
		gapLimit = DefaultContestedGap
	}
	s := Summary{Name: name, Type: a.Type, ServerConfidence: a.Confidence}

	switch a.Type {
	case api.TypeNoul:
		summarizeNoul(&s, a)
	case api.TypeScore:
		summarizeScore(&s, a)
	default: // choice, and any unknown type that carries probabilities
		summarizeChoice(&s, a)
	}

	if s.Label == "" {
		s.Label = "?"
	}
	s.Saturated = s.Prob >= saturatedAt
	s.Contested = len(s.Levels) > 1 && s.Gap < gapLimit
	return s
}

func summarizeNoul(s *Summary, a api.Answer) {
	p := 0.0
	if a.Noul != nil {
		p = *a.Noul
	}
	yes := Level{Key: "yes", Label: "YES", Prob: p}
	no := Level{Key: "no", Label: "NO", Prob: 1 - p}
	win, other := yes, no
	if p < 0.5 {
		win, other = no, yes
	}
	s.Label, s.Prob, s.RunnerUp = win.Label, win.Prob, other
	s.Gap = win.Prob - other.Prob
	s.Levels = []Level{yes, no}
	s.Certainty = Certainty([]float64{p, 1 - p})
}

func summarizeChoice(s *Summary, a api.Answer) {
	levels := make([]Level, 0, len(a.Probabilities))
	for k, p := range a.Probabilities {
		levels = append(levels, Level{Key: k, Label: k, Prob: p})
	}
	sort.Slice(levels, func(i, j int) bool {
		if levels[i].Prob != levels[j].Prob {
			return levels[i].Prob > levels[j].Prob
		}
		return levels[i].Key < levels[j].Key
	})
	s.Levels = levels
	if len(levels) == 0 {
		s.Label = a.Choice
		return
	}
	win := levels[0]
	if a.Choice != "" {
		for _, l := range levels {
			if l.Key == a.Choice {
				win = l
				break
			}
		}
	}
	s.Label, s.Prob = win.Label, win.Prob
	s.RunnerUp = runnerUp(levels, win.Key)
	s.Gap = win.Prob - s.RunnerUp.Prob
	s.Certainty = Certainty(probs(levels))
}

func summarizeScore(s *Summary, a api.Answer) {
	type idxLevel struct {
		idx int
		l   Level
	}
	var tmp []idxLevel
	for k, p := range a.Probabilities {
		idx, err := strconv.Atoi(k)
		if err != nil {
			idx = math.MaxInt32 // unparsable keys sort last
		}
		label := a.Legend[k]
		if label == "" {
			label = fmt.Sprintf("level %s", k)
		}
		tmp = append(tmp, idxLevel{idx, Level{Key: k, Label: label, Prob: p}})
	}
	// numeric order, so "10" does not sort before "2"
	sort.Slice(tmp, func(i, j int) bool { return tmp[i].idx < tmp[j].idx })
	levels := make([]Level, len(tmp))
	for i, t := range tmp {
		levels[i] = t.l
	}
	s.Levels = levels
	s.Expected = a.Score
	if len(levels) == 0 {
		return
	}
	win := levels[0]
	for _, l := range levels {
		if l.Prob > win.Prob {
			win = l
		}
	}
	s.Label, s.Prob = win.Label, win.Prob
	s.RunnerUp = runnerUp(levels, win.Key)
	s.Gap = win.Prob - s.RunnerUp.Prob
	s.Certainty = Certainty(probs(levels))
}

// runnerUp returns the most probable level other than winKey (zero value if none).
func runnerUp(levels []Level, winKey string) Level {
	var best Level
	found := false
	for _, l := range levels {
		if l.Key == winKey {
			continue
		}
		if !found || l.Prob > best.Prob {
			best, found = l, true
		}
	}
	return best
}

func probs(levels []Level) []float64 {
	out := make([]float64, len(levels))
	for i, l := range levels {
		out[i] = l.Prob
	}
	return out
}
