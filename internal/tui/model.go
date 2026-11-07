// Package tui is the Bubble Tea front end. All decision logic lives in the
// lens, lengthguard and telemetry packages; this package only wires them to
// the screen.
package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"playa/internal/api"
	"playa/internal/bench"
	"playa/internal/history"
	"playa/internal/langdetect"
	"playa/internal/lengthguard"
	"playa/internal/lens"
	"playa/internal/telemetry"
)

// shortcuts jump to a preset by its first letter. Expense uses x because email
// already has e.
var shortcuts = map[string]string{
	"g": "guard", "e": "email", "x": "expense", "t": "triage", "m": "moderation",
	"r": "router", "s": "security", "i": "invoice", "c": "customer_service", "h": "harness",
}

func shortcutFor(preset string) string {
	for k, v := range shortcuts {
		if v == preset {
			return k
		}
	}
	return " "
}

// Options configures the TUI.
type Options struct {
	Warmup  bool             // send a tiny request after connecting
	Timeout time.Duration    // per request; first calls can load a model, so keep it generous
	Now     func() time.Time // injectable clock for tests
	// Detect runs `laya detect-lang` for the route preview. Nil disables it.
	Detect func(ctx context.Context, text string) (langdetect.Preview, error)
	// HistoryPath persists run history. Empty keeps it in memory only.
	HistoryPath string
	// BenchPath persists benchmark results. Empty keeps them in memory only.
	BenchPath string
}

type status int

const (
	statusConnecting status = iota
	statusWarming
	statusReady
	statusError
)

type screen int

const (
	screenBrowse screen = iota
	screenState
	screenForge
	screenHistory
	screenBatch
	screenBench
)

// Model is the Bubble Tea model.
type Model struct {
	client        *api.Client
	opts          Options
	width, height int

	status    status
	statusMsg string // detail shown when status is statusError
	health    *api.Health
	presets   []api.Preset
	loadErr   string // presets failed to load
	cursor    int
	model     string // "auto" or a pinned family
	notice    string // transient footer message

	drafts  map[string]*draft
	mode    screen
	stateEd *stateEditor
	forgeEd *forgeEditor

	history       *history.Store
	historyPath   string
	historyErr    string
	historyCursor int
	historyRaw    bool

	batch *batchSession

	bench      *benchSession
	benchStore *bench.Store
	benchPath  string
	benchErr   string
	predictor  *bench.Predictor

	running     bool
	runSeq      int
	runCancel   context.CancelFunc
	runErr      string
	runErrParam string
	result      *api.DecideResponse
	resultReq   api.DecideRequest
	resultOf    string // preset the result belongs to
	summaries   []lens.Summary
	truncated   bool // the response shows the state hit the cap
	showRaw     bool

	routeSeq     int
	routePreset  string
	routeText    string
	routePreview langdetect.Preview
	routeErr     string
	routePending bool

	tele     *telemetry.Tracker
	lastCold bool
}

// New returns a model that talks to c.
func New(c *api.Client, o Options) Model {
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := Model{
		client: c, opts: o, width: 100, height: 30,
		model:      "auto",
		drafts:     map[string]*draft{},
		tele:       telemetry.New(10, 10*time.Second),
		history:    history.New(history.DefaultMax),
		predictor:  bench.NewPredictor(200),
		benchStore: bench.NewStore(bench.DefaultMax),
	}
	if o.HistoryPath != "" {
		st, err := history.Load(o.HistoryPath, history.DefaultMax)
		m.history = st
		m.historyPath = o.HistoryPath
		if err != nil {
			m.historyErr = "history: " + err.Error()
		}
	}
	if o.BenchPath != "" {
		st, err := bench.Load(o.BenchPath, bench.DefaultMax)
		m.benchStore = st
		m.benchPath = o.BenchPath
		if err != nil {
			m.benchErr = "bench: " + err.Error()
		}
	}
	m.seedPredictor()
	return m
}

// seedPredictor learns from the persisted warm runs so the first cost estimate
// already reflects this machine rather than the built-in defaults.
func (m *Model) seedPredictor() {
	if m.history == nil || m.predictor == nil {
		return
	}
	for _, e := range m.history.List() {
		if e.Response == nil || e.Err != "" || e.Cold {
			continue
		}
		r := lengthguard.Check(e.Response.Family, e.Request.State, e.Request.Questions)
		m.predictor.Observe(bench.Sample{
			Family:    e.Response.Family,
			Questions: len(e.Request.Questions),
			Tokens:    r.Longest,
			LatencyMS: e.Response.Usage.LatencyMs,
			At:        e.At,
		})
	}
}

// observeRun feeds one warm call to the latency predictor.
func (m *Model) observeRun(req api.DecideRequest, resp *api.DecideResponse, at time.Time, cold bool) {
	if m.predictor == nil || resp == nil || cold {
		return
	}
	r := lengthguard.Check(resp.Family, req.State, req.Questions)
	m.predictor.Observe(bench.Sample{
		Family:    resp.Family,
		Questions: len(req.Questions),
		Tokens:    r.Longest,
		LatencyMS: resp.Usage.LatencyMs,
		At:        at,
	})
}

