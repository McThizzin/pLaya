// Package launch starts and stops a `laya serve` child process.
//
// The manager owns only the process it spawned. An already-running server is
// left alone (attach mode), which is why Stop never searches for laya by name.
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// StopGrace is how long Stop waits after an interrupt before killing.
const StopGrace = 3 * time.Second

// Config describes one `laya serve` invocation.
type Config struct {
	Path      string // laya binary; "laya" when empty
	ModelsDir string
	Port      int
	Device    string // "vulkan" when empty
	MaxBatch  int    // 0 keeps the GGUF default
	Family    string // "" or "auto" lets the server route
}

// DefaultModelsDir is the directory used by the documented serve command.
func DefaultModelsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "models", "Laya", "models")
}

func (c Config) normalized() Config {
	if c.Path == "" {
		c.Path = "laya"
	}
	if c.Device == "" {
		c.Device = "vulkan"
	}
	return c
}

// Validate checks the values the server cannot fix for us.
func (c Config) Validate() error {
	c = c.normalized()
	if c.ModelsDir == "" {
		return errors.New("models dir is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d is outside 1..65535", c.Port)
	}
	if c.MaxBatch < 0 {
		return errors.New("max batch cannot be negative")
	}
	switch c.Family {
	case "", "auto", "english", "multilingual", "typed-decisions":
	default:
		return fmt.Errorf("--family does not accept %q", c.Family)
	}
	return nil
}

// Args builds the argument list after "serve".
func (c Config) Args() []string {
	c = c.normalized()
	args := []string{
		"serve",
		"--models-dir", c.ModelsDir,
		"--port", strconv.Itoa(c.Port),
		"--device", c.Device,
	}
	if c.MaxBatch > 0 {
		args = append(args, "--max-batch", strconv.Itoa(c.MaxBatch))
	}
	if c.Family != "" && c.Family != "auto" {
		args = append(args, "--family", c.Family)
	}
	return args
}

// URL is the HTTP base URL for this configuration.
func (c Config) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.Port)
}

// State is the child process state.
type State int

const (
	Stopped State = iota
	Running
	Exited
)

func (s State) String() string {
	switch s {
	case Running:
		return "running"
	case Exited:
		return "exited"
	}
	return "stopped"
}

// Manager owns one child process and its bounded output log.
type Manager struct {
	cfg    Config
	logMax int
	grace  time.Duration

	mu     sync.Mutex
	cmd    *exec.Cmd
	state  State
	err    error
	pid    int
	done   chan struct{}
	closed bool // Stop was called; a later Start must not spawn

	logMu sync.Mutex
	log   []byte
}

// NewManager returns a manager for cfg. It does not start anything.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg, state: Stopped, logMax: 64 << 10, grace: StopGrace}
}

// Config returns the configuration this manager was built with.
func (m *Manager) Config() Config { return m.cfg }

// Start spawns the child. It returns as soon as the process exists; readiness
// is the caller's job because `/health` can take a while to answer.
func (m *Manager) Start() error {
	cfg := m.cfg
	if err := cfg.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("launch manager is closed")
	}
	if m.state == Running && m.cmd != nil {
		m.mu.Unlock()
		return errors.New("laya is already running")
	}
	cmd := exec.Command(cfg.normalized().Path, cfg.Args()...)
	cmd.Stdout = managerWriter{m}
	cmd.Stderr = managerWriter{m}
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	m.cmd, m.state, m.err, m.pid, m.done = cmd, Running, nil, cmd.Process.Pid, done
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.state = Exited
			m.err = err
			m.pid = 0
		}
		close(done)
		m.mu.Unlock()
	}()
	return nil
}

// Stop interrupts the child, then kills it after StopGrace. It is safe to call
// more than once and does nothing when no child is running.
func (m *Manager) Stop() error {
	m.mu.Lock()
	m.closed = true
	cmd, done := m.cmd, m.done
	if cmd == nil || cmd.Process == nil || m.state != Running {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	_ = interruptProcess(cmd)
	grace := m.grace
	if grace <= 0 {
		grace = StopGrace
	}
	select {
	case <-done:
		return nil
	case <-time.After(grace):
		if err := killProcess(cmd); err != nil {
			return err
		}
		<-done
		return nil
	}
}

// Running reports whether the spawned child is still alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == Running
}

// State returns the child state.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Err returns the last wait error, if any.
func (m *Manager) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// PID returns the child PID, or 0 when it is not running.
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pid
}

// Logs returns the captured stdout and stderr.
func (m *Manager) Logs() string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	return string(m.log)
}

type managerWriter struct{ m *Manager }

func (w managerWriter) Write(p []byte) (int, error) {
	w.m.appendLog(p)
	return len(p), nil
}

func (m *Manager) appendLog(p []byte) {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	m.log = append(m.log, p...)
	if len(m.log) > m.logMax {
		m.log = append([]byte(nil), m.log[len(m.log)-m.logMax:]...)
	}
}
