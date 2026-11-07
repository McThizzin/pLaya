//go:build unix

package launch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// TestStopTakesDownTheProcessGroup checks that a helper spawned by laya does
// not survive Stop, and that a manager cannot spawn again after it is closed.
func TestStopTakesDownTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pids")
	script := "#!/bin/sh\n" +
		"echo 'fake laya starting'\n" +
		"sleep 60 &\n" +
		fmt.Sprintf("echo \"$$ $!\" > %q\n", pidFile) +
		"wait\n"
	path := filepath.Join(dir, "laya-fake")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewManager(Config{Path: path, ModelsDir: dir, Port: 8080})
	m.grace = 100 * time.Millisecond
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pid file", func() bool {
		_, err := os.Stat(pidFile)
		return err == nil
	})
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		t.Fatalf("pid file = %q", raw)
	}
	shellPID, _ := strconv.Atoi(fields[0])
	helperPID, _ := strconv.Atoi(fields[1])
	if !alive(shellPID) || !alive(helperPID) {
		t.Fatalf("expected both processes alive: shell=%d helper=%d", shellPID, helperPID)
	}

	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the helper to exit", func() bool {
		return !alive(shellPID) && !alive(helperPID)
	})

	if err := m.Start(); err == nil {
		t.Error("a closed manager must refuse to start another child")
	}
}