// Init loads health and presets in parallel.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.healthCmd(), m.presetsCmd())
}

func (m Model) selected() (api.Preset, bool) {
	if m.cursor < 0 || m.cursor >= len(m.presets) {
		return api.Preset{}, false
	}
	return m.presets[m.cursor], true
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.mode == screenState && m.stateEd != nil {
			m.stateEd.resize(m.width, m.height)
			if m.stateEd.form != nil {
				model, cmd := m.stateEd.form.Update(msg)
				m.stateEd.form = model.(*huh.Form)
				return m, cmd
			}
		}
		if m.mode == screenForge && m.forgeEd != nil {
			m.forgeEd.resize(m.width, m.height)
			if m.forgeEd.form != nil {
				model, cmd := m.forgeEd.form.Update(msg)
				m.forgeEd.form = model.(*huh.Form)
				return m, cmd
			}
		}
		if m.mode == screenBatch && m.batch != nil {
			m.batch.resize(m.width, m.height)
		}
		if m.mode == screenBench && m.bench != nil {
			m.bench.resize(m.width, m.height)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case screenState:
			return m.handleStateKey(msg)
		case screenForge:
			return m.handleForgeKey(msg)
		case screenHistory:
			return m.handleHistoryKey(msg)
		case screenBatch:
			return m.handleBatchKey(msg)
		case screenBench:
			return m.handleBenchKey(msg)
		default:
			return m.handleBrowseKey(msg)
		}

	case routeTickMsg:
		if m.mode != screenState || msg.seq != m.routeSeq {
			return m, nil
		}
		return m, m.routeDetectCmd(msg.text, msg.seq)

	case routeMsg:
		if m.mode != screenState || msg.seq != m.routeSeq {
			return m, nil
		}
		m.routePending = false
		if msg.err != nil {
			m.routePreview, m.routeErr = langdetect.Preview{}, msg.err.Error()
			return m, nil
		}
		m.routePreview, m.routeErr = msg.preview, ""
		return m, nil
	}

	// Editor widgets need cursor, blink and form messages too.
	if handled, model, cmd := m.routeEditorMsg(msg); handled {
		return model, cmd
	}

	switch msg := msg.(type) {
	case healthMsg:
		if msg.err != nil {
			m.status, m.statusMsg = statusError, "cannot reach server: "+msg.err.Error()
			return m, nil
		}
		m.health = msg.h
		if m.model != "auto" && !contains(m.health.Families, m.model) {
			m.model = "auto"
		}
		if m.opts.Warmup {
			m.status = statusWarming
			return m, m.warmupCmd()
		}
		m.status = statusReady
		return m, nil

	case presetsMsg:
		if msg.err != nil {
			m.loadErr = "could not load presets: " + msg.err.Error()
			return m, nil
		}
		m.loadErr = ""
		m.presets = msg.ps
		m.drafts = make(map[string]*draft, len(msg.ps))
		for _, p := range msg.ps {
			m.drafts[p.Name] = newDraft(p)
		}
		if m.cursor >= len(m.presets) {
			m.cursor = 0
		}
		return m, nil

	case warmupMsg:
		if msg.err != nil {
			m.status, m.statusMsg = statusError, "warmup failed: "+msg.err.Error()
			return m, nil
		}
		// recorded as cold: sets the idle clock, stays out of the medians
		m.tele.Observe("warmup", msg.resp.Usage.LatencyMs, msg.at)
		m.status = statusReady
		return m, nil

	case decideMsg:
		if msg.seq != m.runSeq {
			return m, nil // cancelled or superseded
		}
		m.running = false
		m.runCancel = nil
		if msg.err != nil {
			m.runErr = msg.err.Error()
			m.runErrParam = ""
			var apiErr *api.APIError
			if errors.As(msg.err, &apiErr) {
				m.runErrParam = apiErr.Param
			}
			m.recordHistory(msg, false, m.runErrParam)
			if msg.replayOf > 0 {
				m.notice = fmt.Sprintf("replay of #%d failed", msg.replayOf)
			}
			return m, nil
		}
		m.runErr = ""
		m.runErrParam = ""
		m.result, m.resultOf = msg.resp, msg.preset
		m.resultReq = msg.request
		m.summaries = summarize(msg.request.Questions, msg.resp)
		kind := "preset"
		if len(msg.request.Questions) == 1 {
			kind = "single"
		}
		m.lastCold = m.tele.Observe(kind, msg.resp.Usage.LatencyMs, msg.at)
		m.truncated = lengthguard.Truncated(msg.resp.Usage.InputTokens, len(msg.request.Questions), lengthguard.Cap(msg.resp.Family))
		m.observeRun(msg.request, msg.resp, msg.at, m.lastCold)
		m.recordHistory(msg, m.lastCold, "")
		if msg.replayOf > 0 {
			m.notice = fmt.Sprintf("replayed run #%d", msg.replayOf)
		}
		return m, nil

	case batchLoadMsg:
		return m.handleBatchLoad(msg)

	case batchRowMsg:
		return m.handleBatchRow(msg)

	case benchRunMsg:
		return m.handleBenchRun(msg)
	}
	return m, nil
}

