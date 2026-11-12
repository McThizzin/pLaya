package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/batch"
	"playa/internal/forge"
	"playa/internal/lens"
)

const batchWorkers = 2

// batchSession is a file of states run through a snapshot of one draft's
// questions. The snapshot means later edits cannot change a run in progress.
type batchSession struct {
	preset    string
	stateKey  string
	model     string
	questions api.QuestionSet
	chunks    []api.QuestionSet

	path    string
	rows    []batch.Row
	status  string
	loadErr string

	threshold float64

	running     bool
	seq         int
	ctx         context.Context
	cancel      context.CancelFunc
	pending     []int
	inFlight    int
	runningRows map[int]bool

	cursor    int
	colOffset int
	sortCol   int // -1 keeps file order; otherwise a question index
	sortDesc  bool
	filter    batch.Filter
	detail    bool
	raw       bool

	prompt bool
	input  textinput.Model
	width  int
	height int
}

func newBatchSession(d *draft, model string, width, height int) *batchSession {
	input := textinput.New()
	input.Placeholder = "path/to/states.jsonl or states.csv"
	input.CharLimit = 4096
	input.Prompt = "file › "
	e := &batchSession{
		preset:      d.preset,
		stateKey:    d.stateKey,
		model:       model,
		questions:   forge.Clone(d.questions),
		path:        "",
		threshold:   0.5,
		sortCol:     -1,
		runningRows: map[int]bool{},
		input:       input,
	}
	e.chunks = batch.Chunks(e.questions, batch.DefaultChunkSize)
	e.resize(width, height)
	return e
}

func (e *batchSession) resize(width, height int) {
	e.width, e.height = width, height
	e.input.Width = max(20, width-8)
}

func (e *batchSession) cycleModel(h *api.Health) {
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

func (m Model) openBatch() (tea.Model, tea.Cmd) {
	d := m.currentDraft()
	if d == nil || len(d.questions) == 0 {
		m.notice = "batch needs a preset with questions"
		return m, nil
	}
	m.batch = newBatchSession(d, m.model, m.bodyW(), m.bodyH())
	m.mode = screenBatch
	return m, nil
}

type batchLoadMsg struct {
	path string
	rows []batch.Row
	err  error
}

type batchRowMsg struct {
	seq   int
	index int
	calls []batch.Call
}

func (m Model) batchLoadCmd(path string) tea.Cmd {
	stateKey := ""
	if m.batch != nil {
		stateKey = m.batch.stateKey
	}
	return func() tea.Msg {
		rows, err := batch.Load(path, stateKey)
		return batchLoadMsg{path: path, rows: rows, err: err}
	}
}

func (m Model) batchRowCmd(ctx context.Context, seq, index int, reqs []api.DecideRequest) tea.Cmd {
	c, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		calls := make([]batch.Call, 0, len(reqs))
		for i, req := range reqs {
			reqCtx, cancel := context.WithTimeout(ctx, to)
			resp, err := c.Decide(reqCtx, req)
			cancel()
			call := batch.Call{Chunk: i, Questions: req.Questions, At: now()}
			if err != nil {
				call.Err = err.Error()
				var apiErr *api.APIError
				if errors.As(err, &apiErr) {
					call.Param = apiErr.Param
				}
				calls = append(calls, call)
				break
			}
			call.Response, call.Wall = resp, resp.Wall
			calls = append(calls, call)
			if ctx.Err() != nil {
				break
			}
		}
		return batchRowMsg{seq: seq, index: index, calls: calls}
	}
}

// nextBatchCmd takes the next pending row and starts its chunk sequence.
func (m *Model) nextBatchCmd() tea.Cmd {
	e := m.batch
	idx := e.pending[0]
	e.pending = e.pending[1:]
	e.inFlight++
	e.runningRows[idx] = true
	row := &e.rows[idx]
	row.Calls = nil
	return m.batchRowCmd(e.ctx, e.seq, idx, batch.Requests(row.State, e.model, e.chunks))
}

