// Package bench estimates decide latency and summarizes benchmark runs.
//
// The server pads every question in a forward to the longest one, so latency
// follows questions x longest sequence rather than the sum of tokens. The
// default formula below is fitted to the captures in docs/SERVER-NOTES.md;
// Predictor replaces it with a rolling ridge fit once enough of the user's own
// warm calls have been observed.
package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultMax is the number of saved benchmark results kept by the TUI.
const DefaultMax = 30

// Server limits used by the default estimate.
const (
	chunkSize = 8
	// Defaults fitted to a warm Vulkan run (RX 580): about 70 ms fixed,
	// 22 ms per short question, and a second forward from the 9th question.
	baseMS        = 70.0
	perQuestionMS = 22.0
	perTokenMS    = 1.6
	tokenFree     = 32
	secondPassMS  = 200.0
)

// Sample is one observed decide call.
type Sample struct {
	Family    string
	Questions int
	Tokens    int // estimated longest sequence
	LatencyMS float64
	At        time.Time
}

// Source says where a prediction came from.
type Source int

const (
	SourceDefault Source = iota
	SourceFit
)

func (s Source) String() string {
	if s == SourceFit {
		return "fit"
	}
	return "default"
}

// Prediction is an estimated latency for one request.
type Prediction struct {
	MS      float64
	Source  Source
	Samples int // observations behind a fit
}

// DefaultMS is the built-in estimate for a request with questions sequences and
// tokens in the longest sequence.
func DefaultMS(questions, tokens int) float64 {
	if questions < 1 {
		questions = 1
	}
	if tokens < 0 {
		tokens = 0
	}
	batch := min(questions, chunkSize)
	ms := baseMS + perQuestionMS*float64(batch)
	if tokens > tokenFree {
		ms += perTokenMS * float64(tokens-tokenFree)
	}
	// ceil(questions/8)-1 extra forward passes.
	ms += secondPassMS * float64((questions-1)/chunkSize)
	return ms
}

// Predictor keeps a bounded window of recent warm calls and fits latency
// against question count and longest-sequence tokens per family.
type Predictor struct {
	max     int
	samples []Sample // oldest first
}

// NewPredictor returns a predictor that keeps at most max samples.
func NewPredictor(max int) *Predictor {
	if max <= 0 {
		max = 200
	}
	return &Predictor{max: max}
}

// Observe adds a warm sample. Cold or failed calls should not be passed in.
func (p *Predictor) Observe(s Sample) {
	if s.Questions <= 0 || s.LatencyMS <= 0 {
		return
	}
	if s.Tokens < 0 {
		s.Tokens = 0
	}
	p.samples = append(p.samples, s)
	if len(p.samples) > p.max {
		p.samples = append([]Sample(nil), p.samples[len(p.samples)-p.max:]...)
	}
}

// Len returns the number of stored samples.
func (p *Predictor) Len() int { return len(p.samples) }

// Predict estimates a request's latency in milliseconds. It uses a per-family
// fit when at least minFitSamples warm calls are available and falls back to
// the built-in formula otherwise.
func (p *Predictor) Predict(family string, questions, tokens int) Prediction {
	def := DefaultMS(questions, tokens)
	fam := p.familySamples(family)
	fit, ok := fitSamples(fam)
	n := len(fam)
	if !ok && len(fam) != len(p.samples) {
		fit, ok = fitSamples(p.samples)
		n = len(p.samples)
	}
	if ok {
		ms := fit.predict(questions, tokens)
		if ms > 0 && !math.IsNaN(ms) && ms <= 20*def {
			return Prediction{MS: ms, Source: SourceFit, Samples: n}
		}
	}
	return Prediction{MS: def, Source: SourceDefault, Samples: len(fam)}
}

func (p *Predictor) familySamples(family string) []Sample {
	if family == "" {
		return p.samples
	}
	out := make([]Sample, 0, len(p.samples))
	for _, s := range p.samples {
		if s.Family == family {
			out = append(out, s)
		}
	}
	return out
}

const minFitSamples = 8

type linearFit struct {
	a, b, c float64 // ms = a + b*questions/8 + c*tokens/128
}

func (f linearFit) predict(questions, tokens int) float64 {
	return f.a + f.b*float64(questions)/8 + f.c*float64(tokens)/128
}

// fitSamples solves a small ridge least-squares problem. The ridge term keeps
// the fit stable when the window holds few calls or little variation.
func fitSamples(samples []Sample) (linearFit, bool) {
	var (
		n                          float64
		sx1, sx2, sx11, sx12, sx22 float64
		sy, sx1y, sx2y             float64
	)
	for _, s := range samples {
		if s.Questions <= 0 || s.LatencyMS <= 0 {
			continue
		}
		x1 := float64(s.Questions) / chunkSize
		x2 := float64(s.Tokens) / 128
		y := s.LatencyMS
		n++
		sx1 += x1
		sx2 += x2
		sx11 += x1 * x1
		sx12 += x1 * x2
		sx22 += x2 * x2
		sy += y
		sx1y += x1 * y
		sx2y += x2 * y
	}
	if n < minFitSamples {
		return linearFit{}, false
	}
	const ridge = 0.5
	a := [3][3]float64{
		{n, sx1, sx2},
		{sx1, sx11 + ridge, sx12},
		{sx2, sx12, sx22 + ridge},
	}
	b := [3]float64{sy, sx1y, sx2y}
	x, ok := solve3(a, b)
	if !ok {
		return linearFit{}, false
	}
	return linearFit{a: x[0], b: x[1], c: x[2]}, true
}

