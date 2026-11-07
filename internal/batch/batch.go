// Package batch runs a file of states through one question set.
//
// The server handles at most eight questions per request, so a larger set is
// split into chunks and the chunks are sent one after another for each state.
// The package owns the file formats, the chunk plan, the result summary and the
// exports; the TUI only drives it.
package batch

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"playa/internal/api"
	"playa/internal/lengthguard"
	"playa/internal/lens"
)

// DefaultChunkSize is the server's max_batch. A ninth question costs an extra
// forward pass, so chunks are kept at or below eight.
const DefaultChunkSize = 8

// Row is one input state and the calls made for it.
type Row struct {
	Index int    `json:"index"`
	State any    `json:"state"`
	Label string `json:"label,omitempty"`
	Calls []Call `json:"calls,omitempty"`
}

// Call is one decide request for a chunk of the question set.
type Call struct {
	Chunk     int             `json:"chunk"`
	Questions api.QuestionSet `json:"questions"`
	Response  *api.DecideResponse
	Err       string        `json:"error,omitempty"`
	Param     string        `json:"param,omitempty"`
	At        time.Time     `json:"at,omitempty"`
	Cold      bool          `json:"cold,omitempty"`
	Wall      time.Duration `json:"-"`
}

// Load reads a JSONL or CSV file. The format comes from the extension.
// stateKey names the CSV column that becomes the main state field.
func Load(path, stateKey string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl", ".ndjson":
		return loadJSONL(f)
	case ".csv":
		return loadCSV(f, stateKey)
	default:
		return nil, fmt.Errorf("unknown batch file type %q: use .jsonl, .ndjson or .csv", filepath.Ext(path))
	}
}

func loadJSONL(r io.Reader) ([]Row, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	var rows []Row
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var v any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("line %d: trailing data", line)
		}
		switch s := v.(type) {
		case string:
			rows = append(rows, Row{Index: len(rows) + 1, State: s, Label: labelFor(s, "")})
		case map[string]any:
			rows = append(rows, Row{Index: len(rows) + 1, State: s, Label: labelFor(s, "")})
		default:
			return nil, fmt.Errorf("line %d: want a string or object, got %T", line, v)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("file has no states")
	}
	return rows, nil
}

func loadCSV(r io.Reader, stateKey string) ([]Row, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("empty CSV")
	}
	header := records[0]
	if len(header) == 0 {
		return nil, errors.New("CSV has no columns")
	}
	seen := map[string]bool{}
	for _, h := range header {
		if h == "" {
			return nil, errors.New("CSV has an empty column name")
		}
		if seen[h] {
			return nil, fmt.Errorf("CSV has duplicate column %q", h)
		}
		seen[h] = true
	}
	keyCol := 0
	if stateKey != "" {
		keyCol = -1
		for i, h := range header {
			if h == stateKey {
				keyCol = i
				break
			}
		}
		if keyCol < 0 {
			return nil, fmt.Errorf("CSV has no %q column for the state text", stateKey)
		}
	}
	labelKey := stateKey
	if labelKey == "" {
		labelKey = header[keyCol]
	}
	var rows []Row
	for _, rec := range records[1:] {
		state := make(map[string]any, len(header))
		empty := true
		for i, h := range header {
			v := ""
			if i < len(rec) {
				v = rec[i]
			}
			state[h] = v
			if v != "" {
				empty = false
			}
		}
		if empty {
			continue
		}
		rows = append(rows, Row{Index: len(rows) + 1, State: state, Label: labelFor(state, labelKey)})
	}
	if len(rows) == 0 {
		return nil, errors.New("file has no states")
	}
	return rows, nil
}

func labelFor(state any, stateKey string) string {
	if s, ok := state.(string); ok {
		return oneLine(s)
	}
	if m, ok := state.(map[string]any); ok {
		if stateKey != "" {
			if s, ok := m[stateKey].(string); ok && s != "" {
				return oneLine(s)
			}
		}
		// Fall back to a stable compact rendering, preferring a text-looking field.
		for _, key := range []string{"text", "prompt", "body", "input", "input_keys"} {
			if s, ok := m[key].(string); ok && s != "" {
				return oneLine(s)
			}
		}
	}
	b, err := json.Marshal(state)
	if err != nil {
		return oneLine(fmt.Sprint(state))
	}
	return oneLine(string(b))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > 80 {
		r := []rune(s)
		return string(r[:79]) + "…"
	}
	return s
}

// Chunks splits a question set into the requests the server will accept.
func Chunks(qs api.QuestionSet, size int) []api.QuestionSet {
	if size <= 0 {
		size = DefaultChunkSize
	}
	if len(qs) == 0 {
		return nil
	}
	var out []api.QuestionSet
	for i := 0; i < len(qs); i += size {
		end := i + size
		if end > len(qs) {
			end = len(qs)
		}
		out = append(out, cloneQuestions(qs[i:end]))
	}
	return out
}

