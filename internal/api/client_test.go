package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeServer serves fixtures for the endpoints the client uses and records
// what the last decide request looked like.
type fakeServer struct {
	*httptest.Server
	lastPath   string
	lastAuth   string
	lastBody   []byte
	decideFile string // fixture returned by decide
	decideCode int
}

func newFake(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{decideFile: "score.json", decideCode: 200}
	mux := http.NewServeMux()
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.lastAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(fixture(t, name))
		}
	}
	mux.HandleFunc("/health", serve("health.json"))
	mux.HandleFunc("/v1/models", serve("models.json"))
	mux.HandleFunc("/v1/presets", serve("presets.json"))
	decide := func(w http.ResponseWriter, r *http.Request) {
		f.lastPath = r.URL.Path
		f.lastAuth = r.Header.Get("Authorization")
		f.lastBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.decideCode)
		_, _ = w.Write(fixture(t, f.decideFile))
	}
	mux.HandleFunc("/v1/systemone", decide)
	mux.HandleFunc("/v1/decide", decide)
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestHealth(t *testing.T) {
	f := newFake(t)
	h, err := NewClient(f.URL).Health(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || h.Device != "vulkan:0" || h.Family != "auto" {
		t.Errorf("unexpected health: %+v", h)
	}
	want := []string{"english", "kev-0.8b", "multilingual", "typed-decisions"}
	if !reflect.DeepEqual(h.Families, want) {
		t.Errorf("families = %v, want %v", h.Families, want)
	}
}

func TestModels(t *testing.T) {
	f := newFake(t)
	ms, err := NewClient(f.URL).Models(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 9 || ms[0].Name != "english" {
		t.Errorf("got %d models, first %q", len(ms), ms[0].Name)
	}
}

func TestPresetsKeepQuestionOrder(t *testing.T) {
	f := newFake(t)
	ps, err := NewClient(f.URL).Presets(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 10 {
		t.Fatalf("got %d presets, want 10", len(ps))
	}
	email := ps[0]
	if email.Name != "email" || email.StateKey != "body" {
		t.Fatalf("unexpected first preset: %s / %s", email.Name, email.StateKey)
	}
	want := []string{"category", "is_spam", "is_phishing", "urgency", "needs_reply", "churn_risk", "refund_requested"}
	if got := email.Questions.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("email question order = %v, want %v", got, want)
	}
	if !strings.Contains(email.StateText(), "billed twice") {
		t.Errorf("StateText() = %q", email.StateText())
	}
}

func TestCriteriaShapes(t *testing.T) {
	f := newFake(t)
	ps, _ := NewClient(f.URL).Presets(ctx(t))
	byName := map[string]Preset{}
	for _, p := range ps {
		byName[p.Name] = p
	}
	find := func(preset, q string) Question {
		for _, nq := range byName[preset].Questions {
			if nq.Name == q {
				return nq.Question
			}
		}
		t.Fatalf("%s/%s not found", preset, q)
		return Question{}
	}

	// choice with descriptions, in order
	opts := find("email", "category").Options()
	if len(opts) != 6 || opts[0].Key != "billing" || opts[5].Key != "other" {
		t.Errorf("category options = %+v", opts)
	}
	if opts[0].Description == "" {
		t.Error("billing description should not be empty")
	}
	// choice with null descriptions
	for _, o := range find("guard", "topic").Options() {
		if o.Description != "" {
			t.Errorf("guard topic %q should have a null description, got %q", o.Key, o.Description)
		}
	}
	// noul with true/false criteria; "false" is listed first in this preset
	nopts := find("email", "is_phishing").Options()
	if len(nopts) != 2 || nopts[0].Key != "false" || nopts[1].Key != "true" {
		t.Errorf("is_phishing options = %+v", nopts)
	}
	// score is an ordered array
	levels := find("email", "urgency").Levels()
	want := []string{"no time pressure", "needs attention soon", "blocking issue or hard deadline"}
	if !reflect.DeepEqual(levels, want) {
		t.Errorf("urgency levels = %v", levels)
	}
	if find("email", "urgency").Options() != nil {
		t.Error("score question should have no Options()")
	}
}

func TestQuestionSetMarshalKeepsOrder(t *testing.T) {
	f := newFake(t)
	ps, _ := NewClient(f.URL).Presets(ctx(t))
	b, err := json.Marshal(ps[0].Request())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	last := -1
	for _, name := range []string{`"category"`, `"is_spam"`, `"is_phishing"`, `"urgency"`, `"needs_reply"`, `"churn_risk"`, `"refund_requested"`} {
		i := strings.Index(s, name)
		if i < 0 || i < last {
			t.Fatalf("%s out of order in %s", name, s)
		}
		last = i
	}
	// criteria must round-trip with its shape intact
	if !strings.Contains(s, `"criteria":["no time pressure"`) {
		t.Errorf("score criteria array not preserved: %s", s)
	}
}

func TestDecideScore(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.URL)
	resp, err := c.Decide(ctx(t), DecideRequest{
		State: map[string]any{"document": "I was charged twice. Please fix this ASAP."},
		Questions: QuestionSet{{Name: "urgency", Question: Question{
			Type: TypeScore, Instructions: "How urgent is this?",
			Criteria: json.RawMessage(`["can wait","this week","today"]`),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.lastPath != EndpointSystemOne {
		t.Errorf("default endpoint = %s, want %s", f.lastPath, EndpointSystemOne)
	}
	a := resp.Answers["urgency"]
	if a.Type != TypeScore || a.Score == nil || *a.Score < 1.88 || *a.Score > 1.89 {
		t.Errorf("score answer = %+v", a)
	}
	if a.Legend["2"] != "today" || a.Probabilities["2"] < 0.91 {
		t.Errorf("legend/probabilities wrong: %+v", a)
	}
	if resp.Family != "english" || resp.Route != "english words 5/8" {
		t.Errorf("family/route = %s / %s", resp.Family, resp.Route)
	}
	if resp.Usage.InputTokens != 45 || resp.Usage.LatencyMs < 297 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if resp.Wall <= 0 || len(resp.Raw) == 0 {
		t.Error("Wall and Raw should be filled in by the client")
	}
	if !strings.Contains(resp.PrettyRaw(), "\n  \"answers\"") {
		t.Errorf("PrettyRaw not indented: %q", resp.PrettyRaw())
	}
	if !strings.HasPrefix(string(f.lastBody), `{"state":`) {
		t.Errorf("request body = %s", f.lastBody)
	}
}

func TestDecideNoulAndChoiceFixtures(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.URL)

	f.decideFile = "noul.json"
	resp, err := c.Decide(ctx(t), DecideRequest{State: "x", Questions: QuestionSet{{Name: "billing", Question: Question{Type: TypeNoul, Instructions: "?"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a := resp.Answers["billing"]; a.Noul == nil || *a.Noul != 0.9196 || a.Confidence != 0.9196 {
		t.Errorf("noul answer = %+v", a)
	}

	f.decideFile = "choice.json"
	resp, err = c.Decide(ctx(t), DecideRequest{State: "x", Questions: QuestionSet{{Name: "tone", Question: Question{Type: TypeChoice, Instructions: "?"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if a := resp.Answers["tone"]; a.Choice != "angry" || a.Probabilities["calm"] != 0.0331 {
		t.Errorf("choice answer = %+v", a)
	}
}

func TestAlternateEndpoint(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.URL)
	c.Endpoint = EndpointDecide
	if _, err := c.Warmup(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if f.lastPath != EndpointDecide {
		t.Errorf("path = %s", f.lastPath)
	}
}

func TestAPIError(t *testing.T) {
	f := newFake(t)
	f.decideFile, f.decideCode = "error_score.json", 400
	_, err := NewClient(f.URL).Decide(ctx(t), DecideRequest{State: "x"})
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *APIError, got %T: %v", err, err)
	}
	if ae.Status != 400 || ae.Code != "invalid_request_error" || ae.Param != "questions.urgency.criteria" {
		t.Errorf("APIError = %+v", ae)
	}
	if !strings.Contains(ae.Error(), "nonempty criteria list") || !strings.Contains(ae.Error(), "questions.urgency.criteria") {
		t.Errorf("Error() = %q", ae.Error())
	}
}

func TestNonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL).Health(ctx(t))
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 502 || !strings.Contains(ae.Message, "upstream exploded") {
		t.Errorf("err = %v", err)
	}
}

func TestBearerAuth(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.URL)
	if _, err := c.Health(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if f.lastAuth != "" {
		t.Errorf("no key set, but Authorization = %q", f.lastAuth)
	}
	c.APIKey = "secret"
	if _, err := c.Health(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if f.lastAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", f.lastAuth)
	}
}

func TestContextCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	c, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewClient(srv.URL).Health(c)
	if err == nil {
		t.Fatal("expected an error from a cancelled request")
	}
	if time.Since(start) > 2*time.Second {
		t.Error("cancel did not abort the request promptly")
	}
}
