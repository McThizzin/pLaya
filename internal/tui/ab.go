package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/forge"
	"playa/internal/lens"
)

// abTarget is one side of an A/B comparison: a server plus an optional pinned
// family. The same URL on both sides compares families; two URLs compare
// builds or quants on different ports.
type abTarget struct {
	URL   string
	Model string // "" or "auto" lets the server route
}

func (t abTarget) request(state map[string]any, qs api.QuestionSet) api.DecideRequest {
	req := api.DecideRequest{State: cloneState(state), Questions: forge.Clone(qs)}
	if t.Model != "" && t.Model != "auto" {
		req.Model = t.Model
	}
	return req
}

// abOutcome is one completed call. Err is kept so it can also be recorded in
// the run history.
type abOutcome struct {
	Resp *api.DecideResponse
	Err  error
	Wall time.Duration
	At   time.Time
}

type abSession struct {
	preset     string
	state      map[string]any
	questions  api.QuestionSet
	primaryURL string

	targets  [2]abTarget
	selected int // 0 = A, 1 = B
	results  [2]abOutcome
	ran      bool

	running bool
	seq     int
	ctx     context.Context
	cancel  context.CancelFunc

	status  string
	editing bool
	input   textinput.Model

	width, height int
}

func newABSession(d *draft, model, server string, h *api.Health, width, height int) *abSession {
	input := textinput.New()
	input.Placeholder = "http://127.0.0.1:8080"
	input.CharLimit = 2048
	input.Prompt = "url › "

	a := abTarget{URL: server, Model: model}
	b := abTarget{URL: server, Model: ""}
	if model == "" || model == "auto" {
		// Start with a meaningful pair: the routed family against a pinned one.
		if h != nil && len(h.Families) > 0 {
			b.Model = h.Families[0]
		}
	}
	e := &abSession{
		preset:     d.preset,
		state:      cloneState(d.state),
		questions:  forge.Clone(d.questions),
		primaryURL: server,
		targets:    [2]abTarget{a, b},
		input:      input,
	}
	e.resize(width, height)
	return e
}

func (e *abSession) resize(width, height int) {
	e.width, e.height = width, height
	e.input.Width = max(20, width-10)
}

func (m Model) openAB() (tea.Model, tea.Cmd) {
	d := m.currentDraft()
	if d == nil || len(d.questions) == 0 {
		m.notice = "A/B compare needs a preset with questions"
		return m, nil
	}
	server := ""
	if m.client != nil {
		server = m.client.BaseURL
	}
	m.ab = newABSession(d, m.model, server, m.health, m.width, m.height)
	m.mode = screenAB
	return m, nil
}

func abName(i int) string {
	if i == 1 {
		return "B"
	}
	return "A"
}

func (e *abSession) cycleModel(h *api.Health) {
	choices := []string{"auto"}
	if h != nil {
		for _, f := range h.Families {
			if f != "" {
				choices = append(choices, f)
			}
		}
	}
	cur := e.targets[e.selected].Model
	if cur == "" {
		cur = "auto"
	}
	idx := 0
	for i, c := range choices {
		if c == cur {
			idx = i
			break
		}
	}
	next := choices[(idx+1)%len(choices)]
	if next == "auto" {
		next = ""
	}
	e.targets[e.selected].Model = next
	e.status = fmt.Sprintf("%s model %s", abName(e.selected), familyLabel(next))
}

func (m *Model) startAB() tea.Cmd {
	e := m.ab
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
	e.ran = false
	e.results = [2]abOutcome{}
	e.editing = false
	e.input.Blur()
	e.status = "running both targets…"

	if m.client == nil {
		e.running = false
		e.cancel, e.ctx = nil, nil
		cancel()
		e.status = "no server configured"
		return nil
	}
	reqs := [2]api.DecideRequest{
		e.targets[0].request(e.state, e.questions),
		e.targets[1].request(e.state, e.questions),
	}
	return m.abRunCmd(ctx, e.seq, e.targets, reqs)
}

func (m *Model) cancelAB() bool {
	e := m.ab
	if e == nil || !e.running {
		return false
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.seq++ // ignore the in-flight results
	e.running, e.cancel, e.ctx = false, nil, nil
	e.ran = false
	e.status = "cancelled"
	return true
}

type abRunMsg struct {
	seq  int
	reqs [2]api.DecideRequest
	out  [2]abOutcome
	at   time.Time
}

// abRunCmd sends both requests concurrently. The two calls are independent, so
// one slow or failing target does not hide the other's result.
func (m Model) abRunCmd(parent context.Context, seq int, targets [2]abTarget, reqs [2]api.DecideRequest) tea.Cmd {
	base, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		var (
			out [2]abOutcome
			wg  sync.WaitGroup
		)
		at := now()
		for i := range targets {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c := *base
				c.BaseURL = strings.TrimRight(targets[i].URL, "/")
				ctx, cancel := context.WithTimeout(parent, to)
				defer cancel()
				start := time.Now()
				resp, err := c.Decide(ctx, reqs[i])
				wall := time.Since(start)
				if resp != nil {
					wall = resp.Wall
				}
				out[i] = abOutcome{Resp: resp, Err: err, Wall: wall, At: at}
			}(i)
		}
		wg.Wait()
		return abRunMsg{seq: seq, reqs: reqs, out: out, at: at}
	}
}

