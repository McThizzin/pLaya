package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeLaya is a child that stays alive and prints one line, so Start/Stop and
// the log tail can be exercised without a GPU or real model files.
func fakeLaya(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "laya-fake")
	script := "#!/bin/sh\necho 'fake laya starting'\nsleep 60\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// launchStep runs commands until the launch flow goes idle. Background log
// ticks are dropped so a running child cannot keep the test alive.
func launchStep(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = launchStep(t, m, c)
		}
		return m
	}
	if _, ok := msg.(launchLogMsg); ok {
		return m
	}
	next, nextCmd := m.Update(msg)
	return launchStep(t, next.(Model), nextCmd)
}

func waitUntil(t *testing.T, what string, ok func() bool) {
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

func TestLaunchStartStopAndAttach(t *testing.T) {
	f := newFake(t)
	external := newFake(t)
	m := connected(t, f)

	m, _ = press(m, "alt+8")
	if m.mode != screenLaunch || m.launch == nil {
		t.Fatal("L should open the Launchpad")
	}
	if m.opts.ModelsDir == "" || m.opts.Device != "vulkan" || m.opts.LayaPath != "laya" {
		t.Errorf("Launchpad defaults = %+v", m.opts)
	}

	// The fake binary does not bind the port; the primary test server answers
	// /health so the ready poll can succeed.
	m.launch.fields[launchBinary] = fakeLaya(t)
	m.launch.fields[launchModels] = t.TempDir()
	m.launch.fields[launchPort] = strconv.Itoa(portFromURL(f.URL))

	m, cmd := press(m, "s")
	if !m.launch.starting {
		t.Fatal("s should start the launch flow")
	}
	m = launchStep(t, m, cmd)
	if m.launch.starting {
		t.Fatalf("launch is still polling: %q", m.launch.status)
	}
	if m.launcher == nil || !m.launcher.Running() {
		t.Fatalf("spawned process is not running: %q", m.launch.status)
	}
	if pid := m.launcher.PID(); pid <= 0 {
		t.Errorf("PID = %d", pid)
	}
	if !strings.Contains(m.launch.status, "server ready") {
		t.Errorf("status = %q", m.launch.status)
	}
	if m.client.BaseURL != "http://127.0.0.1:"+m.launch.fields[launchPort] {
		t.Errorf("client URL = %q", m.client.BaseURL)
	}
	waitUntil(t, "the child log", func() bool {
		return strings.Contains(m.launcher.Logs(), "fake laya starting")
	})
	out := plain(m.View())
	for _, want := range []string{"Launchpad", "spawned pid", "running", "fake laya starting", "s start"} {
		if !strings.Contains(out, want) {
			t.Errorf("launch view missing %q:\n%s", want, out)
		}
	}

	m, cmd = press(m, "x")
	m = launchStep(t, m, cmd)
	if m.launcher.Running() || m.launcher.PID() != 0 {
		t.Errorf("stop left running=%v pid=%d", m.launcher.Running(), m.launcher.PID())
	}
	if !strings.Contains(m.launch.status, "stopped") {
		t.Errorf("status after stop = %q", m.launch.status)
	}

	// Attach mode: point at the second fake server and make sure x never stops
	// a server this client did not spawn.
	m.launch.fields[launchPort] = strconv.Itoa(portFromURL(external.URL))
	m, cmd = press(m, "a")
	m = launchStep(t, m, cmd)
	if m.client.BaseURL != external.URL {
		t.Errorf("attach URL = %q, want %q", m.client.BaseURL, external.URL)
	}
	if m.health == nil || m.status != statusReady {
		t.Errorf("attach health = %+v status = %v", m.health, m.status)
	}
	if !strings.Contains(m.launch.status, "attached") {
		t.Errorf("attach status = %q", m.launch.status)
	}
	m, cmd = press(m, "x")
	m = launchStep(t, m, cmd)
	if m.launcher.Running() {
		t.Error("x must not leave a spawned process behind")
	}
	if !strings.Contains(m.launch.status, "no spawned process") {
		t.Errorf("x in attach mode should be explicit, got %q", m.launch.status)
	}
	if _, err := external.Client().Get(external.URL + "/health"); err != nil {
		t.Errorf("external server was disturbed: %v", err)
	}

	m.Close()
}

func TestLaunchWarmAllFamilies(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	before := f.decideCount()
	m, _ = press(m, "alt+8")
	m, cmd := press(m, "w")
	m = launchStep(t, m, cmd)

	want := len(m.health.Families)
	if !strings.Contains(m.launch.status, "warmed") {
		t.Fatalf("warm status = %q", m.launch.status)
	}
	if got := f.decideCount() - before; got != want {
		t.Errorf("warm calls = %d, want %d", got, want)
	}
	if m.launcher != nil {
		t.Error("warming must not spawn a process")
	}
	m.Close()
}

func TestLaunchRejectsBadConfigAndCancelLeaves(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "alt+8")

	m.launch.fields[launchModels] = ""
	m, cmd := press(m, "s")
	if cmd != nil || m.launcher != nil {
		t.Fatal("an invalid config must not spawn anything")
	}
	if !strings.Contains(m.launch.status, "models dir") {
		t.Errorf("status = %q", m.launch.status)
	}

	m.launch.fields[launchModels] = t.TempDir()
	m.launch.fields[launchPort] = "not-a-port"
	m, cmd = press(m, "s")
	if cmd != nil || m.launcher != nil {
		t.Fatal("a non-numeric port must not spawn anything")
	}
	if !strings.Contains(m.launch.status, "not a number") {
		t.Errorf("status = %q", m.launch.status)
	}

	m.launch.fields[launchPort] = "8080"
	m.launch.fields[launchBinary] = fakeLaya(t)
	m, cmd = press(m, "s")
	if cmd == nil || m.launcher == nil {
		t.Fatal("a valid config should start")
	}
	// Esc while waiting cancels the UI flow but keeps the process owned by
	// the manager. The Server tab stays visible.
	m, _ = press(m, "esc")
	if m.mode != screenLaunch || m.launch.starting {
		t.Fatalf("esc mode=%v starting=%v", m.mode, m.launch.starting)
	}
	if !strings.Contains(m.launch.status, "wait cancelled") {
		t.Errorf("esc status = %q", m.launch.status)
	}
	m = launchStep(t, m, cmd)
	seq := m.launch.seq
	next, _ := m.Update(launchHealthMsg{seq: seq, err: os.ErrDeadlineExceeded})
	m = next.(Model)
	if m.launcher == nil || !m.launcher.Running() {
		t.Fatal("the spawned process should still be owned and running")
	}
	if !strings.Contains(m.launch.status, "start") {
		t.Errorf("a stale health result changed the status to %q", m.launch.status)
	}

	m.Close()
	if m.launcher.Running() {
		t.Error("Close must stop the spawned process")
	}
}