// startBatch runs every row that is not complete. rerun resets all rows first.
func (m *Model) startBatch(rerun bool) tea.Cmd {
	e := m.batch
	if e == nil || e.running {
		return nil
	}
	if len(e.rows) == 0 {
		e.status = "open a file first (ctrl+o)"
		return nil
	}
	if rerun {
		for i := range e.rows {
			e.rows[i].Calls = nil
		}
	}
	var targets []int
	for i := range e.rows {
		if e.rows[i].Complete(len(e.chunks)) {
			continue
		}
		e.rows[i].Calls = nil
		targets = append(targets, i)
	}
	if len(targets) == 0 {
		e.status = "all rows complete; R re-runs everything"
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.seq++
	e.ctx, e.cancel = ctx, cancel
	e.running, e.pending = true, targets
	e.inFlight = 0
	e.runningRows = map[int]bool{}
	e.status = fmt.Sprintf("running %d rows…", len(targets))

	var cmds []tea.Cmd
	for e.inFlight < batchWorkers && len(e.pending) > 0 {
		cmds = append(cmds, m.nextBatchCmd())
	}
	return tea.Batch(cmds...)
}

func (m *Model) cancelBatch() bool {
	e := m.batch
	if e == nil || !e.running {
		return false
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.seq++ // ignore messages already in flight
	e.running, e.cancel, e.ctx = false, nil, nil
	e.pending, e.inFlight = nil, 0
	for idx := range e.runningRows {
		if idx >= 0 && idx < len(e.rows) {
			e.rows[idx].Calls = nil
		}
	}
	e.runningRows = map[int]bool{}
	e.status = "cancelled; r resumes the remaining rows"
	return true
}

func (m Model) handleBatchLoad(msg batchLoadMsg) (tea.Model, tea.Cmd) {
	if m.batch == nil {
		return m, nil
	}
	e := m.batch
	e.prompt = false
	e.input.Blur()
	if msg.err != nil {
		e.loadErr = msg.err.Error()
		e.status = ""
		return m, nil
	}
	e.path = msg.path
	e.rows = msg.rows
	e.loadErr = ""
	e.status = fmt.Sprintf("loaded %d states", len(msg.rows))
	e.cursor, e.colOffset = 0, 0
	e.sortCol, e.sortDesc = -1, false
	e.filter = batch.FilterAll
	e.detail, e.raw = false, false
	return m, nil
}

func (m Model) handleBatchRow(msg batchRowMsg) (tea.Model, tea.Cmd) {
	e := m.batch
	if e == nil || msg.seq != e.seq {
		return m, nil
	}
	if msg.index >= 0 && msg.index < len(e.rows) {
		row := &e.rows[msg.index]
		row.Calls = msg.calls
		for i := range row.Calls {
			c := &row.Calls[i]
			if c.Response != nil {
				c.Cold = m.tele.Observe("batch", c.Response.Usage.LatencyMs, c.At)
				req := api.DecideRequest{State: row.State, Questions: c.Questions}
				m.observeRun(req, c.Response, c.At, c.Cold)
			}
		}
	}
	delete(e.runningRows, msg.index)
	if e.inFlight > 0 {
		e.inFlight--
	}

	var cmd tea.Cmd
	if len(e.pending) > 0 {
		cmd = m.nextBatchCmd()
	}
	if e.inFlight == 0 && len(e.pending) == 0 {
		e.running = false
		if e.cancel != nil {
			e.cancel()
		}
		e.cancel, e.ctx = nil, nil
		s := batch.Summarize(e.rows, len(e.chunks), e.threshold)
		e.status = fmt.Sprintf("done: %d/%d rows", s.Complete, s.Rows)
		if s.Failed > 0 {
			e.status += fmt.Sprintf(", %d failed", s.Failed)
		}
	}
	return m, cmd
}

func (m Model) handleBatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e := m.batch
	if e == nil {
		m.mode = screenBrowse
		return m, nil
	}
	key := msg.String()

	if e.prompt {
		switch key {
		case "esc":
			e.prompt = false
			e.input.Blur()
			return m, nil
		case "enter":
			path := strings.TrimSpace(e.input.Value())
			e.prompt = false
			e.input.Blur()
			if path == "" {
				e.status = "no file given"
				return m, nil
			}
			e.status = "loading " + filepath.Base(path) + "…"
			return m, m.batchLoadCmd(path)
		}
		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		return m, cmd
	}

	if e.detail {
		switch key {
		case "esc":
			e.detail = false
		case "v":
			e.raw = !e.raw
		}
		return m, nil
	}

	order := m.batchOrder()
	switch key {
	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}
	case "down", "j":
		if e.cursor < len(order)-1 {
			e.cursor++
		}
	case "g", "home":
		e.cursor = 0
	case "G", "end":
		if len(order) > 0 {
			e.cursor = len(order) - 1
		}
	case "left", "h":
		if e.colOffset > 0 {
			e.colOffset--
		}
	case "right", "l":
		if e.colOffset < len(e.questions)-1 {
			e.colOffset++
		}
	case "enter":
		if len(order) > 0 {
			e.detail, e.raw = true, false
		}
	case "r":
		return m, m.startBatch(false)
	case "F":
		e.filter = e.filter.Next()
		e.cursor = 0
	case "s":
		e.sortCol++
		if e.sortCol >= len(e.questions) {
			e.sortCol, e.sortDesc = -1, false
		} else {
			e.sortDesc = true
		}
		e.cursor = 0
	case "S":
		e.sortDesc = !e.sortDesc
		e.cursor = 0
	case "t":
		e.cycleThreshold()
	case "ctrl+o":
		if !e.running {
			e.prompt = true
			e.input.SetValue(e.path)
			e.input.CursorEnd()
			return m, e.input.Focus()
		}
	case "x":
		m.exportBatch()
	}
	return m, nil
}