func cloneQuestions(qs api.QuestionSet) api.QuestionSet {
	out := make(api.QuestionSet, len(qs))
	for i, nq := range qs {
		out[i] = api.NamedQuestion{
			Name: nq.Name,
			Question: api.Question{
				Type:         nq.Type,
				Instructions: nq.Instructions,
				Criteria:     append(json.RawMessage(nil), nq.Criteria...),
			},
		}
	}
	return out
}

// Requests builds one decide request per chunk for state.
func Requests(state any, model string, chunks []api.QuestionSet) []api.DecideRequest {
	out := make([]api.DecideRequest, len(chunks))
	for i, qs := range chunks {
		out[i] = api.DecideRequest{State: state, Questions: qs}
		if model != "" && model != "auto" {
			out[i].Model = model
		}
	}
	return out
}

// Answers merges the answers of every successful call.
func (r Row) Answers() map[string]api.Answer {
	out := map[string]api.Answer{}
	for _, c := range r.Calls {
		if c.Response == nil {
			continue
		}
		for k, v := range c.Response.Answers {
			out[k] = v
		}
	}
	return out
}

// Failed reports whether any call failed.
func (r Row) Failed() bool {
	for _, c := range r.Calls {
		if c.Err != "" {
			return true
		}
	}
	return false
}

// Complete reports whether every expected chunk succeeded.
func (r Row) Complete(chunks int) bool {
	if chunks <= 0 || len(r.Calls) != chunks {
		return false
	}
	for _, c := range r.Calls {
		if c.Err != "" || c.Response == nil {
			return false
		}
	}
	return true
}

// Truncated reports whether any call shows a silently cut state.
func (r Row) Truncated() bool {
	for _, c := range r.Calls {
		if c.Response == nil {
			continue
		}
		if lengthguard.Truncated(c.Response.Usage.InputTokens, len(c.Questions), lengthguard.Cap(c.Response.Family)) {
			return true
		}
	}
	return false
}

// Usage sums tokens and server latency across successful calls.
func (r Row) Usage() (input, output int, latencyMs float64) {
	for _, c := range r.Calls {
		if c.Response == nil {
			continue
		}
		input += c.Response.Usage.InputTokens
		output += c.Response.Usage.OutputTokens
		latencyMs += c.Response.Usage.LatencyMs
	}
	return input, output, latencyMs
}

// Wall sums client wall time across successful calls.
func (r Row) Wall() time.Duration {
	var d time.Duration
	for _, c := range r.Calls {
		if c.Response != nil {
			d += c.Response.Wall
		}
	}
	return d
}

// Summary is the aggregate strip shown under the table.
type Summary struct {
	Rows, Complete, Failed  int
	Calls                   int
	Truncated               int
	Choices                 map[string]int
	NoulPositive, NoulTotal int
	P50, P95                float64
	TokensPerSec            float64
	RowsPerSec              float64
	InputTokens             int
	OutputTokens            int
}

