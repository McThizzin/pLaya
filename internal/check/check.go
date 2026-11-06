// Package check runs a plain-text connectivity check against a laya server.
// It exercises the same client the TUI uses, so it is the quickest way to see
// whether a URL, API key or endpoint is wrong.
package check

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"playa/internal/api"
	"playa/internal/lens"
)

// Sample is the request sent by the final step: the same sentence used in the
// manual curl tests, so results can be compared directly.
const Sample = "I was charged twice. Please fix this ASAP."

// Run checks health, models, presets, warmup and one real decide call, printing
// one line per step to w. It stops at the first failure and returns its error.
func Run(ctx context.Context, c *api.Client, w io.Writer) error {
	fmt.Fprintf(w, "laya server: %s (endpoint %s)\n", c.BaseURL, endpoint(c))

	h, err := c.Health(ctx)
	if err != nil {
		return fmt.Errorf("health: %w", err)
	}
	fmt.Fprintf(w, "  health   %s · device %s · families %s\n", h.Status, h.Device, strings.Join(h.Families, ", "))

	ms, err := c.Models(ctx)
	if err != nil {
		return fmt.Errorf("models: %w", err)
	}
	aliases := len(ms) - len(h.Families)
	if aliases < 0 {
		aliases = 0
	}
	fmt.Fprintf(w, "  models   %d names (%d families, %d aliases)\n", len(ms), len(h.Families), aliases)

	ps, err := c.Presets(ctx)
	if err != nil {
		return fmt.Errorf("presets: %w", err)
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	fmt.Fprintf(w, "  presets  %d: %s\n", len(ps), strings.Join(names, ", "))

	wr, err := c.Warmup(ctx)
	if err != nil {
		return fmt.Errorf("warmup: %w", err)
	}
	fmt.Fprintf(w, "  warmup   %s · %.0f ms server · %s wall (first call; slower than steady state)\n",
		wr.Family, wr.Usage.LatencyMs, wr.Wall.Round(time.Millisecond))

	resp, err := c.Decide(ctx, api.DecideRequest{
		State: map[string]any{"document": Sample},
		Questions: api.QuestionSet{{
			Name:     "billing",
			Question: api.Question{Type: api.TypeNoul, Instructions: "Is this about billing?"},
		}},
	})
	if err != nil {
		return fmt.Errorf("decide: %w", err)
	}
	a, ok := resp.Answers["billing"]
	if !ok {
		return fmt.Errorf("decide: response has no answer for %q", "billing")
	}
	s := lens.Summarize("billing", a, lens.Options{})
	fmt.Fprintf(w, "  decide   billing: %s %.0f%% (certainty %.2f) · %s · route %q · %d tokens · %.0f ms server · %s wall\n",
		s.Label, 100*s.Prob, s.Certainty, resp.Family, resp.Route, resp.Usage.InputTokens,
		resp.Usage.LatencyMs, resp.Wall.Round(time.Millisecond))
	fmt.Fprintln(w, "ok")
	return nil
}

func endpoint(c *api.Client) string {
	if c.Endpoint == "" {
		return api.EndpointSystemOne
	}
	return c.Endpoint
}
