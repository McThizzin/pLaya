package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/bench"
	"playa/internal/forge"
	"playa/internal/lengthguard"
)

const (
	benchDefaultWarmup = 3
	benchDefaultRuns   = 20
	benchMinRuns       = 5
	benchMaxRuns       = 100
	benchRunStep       = 5
	benchKeepResults   = 2
)

// benchSession runs the current preset's questions N times, discarding a few
// warm-up calls first. It snapshots the state and questions at open time.
type benchSession struct {
	preset    string
	model     string // "" or "auto" means let the server route
	state     map[string]any
	questions api.QuestionSet
	tokens    int
	device    string
	server    string

	warmup, runs int
	warmupLeft   int
	done         int
	running      bool
	seq          int
	ctx          context.Context
	cancel       context.CancelFunc

	latencies    []float64
	inputTokens  int
	outputTokens int
	family       string
	route        string

	err     string
	status  string
	results []bench.Result // newest first, at most benchKeepResults
	width   int
	height  int
}

func newBenchSession(d *draft, model, device, server string, saved []bench.Result, width, height int) *benchSession {
	e := &benchSession{
		preset:    d.preset,
		model:     model,
		state:     cloneState(d.state),
		questions: forge.Clone(d.questions),
		device:    device,
		server:    server,
		warmup:    benchDefaultWarmup,
		runs:      benchDefaultRuns,
	}
	if e.model == "auto" {
		e.model = ""
	}
	family := model
	if family == "" || family == "auto" {
		family = "english"
	}
	e.tokens = lengthguard.Check(family, e.state, e.questions).Longest
	e.resize(width, height)
	for _, r := range saved {
		if r.Preset == d.preset {
			e.results = append(e.results, r)
			if len(e.results) == benchKeepResults {
				break
			}
		}
	}
	return e
}

func (e *benchSession) resize(width, height int) {
	e.width, e.height = width, height
}

func (m Model) openBench() (tea.Model, tea.Cmd) {
	d := m.currentDraft()
	if d == nil || len(d.questions) == 0 {
		m.notice = "benchmark needs a preset with questions"
		return m, nil
	}
	var saved []bench.Result
	if m.benchStore != nil {
		saved = m.benchStore.List()
	}
	device, server := "", ""
	if m.health != nil {
		device = m.health.Device
	}
	if m.client != nil {
		server = m.client.BaseURL
	}
	m.bench = newBenchSession(d, m.model, device, server, saved, m.bodyW(), m.bodyH())
	m.mode = screenBench
	return m, nil
}

func (e *benchSession) request() api.DecideRequest {
	req := api.DecideRequest{State: cloneState(e.state), Questions: forge.Clone(e.questions)}
	if e.model != "" && e.model != "auto" {
		req.Model = e.model
	}
	return req
}

type benchRunMsg struct {
	seq     int
	request api.DecideRequest
	resp    *api.DecideResponse
	err     error
	at      time.Time
}

func (m Model) benchRunCmd(ctx context.Context, seq int, req api.DecideRequest) tea.Cmd {
	c, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, to)
		defer cancel()
		resp, err := c.Decide(reqCtx, req)
		return benchRunMsg{seq: seq, request: req, resp: resp, err: err, at: now()}
	}
}

func (m *Model) benchStepCmd() tea.Cmd {
	e := m.bench
	if e == nil || e.ctx == nil {
		return nil
	}
	return m.benchRunCmd(e.ctx, e.seq, e.request())
}

