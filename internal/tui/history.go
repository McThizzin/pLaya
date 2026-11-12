package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/history"
	"playa/internal/lengthguard"
	"playa/internal/lens"
)

// summarize builds the Decision Lens rows for a request/response pair.
func summarize(qs api.QuestionSet, resp *api.DecideResponse) []lens.Summary {
	if resp == nil {
		return nil
	}
	var out []lens.Summary
	for _, nq := range qs {
		if a, ok := resp.Answers[nq.Name]; ok {
			out = append(out, lens.Summarize(nq.Name, a, lens.Options{}))
		}
	}
	return out
}

// recordHistory stores the completed run and persists the store when a path
// is configured. It is called only for messages that matched the active run.
func (m *Model) recordHistory(msg decideMsg, cold bool, param string) {
	if m.history == nil {
		m.history = history.New(history.DefaultMax)
	}
	e := history.Entry{
		At:       msg.at,
		Preset:   msg.preset,
		Model:    msg.request.Model,
		Request:  msg.request,
		Response: msg.resp,
		Err:      "",
		Param:    param,
		Cold:     cold,
		ReplayOf: msg.replayOf,
	}
	if msg.err != nil {
		e.Err = msg.err.Error()
	} else if msg.resp != nil {
		e.Raw = append([]byte(nil), msg.resp.Raw...)
		e.WallMs = float64(msg.resp.Wall) / float64(time.Millisecond)
	}
	m.history.Add(e)
	if m.mode == screenHistory {
		m.historyCursor = 0
		m.historyRaw = false
	}
	m.persistHistory()
}

func (m *Model) persistHistory() {
	if m.historyPath == "" {
		return
	}
	if err := m.history.Save(m.historyPath); err != nil {
		m.historyErr = "history: " + err.Error()
		return
	}
	m.historyErr = ""
}

func (m Model) openHistory() (tea.Model, tea.Cmd) {
	if m.history == nil {
		m.history = history.New(history.DefaultMax)
	}
	m.mode = screenHistory
	m.historyCursor = 0
	m.historyRaw = false
	return m, nil
}

// reloadHistory re-reads the history file. With no path the store is already
// in memory, so only the cursor is normalized.
func (m *Model) reloadHistory() {
	if m.historyPath == "" {
		m.historyErr = ""
		return
	}
	st, err := history.Load(m.historyPath, history.DefaultMax)
	m.history = st
	if err != nil {
		m.historyErr = "history: " + err.Error()
		return
	}
	m.historyErr = ""
	if n := len(st.List()); m.historyCursor >= n {
		m.historyCursor = max(0, n-1)
	}
}

func (m Model) selectedHistory() (history.Entry, bool) {
	if m.history == nil {
		return history.Entry{}, false
	}
	list := m.history.List()
	if len(list) == 0 {
		return history.Entry{}, false
	}
	return list[clamp(m.historyCursor, 0, len(list)-1)], true
}

// cancelRun stops the in-flight request and invalidates its message.
func (m *Model) cancelRun() bool {
	if !m.running {
		return false
	}
	if m.runCancel != nil {
		m.runCancel()
	}
	m.running, m.runCancel = false, nil
	m.runSeq++
	m.runErr, m.runErrParam = "", ""
	return true
}

func (m Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.history == nil {
		m.history = history.New(history.DefaultMax)
	}
	entries := m.history.List()

	switch key {
	case "up", "k":
		if m.historyCursor > 0 {
			m.historyCursor--
		}
	case "down", "j":
		if m.historyCursor < len(entries)-1 {
			m.historyCursor++
		}
	case "g", "home":
		m.historyCursor = 0
	case "G", "end":
		if len(entries) > 0 {
			m.historyCursor = len(entries) - 1
		}
	case "v":
		if !m.running {
			m.historyRaw = !m.historyRaw
		}
	case "enter":
		if m.running || len(entries) == 0 {
			return m, nil
		}
		e := entries[clamp(m.historyCursor, 0, len(entries)-1)]
		m.running, m.runErr, m.runErrParam = true, "", ""
		m.runTab = 7
		m.runSeq++
		ctx, cancel := context.WithCancel(context.Background())
		m.runCancel = cancel
		m.notice = ""
		return m, m.decideCmd(ctx, e.Preset, history.CloneRequest(e.Request), m.runSeq, e.ID)
	}
	return m, nil
}

// prettyJSON re-indents a stored raw response.
func prettyJSON(raw []byte) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func entryTruncated(e history.Entry) bool {
	if e.Response == nil || len(e.Request.Questions) == 0 {
		return false
	}
	return lengthguard.Truncated(e.Response.Usage.InputTokens, len(e.Request.Questions), lengthguard.Cap(e.Response.Family))
}
