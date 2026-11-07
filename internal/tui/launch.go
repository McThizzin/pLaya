package tui

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/launch"
)

const (
	launchPollEvery = 500 * time.Millisecond
	launchPollLimit = 60 // 30 seconds at 500 ms
)

type launchField int

const (
	launchBinary launchField = iota
	launchModels
	launchPort
	launchDevice
	launchMaxBatch
	launchFamily
	launchFieldCount
)

var launchLabels = [...]string{
	"binary", "models dir", "port", "device", "max batch", "family",
}

type launchSession struct {
	fields   [launchFieldCount]string
	selected int
	editing  bool
	input    textinput.Model

	targetURL string
	status    string
	starting  bool
	seq       int
	polls     int
	logSeq    int
	warmSeq   int

	width, height int
}

func newLaunchSession(o Options, currentURL string, width, height int) *launchSession {
	input := textinput.New()
	input.Placeholder = "value"
	input.CharLimit = 4096
	input.Prompt = "› "

	models := o.ModelsDir
	if models == "" {
		models = launch.DefaultModelsDir()
	}
	device := o.Device
	if device == "" {
		device = "vulkan"
	}
	binary := o.LayaPath
	if binary == "" {
		binary = "laya"
	}
	port := portFromURL(currentURL)
	e := &launchSession{
		fields: [launchFieldCount]string{
			launchBinary:   binary,
			launchModels:   models,
			launchPort:     strconv.Itoa(port),
			launchDevice:   device,
			launchMaxBatch: "",
			launchFamily:   "auto",
		},
		input:     input,
		targetURL: fmt.Sprintf("http://127.0.0.1:%d", port),
	}
	e.resize(width, height)
	return e
}

