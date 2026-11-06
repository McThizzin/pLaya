package lengthguard

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"playa/internal/api"
)

func presets(t *testing.T) map[string]api.Preset {
	t.Helper()
	b, err := os.ReadFile("../api/testdata/presets.json")
	if err != nil {
		t.Fatal(err)
	}
	var ps []api.Preset
	if err := json.Unmarshal(b, &ps); err != nil {
		t.Fatal(err)
	}
	m := map[string]api.Preset{}
	for _, p := range ps {
		m[p.Name] = p
	}
	return m
}

// measured is the real input_tokens (summed over questions) of each built-in
// preset, as returned by the server with its own default state.
var measured = map[string]int{
	"email": 648, "triage": 484, "guard": 358, "moderation": 401, "router": 348,
	"expense": 402, "security": 415, "invoice": 408, "customer_service": 280, "harness": 408,
}

// The estimate is fitted to this data, so it must stay close to it. It may
// run a little high (a conservative warning) but should not run low by much.
func TestEstimateTracksMeasuredPresets(t *testing.T) {
	ps := presets(t)
	for name, want := range measured {
		p := ps[name]
		got := 0
		for _, nq := range p.Questions {
			got += SequenceTokens(p.State, nq.Question)
		}
		lo, hi := int(0.90*float64(want)), int(1.20*float64(want))
		if got < lo || got > hi {
			t.Errorf("%s: estimated %d, measured %d (accepted %d..%d)", name, got, want, lo, hi)
		}
	}
}

// email's longest sequence was measured at S=124.
func TestEmailLongestSequence(t *testing.T) {
	r := Check("english", presets(t)["email"].State, presets(t)["email"].Questions)
	if r.Longest < 105 || r.Longest > 145 {
		t.Errorf("estimated longest = %d, measured S = 124", r.Longest)
	}
	if r.Level != OK {
		t.Errorf("email on english should be OK, got %v (%d/%d)", r.Level, r.Longest, r.Cap)
	}
}

func TestCaps(t *testing.T) {
	for family, want := range map[string]int{
		"english": 512, "typed-decisions": 512, "multilingual": 1024, "kev-0.8b": 2048, "something-new": 512,
	} {
		if got := Cap(family); got != want {
			t.Errorf("Cap(%q) = %d, want %d", family, got, want)
		}
	}
	if HeadCap("english") != 192 || HeadCap("multilingual") != 0 {
		t.Error("HeadCap values changed")
	}
}

func TestLevelFor(t *testing.T) {
	for _, c := range []struct {
		est, limit int
		want       Level
	}{
		{100, 512, OK}, {358, 512, OK}, {359, 512, Warn}, {511, 512, Warn}, {512, 512, Over}, {900, 512, Over}, {10, 0, OK},
	} {
		if got := LevelFor(c.est, c.limit); got != c.want {
			t.Errorf("LevelFor(%d, %d) = %v, want %v", c.est, c.limit, got, c.want)
		}
	}
}

func TestLongStateOverflowsEnglishButNotKev(t *testing.T) {
	qs := api.QuestionSet{{Name: "q", Question: api.Question{Type: api.TypeNoul, Instructions: "Is this billing?"}}}
	long := strings.Repeat("The weather was mild and the meeting ran long. ", 60) // ~2800 chars
	if r := Check("english", long, qs); r.Level != Over {
		t.Errorf("english: %v (%d/%d)", r.Level, r.Longest, r.Cap)
	}
	if r := Check("kev-0.8b", long, qs); r.Level != OK {
		t.Errorf("kev: %v (%d/%d)", r.Level, r.Longest, r.Cap)
	}
}

func TestWideScriptsCountPerCharacter(t *testing.T) {
	if got := EstimateTokens("二重に請求されました"); got != 10 {
		t.Errorf("EstimateTokens(japanese) = %d, want 10", got)
	}
}

func TestHeadBudget(t *testing.T) {
	q := api.Question{Type: api.TypeNoul, Instructions: strings.Repeat("Is this about billing or refunds? ", 40)}
	r := Check("english", "x", api.QuestionSet{{Name: "q", Question: q}})
	if r.HeadLevel != Over {
		t.Errorf("a very long question should exceed the 192-token head budget: %+v", r)
	}
	if r := Check("multilingual", "x", api.QuestionSet{{Name: "q", Question: q}}); r.HeadLevel != OK {
		t.Errorf("unknown head cap should not warn: %+v", r)
	}
}

// Real observation: a state far over the cap made input_tokens exactly
// n_questions x cap (4096 = 2 x 2048 for Kev, 1024 for one multilingual question).
func TestTruncatedDetection(t *testing.T) {
	for _, c := range []struct {
		tokens, n, limit int
		want             bool
	}{
		{4096, 2, 2048, true},
		{1024, 1, 1024, true},
		{512, 1, 512, true},
		{648, 7, 512, false},
		{1023, 1, 1024, false},
		{10, 0, 512, false},
		{10, 1, 0, false},
	} {
		if got := Truncated(c.tokens, c.n, c.limit); got != c.want {
			t.Errorf("Truncated(%d, %d, %d) = %v, want %v", c.tokens, c.n, c.limit, got, c.want)
		}
	}
}