func (m *Model) startBench() tea.Cmd {
	e := m.bench
	if e == nil || e.running {
		return nil
	}
	if len(e.questions) == 0 {
		e.status = "no questions to run"
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.seq++
	e.ctx, e.cancel = ctx, cancel
	e.running = true
	e.done, e.warmupLeft = 0, e.warmup
	e.latencies = nil
	e.inputTokens, e.outputTokens = 0, 0
	e.family, e.route = "", ""
	e.err = ""
	if e.warmup > 0 {
		e.status = fmt.Sprintf("warming up %d…", e.warmup)
	} else {
		e.status = "running…"
	}
	return m.benchStepCmd()
}

func (m Model) handleBenchRun(msg benchRunMsg) (tea.Model, tea.Cmd) {
	e := m.bench
	if e == nil || msg.seq != e.seq {
		return m, nil // cancelled or superseded
	}
	if msg.err != nil {
		e.running = false
		e.err = msg.err.Error()
		e.status = "failed"
		if e.cancel != nil {
			e.cancel()
		}
		e.cancel, e.ctx = nil, nil
		return m, nil
	}
	if msg.resp == nil {
		e.running = false
		e.err = "empty response"
		e.status = "failed"
		e.cancel, e.ctx = nil, nil
		return m, nil
	}
	e.family, e.route = msg.resp.Family, msg.resp.Route

	if e.warmupLeft > 0 {
		e.warmupLeft--
		if e.warmupLeft == 0 {
			e.status = "running…"
		}
		return m, m.benchStepCmd()
	}

	e.latencies = append(e.latencies, msg.resp.Usage.LatencyMs)
	e.inputTokens += msg.resp.Usage.InputTokens
	e.outputTokens += msg.resp.Usage.OutputTokens
	e.done++
	m.observeRun(msg.request, msg.resp, msg.at, false)
	if e.done >= e.runs {
		m.finishBench()
		return m, nil
	}
	e.status = fmt.Sprintf("running %d/%d…", e.done, e.runs)
	return m, m.benchStepCmd()
}

func (m *Model) finishBench() {
	e := m.bench
	if e == nil {
		return
	}
	e.running = false
	if e.cancel != nil {
		e.cancel()
	}
	e.cancel, e.ctx = nil, nil

	r := bench.Result{
		At:        m.opts.Now(),
		Preset:    e.preset,
		Model:     e.model,
		Family:    e.family,
		Route:     e.route,
		Device:    e.device,
		Server:    e.server,
		Warmup:    e.warmup,
		Runs:      e.done,
		Questions: len(e.questions),
		Tokens:    e.tokens,
		Stats:     bench.Summarize(e.latencies, len(e.questions), e.inputTokens, e.outputTokens),
	}
	e.results = append([]bench.Result{r}, e.results...)
	if len(e.results) > benchKeepResults {
		e.results = e.results[:benchKeepResults]
	}
	if m.benchStore == nil {
		m.benchStore = bench.NewStore(bench.DefaultMax)
	}
	m.benchStore.Add(r)
	m.persistBench()
	e.status = fmt.Sprintf("done · %d warm runs saved", e.done)
}

func (m *Model) persistBench() {
	if m.benchPath == "" || m.benchStore == nil {
		return
	}
	if err := m.benchStore.Save(m.benchPath); err != nil {
		m.benchErr = "bench: " + err.Error()
		return
	}
	m.benchErr = ""
}

func (m *Model) cancelBench() bool {
	e := m.bench
	if e == nil || !e.running {
		return false
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.seq++ // ignore the in-flight result
	e.running, e.cancel, e.ctx = false, nil, nil
	e.status = fmt.Sprintf("cancelled after %d warm runs", e.done)
	return true
}

func (m Model) handleBenchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.bench
	if e == nil {
		m.mode = screenBrowse
		return m, nil
	}
	switch msg.String() {
	case "enter":
		if !e.running {
			return m, m.startBench()
		}
	case "[":
		if !e.running && e.runs > benchMinRuns {
			e.runs = max(benchMinRuns, e.runs-benchRunStep)
		}
	case "]":
		if !e.running && e.runs < benchMaxRuns {
			e.runs = min(benchMaxRuns, e.runs+benchRunStep)
		}
	case "w":
		if !e.running {
			e.warmup = (e.warmup + 1) % 4
		}
	}
	return m, nil
}

// reloadSaved replaces the panel list with what is currently on disk.
func (e *benchSession) reloadSaved(store *bench.Store) {
	if store != nil {
		e.results = store.List()
	}
}

func (e *benchSession) cycleModel(h *api.Health) {
	choices := []string{""}
	if h != nil {
		for _, f := range h.Families {
			if f != "" {
				choices = append(choices, f)
			}
		}
	}
	idx := 0
	for i, c := range choices {
		if c == e.model {
			idx = i
			break
		}
	}
	e.model = choices[(idx+1)%len(choices)]
	e.status = "model " + familyLabel(e.model)
}

func familyLabel(model string) string {
	if model == "" || model == "auto" {
		return "auto"
	}
	return model
}