func TestLaunchStopBeforeStartRunsAndHealthTimeout(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)

	// x before the start command executes must close the manager instead of
	// letting the pending Start spawn an orphan.
	m, _ = press(m, "alt+8")
	m.launch.fields[launchBinary] = fakeLaya(t)
	m.launch.fields[launchModels] = t.TempDir()
	m.launch.fields[launchPort] = strconv.Itoa(portFromURL(f.URL))
	m, startCmd := press(m, "s")
	m, stopCmd := press(m, "x")
	m = launchStep(t, m, stopCmd)
	m = launchStep(t, m, startCmd)
	if m.launcher == nil {
		t.Fatal("the manager should still be tracked")
	}
	if m.launcher.Running() {
		t.Error("x during startup must prevent the child from spawning")
	}
	if !strings.Contains(m.launch.status, "stopped") {
		t.Errorf("status = %q", m.launch.status)
	}

	// The ready poll gives up after launchPollLimit attempts but leaves the
	// owned child running so the log can be inspected or x can stop it.
	m, _ = press(m, "s")
	if m.launcher == nil {
		t.Fatal("s should create a manager")
	}
	if err := m.launcher.Start(); err != nil {
		t.Fatal(err)
	}
	m.launch.starting = true
	m.launch.polls = launchPollLimit - 1
	next, _ := m.Update(launchHealthMsg{seq: m.launch.seq, err: os.ErrDeadlineExceeded})
	m = next.(Model)
	if m.launch.starting {
		t.Error("the poll limit should end the wait")
	}
	if !strings.Contains(m.launch.status, "did not become ready") {
		t.Errorf("status = %q", m.launch.status)
	}
	if m.launcher == nil || !m.launcher.Running() {
		t.Fatal("a timed-out wait must not lose the owned child")
	}
	m.Close()
}

func TestLaunchViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "alt+8")
	m.launch.status = strings.Repeat("a long status ", 12)
	m.launch.editing = true
	m.launch.selected = int(launchModels)
	m.launch.input.SetValue(strings.Repeat("m", 200))

	for _, size := range []struct{ w, h int }{{100, 30}, {120, 40}, {140, 50}, {110, 32}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: launch view has %d lines:\n%s", size.w, size.h, len(lines), out)
		}
	}
}
