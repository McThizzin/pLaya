package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/batch"
)

func (f *fake) setDecideFunc(fn func(body []byte, n int) (int, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decideFunc = fn
}

func (f *fake) decideCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.decideN
}

// answerAll answers every question in the request. It is deliberately shaped
// like a real response so the batch views exercise the same code paths.
func answerAll(body []byte, _ int) (int, string) {
	var req struct {
		Questions map[string]struct {
			Type string `json:"type"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 400, `{"error":{"message":"bad request"}}`
	}
	answers := map[string]any{}
	for name, q := range req.Questions {
		switch q.Type {
		case api.TypeChoice:
			answers[name] = map[string]any{
				"type": "choice", "confidence": 0.5, "choice": "yes",
				"probabilities": map[string]float64{"yes": 0.8, "no": 0.2},
			}
		case api.TypeScore:
			answers[name] = map[string]any{
				"type": "score", "confidence": 0.5, "score": 1,
				"legend":        map[string]string{"0": "low", "1": "high"},
				"probabilities": map[string]float64{"0": 0.2, "1": 0.8},
			}
		default:
			answers[name] = map[string]any{"type": "noul", "confidence": 0.9, "noul": 0.9}
		}
	}
	resp := map[string]any{
		"model": "laya", "family": "english", "route": "test route", "answers": answers,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 0, "latency_ms": 50},
	}
	b, _ := json.Marshal(resp)
	return 200, string(b)
}

// drain executes a command and every follow-up command it produces. tea.Batch
// values are run in order, which is enough to drive the worker pool from a test.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batchMsg, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batchMsg {
			m = drain(t, m, c)
		}
		return m
	}
	next, nextCmd := m.Update(msg)
	return drain(t, next.(Model), nextCmd)
}

func writeStates(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBatchLoadRunAndExport(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)

	path := writeStates(t, "states.jsonl", "\"one\"\n{\"text\":\"two\"}\n\"three\"\n")
	m, _ = press(m, "alt+3")
	if m.mode != screenBatch || m.batch == nil {
		t.Fatal("B should open the batch screen")
	}
	if len(m.batch.questions) != 5 || len(m.batch.chunks) != 1 {
		t.Errorf("questions/chunks = %d/%d", len(m.batch.questions), len(m.batch.chunks))
	}

	m, cmd := press(m, "ctrl+o")
	if !m.batch.prompt {
		t.Fatal("ctrl+o should open the path prompt")
	}
	m, _ = press(m, path)
	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)
	if m.batch.prompt || m.batch.loadErr != "" || len(m.batch.rows) != 3 {
		t.Fatalf("load: prompt=%v err=%q rows=%d", m.batch.prompt, m.batch.loadErr, len(m.batch.rows))
	}

	m, cmd = press(m, "r")
	if !m.batch.running {
		t.Fatal("r should start the batch")
	}
	m = drain(t, m, cmd)
	if m.batch.running {
		t.Fatal("batch should be finished")
	}
	for i := range m.batch.rows {
		if !m.batch.rows[i].Complete(len(m.batch.chunks)) {
			t.Errorf("row %d incomplete: %+v", i+1, m.batch.rows[i])
		}
	}

	out := plain(m.View())
	for _, want := range []string{"Batch", "States", "#1", "one", "#3", "three", "90%", "3/3 rows", "p50 50ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("batch view missing %q:\n%s", want, out)
		}
	}

	m, _ = press(m, "enter")
	if !m.batch.detail {
		t.Fatal("enter should open the row detail")
	}
	out = plain(m.View())
	for _, want := range []string{"State", "Calls", "test route", "Decisions", "jailbreak"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
	m, _ = press(m, "v")
	out = plain(m.View())
	if !strings.Contains(out, "Raw response") || !strings.Contains(out, `"answers"`) {
		t.Errorf("raw detail missing:\n%s", out)
	}
	m, _ = press(m, "esc")

	m, _ = press(m, "x")
	jsonlPath, csvPath := batch.ExportPaths(path)
	raw, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"route":"test route"`) || !strings.Contains(string(raw), `"response"`) {
		t.Errorf("JSONL export missing full response:\n%s", raw)
	}
	csvRaw, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"id,state,", "jailbreak_answer", "family,route", "english,test route"} {
		if !strings.Contains(string(csvRaw), want) {
			t.Errorf("CSV export missing %q:\n%s", want, csvRaw)
		}
	}
}

