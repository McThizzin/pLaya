package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
)

// Messages produced by commands. Every network call runs inside a tea.Cmd so
// the UI never blocks on the server.
type (
	healthMsg struct {
		h   *api.Health
		err error
	}
	presetsMsg struct {
		ps  []api.Preset
		err error
	}
	warmupMsg struct {
		resp *api.DecideResponse
		err  error
		at   time.Time
	}
	decideMsg struct {
		preset    string
		questions api.QuestionSet // request order, for display
		resp      *api.DecideResponse
		err       error
		at        time.Time
	}
)

func (m Model) healthCmd() tea.Cmd {
	c, to := m.client, m.opts.Timeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		h, err := c.Health(ctx)
		return healthMsg{h: h, err: err}
	}
}

func (m Model) presetsCmd() tea.Cmd {
	c, to := m.client, m.opts.Timeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		ps, err := c.Presets(ctx)
		return presetsMsg{ps: ps, err: err}
	}
}

// warmupCmd sends one tiny request so the first real timing is not a cold one.
func (m Model) warmupCmd() tea.Cmd {
	c, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		resp, err := c.Warmup(ctx)
		return warmupMsg{resp: resp, err: err, at: now()}
	}
}

func (m Model) decideCmd(p api.Preset) tea.Cmd {
	c, to, now := m.client, m.opts.Timeout, m.opts.Now
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), to)
		defer cancel()
		resp, err := c.Decide(ctx, p.Request())
		return decideMsg{preset: p.Name, questions: p.Questions, resp: resp, err: err, at: now()}
	}
}
