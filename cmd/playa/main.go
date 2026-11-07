// Command playa is a terminal client for `laya serve`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/bench"
	"playa/internal/check"
	"playa/internal/history"
	"playa/internal/langdetect"
	"playa/internal/launch"
	"playa/internal/tui"
)

func main() {
	var (
		url       = flag.String("url", envOr("LAYA_URL", "http://127.0.0.1:8080"), "base URL of `laya serve` (env LAYA_URL)")
		apiKey    = flag.String("api-key", firstEnv("LAYA_API_KEY", "TYPESAFE_API_KEY"), "Bearer token (env LAYA_API_KEY or TYPESAFE_API_KEY)")
		endpoint  = flag.String("endpoint", "systemone", "decide endpoint: systemone or decide")
		timeout   = flag.Duration("timeout", 30*time.Second, "per-request timeout; the first call to a family can load a model")
		noWarmup  = flag.Bool("no-warmup", false, "skip the warm-up request after connecting")
		layaPath  = flag.String("laya", envOr("LAYA_BIN", "laya"), "path to the laya CLI for the route preview and Launchpad (env LAYA_BIN)")
		modelsDir = flag.String("models-dir", envOr("LAYA_MODELS_DIR", launch.DefaultModelsDir()), "models dir for the Launchpad's `laya serve` (env LAYA_MODELS_DIR)")
		device    = flag.String("device", envOr("LAYA_DEVICE", "vulkan"), "device for the Launchpad's `laya serve` (env LAYA_DEVICE)")
		doCheck   = flag.Bool("check", false, "run a plain-text connectivity check and exit (no TUI)")
	)
	flag.Parse()

	c := api.NewClient(*url)
	c.APIKey = *apiKey
	switch *endpoint {
	case "systemone":
		c.Endpoint = api.EndpointSystemOne
	case "decide":
		c.Endpoint = api.EndpointDecide
	default:
		fmt.Fprintf(os.Stderr, "playa: unknown -endpoint %q (want systemone or decide)\n", *endpoint)
		os.Exit(2)
	}

	if *doCheck {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		if err := check.Run(ctx, c, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "playa:", err)
			os.Exit(1)
		}
		return
	}

	historyPath, _ := history.DefaultPath()
	benchPath, _ := bench.DefaultPath()
	model := tui.New(c, tui.Options{
		Warmup:      !*noWarmup,
		Timeout:     *timeout,
		HistoryPath: historyPath,
		BenchPath:   benchPath,
		LayaPath:    *layaPath,
		ModelsDir:   *modelsDir,
		Device:      *device,
		Detect: func(ctx context.Context, text string) (langdetect.Preview, error) {
			return langdetect.Detect(ctx, *layaPath, text)
		},
	})
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Quit through Bubble Tea so the final model reaches Close and a spawned
	// server is stopped. SIGINT normally arrives as a ctrl+c key in raw mode;
	// this path matters for SIGTERM.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-sigCtx.Done():
			p.Quit()
		case <-done:
		}
	}()

	final, err := p.Run()
	close(done)
	if fm, ok := final.(tui.Model); ok {
		fm.Close()
	} else {
		model.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "playa:", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