// routeEditorMsg forwards non-key messages to the active editor widget.
func (m Model) routeEditorMsg(msg tea.Msg) (bool, tea.Model, tea.Cmd) {
	switch m.mode {
	case screenState:
		e, d := m.stateEd, m.currentDraft()
		if e == nil || d == nil {
			return false, m, nil
		}
		if e.form != nil {
			model, cmd := e.form.Update(msg)
			e.form = model.(*huh.Form)
			if e.form.State == huh.StateCompleted {
				next, cmd := m.finishFieldForm(d)
				return true, next, cmd
			}
			return true, m, cmd
		}
		switch e.sub {
		case stateSubRaw:
			var cmd tea.Cmd
			e.raw, cmd = e.raw.Update(msg)
			return true, m, cmd
		case stateSubText:
			var cmd tea.Cmd
			e.area, cmd = e.area.Update(msg)
			return true, m, cmd
		}
	case screenForge:
		e, d := m.forgeEd, m.currentDraft()
		if e == nil || d == nil {
			return false, m, nil
		}
		if e.form != nil {
			model, cmd := e.form.Update(msg)
			e.form = model.(*huh.Form)
			if e.form.State == huh.StateCompleted {
				next, cmd := m.finishForgeForm(d)
				return true, next, cmd
			}
			return true, m, cmd
		}
		switch e.sub {
		case forgeSubRaw:
			var cmd tea.Cmd
			e.raw, cmd = e.raw.Update(msg)
			return true, m, cmd
		case forgeSubPreview:
			var cmd tea.Cmd
			e.preview, cmd = e.preview.Update(msg)
			return true, m, cmd
		}
	}
	return false, m, nil
}

func (m Model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.presets)-1 {
			m.cursor++
		}
	case "enter":
		p, ok := m.selected()
		if !ok || m.running {
			return m, nil
		}
		m.running, m.runErr, m.runErrParam = true, "", ""
		m.runSeq++
		ctx, cancel := context.WithCancel(context.Background())
		m.runCancel = cancel
		return m, m.decideCmd(ctx, p.Name, m.draftFor(p).request(m.model), m.runSeq, 0)
	case "esc":
		if m.cancelRun() {
			m.notice = "cancelled the in-flight request"
		}
	case "v":
		m.showRaw = !m.showRaw
	case "f":
		m.cycleModel()
	case "H":
		return m.openHistory()
	case "B":
		return m.openBatch()
	case "ctrl+b":
		return m.openBench()
	case "ctrl+e":
		return m.openStateEditor()
	case "ctrl+q":
		return m.openForge()
	case "R":
		p, ok := m.selected()
		if ok {
			m.drafts[p.Name] = newDraft(p)
			m.invalidate(p.Name)
			m.notice = "reset " + p.Name + " to the server preset"
		}
	case "ctrl+r":
		m.cancelRun()
		m.status, m.statusMsg, m.loadErr = statusConnecting, "", ""
		m.notice = ""
		return m, tea.Batch(m.healthCmd(), m.presetsCmd())
	default:
		if name, ok := shortcuts[k]; ok {
			for i, p := range m.presets {
				if p.Name == name {
					m.cursor = i
					break
				}
			}
		}
	}
	return m, nil
}

// cycleModel advances auto -> family -> ... -> auto.
func (m *Model) cycleModel() {
	opts := []string{"auto"}
	if m.health != nil {
		opts = append(opts, m.health.Families...)
	}
	idx := 0
	for i, v := range opts {
		if v == m.model {
			idx = i
			break
		}
	}
	m.model = opts[(idx+1)%len(opts)]
	if m.model == "auto" {
		m.notice = "model: auto (routed by the server)"
	} else {
		m.notice = "model pinned: " + m.model
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// modelLabel is the short form shown in the UI.
func (m Model) modelLabel() string {
	if m.model == "" || m.model == "auto" {
		return "auto"
	}
	return m.model
}

// previewFamily is the family used for the length guard before a request.
func (m Model) previewFamily(d *draft) string {
	if m.model != "" && m.model != "auto" {
		return m.model
	}
	if f := m.routeFamily(d); f != "" {
		return f
	}
	if d != nil && m.result != nil && m.resultOf == d.preset {
		return m.result.Family
	}
	return "english"
}

func (m Model) dirty(p api.Preset) bool {
	d, ok := m.drafts[p.Name]
	return ok && d != nil && d.dirty
}

func (m Model) statusLine() string {
	if m.notice != "" {
		return m.notice
	}
	return fmt.Sprintf("model %s", m.modelLabel())
}