func portFromURL(raw string) int {
	u, err := url.Parse(raw)
	if err == nil {
		if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return 8080
}

func (e *launchSession) resize(width, height int) {
	e.width, e.height = width, height
	e.input.Width = max(8, width-16)
}

func (e *launchSession) url() string {
	port, err := strconv.Atoi(strings.TrimSpace(e.fields[launchPort]))
	if err != nil || port < 1 || port > 65535 {
		port = 8080
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

func (e *launchSession) config() (launch.Config, error) {
	port, err := strconv.Atoi(strings.TrimSpace(e.fields[launchPort]))
	if err != nil {
		return launch.Config{}, fmt.Errorf("port %q is not a number", e.fields[launchPort])
	}
	maxBatch := 0
	if raw := strings.TrimSpace(e.fields[launchMaxBatch]); raw != "" {
		maxBatch, err = strconv.Atoi(raw)
		if err != nil {
			return launch.Config{}, fmt.Errorf("max batch %q is not a number", raw)
		}
	}
	cfg := launch.Config{
		Path:      strings.TrimSpace(e.fields[launchBinary]),
		ModelsDir: strings.TrimSpace(e.fields[launchModels]),
		Port:      port,
		Device:    strings.TrimSpace(e.fields[launchDevice]),
		MaxBatch:  maxBatch,
		Family:    strings.TrimSpace(e.fields[launchFamily]),
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (m Model) openLaunch() (tea.Model, tea.Cmd) {
	if m.launch == nil {
		current := ""
		if m.client != nil {
			current = m.client.BaseURL
		}
		m.launch = newLaunchSession(m.opts, current, m.width, m.height)
	}
	m.mode = screenLaunch
	if m.launcher != nil && m.launcher.Running() {
		m.launch.logSeq++
		return m, launchLogTick(m.launch.logSeq)
	}
	return m, nil
}

type launchStartMsg struct {
	seq int
	cfg launch.Config
	err error
}

type launchHealthMsg struct {
	seq int
	h   *api.Health
	err error
}

type launchPollMsg struct{ seq int }

type launchStopMsg struct {
	seq int
	err error
}

type launchAttachMsg struct {
	seq int
	url string
	h   *api.Health
	err error
}

type launchWarmMsg struct {
	seq      int
	total    int
	warmed   int
	failed   int
	firstErr error
}

type launchLogMsg struct{ seq int }

func launchLogTick(seq int) tea.Cmd {
	return tea.Tick(launchPollEvery, func(time.Time) tea.Msg { return launchLogMsg{seq: seq} })
}

func launchPollTick(seq int) tea.Cmd {
	return tea.Tick(launchPollEvery, func(time.Time) tea.Msg { return launchPollMsg{seq: seq} })
}

func (m *Model) startLaunch() tea.Cmd {
	e := m.launch
	if e == nil || e.starting {
		return nil
	}
	if m.launcher != nil && m.launcher.Running() {
		e.status = "a spawned server is already running"
		return nil
	}
	cfg, err := e.config()
	if err != nil {
		e.status = err.Error()
		return nil
	}
	e.seq++
	e.polls = 0
	e.starting = true
	e.targetURL = cfg.URL()
	e.status = "starting " + cfg.URL() + "…"
	m.launcher = launch.NewManager(cfg)
	m.launchers = append(m.launchers, m.launcher)
	mgr, seq := m.launcher, e.seq
	e.logSeq++
	start := func() tea.Msg {
		return launchStartMsg{seq: seq, cfg: cfg, err: mgr.Start()}
	}
	return tea.Batch(start, launchLogTick(e.logSeq))
}

func (m Model) handleLaunchStart(msg launchStartMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.seq {
		return m, nil
	}
	if msg.err != nil {
		e.starting = false
		e.status = "start failed: " + msg.err.Error()
		e.logSeq++
		return m, nil
	}
	e.status = "waiting for " + msg.cfg.URL() + "…"
	return m, m.launchHealthCmd(msg.seq)
}

func (m Model) launchHealthCmd(seq int) tea.Cmd {
	e := m.launch
	if e == nil || m.client == nil {
		return nil
	}
	c := *m.client
	c.BaseURL = e.targetURL
	to := 2 * time.Second
	if m.opts.Timeout > 0 && m.opts.Timeout < to {
		to = m.opts.Timeout
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		h, err := c.Health(ctx)
		return launchHealthMsg{seq: seq, h: h, err: err}
	}
}

func (m Model) handleLaunchHealth(msg launchHealthMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.seq || !e.starting {
		return m, nil
	}
	if msg.err != nil {
		e.polls++
		if e.polls >= launchPollLimit {
			e.starting = false
			e.status = "server did not become ready; check the log or press x"
			e.logSeq++
			return m, nil
		}
		e.status = fmt.Sprintf("waiting for server… (%d)", e.polls)
		return m, launchPollTick(e.seq)
	}
	e.starting = false
	e.status = "server ready · " + msg.h.Device
	e.logSeq++
	if m.client != nil {
		m.client.BaseURL = e.targetURL
	}
	m.health = msg.h
	m.status, m.statusMsg, m.loadErr = statusReady, "", ""
	if m.model != "auto" && !contains(msg.h.Families, m.model) {
		m.model = "auto"
	}
	m.notice = "connected to spawned server"
	return m, tea.Batch(m.presetsCmd(), launchLogTick(e.logSeq))
}

func (m Model) handleLaunchPoll(msg launchPollMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.seq || !e.starting {
		return m, nil
	}
	return m, m.launchHealthCmd(msg.seq)
}

func (m *Model) stopLaunch() tea.Cmd {
	e := m.launch
	if e == nil {
		return nil
	}
	if m.launcher == nil || (!m.launcher.Running() && !e.starting) {
		e.status = "no spawned process to stop (attach mode is untouched)"
		return nil
	}
	// During startup the child exists before the start command has run. Closing
	// the manager here makes that pending Start refuse to spawn.
	e.seq++
	e.starting = false
	e.logSeq++
	e.status = "stopping…"
	mgr, seq := m.launcher, e.seq
	return func() tea.Msg { return launchStopMsg{seq: seq, err: mgr.Stop()} }
}

func (m Model) handleLaunchStop(msg launchStopMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.seq {
		return m, nil
	}
	if msg.err != nil {
		e.status = "stop failed: " + msg.err.Error()
		return m, nil
	}
	e.status = "stopped"
	e.starting = false
	m.status, m.statusMsg = statusConnecting, ""
	m.health = nil
	m.notice = "spawned server stopped"
	return m, nil
}

func (m *Model) attachLaunch() tea.Cmd {
	e := m.launch
	if e == nil || e.starting || m.client == nil {
		return nil
	}
	target := e.url()
	e.seq++
	e.targetURL = target
	e.status = "checking " + target + "…"
	c := *m.client
	c.BaseURL = target
	to := m.opts.Timeout
	if to <= 0 {
		to = 5 * time.Second
	}
	seq := e.seq
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		h, err := c.Health(ctx)
		return launchAttachMsg{seq: seq, url: target, h: h, err: err}
	}
}

func (m Model) handleLaunchAttach(msg launchAttachMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.seq {
		return m, nil
	}
	if msg.err != nil {
		e.status = "no server at " + msg.url + ": " + msg.err.Error()
		return m, nil
	}
	if m.client != nil {
		m.client.BaseURL = msg.url
	}
	m.health = msg.h
	m.status, m.statusMsg, m.loadErr = statusReady, "", ""
	if m.model != "auto" && !contains(msg.h.Families, m.model) {
		m.model = "auto"
	}
	e.status = "attached; the external server is never stopped"
	m.notice = "attached to " + msg.url
	return m, m.presetsCmd()
}

func (m *Model) warmFamilies() tea.Cmd {
	e := m.launch
	if e == nil || e.starting {
		return nil
	}
	if m.health == nil || len(m.health.Families) == 0 {
		e.status = "no families from /health"
		return nil
	}
	if m.client == nil {
		e.status = "no server client"
		return nil
	}
	if m.launcher != nil && m.launcher.Running() && !m.healthReady(e.targetURL) {
		e.status = "waiting for the spawned server before warming"
		return nil
	}
	families := append([]string(nil), m.health.Families...)
	c, to := *m.client, m.opts.Timeout
	if to <= 0 {
		to = 30 * time.Second
	}
	e.warmSeq++
	seq := e.warmSeq
	e.status = fmt.Sprintf("warming %d families…", len(families))
	return func() tea.Msg {
		msg := launchWarmMsg{seq: seq, total: len(families)}
		for _, family := range families {
			ctx, cancel := context.WithTimeout(context.Background(), to)
			_, err := c.Decide(ctx, api.DecideRequest{
				Model: family,
				State: map[string]any{"document": "hello"},
				Questions: api.QuestionSet{{
					Name:     "warmup",
					Question: api.Question{Type: api.TypeNoul, Instructions: "Is this a greeting?"},
				}},
			})
			cancel()
			if err != nil {
				msg.failed++
				if msg.firstErr == nil {
					msg.firstErr = err
				}
				continue
			}
			msg.warmed++
		}
		return msg
	}
}

func (m Model) healthReady(url string) bool {
	return m.client != nil && m.client.BaseURL == url && m.status == statusReady
}

func (m Model) handleLaunchWarm(msg launchWarmMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.warmSeq {
		return m, nil
	}
	text := fmt.Sprintf("warmed %d/%d families", msg.warmed, msg.total)
	if msg.failed > 0 && msg.firstErr != nil {
		text += fmt.Sprintf(" · %d failed: %s", msg.failed, oneLine(msg.firstErr.Error()))
	}
	e.status = text
	return m, nil
}

func (m Model) handleLaunchLog(msg launchLogMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil || msg.seq != e.logSeq || m.mode != screenLaunch {
		return m, nil
	}
	if m.launcher == nil || !m.launcher.Running() {
		return m, nil
	}
	return m, launchLogTick(msg.seq)
}

func (m Model) handleLaunchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.launch
	if e == nil {
		m.mode = screenBrowse
		return m, nil
	}
	key := msg.String()

	if e.editing {
		switch key {
		case "esc":
			e.editing = false
			e.input.Blur()
			e.status = "edit cancelled"
			return m, nil
		case "enter":
			e.fields[e.selected] = strings.TrimSpace(e.input.Value())
			e.editing = false
			e.input.Blur()
			e.status = launchLabels[e.selected] + " set"
			return m, nil
		}
		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		return m, cmd
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		wasStarting := e.starting
		e.seq++
		e.starting = false
		e.logSeq++
		if wasStarting {
			if m.launcher != nil && m.launcher.Running() {
				e.status = "wait cancelled; the child keeps running (a attach · x stop)"
			} else {
				e.status = "start wait cancelled"
			}
		}
		m.mode = screenBrowse
	case "up", "k":
		if e.selected > 0 {
			e.selected--
		}
	case "down", "j":
		if e.selected < int(launchFieldCount)-1 {
			e.selected++
		}
	case "e", "enter":
		e.editing = true
		e.input.SetValue(e.fields[e.selected])
		e.input.CursorEnd()
		return m, e.input.Focus()
	case "s", "ctrl+s":
		return m, m.startLaunch()
	case "x":
		return m, m.stopLaunch()
	case "a":
		return m, m.attachLaunch()
	case "w":
		return m, m.warmFamilies()
	}
	return m, nil
}

func (m Model) Close() {
	// Stop every manager this session created, not just the newest: a rapid
	// stop/start or a quit during the start command must not leave a child.
	for _, mgr := range m.launchers {
		if mgr != nil {
			_ = mgr.Stop()
		}
	}
	if m.launcher != nil && !containsManager(m.launchers, m.launcher) {
		_ = m.launcher.Stop()
	}
}

func containsManager(list []*launch.Manager, want *launch.Manager) bool {
	for _, mgr := range list {
		if mgr == want {
			return true
		}
	}
	return false
}
