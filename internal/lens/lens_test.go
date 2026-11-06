package lens

import (
	"math"
	"testing"

	"playa/internal/api"
)

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (+/- %.4f)", what, got, want, tol)
	}
}

func f64(v float64) *float64 { return &v }

// The server's own confidence for choice and score answers equals
// 1 - normalized entropy, so our Certainty must reproduce it from the
// probabilities alone. These numbers are from real responses.
func TestCertaintyReproducesServerConfidence(t *testing.T) {
	near(t, "choice tone", Certainty([]float64{0.0331, 0.9669}), 0.7902, 0.001)
	near(t, "score urgency", Certainty([]float64{0.0258, 0.0589, 0.9152}), 0.6883, 0.002)
}

func TestCertaintyEdges(t *testing.T) {
	near(t, "one-hot", Certainty([]float64{1, 0, 0}), 1, 1e-9)
	near(t, "uniform", Certainty([]float64{0.25, 0.25, 0.25, 0.25}), 0, 1e-9)
	near(t, "single class", Certainty([]float64{1}), 1, 0)
	near(t, "empty", Certainty(nil), 1, 0)
}

func TestNoulYes(t *testing.T) {
	s := Summarize("billing", api.Answer{Type: api.TypeNoul, Noul: f64(0.9196), Confidence: 0.9196}, Options{})
	if s.Label != "YES" {
		t.Errorf("label = %s", s.Label)
	}
	near(t, "prob", s.Prob, 0.9196, 1e-9)
	near(t, "gap", s.Gap, 0.9196-0.0804, 1e-9)
	if s.RunnerUp.Label != "NO" || s.Saturated || s.Contested {
		t.Errorf("unexpected flags: %+v", s)
	}
	// server confidence says 0.92, but the uniform certainty is much lower
	near(t, "certainty", s.Certainty, 0.596, 0.01)
}

// noul = 0 with server confidence = 1: the winner is NO, and it is saturated.
func TestNoulNoSaturated(t *testing.T) {
	s := Summarize("cooking", api.Answer{Type: api.TypeNoul, Noul: f64(0), Confidence: 1}, Options{})
	if s.Label != "NO" || s.Prob != 1 || !s.Saturated {
		t.Errorf("summary = %+v", s)
	}
}

// 0.6465 reads as fairly sure from the server, but is close to a coin flip.
func TestNoulWeak(t *testing.T) {
	s := Summarize("spam", api.Answer{Type: api.TypeNoul, Noul: f64(0.6465), Confidence: 0.6465}, Options{})
	if s.Label != "YES" || s.Contested {
		t.Errorf("summary = %+v", s)
	}
	near(t, "certainty", s.Certainty, 0.063, 0.01)

	// the same shape just under the line is contested
	c := Summarize("x", api.Answer{Type: api.TypeNoul, Noul: f64(0.55)}, Options{})
	if !c.Contested {
		t.Errorf("0.55 should be contested: %+v", c)
	}
	// ...and a wider threshold can be requested
	w := Summarize("x", api.Answer{Type: api.TypeNoul, Noul: f64(0.6465)}, Options{ContestedGap: 0.5})
	if !w.Contested {
		t.Errorf("gap 0.29 should be contested at threshold 0.5: %+v", w)
	}
}

func TestChoice(t *testing.T) {
	a := api.Answer{
		Type: api.TypeChoice, Choice: "angry", Confidence: 0.7902,
		Probabilities: map[string]float64{"calm": 0.0331, "angry": 0.9669},
	}
	s := Summarize("tone", a, Options{})
	if s.Label != "angry" || s.RunnerUp.Label != "calm" {
		t.Errorf("summary = %+v", s)
	}
	near(t, "prob", s.Prob, 0.9669, 1e-9)
	near(t, "gap", s.Gap, 0.9338, 1e-9)
	near(t, "certainty", s.Certainty, 0.7902, 0.001)
	if s.Levels[0].Key != "angry" || s.Levels[1].Key != "calm" {
		t.Errorf("levels should be sorted by probability: %+v", s.Levels)
	}
}

func TestChoiceContestedWhenFlat(t *testing.T) {
	// four options, nearly flat: the harness "next" case (server confidence 0.0787)
	a := api.Answer{
		Type: api.TypeChoice, Choice: "tool",
		Probabilities: map[string]float64{"act": 0.22, "tool": 0.30, "ask_user": 0.26, "stop": 0.22},
	}
	s := Summarize("next", a, Options{})
	if !s.Contested {
		t.Errorf("flat choice should be contested: %+v", s)
	}
	if s.RunnerUp.Label != "ask_user" {
		t.Errorf("runner-up = %s", s.RunnerUp.Label)
	}
	if s.Certainty > 0.1 {
		t.Errorf("certainty = %.3f, want near 0", s.Certainty)
	}
}

func TestChoiceTrustsServerLabel(t *testing.T) {
	// if the server's label is not the arg-max, the headline follows the server
	a := api.Answer{
		Type: api.TypeChoice, Choice: "b",
		Probabilities: map[string]float64{"a": 0.51, "b": 0.49},
	}
	s := Summarize("q", a, Options{})
	if s.Label != "b" || s.RunnerUp.Label != "a" {
		t.Errorf("summary = %+v", s)
	}
}

func TestScore(t *testing.T) {
	a := api.Answer{
		Type: api.TypeScore, Score: f64(1.8894), Confidence: 0.6883,
		Legend:        map[string]string{"0": "can wait", "1": "this week", "2": "today"},
		Probabilities: map[string]float64{"0": 0.0258, "1": 0.0589, "2": 0.9152},
	}
	s := Summarize("urgency", a, Options{})
	if s.Label != "today" || s.RunnerUp.Label != "this week" {
		t.Errorf("summary = %+v", s)
	}
	near(t, "prob", s.Prob, 0.9152, 1e-9)
	near(t, "gap", s.Gap, 0.9152-0.0589, 1e-9)
	near(t, "certainty", s.Certainty, 0.6883, 0.002)
	if s.Expected == nil || *s.Expected != 1.8894 {
		t.Errorf("expected = %v", s.Expected)
	}
	if len(s.Levels) != 3 || s.Levels[0].Label != "can wait" || s.Levels[2].Label != "today" {
		t.Errorf("levels should be in level order: %+v", s.Levels)
	}
}

func TestScoreLevelsSortNumerically(t *testing.T) {
	// unpadded keys like the server sends: "0", "1", ..., "10", "11"
	probs := map[string]float64{"0": 0.1, "1": 0.1, "2": 0.1, "10": 0.6, "11": 0.1}
	s := Summarize("q", api.Answer{Type: api.TypeScore, Probabilities: probs}, Options{})
	var got []string
	for _, l := range s.Levels {
		got = append(got, l.Key)
	}
	want := []string{"0", "1", "2", "10", "11"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if s.Label != "level 10" {
		t.Errorf("label without a legend = %q", s.Label)
	}
}

func TestEmptyAnswerDoesNotPanic(t *testing.T) {
	for _, typ := range []string{api.TypeNoul, api.TypeChoice, api.TypeScore, "mystery"} {
		s := Summarize("q", api.Answer{Type: typ}, Options{})
		if s.Label == "" {
			t.Errorf("%s: empty label", typ)
		}
	}
}