// Summarize aggregates the finished calls. chunks is the number of calls a
// complete row should have. threshold is the noul positive cutoff.
func Summarize(rows []Row, chunks int, threshold float64) Summary {
	if threshold <= 0 || threshold >= 1 {
		threshold = 0.5
	}
	s := Summary{Rows: len(rows), Choices: map[string]int{}}
	var latencies []float64
	var totalLatency float64
	for _, r := range rows {
		if r.Failed() {
			s.Failed++
		}
		if r.Complete(chunks) {
			s.Complete++
		}
		for _, c := range r.Calls {
			if c.Response == nil || c.Err != "" {
				continue
			}
			s.Calls++
			ms := c.Response.Usage.LatencyMs
			latencies = append(latencies, ms)
			totalLatency += ms
			s.InputTokens += c.Response.Usage.InputTokens
			s.OutputTokens += c.Response.Usage.OutputTokens
			if lengthguard.Truncated(c.Response.Usage.InputTokens, len(c.Questions), lengthguard.Cap(c.Response.Family)) {
				s.Truncated++
			}
			for _, a := range c.Response.Answers {
				switch a.Type {
				case api.TypeNoul:
					s.NoulTotal++
					if a.Noul != nil && *a.Noul >= threshold {
						s.NoulPositive++
					}
				case api.TypeChoice:
					sum := lens.Summarize("", a, lens.Options{})
					s.Choices[sum.Label]++
				}
			}
		}
	}
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		s.P50 = percentile(latencies, 50)
		s.P95 = percentile(latencies, 95)
	}
	if totalLatency > 0 {
		tokens := float64(s.InputTokens + s.OutputTokens)
		s.TokensPerSec = tokens / (totalLatency / 1000)
		s.RowsPerSec = float64(s.Complete) / (totalLatency / 1000)
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

// Filter limits the table to rows with a useful property.
type Filter int

const (
	FilterAll Filter = iota
	FilterContested
	FilterSaturated
	FilterTruncated
	FilterErrored
	filterCount
)

func (f Filter) String() string {
	switch f {
	case FilterContested:
		return "contested"
	case FilterSaturated:
		return "saturated"
	case FilterTruncated:
		return "truncated"
	case FilterErrored:
		return "errors"
	}
	return "all"
}

// Next returns the following filter, wrapping around.
func (f Filter) Next() Filter { return (f + 1) % filterCount }

// Match reports whether row passes f.
func Match(r Row, f Filter) bool {
	if f == FilterAll {
		return true
	}
	if f == FilterErrored {
		return r.Failed()
	}
	if f == FilterTruncated {
		return r.Truncated()
	}
	for _, a := range r.Answers() {
		s := lens.Summarize("", a, lens.Options{})
		switch f {
		case FilterContested:
			if s.Contested {
				return true
			}
		case FilterSaturated:
			if s.Saturated {
				return true
			}
		}
	}
	return false
}

// ExportPaths returns the two export paths next to the input file.
func ExportPaths(input string) (jsonl, csv string) {
	ext := filepath.Ext(input)
	base := strings.TrimSuffix(input, ext)
	if base == "" {
		base = "batch"
	}
	return base + ".results.jsonl", base + ".results.csv"
}

type exportRow struct {
	Index int          `json:"index"`
	Label string       `json:"label,omitempty"`
	State any          `json:"state"`
	Calls []exportCall `json:"calls"`
}

type exportCall struct {
	Chunk     int             `json:"chunk"`
	Questions api.QuestionSet `json:"questions"`
	Error     string          `json:"error,omitempty"`
	Param     string          `json:"param,omitempty"`
	Family    string          `json:"family,omitempty"`
	Route     string          `json:"route,omitempty"`
	Usage     *api.Usage      `json:"usage,omitempty"`
	WallMs    float64         `json:"wall_ms,omitempty"`
	Cold      bool            `json:"cold,omitempty"`
	Response  json.RawMessage `json:"response,omitempty"`
}

// WriteJSONL writes the full responses, one JSON object per input state.
func WriteJSONL(path string, rows []Row) error {
	var b strings.Builder
	for _, r := range rows {
		out := exportRow{Index: r.Index, Label: r.Label, State: r.State}
		for _, c := range r.Calls {
			ec := exportCall{
				Chunk:     c.Chunk,
				Questions: c.Questions,
				Error:     c.Err,
				Param:     c.Param,
				Cold:      c.Cold,
			}
			if c.Response != nil {
				ec.Family = c.Response.Family
				ec.Route = c.Response.Route
				ec.Usage = &c.Response.Usage
				ec.WallMs = float64(c.Response.Wall) / float64(time.Millisecond)
				raw := c.Response.Raw
				if len(raw) == 0 || !json.Valid(raw) {
					raw, _ = json.Marshal(c.Response)
				}
				ec.Response = json.RawMessage(raw)
			}
			out.Calls = append(out.Calls, ec)
		}
		line, err := json.Marshal(out)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// WriteCSV writes the flat table: one answer, probability and certainty per
// question, plus family, route and cost columns.
func WriteCSV(path string, rows []Row, questions api.QuestionSet) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{"id", "state"}
	for _, q := range questions {
		header = append(header, q.Name+"_answer", q.Name+"_prob", q.Name+"_certainty")
	}
	header = append(header, "family", "route", "wall_ms", "latency_ms", "input_tokens", "output_tokens", "error")
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range rows {
		rec := []string{strconv.Itoa(r.Index), stateText(r.State)}
		answers := r.Answers()
		for _, q := range questions {
			a, ok := answers[q.Name]
			if !ok {
				rec = append(rec, "", "", "")
				continue
			}
			s := lens.Summarize(q.Name, a, lens.Options{})
			rec = append(rec, s.Label,
				strconv.FormatFloat(s.Prob, 'f', 4, 64),
				strconv.FormatFloat(s.Certainty, 'f', 4, 64))
		}
		family, route := "", ""
		for _, c := range r.Calls {
			if c.Response != nil {
				if family == "" {
					family = c.Response.Family
				}
				if route == "" {
					route = c.Response.Route
				}
			}
		}
		input, output, latency := r.Usage()
		var errs []string
		for _, c := range r.Calls {
			if c.Err != "" {
				errs = append(errs, c.Err)
			}
		}
		rec = append(rec,
			family, route,
			strconv.FormatFloat(float64(r.Wall())/float64(time.Millisecond), 'f', 0, 64),
			strconv.FormatFloat(latency, 'f', 0, 64),
			strconv.Itoa(input), strconv.Itoa(output),
			strings.Join(errs, "; "))
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return w.Error()
}

func stateText(state any) string {
	if s, ok := state.(string); ok {
		return s
	}
	b, err := json.Marshal(state)
	if err != nil {
		return fmt.Sprint(state)
	}
	return string(b)
}