func (m Model) handleABRun(msg abRunMsg) (tea.Model, tea.Cmd) {
	e := m.ab
	if e == nil || msg.seq != e.seq {
		return m, nil // cancelled or superseded
	}
	e.running = false
	if e.cancel != nil {
		e.cancel()
	}
	e.cancel, e.ctx = nil, nil
	e.results = msg.out
	e.ran = true

	for i, o := range msg.out {
		kind := "preset"
		if len(msg.reqs[i].Questions) == 1 {
			kind = "single"
		}
		cold := false
		if o.Resp != nil {
			cold = m.tele.Observe(kind, o.Resp.Usage.LatencyMs, msg.at)
			m.observeRun(msg.reqs[i], o.Resp, msg.at, cold)
		}
		param := ""
		var apiErr *api.APIError
		if errors.As(o.Err, &apiErr) {
			param = apiErr.Param
		}
		m.recordHistory(decideMsg{
			preset:  e.preset,
			request: msg.reqs[i],
			resp:    o.Resp,
			err:     o.Err,
			at:      o.At,
		}, cold, param)
	}

	failed := 0
	for _, o := range msg.out {
		if o.Err != nil || o.Resp == nil {
			failed++
		}
	}
	switch {
	case failed == 2:
		e.status = "both targets failed"
	case failed == 1:
		e.status = "one target failed; the other result is shown"
	default:
		diffs := len(abDiffIndexes(
			abSummaries(e.questions, msg.out[0].Resp),
			abSummaries(e.questions, msg.out[1].Resp),
		))
		e.status = fmt.Sprintf("done · winners differ on %d of %d", diffs, len(e.questions))
	}
	return m, nil
}

func (m Model) handleABKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.ab
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
			e.status = "URL unchanged"
			return m, nil
		case "enter":
			raw := strings.TrimSpace(e.input.Value())
			if raw == "" {
				raw = e.primaryURL
			}
			if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
				e.status = "URL must start with http:// or https://"
				return m, nil
			}
			e.targets[e.selected].URL = strings.TrimRight(raw, "/")
			e.editing = false
			e.input.Blur()
			e.status = abName(e.selected) + " URL set"
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
		if m.cancelAB() {
			return m, nil
		}
		m.mode = screenBrowse
	case "tab", "shift+tab":
		e.selected = 1 - e.selected
		e.status = abName(e.selected) + " selected"
	case "m":
		if !e.running {
			e.cycleModel(m.health)
		}
	case "u":
		if !e.running {
			e.editing = true
			e.input.SetValue(e.targets[e.selected].URL)
			e.input.CursorEnd()
			return m, e.input.Focus()
		}
	case "r":
		if !e.running {
			e.targets[e.selected].URL = e.primaryURL
			e.status = abName(e.selected) + " URL reset"
		}
	case "enter":
		if !e.running {
			return m, m.startAB()
		}
	}
	return m, nil
}

// abSummaries aligns answers with the drafted questions. A missing answer gets
// an empty Label so the comparison can show it as a dash.
func abSummaries(qs api.QuestionSet, resp *api.DecideResponse) []lens.Summary {
	out := make([]lens.Summary, len(qs))
	for i, nq := range qs {
		out[i].Name = nq.Name
	}
	if resp == nil {
		return out
	}
	byName := make(map[string]lens.Summary, len(qs))
	for _, s := range summarize(qs, resp) {
		byName[s.Name] = s
	}
	for i, nq := range qs {
		if s, ok := byName[nq.Name]; ok {
			out[i] = s
		}
	}
	return out
}

// abDiffIndexes returns the questions whose winners differ. Missing answers on
// both sides count as agreement.
func abDiffIndexes(a, b []lens.Summary) []int {
	var out []int
	for i := range a {
		var other lens.Summary
		if i < len(b) {
			other = b[i]
		}
		if a[i].Label != other.Label {
			out = append(out, i)
		}
	}
	return out
}

func abAnswer(s lens.Summary) string {
	if s.Label == "" {
		return "—"
	}
	return fmt.Sprintf("%s %.0f%%", shortLabel(s.Label), 100*s.Prob)
}
