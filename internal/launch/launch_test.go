package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "laya-fake")
	script := "#!/bin/sh\necho 'fake laya starting'\nsleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestConfigArgs(t *testing.T) {
	cfg := Config{
		ModelsDir: "/models/Laya",
		Port:      8080,
		Device:    "vulkan",
		MaxBatch:  6,
		Family:    "english",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.Args(), " ")
	for _, want := range []string{
		"serve", "--models-dir /models/Laya", "--port 8080", "--device vulkan",
		"--max-batch 6", "--family english",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
	if cfg.URL() != "http://127.0.0.1:8080" {
		t.Errorf("URL = %q", cfg.URL())
	}

	minimal := Config{ModelsDir: "m", Port: 9000}
	args := strings.Join(minimal.Args(), " ")
	if strings.Contains(args, "--max-batch") || strings.Contains(args, "--family") {
		t.Errorf("optional flags leaked into %q", args)
	}
	if !strings.Contains(args, "--device vulkan") {
		t.Errorf("device should default to vulkan: %q", args)
	}
	if minimal.normalized().Path != "laya" {
		t.Errorf("binary should default to laya")
	}
}

func TestConfigValidate(t *testing.T) {
	bad := []Config{
		{Port: 8080},
		{ModelsDir: "m", Port: 0},
		{ModelsDir: "m", Port: 70000},
		{ModelsDir: "m", Port: 8080, MaxBatch: -1},
		{ModelsDir: "m", Port: 8080, Family: "kev-0.8b"},
	}
	for _, cfg := range bad {
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate(%+v) passed", cfg)
		}
	}
	if err := (Config{ModelsDir: "m", Port: 8080, Family: "auto"}).Validate(); err != nil {
		t.Errorf("auto family should validate: %v", err)
	}
}

func TestManagerStartStop(t *testing.T) {
	cfg := Config{
		Path:      fakeBinary(t),
		ModelsDir: t.TempDir(),
		Port:      8080,
		Device:    "vulkan",
	}
	m := NewManager(cfg)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the child to run", m.Running)
	if m.PID() <= 0 {
		t.Fatalf("PID = %d", m.PID())
	}
	waitFor(t, "startup output", func() bool {
		return strings.Contains(m.Logs(), "fake laya starting")
	})
	if err := m.Start(); err == nil {
		t.Error("starting twice should fail")
	}

	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	if m.Running() || m.PID() != 0 {
		t.Errorf("after stop: running=%v pid=%d", m.Running(), m.PID())
	}
	if m.State() != Exited {
		t.Errorf("state = %v, want exited", m.State())
	}
	if err := m.Stop(); err != nil {
		t.Errorf("second stop: %v", err)
	}
	if !strings.Contains(m.Logs(), "fake laya starting") {
		t.Error("logs were lost")
	}
}

func TestManagerMissingBinary(t *testing.T) {
	m := NewManager(Config{
		Path:      filepath.Join(t.TempDir(), "does-not-exist"),
		ModelsDir: "m",
		Port:      8080,
	})
	if err := m.Start(); err == nil {
		t.Fatal("starting a missing binary should fail")
	}
	if m.Running() {
		t.Error("a failed start must not leave a running state")
	}
}

func TestLogsAreBounded(t *testing.T) {
	m := NewManager(Config{})
	m.logMax = 16
	m.appendLog([]byte("0123456789abcdefghij"))
	if got := m.Logs(); len(got) > 16 {
		t.Errorf("logs kept %d bytes, want at most 16", len(got))
	}
}