func solve3(a [3][3]float64, b [3]float64) ([3]float64, bool) {
	var m [3][4]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			m[i][j] = a[i][j]
		}
		m[i][3] = b[i]
	}
	for col := 0; col < 3; col++ {
		pivot := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[pivot][col]) {
				pivot = r
			}
		}
		if math.Abs(m[pivot][col]) < 1e-12 {
			return [3]float64{}, false
		}
		m[col], m[pivot] = m[pivot], m[col]
		pv := m[col][col]
		for j := col; j < 4; j++ {
			m[col][j] /= pv
		}
		for r := 0; r < 3; r++ {
			if r == col {
				continue
			}
			f := m[r][col]
			if f == 0 {
				continue
			}
			for j := col; j < 4; j++ {
				m[r][j] -= f * m[col][j]
			}
		}
	}
	return [3]float64{m[0][3], m[1][3], m[2][3]}, true
}

// Stats summarizes a set of warm runs of the same request.
type Stats struct {
	N               int     `json:"n"`
	MinMS           float64 `json:"min_ms"`
	MaxMS           float64 `json:"max_ms"`
	MeanMS          float64 `json:"mean_ms"`
	P50MS           float64 `json:"p50_ms"`
	P95MS           float64 `json:"p95_ms"`
	TotalMS         float64 `json:"total_ms"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	Questions       int     `json:"questions"` // total questions sent
	TokensPerSec    float64 `json:"tokens_per_sec"`
	QuestionsPerSec float64 `json:"questions_per_sec"`
	MSPerQuestion   float64 `json:"ms_per_question"`
}

// Summarize aggregates warm latencies. perRunQuestions is the number of
// questions in the request; input and output tokens are totals for every run.
func Summarize(latencies []float64, perRunQuestions, inputTokens, outputTokens int) Stats {
	s := Stats{N: len(latencies), InputTokens: inputTokens, OutputTokens: outputTokens}
	if perRunQuestions > 0 {
		s.Questions = perRunQuestions * len(latencies)
	}
	if len(latencies) == 0 {
		return s
	}
	sorted := append([]float64(nil), latencies...)
	sort.Float64s(sorted)
	s.MinMS, s.MaxMS = sorted[0], sorted[len(sorted)-1]
	s.P50MS = percentile(sorted, 50)
	s.P95MS = percentile(sorted, 95)
	for _, ms := range sorted {
		s.TotalMS += ms
	}
	s.MeanMS = s.TotalMS / float64(len(sorted))
	if perRunQuestions > 0 {
		s.MSPerQuestion = s.MeanMS / float64(perRunQuestions)
	}
	seconds := s.TotalMS / 1000
	if seconds > 0 {
		s.TokensPerSec = float64(inputTokens+outputTokens) / seconds
		s.QuestionsPerSec = float64(s.Questions) / seconds
	}
	return s
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

// Result is one saved benchmark.
type Result struct {
	At        time.Time `json:"at"`
	Preset    string    `json:"preset,omitempty"`
	Model     string    `json:"model,omitempty"`  // pinned family; empty means auto
	Family    string    `json:"family,omitempty"` // family reported by the server
	Route     string    `json:"route,omitempty"`
	Device    string    `json:"device,omitempty"`
	Server    string    `json:"server,omitempty"`
	Warmup    int       `json:"warmup"`
	Runs      int       `json:"runs"`
	Questions int       `json:"questions"`
	Tokens    int       `json:"tokens,omitempty"` // estimated longest sequence
	Stats     Stats     `json:"stats"`
}

// Store is a bounded list of saved benchmark results, oldest first.
type Store struct {
	max     int
	results []Result
}

// NewStore returns a store that keeps at most max results.
func NewStore(max int) *Store {
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{max: max}
}

// Add stores r, evicting the oldest results once the store is full.
func (s *Store) Add(r Result) Result {
	s.results = append(s.results, r)
	if len(s.results) > s.max {
		s.results = append([]Result(nil), s.results[len(s.results)-s.max:]...)
	}
	return r
}

// Len returns the number of saved results.
func (s *Store) Len() int { return len(s.results) }

// List returns copies of the results, newest first.
func (s *Store) List() []Result {
	out := make([]Result, len(s.results))
	for i := range s.results {
		out[len(s.results)-1-i] = s.results[i]
	}
	return out
}

// DefaultPath is where the TUI persists benchmark results.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "playa", "bench.json"), nil
}

type file struct {
	Version int      `json:"version"`
	Results []Result `json:"results"`
}

// Save writes the results to path atomically, creating the directory.
func (s *Store) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(file{Version: 1, Results: s.results}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, ".bench-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Load reads benchmark results from path. A missing file is an empty store.
func Load(path string, max int) (*Store, error) {
	s := NewStore(max)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return s, fmt.Errorf("decode %s: %w", path, err)
	}
	for _, r := range f.Results {
		s.Add(r)
	}
	return s, nil
}
