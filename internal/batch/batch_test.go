package batch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"playa/internal/api"
)

func ptr(v float64) *float64 { return &v }

func TestLoadJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.jsonl")
	body := "\"plain text\"\n{\"text\":\"object text\",\"locale\":\"en\"}\n\n# comment\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := Load(path, "text")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].State != "plain text" || rows[0].Index != 1 {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if m, ok := rows[1].State.(map[string]any); !ok || m["text"] != "object text" || m["locale"] != "en" {
		t.Errorf("row 1 state = %#v", rows[1].State)
	}
	if rows[1].Label != "object text" {
		t.Errorf("label = %q", rows[1].Label)
	}
}

func TestLoadJSONLRejectsUnsupportedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(path, []byte("42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, ""); err == nil || !strings.Contains(err.Error(), "want a string or object") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadJSONLRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(path, []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, ""); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.csv")
	body := "text,locale,tier\nhello,en,gold\n,fr,silver\nworld,en,gold\n,,\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := Load(path, "text")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (the all-empty row is skipped)", len(rows))
	}
	m := rows[0].State.(map[string]any)
	if m["text"] != "hello" || m["locale"] != "en" || m["tier"] != "gold" {
		t.Errorf("state = %#v", m)
	}
	if rows[0].Label != "hello" {
		t.Errorf("label = %q", rows[0].Label)
	}
}

func TestLoadCSVRequiresStateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "states.csv")
	if err := os.WriteFile(path, []byte("body,locale\nhello,en\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, "text"); err == nil || !strings.Contains(err.Error(), `no "text" column`) {
		t.Fatalf("err = %v", err)
	}
}

func TestChunksAndRequests(t *testing.T) {
	var qs api.QuestionSet
	for i := 0; i < 10; i++ {
		qs = append(qs, api.NamedQuestion{Name: string(rune('a' + i)), Question: api.Question{Type: api.TypeNoul, Instructions: "?"}})
	}
	chunks := Chunks(qs, 0)
	if len(chunks) != 2 || len(chunks[0]) != 8 || len(chunks[1]) != 2 {
		t.Fatalf("chunks = %v", []int{len(chunks[0]), len(chunks[1])})
	}
	reqs := Requests("state", "english", chunks)
	if len(reqs) != 2 || reqs[0].Model != "english" || reqs[0].State != "state" || len(reqs[0].Questions) != 8 {
		t.Errorf("requests = %+v", reqs)
	}
	if auto := Requests("x", "auto", chunks); auto[0].Model != "" {
		t.Errorf("auto model leaked: %q", auto[0].Model)
	}
}

func response(family string, input int, latency float64, answers map[string]api.Answer) *api.DecideResponse {
	return &api.DecideResponse{
		Family:  family,
		Route:   "route",
		Answers: answers,
		Usage:   api.Usage{InputTokens: input, OutputTokens: 2, LatencyMs: latency},
		Raw:     []byte(`{"answers":{}}`),
		Wall:    5 * time.Millisecond,
	}
}

func TestRowHelpers(t *testing.T) {
	qs := api.QuestionSet{
		{Name: "yes", Question: api.Question{Type: api.TypeNoul}},
		{Name: "topic", Question: api.Question{Type: api.TypeChoice}},
	}
	rows := []Row{{
		State: "x",
		Calls: []Call{
			{Chunk: 0, Questions: qs[:1], Response: response("english", 10, 5, map[string]api.Answer{
				"yes": {Type: api.TypeNoul, Noul: ptr(0.9)},
			})},
			{Chunk: 1, Questions: qs[1:], Response: response("english", 20, 7, map[string]api.Answer{
				"topic": {Type: api.TypeChoice, Choice: "a", Probabilities: map[string]float64{"a": 1, "b": 0}},
			})},
		},
	}}
	r := rows[0]
	if !r.Complete(2) || r.Failed() || r.Truncated() {
		t.Errorf("row flags: complete=%v failed=%v truncated=%v", r.Complete(2), r.Failed(), r.Truncated())
	}
	if got := len(r.Answers()); got != 2 {
		t.Errorf("answers = %d", got)
	}
	if in, out, ms := r.Usage(); in != 30 || out != 4 || ms != 12 {
		t.Errorf("usage = %d/%d/%.0f", in, out, ms)
	}
	if r.Wall() != 10*time.Millisecond {
		t.Errorf("wall = %v", r.Wall())
	}

	broken := r
	broken.Calls = append([]Call(nil), r.Calls...)
	broken.Calls[0].Err = "boom"
	if broken.Complete(2) || !broken.Failed() {
		t.Error("failed row should be incomplete")
	}
}