func (e *batchSession) cycleThreshold() {
	switch {
	case e.threshold < 0.6:
		e.threshold = 0.7
	case e.threshold < 0.8:
		e.threshold = 0.9
	default:
		e.threshold = 0.5
	}
	e.status = fmt.Sprintf("noul positive threshold %.2f", e.threshold)
}

func (m *Model) exportBatch() {
	e := m.batch
	if e == nil || len(e.rows) == 0 {
		return
	}
	jsonl, csv := batch.ExportPaths(e.path)
	if err := batch.WriteJSONL(jsonl, e.rows); err != nil {
		e.loadErr = "export: " + err.Error()
		return
	}
	if err := batch.WriteCSV(csv, e.rows, e.questions); err != nil {
		e.loadErr = "export: " + err.Error()
		return
	}
	e.loadErr = ""
	e.status = fmt.Sprintf("exported %s and %s", filepath.Base(jsonl), filepath.Base(csv))
}

// batchOrder returns the file indexes after filtering and sorting.
func (m Model) batchOrder() []int {
	e := m.batch
	if e == nil {
		return nil
	}
	idxs := make([]int, 0, len(e.rows))
	for i := range e.rows {
		if batch.Match(e.rows[i], e.filter) {
			idxs = append(idxs, i)
		}
	}
	if e.sortCol < 0 || e.sortCol >= len(e.questions) {
		return idxs
	}
	q := e.questions[e.sortCol]
	sort.SliceStable(idxs, func(a, b int) bool {
		pa, oka := rowQuestionProb(e.rows[idxs[a]], q)
		pb, okb := rowQuestionProb(e.rows[idxs[b]], q)
		if oka != okb {
			return oka // rows with an answer come first even when descending
		}
		if !oka {
			return idxs[a] < idxs[b]
		}
		if pa != pb {
			if e.sortDesc {
				return pa > pb
			}
			return pa < pb
		}
		return idxs[a] < idxs[b]
	})
	return idxs
}

func rowQuestionProb(r batch.Row, q api.NamedQuestion) (float64, bool) {
	a, ok := r.Answers()[q.Name]
	if !ok {
		return 0, false
	}
	return lens.Summarize(q.Name, a, lens.Options{}).Prob, true
}

func (m Model) selectedBatch() (batch.Row, int, bool) {
	e := m.batch
	if e == nil {
		return batch.Row{}, 0, false
	}
	order := m.batchOrder()
	if len(order) == 0 {
		return batch.Row{}, 0, false
	}
	i := clamp(e.cursor, 0, len(order)-1)
	idx := order[i]
	return e.rows[idx], idx, true
}
