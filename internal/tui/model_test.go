package tui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// fake is a laya server backed by the captured fixtures for everything except
// decide, whose body each test sets. The decide bodies below are SYNTHETIC:
// shaped like real responses, but the numbers are made up for the test.
type fake struct {
	*httptest.Server
	decideBody string
	decideCode int
	lastBody   []byte
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{decideCode: 200}
	file := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join("..", "api", "testdata", name))
			if err != nil {
				t.Error(err)
			}
			_, _ = w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", file("health.json"))
	mux.HandleFunc("/v1/presets", file("presets.json"))
	mux.HandleFunc("/v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		f.lastBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(f.decideCode)
		_, _ = w.Write([]byte(f.decideBody))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// clock returns a time source that advances one second per call.
func clock() func() time.Time {
	t := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	return func() time.Time { t = t.Add(time.Second); return t }
}

func newModel(f *fake) Model {
	return New(api.NewClient(f.URL), Options{Warmup: true, Timeout: 5 * time.Second, Now: clock()})
}

// step runs a command and feeds its message back into the model.
func step(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "ctrl+e":
		return tea.KeyMsg{Type: tea.KeyCtrlE}
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	case "ctrl+q":
		return tea.KeyMsg{Type: tea.KeyCtrlQ}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m Model, s string) (Model, tea.Cmd) {
	next, cmd := m.Update(key(s))
	return next.(Model), cmd
}

// connected returns a model that has loaded health, presets and warmed up.
func connected(t *testing.T, f *fake) Model {
	t.Helper()
	f.decideBody = warmupBody
	m := newModel(f)
	m = step(t, m, m.healthCmd())
	if m.status != statusWarming {
		t.Fatalf("status after health = %v, want warming", m.status)
	}
	m = step(t, m, m.warmupCmd())
	m = step(t, m, m.presetsCmd())
	return m
}

const warmupBody = `{"model":"laya","family":"english","route":"forced english",
 "answers":{"warmup":{"type":"noul","action":{"act_probability":1},"confidence":0.9,"noul":0.9}},
 "usage":{"input_tokens":30,"output_tokens":0,"latency_ms":650}}`

const guardBody = `{"model":"laya","family":"english","route":"english words 18/20",
 "answers":{
  "jailbreak":{"type":"noul","confidence":1,"noul":1},
  "prompt_injection":{"type":"noul","confidence":1,"noul":1},
  "sensitive_data":{"type":"noul","confidence":0.8202,"noul":0.8202},
  "harm_severity":{"type":"score","confidence":0.29,"score":2.34,
    "legend":{"0":"none: ordinary request","1":"minor: mildly inappropriate","2":"serious: unsafe advice or abuse","3":"severe: dangerous or illegal"},
    "probabilities":{"0":0.02,"1":0.10,"2":0.50,"3":0.38}},
  "topic":{"type":"choice","confidence":0.75,"choice":"security_testing",
    "probabilities":{"product_support":0.02,"coding":0.03,"general_knowledge":0.05,"personal_advice":0.02,"security_testing":0.85,"other":0.03}}},
 "usage":{"input_tokens":358,"output_tokens":0,"latency_ms":270}}`

const harnessBody = `{"model":"laya","family":"english","route":"english words 30/40",
 "answers":{
  "next":{"type":"choice","confidence":0.0787,"choice":"tool",
    "probabilities":{"act":0.22,"tool":0.30,"ask_user":0.26,"stop":0.22}},
  "looping":{"type":"noul","confidence":0.7769,"noul":0.7769},
  "unsafe":{"type":"noul","confidence":0.8542,"noul":0.1458},
  "progress":{"type":"score","confidence":0.38,"score":1.29,"legend":{"0":"none / spinning","1":"diagnosed but not fixed","2":"partial fix","3":"ready to stop"},
    "probabilities":{"0":0.20,"1":0.45,"2":0.25,"3":0.10}}},
 "usage":{"input_tokens":408,"output_tokens":0,"latency_ms":254}}`

func TestConnectFlow(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	if m.status != statusReady {
		t.Errorf("status = %v, want ready", m.status)
	}
	if len(m.presets) != 10 || m.health == nil || m.health.Device != "vulkan:0" {
		t.Errorf("presets=%d health=%+v", len(m.presets), m.health)
	}
	out := plain(m.View())
	for _, want := range []string{"playa", "Presets", "[g] guard", "[x] expense", "Inbound email triage", "ready", "vulkan:0"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestWarmupIsColdAndNotSampled(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	if _, _, ok := m.tele.Median("single"); ok {
		t.Error("warmup must not become a sample")
	}
}

func TestUnreachableServerShowsError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	m := New(api.NewClient(url), Options{Timeout: time.Second})
	m = step(t, m, m.healthCmd())
	if m.status != statusError {
		t.Fatalf("status = %v", m.status)
	}
	out := plain(m.View())
	if !strings.Contains(out, "cannot reach server") {
		t.Errorf("view should explain the failure:\n%s", out)
	}
}

func TestNavigationAndShortcuts(t *testing.T) {
	m := connected(t, newFake(t))
	if p, _ := m.selected(); p.Name != "email" {
		t.Fatalf("initial selection = %s", p.Name)
	}
	m, _ = press(m, "up")
	if m.cursor != 0 {
		t.Error("cursor must not go above the first preset")
	}
	m, _ = press(m, "down")
	if p, _ := m.selected(); p.Name != "triage" {
		t.Errorf("after down: %s", p.Name)
	}
	for _, c := range []struct{ key, preset string }{
		{"g", "guard"}, {"e", "email"}, {"x", "expense"}, {"t", "triage"}, {"m", "moderation"},
		{"r", "router"}, {"s", "security"}, {"i", "invoice"}, {"c", "customer_service"}, {"h", "harness"},
	} {
		m, _ = press(m, c.key)
		if p, _ := m.selected(); p.Name != c.preset {
			t.Errorf("key %q selected %s, want %s", c.key, p.Name, c.preset)
		}
	}
	for i := 0; i < 20; i++ {
		m, _ = press(m, "down")
	}
	if m.cursor != len(m.presets)-1 {
		t.Errorf("cursor = %d, want last", m.cursor)
	}
}

func TestQuit(t *testing.T) {
	m := connected(t, newFake(t))
	_, cmd := press(m, "q")
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	// compare with what tea.Quit itself returns (the message type is unexported
	// in older Bubble Tea versions)
	if cmd() != tea.Quit() {
		t.Error("q should quit")
	}
}

func TestRunPresetShowsDecisionsInQuestionOrder(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody

	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	if !m.running {
		t.Fatal("enter should start a run")
	}
	if _, c2 := press(m, "enter"); c2 != nil {
		t.Error("a second enter while running must be ignored")
	}
	m = step(t, m, cmd)
	if m.running || m.runErr != "" {
		t.Fatalf("running=%v err=%q", m.running, m.runErr)
	}

	var names []string
	for _, s := range m.summaries {
		names = append(names, s.Name)
	}
	want := "jailbreak,prompt_injection,sensitive_data,harm_severity,topic"
	if strings.Join(names, ",") != want {
		t.Errorf("summary order = %v, want %s", names, want)
	}

	out := plain(m.View())
	for _, s := range []string{"jailbreak", "YES", "100%", "serious", "security_testing", "sat", "english words 18/20"} {
		if !strings.Contains(out, s) {
			t.Errorf("view missing %q:\n%s", s, out)
		}
	}
	if m.lastCold {
		t.Error("first run after warmup should not be cold")
	}
}

func TestSecondRunProducesMedian(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	for i := 0; i < 2; i++ {
		var cmd tea.Cmd
		m, cmd = press(m, "enter")
		m = step(t, m, cmd)
	}
	if !strings.Contains(plain(m.View()), "preset p50 270ms (n=2)") {
		t.Errorf("telemetry bar missing median:\n%s", plain(m.View()))
	}
}

func TestHarnessBannerAndContested(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "h")
	f.decideBody = harnessBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	out := plain(m.View())
	for _, s := range []string{"T O O L", "CONTESTED", "runner-up ask_user 26%"} {
		if !strings.Contains(out, s) {
			t.Errorf("view missing %q:\n%s", s, out)
		}
	}
}

func TestRawToggle(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	m, _ = press(m, "v")
	if out := plain(m.View()); !strings.Contains(out, "Raw response") || !strings.Contains(out, `"answers"`) {
		t.Errorf("raw view missing:\n%s", out)
	}
	m, _ = press(m, "v")
	if strings.Contains(plain(m.View()), "Raw response") {
		t.Error("v should toggle raw off")
	}
}

func TestAPIErrorIsShown(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	f.decideCode = 400
	f.decideBody = `{"error":{"code":"invalid_request_error","message":"score questions require a nonempty criteria list","param":"questions.urgency.criteria"}}`
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	out := plain(m.View())
	if !strings.Contains(out, "nonempty criteria list") || !strings.Contains(out, "questions.urgency.criteria") {
		t.Errorf("error not shown:\n%s", out)
	}
	if m.running {
		t.Error("running must be cleared after an error")
	}
}

func TestTruncationNotice(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "c") // customer_service: 3 questions
	// 3 questions x 512 tokens: every sequence hit the english cap
	f.decideBody = `{"model":"laya","family":"english","route":"forced english",
	 "answers":{"next_action":{"type":"choice","confidence":0.5,"choice":"escalate","probabilities":{"escalate":0.6,"refund_cancel":0.4}},
	            "already_churned":{"type":"noul","confidence":0.9,"noul":0.9},
	            "save_probability":{"type":"score","confidence":0.5,"score":0.5,"probabilities":{"0":0.6,"1":0.4}}},
	 "usage":{"input_tokens":1536,"output_tokens":0,"latency_ms":300}}`
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	if !m.truncated || !strings.Contains(plain(m.View()), "hit the cap") {
		t.Errorf("truncation notice missing:\n%s", plain(m.View()))
	}
}

func TestViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "h")
	f.decideBody = harnessBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	m, _ = press(m, "v") // raw JSON is the longest content

	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		lines := strings.Split(out, "\n")
		if len(lines) > size.h {
			t.Errorf("%dx%d: view has %d lines", size.w, size.h, len(lines))
		}
	}
}

func TestReloadResetsStatus(t *testing.T) {
	m := connected(t, newFake(t))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(Model)
	if m.status != statusConnecting || cmd == nil {
		t.Errorf("status = %v, cmd nil = %v", m.status, cmd == nil)
	}
}