func TestSummarizeAndFilters(t *testing.T) {
	qs := api.QuestionSet{
		{Name: "yes", Question: api.Question{Type: api.TypeNoul}},
		{Name: "topic", Question: api.Question{Type: api.TypeChoice}},
	}
	ok := Row{Index: 1, State: "one", Calls: []Call{{Chunk: 0, Questions: qs, Response: response("english", 100, 10, map[string]api.Answer{
		"yes":   {Type: api.TypeNoul, Noul: ptr(0.9)},
		"topic": {Type: api.TypeChoice, Choice: "billing", Probabilities: map[string]float64{"billing": 0.4, "sales": 0.35, "other": 0.25}},
	})}}}
	truncated := Row{Index: 2, State: "two", Calls: []Call{{Chunk: 0, Questions: qs, Response: response("english", 1024, 30, map[string]api.Answer{
		"yes": {Type: api.TypeNoul, Noul: ptr(0.2)},
	})}}}
	failed := Row{Index: 3, State: "three", Calls: []Call{{Chunk: 0, Questions: qs, Err: "boom"}}}
	rows := []Row{ok, truncated, failed}

	s := Summarize(rows, 1, 0.5)
	if s.Rows != 3 || s.Complete != 2 || s.Failed != 1 || s.Calls != 2 {
		t.Errorf("summary counts = %+v", s)
	}
	if s.NoulTotal != 2 || s.NoulPositive != 1 {
		t.Errorf("noul = %d/%d", s.NoulPositive, s.NoulTotal)
	}
	if s.Choices["billing"] != 1 {
		t.Errorf("choices = %v", s.Choices)
	}
	if s.Truncated != 1 {
		t.Errorf("truncated = %d", s.Truncated)
	}
	if s.P50 == 0 || s.P95 == 0 || s.TokensPerSec == 0 {
		t.Errorf("timing = p50 %.0f p95 %.0f tok/s %.0f", s.P50, s.P95, s.TokensPerSec)
	}

	if !Match(ok, FilterAll) || !Match(ok, FilterContested) || Match(ok, FilterSaturated) {
		t.Error("contested matching is wrong")
	}
	if !Match(truncated, FilterTruncated) || !Match(failed, FilterErrored) {
		t.Error("truncated/error matching is wrong")
	}
	if Match(ok, FilterErrored) {
		t.Error("an okay row must not match errors")
	}
}

func TestExport(t *testing.T) {
	qs := api.QuestionSet{
		{Name: "yes", Question: api.Question{Type: api.TypeNoul, Instructions: "Yes?"}},
		{Name: "topic", Question: api.Question{Type: api.TypeChoice, Instructions: "Topic?"}},
	}
	rows := []Row{
		{Index: 1, State: "hello", Label: "hello", Calls: []Call{{Chunk: 0, Questions: qs, Response: response("english", 100, 10, map[string]api.Answer{
			"yes":   {Type: api.TypeNoul, Noul: ptr(0.9)},
			"topic": {Type: api.TypeChoice, Choice: "billing", Probabilities: map[string]float64{"billing": 1, "sales": 0}},
		})}}},
		{Index: 2, State: map[string]any{"text": "world"}, Calls: []Call{{Chunk: 0, Questions: qs, Err: "boom", Param: "questions.yes"}}},
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "states.jsonl")
	jsonlPath, csvPath := ExportPaths(input)
	if jsonlPath != filepath.Join(dir, "states.results.jsonl") || csvPath != filepath.Join(dir, "states.results.csv") {
		t.Fatalf("export paths = %s, %s", jsonlPath, csvPath)
	}
	if err := WriteJSONL(jsonlPath, rows); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d", len(lines))
	}
	var first exportRow
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Calls[0].Family != "english" || string(first.Calls[0].Response) != `{"answers":{}}` {
		t.Errorf("full response missing: %+v", first.Calls[0])
	}

	if err := WriteCSV(csvPath, rows, qs); err != nil {
		t.Fatal(err)
	}
	csvRaw, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(csvRaw)
	for _, want := range []string{"id,state,", "yes_answer", "topic_prob", "family,route", "YES,0.9000", "billing,1.0000"} {
		if !strings.Contains(text, want) {
			t.Errorf("csv missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "boom") {
		t.Errorf("csv error missing:\n%s", text)
	}
}
