package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Decide endpoints. /v1/systemone is the documented TypeSafe-compatible one;
// /v1/decide returned the same response in the one case checked.
const (
	EndpointSystemOne = "/v1/systemone"
	EndpointDecide    = "/v1/decide"
)

const maxBody = 16 << 20 // 16 MiB

// Client talks to a running `laya serve`. It has no built-in timeout: callers
// pass a context, because the first call to a family can load a model and take
// over a second.
type Client struct {
	BaseURL  string
	APIKey   string // sent as "Authorization: Bearer <key>" when non-empty
	Endpoint string // EndpointSystemOne (default) or EndpointDecide
	HTTP     *http.Client
}

// NewClient returns a client for baseURL such as "http://127.0.0.1:8080".
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Endpoint: EndpointSystemOne,
		HTTP:     &http.Client{},
	}
}

// APIError is a non-2xx response. Param is a field path such as
// "questions.urgency.criteria" and can be mapped onto a form field.
type APIError struct {
	Status  int
	Code    string
	Message string
	Param   string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "laya: %s", e.Message)
	if e.Param != "" {
		fmt.Fprintf(&b, " (%s)", e.Param)
	}
	if e.Code != "" {
		fmt.Fprintf(&b, " [%s, HTTP %d]", e.Code, e.Status)
	} else {
		fmt.Fprintf(&b, " [HTTP %d]", e.Status)
	}
	return b.String()
}

func parseError(status int, body []byte) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return &APIError{Status: status, Code: env.Error.Code, Message: env.Error.Message, Param: env.Error.Param}
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{Status: status, Code: "http_error", Message: msg}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return raw, parseError(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return raw, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return raw, nil
}

// Health calls GET /health.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	var h Health
	if _, err := c.do(ctx, http.MethodGet, "/health", nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Models calls GET /v1/models.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	var env struct {
		Models []ModelInfo `json:"models"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/models", nil, &env); err != nil {
		return nil, err
	}
	return env.Models, nil
}

// Presets calls GET /v1/presets (a bare JSON array).
func (c *Client) Presets(ctx context.Context) ([]Preset, error) {
	var ps []Preset
	if _, err := c.do(ctx, http.MethodGet, "/v1/presets", nil, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// Decide sends a decide request. The returned response carries the raw body
// and the client-side wall time.
func (c *Client) Decide(ctx context.Context, req DecideRequest) (*DecideResponse, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = EndpointSystemOne
	}
	start := time.Now()
	var out DecideResponse
	raw, err := c.do(ctx, http.MethodPost, endpoint, req, &out)
	if err != nil {
		return nil, err
	}
	out.Raw = raw
	out.Wall = time.Since(start)
	return &out, nil
}

// Warmup sends one tiny noul question. The first call after server start or an
// idle pause is slow (250-650 ms observed), so do this before showing timings.
func (c *Client) Warmup(ctx context.Context) (*DecideResponse, error) {
	return c.Decide(ctx, DecideRequest{
		State: map[string]any{"document": "hello"},
		Questions: QuestionSet{{
			Name:     "warmup",
			Question: Question{Type: TypeNoul, Instructions: "Is this a greeting?"},
		}},
	})
}