func TestBatchSplitsQuestionChunksAndControls(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	d := m.currentDraft()
	for i := 0; i < 6; i++ {
		d.questions = append(d.questions, api.NamedQuestion{
			Name:     fmt.Sprintf("extra_%d", i),
			Question: api.Question{Type: api.TypeNoul, Instructions: "Extra?"},
		})
	}
	f.setDecideFunc(answerAll)

	path := writeStates(t, "one.jsonl", "\"single state\"\n")
	m, _ = press(m, "alt+3")
	if len(m.batch.questions) != 11 || len(m.batch.chunks) != 2 {
		t.Fatalf("questions/chunks = %d/%d, want 11/2", len(m.batch.questions), len(m.batch.chunks))
	}
	m, cmd := press(m, "ctrl+o")
	m, _ = press(m, path)
	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)
	m, cmd = press(m, "r")
	m = drain(t, m, cmd)

	row := m.batch.rows[0]
	if len(row.Calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(row.Calls))
	}
	if len(row.Calls[0].Questions) != 8 || len(row.Calls[1].Questions) != 3 {
		t.Errorf("chunk sizes = %d/%d", len(row.Calls[0].Questions), len(row.Calls[1].Questions))
	}
	if got := len(row.Answers()); got != 11 {
		t.Errorf("merged answers = %d, want 11", got)
	}
	if got := f.decideCount(); got != 3 { // warmup + two chunks
		t.Errorf("decide calls = %d, want 3", got)
	}

	m, _ = press(m, "right")
	if m.batch.colOffset != 1 {
		t.Errorf("right should scroll columns, colOffset = %d", m.batch.colOffset)
	}
	m, _ = press(m, "s")
	if m.batch.sortCol != 0 || !m.batch.sortDesc {
		t.Errorf("s sort = col %d desc %v", m.batch.sortCol, m.batch.sortDesc)
	}
	m, _ = press(m, "S")
	if m.batch.sortDesc {
		t.Error("S should reverse the sort")
	}
	m, _ = press(m, "F")
	if m.batch.filter == batch.FilterAll {
		t.Error("f should change the filter")
	}
}

func TestBatchCancelThenResume(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)
	path := writeStates(t, "states.jsonl", "\"one\"\n\"two\"\n\"three\"\n")
	m, _ = press(m, "alt+3")
	m, cmd := press(m, "ctrl+o")
	m, _ = press(m, path)
	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)

	m, cmd = press(m, "r")
	if !m.batch.running {
		t.Fatal("r should start the batch")
	}
	m, _ = press(m, "esc")
	if m.batch.running {
		t.Fatal("esc should cancel the batch")
	}
	m = drain(t, m, cmd) // late results from the cancelled run are ignored
	for i := range m.batch.rows {
		if len(m.batch.rows[i].Calls) != 0 {
			t.Errorf("cancelled row %d kept calls: %+v", i+1, m.batch.rows[i].Calls)
		}
	}
	if !strings.Contains(m.batch.status, "cancelled") {
		t.Errorf("status = %q", m.batch.status)
	}

	m, cmd = press(m, "r")
	m = drain(t, m, cmd)
	for i := range m.batch.rows {
		if !m.batch.rows[i].Complete(len(m.batch.chunks)) {
			t.Errorf("resumed row %d incomplete", i+1)
		}
	}
}

func TestBatchViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)
	path := writeStates(t, "many.jsonl",
		"\"one\"\n\"two\"\n\"three\"\n\"four\"\n\"five\"\n\"six\"\n\"seven\"\n\"eight\"\n")
	m, _ = press(m, "alt+3")
	m, cmd := press(m, "ctrl+o")
	m, _ = press(m, path)
	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)
	m, cmd = press(m, "r")
	m = drain(t, m, cmd)

	for _, size := range []struct{ w, h int }{{100, 30}, {120, 40}, {140, 50}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: batch view has %d lines:\n%s", size.w, size.h, len(lines), out)
		}
	}
	m, _ = press(m, "enter")
	for _, size := range []struct{ w, h int }{{100, 30}, {120, 40}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d detail has %d lines", size.w, size.h, len(lines))
		}
	}
}
