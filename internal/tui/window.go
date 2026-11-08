package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/forge"
	"playa/internal/lens"
	"playa/internal/window"
)

const (
	windowSizeStep = 16
	windowSizeMin  = 8
	windowSizeMax  = 512
)

var windowOverlaps = []float64{0, 0.10, 0.25, 0.50}

type windowResult struct {
	Req  api.DecideRequest
	Resp *api.DecideResponse
	Err  error
	At   time.Time
	Cold bool
}

// windowSession runs a snapshot of the draft's main text as overlapping,
// token-bounded windows. The snapshot means edits in the State Canvas cannot
// change a run in progress.
type windowSession struct {
	preset    string
	stateKey  string
	state     map[string]any
	text      string
	questions api.QuestionSet
	model     string

	size       int
	overlapPct float64
	windows    []window.Window
	results    []windowResult
	strongest  int // index of the highest mean winner probability; -1 before a run

	running bool
	seq     int
	ctx     context.Context
	cancel  context.CancelFunc

	cursor        int
	raw           bool
	status        string
	err           string
	width, height int
}

func newWindowSession(d *draft, model string, width, height int) *windowSession {
	e := &windowSession{
		preset:     d.preset,
		stateKey:   d.stateKey,
		state:      cloneState(d.state),
		text:       d.stateText(),
		questions:  forge.Clone(d.questions),
		model:      model,
		size:       window.DefaultSize,
		overlapPct: 0.25,
		strongest:  -1,
	}
	e.resize(width, height)
	e.rebuild()
	return e
}

func (e *windowSession) resize(width, height int) {
	e.width, e.height = width, height
}

func (e *windowSession) overlapTokens() int {
	tokens := int(float64(e.size)*e.overlapPct + 0.5)
	if tokens == 0 && e.overlapPct > 0 {
		tokens = 1
	}
	return tokens
}

// rebuild splits the snapshot after the size or overlap changed.
func (e *windowSession) rebuild() {
	e.windows = window.Split(e.text, e.size, e.overlapTokens())
	e.results = make([]windowResult, len(e.windows))
	e.strongest = -1
	if e.cursor >= len(e.windows) {
		e.cursor = max(0, len(e.windows)-1)
	}
	if len(e.windows) == 0 {
		e.status = "no text to window"
		return
	}
	e.status = fmt.Sprintf("%d windows · ~%d tokens each · %d%% overlap",
		len(e.windows), e.size, int(e.overlapPct*100))
}

func (m Model) openWindow() (tea.Model, tea.Cmd) {
	d := m.currentDraft()
	if d == nil {
		return m, nil
	}
	if strings.TrimSpace(d.stateText()) == "" {
		// The canvas is the only caller, so put the message where it shows.
		if m.stateEd != nil {
			m.stateEd.err = "windowed mode needs some state text"
		}
		return m, nil
	}
	if m.stateEd != nil {
		m.stateEd.err = ""
	}
	m.windowed = newWindowSession(d, m.model, m.width, m.height)
	m.mode = screenWindow
	return m, nil
}

type windowResultMsg struct {
	seq   int
	index int
	req   api.DecideRequest
	resp  *api.DecideResponse
	err   error
	at    time.Time
}

func (m Model) windowCmd(ctx context.Context, seq, index int, req api.DecideRequest) tea.Cmd {
	c, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, to)
		defer cancel()
		resp, err := c.Decide(reqCtx, req)
		return windowResultMsg{seq: seq, index: index, req: req, resp: resp, err: err, at: now()}
	}
}

func (e *windowSession) request(w window.Window) api.DecideRequest {
	state := cloneState(e.state)
	state[e.stateKey] = w.Text
	req := api.DecideRequest{State: state, Questions: forge.Clone(e.questions)}
	if e.model != "" && e.model != "auto" {
		req.Model = e.model
	}
	return req
}

