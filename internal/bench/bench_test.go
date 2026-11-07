package bench

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultMS(t *testing.T) {
	// Short questions: fixed cost plus a per-question term.
	if got := DefaultMS(1, 0); math.Abs(got-92) > 0.01 {
		t.Errorf("DefaultMS(1, 0) = %.1f, want 92", got)
	}
	if got := DefaultMS(7, 0); math.Abs(got-224) > 0.01 {
		t.Errorf("DefaultMS(7, 0) = %.1f, want 224", got)
	}
	// Long sequences add a per-token term.
	if got := DefaultMS(7, 124); got < 350 || got > 390 {
		t.Errorf("DefaultMS(7, 124) = %.1f, want about 359", got)
	}
	// A ninth question costs another forward, not just another question.
	eight, nine := DefaultMS(8, 0), DefaultMS(9, 0)
	if nine-eight < 180 {
		t.Errorf("second forward = %.1f ms, want at least 180", nine-eight)
	}
	// Never negative or zero for an empty-ish request.
	if got := DefaultMS(0, -10); got <= 0 {
		t.Errorf("DefaultMS(0, -10) = %.1f", got)
	}
}

func TestPredictorFallsBack(t *testing.T) {
	p := NewPredictor(10)
	got := p.Predict("english", 7, 124)
	if got.Source != SourceDefault || got.MS != DefaultMS(7, 124) || got.Samples != 0 {
		t.Fatalf("prediction = %+v, want default", got)
	}
}

func TestPredictorFitsWarmSamples(t *testing.T) {
	p := NewPredictor(200)
	// A deliberately different machine: y = 40 + 12*q + 0.8*tokens.
	want := func(q, tok int) float64 { return 40 + 12*float64(q) + 0.8*float64(tok) }
	at := time.Now()
	n := 0
	for _, q := range []int{3, 5, 7, 9, 12, 16} {
		for _, tok := range []int{40, 90, 180, 320} {
			p.Observe(Sample{Family: "english", Questions: q, Tokens: tok, LatencyMS: want(q, tok), At: at})
			n++
		}
	}
	got := p.Predict("english", 10, 250)
	if got.Source != SourceFit || got.Samples != n {
		t.Fatalf("prediction = %+v, want a fit over %d samples", got, n)
	}
	if diff := math.Abs(got.MS - want(10, 250)); diff > 8 {
		t.Errorf("fit = %.1f ms, want %.1f (diff %.1f)", got.MS, want(10, 250), diff)
	}
}

func TestPredictorUsesGlobalFitForAQuietFamily(t *testing.T) {
	p := NewPredictor(200)
	for i := 0; i < 10; i++ {
		p.Observe(Sample{Family: "english", Questions: 5, Tokens: 100, LatencyMS: 200 + float64(i)})
	}
	got := p.Predict("multilingual", 5, 100)
	if got.Source != SourceFit {
		t.Fatalf("prediction = %+v, want the global fit", got)
	}
}

func TestPredictorBoundsAndSkipsBadSamples(t *testing.T) {
	p := NewPredictor(3)
	for i := 0; i < 10; i++ {
		p.Observe(Sample{Family: "english", Questions: 5, Tokens: 100, LatencyMS: 200})
	}
	if p.Len() != 3 {
		t.Errorf("Len = %d, want capped at 3", p.Len())
	}
	p.Observe(Sample{Family: "english", Questions: 0, LatencyMS: 10})
	p.Observe(Sample{Family: "english", Questions: 2, Tokens: 10, LatencyMS: -1})
	if p.Len() != 3 {
		t.Errorf("bad samples changed Len to %d", p.Len())
	}
}

func TestSummarize(t *testing.T) {
	got := Summarize([]float64{100, 200, 300, 400}, 5, 1000, 100)
	if got.N != 4 || got.MinMS != 100 || got.MaxMS != 400 || got.MeanMS != 250 {
		t.Errorf("summary = %+v", got)
	}
	if got.P50MS != 200 || got.P95MS != 400 {
		t.Errorf("percentiles = p50 %.0f p95 %.0f", got.P50MS, got.P95MS)
	}
	if got.Questions != 20 || got.MSPerQuestion != 50 {
		t.Errorf("questions/ms = %d / %.1f", got.Questions, got.MSPerQuestion)
	}
	if math.Abs(got.TokensPerSec-1100) > 0.001 {
		t.Errorf("tok/s = %.2f", got.TokensPerSec)
	}
	if math.Abs(got.QuestionsPerSec-20) > 0.001 {
		t.Errorf("q/s = %.2f", got.QuestionsPerSec)
	}
	empty := Summarize(nil, 5, 0, 0)
	if empty.N != 0 || empty.TokensPerSec != 0 || empty.MSPerQuestion != 0 {
		t.Errorf("empty summary = %+v", empty)
	}
}

func TestStore(t *testing.T) {
	s := NewStore(2)
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	s.Add(Result{At: base, Model: "english"})
	s.Add(Result{At: base.Add(time.Minute), Model: "multilingual"})
	s.Add(Result{At: base.Add(2 * time.Minute), Model: "kev"})
	if s.Len() != 2 {
		t.Fatalf("Len = %d", s.Len())
	}
	list := s.List()
	if list[0].Model != "kev" || list[1].Model != "multilingual" {
		t.Errorf("list = %+v", list)
	}

	path := filepath.Join(t.TempDir(), "bench.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	loaded, err := Load(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 2 || loaded.List()[0].Model != "kev" {
		t.Errorf("loaded = %+v", loaded.List())
	}

	empty, err := Load(filepath.Join(t.TempDir(), "missing.json"), 5)
	if err != nil || empty.Len() != 0 {
		t.Errorf("missing load = %v, %v", empty, err)
	}
}
