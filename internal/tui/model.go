// Package tui is the Bubble Tea front end. All decision logic lives in the
// lens, lengthguard and telemetry packages; this package only wires them to
// the screen.
package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
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
}

type status int

const (
	statusConnecting status = iota
	statusWarming
	statusReady
	statusError
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

	running   bool
	runErr    string
	result    *api.DecideResponse
	resultOf  string // preset the result belongs to
	summaries []lens.Summary
	truncated bool // the response shows the state hit the cap
	showRaw   bool

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
	return Model{
		client: c, opts: o, width: 100, height: 30,
		tele: telemetry.New(10, 10*time.Second),
	}
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
		return m, nil

	case healthMsg:
		if msg.err != nil {
			m.status, m.statusMsg = statusError, "cannot reach server: "+msg.err.Error()
			return m, nil
		}
		m.health = msg.h
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
		m.running = false
		if msg.err != nil {
			m.runErr = msg.err.Error()
			return m, nil
		}
		m.runErr = ""
		m.result, m.resultOf = msg.resp, msg.preset
		m.summaries = m.summaries[:0:0]
		for _, nq := range msg.questions {
			if a, ok := msg.resp.Answers[nq.Name]; ok {
				m.summaries = append(m.summaries, lens.Summarize(nq.Name, a, lens.Options{}))
			}
		}
		kind := "preset"
		if len(msg.questions) == 1 {
			kind = "single"
		}
		m.lastCold = m.tele.Observe(kind, msg.resp.Usage.LatencyMs, msg.at)
		m.truncated = lengthguard.Truncated(msg.resp.Usage.InputTokens, len(msg.questions), lengthguard.Cap(msg.resp.Family))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		m.running, m.runErr = true, ""
		return m, m.decideCmd(p)
	case "v":
		m.showRaw = !m.showRaw
	case "ctrl+r":
		m.status, m.statusMsg, m.loadErr = statusConnecting, "", ""
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