func (m *Model) startWindow() tea.Cmd {
	e := m.windowed
	if e == nil || e.running {
		return nil
	}
	if len(e.windows) == 0 {
		e.status = "no windows to run"
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.seq++
	e.ctx, e.cancel = ctx, cancel
	e.running = true
	e.results = make([]windowResult, len(e.windows))
	e.strongest = -1
	e.raw = false
	e.status = fmt.Sprintf("running window 1 of %d…", len(e.windows))
	return m.windowCmd(ctx, e.seq, 0, e.request(e.windows[0]))
}

func (m *Model) cancelWindow() bool {
	e := m.windowed
	if e == nil || !e.running {
		return false
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.seq++ // ignore the in-flight result
	e.running, e.cancel, e.ctx = false, nil, nil
	e.strongest = strongestWindow(e.results, e.questions)
	e.status = "cancelled; completed windows are kept"
	return true
}

func (m Model) handleWindowResult(msg windowResultMsg) (tea.Model, tea.Cmd) {
	e := m.windowed
	if e == nil || m.mode != screenWindow || msg.seq != e.seq {
		return m, nil // cancelled or superseded
	}
	if msg.index < 0 || msg.index >= len(e.results) {
		return m, nil
	}

	kind := "preset"
	if len(msg.req.Questions) == 1 {
		kind = "single"
	}
	cold := false
	if msg.resp != nil {
		cold = m.tele.Observe(kind, msg.resp.Usage.LatencyMs, msg.at)
		m.observeRun(msg.req, msg.resp, msg.at, cold)
	}
	param := ""
	var apiErr *api.APIError
	if errors.As(msg.err, &apiErr) {
		param = apiErr.Param
	}
	m.recordHistory(decideMsg{
		preset:  e.preset,
		request: msg.req,
		resp:    msg.resp,
		err:     msg.err,
		at:      msg.at,
	}, cold, param)
	e.results[msg.index] = windowResult{Req: msg.req, Resp: msg.resp, Err: msg.err, At: msg.at, Cold: cold}

	if e.ctx == nil || e.ctx.Err() != nil {
		e.running = false
		if e.cancel != nil {
			e.cancel()
		}
		e.cancel, e.ctx = nil, nil
		e.strongest = strongestWindow(e.results, e.questions)
		e.status = "cancelled"
		return m, nil
	}
	if next := msg.index + 1; next < len(e.windows) {
		e.status = fmt.Sprintf("running window %d of %d…", next+1, len(e.windows))
		return m, m.windowCmd(e.ctx, e.seq, next, e.request(e.windows[next]))
	}

	e.running = false
	if e.cancel != nil {
		e.cancel()
	}
	e.cancel, e.ctx = nil, nil
	e.strongest = strongestWindow(e.results, e.questions)
	failed := 0
	for _, r := range e.results {
		if r.Err != nil || r.Resp == nil {
			failed++
		}
	}
	e.status = fmt.Sprintf("done: %d windows", len(e.windows))
	if failed > 0 {
		e.status += fmt.Sprintf(", %d failed", failed)
	}
	if e.strongest >= 0 {
		e.status += fmt.Sprintf(" · strongest #%d", e.strongest+1)
	}
	return m, nil
}

// strongestWindow picks the window whose mean winner probability is highest.
func strongestWindow(results []windowResult, qs api.QuestionSet) int {
	best, bestP := -1, -1.0
	for i, r := range results {
		if r.Resp == nil || r.Err != nil {
			continue
		}
		p, ok := meanWinner(r.Resp, qs)
		if !ok {
			continue
		}
		if p > bestP {
			best, bestP = i, p
		}
	}
	return best
}

func meanWinner(resp *api.DecideResponse, qs api.QuestionSet) (float64, bool) {
	if resp == nil {
		return 0, false
	}
	sum, n := 0.0, 0
	for _, nq := range qs {
		a, ok := resp.Answers[nq.Name]
		if !ok {
			continue
		}
		sum += lens.Summarize(nq.Name, a, lens.Options{}).Prob
		n++
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func (m Model) handleWindowKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.windowed
	if e == nil {
		m.mode = screenState
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc":
		if m.cancelWindow() {
			return m, nil
		}
		m.mode = screenState
	case "enter":
		if !e.running {
			return m, m.startWindow()
		}
	case "[":
		if !e.running {
			e.size = max(windowSizeMin, e.size-windowSizeStep)
			e.rebuild()
		}
	case "]":
		if !e.running {
			e.size = min(windowSizeMax, e.size+windowSizeStep)
			e.rebuild()
		}
	case "o":
		if !e.running {
			idx := 0
			for i, v := range windowOverlaps {
				if v == e.overlapPct {
					idx = i
					break
				}
			}
			e.overlapPct = windowOverlaps[(idx+1)%len(windowOverlaps)]
			e.rebuild()
		}
	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}
	case "down", "j":
		if e.cursor < len(e.windows)-1 {
			e.cursor++
		}
	case "g", "home":
		e.cursor = 0
	case "G", "end":
		if len(e.windows) > 0 {
			e.cursor = len(e.windows) - 1
		}
	case "v":
		e.raw = !e.raw
	}
	return m, nil
}
