package check

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"playa/internal/api"
)

func serve(t *testing.T, routes map[string]string, status map[string]int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, file := range routes {
		path, file := path, file
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join("..", "api", "testdata", file))
			if err != nil {
				t.Error(err)
			}
			if code := status[path]; code != 0 {
				w.WriteHeader(code)
			}
			_, _ = w.Write(b)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestRunAllSteps(t *testing.T) {
	srv := serve(t, map[string]string{
		"/health": "health.json", "/v1/models": "models.json", "/v1/presets": "presets.json",
		"/v1/systemone": "noul.json",
	}, nil)
	var out bytes.Buffer
	if err := Run(ctx(t), api.NewClient(srv.URL), &out); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"device vulkan:0",
		"9 names (4 families, 5 aliases)",
		"10: email, triage, guard",
		"warmup",
		"billing: YES 92%",
		`route "forced english"`,
		"47 tokens",
		"\nok\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRunStopsAtFirstFailure(t *testing.T) {
	srv := serve(t, map[string]string{"/health": "health.json", "/v1/models": "models.json"}, nil)
	var out bytes.Buffer
	err := Run(ctx(t), api.NewClient(srv.URL), &out)
	if err == nil || !strings.HasPrefix(err.Error(), "presets:") {
		t.Fatalf("want a presets error, got %v", err)
	}
	if strings.Contains(out.String(), "\nok") {
		t.Error("must not print ok after a failure")
	}
}

func TestRunReportsAPIErrors(t *testing.T) {
	srv := serve(t, map[string]string{
		"/health": "health.json", "/v1/models": "models.json", "/v1/presets": "presets.json",
		"/v1/systemone": "error_score.json",
	}, map[string]int{"/v1/systemone": 400})
	err := Run(ctx(t), api.NewClient(srv.URL), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "warmup:") || !strings.Contains(err.Error(), "nonempty criteria list") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunUnreachableServer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more
	err := Run(ctx(t), api.NewClient(url), &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), "health:") {
		t.Fatalf("err = %v", err)
	}
}
