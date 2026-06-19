// Package browser wraps agent-browser CLI execution and manages session state.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

// Session tracks an active agent-browser session.
type Session struct {
	Name       string
	CDPURL     string
	LastActive time.Time
	mu         sync.Mutex
}

// Manager executes agent-browser commands and manages named sessions.
type Manager struct {
	cfg      *config.Config
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a browser manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*Session),
	}
}

// Sessions returns a copy of all active sessions.
func (m *Manager) Sessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// GetSession returns a session by name, or nil.
func (m *Manager) GetSession(name string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[name]
}

// TrackSession records an active session.
func (m *Manager) TrackSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[name]; !ok {
		m.sessions[name] = &Session{
			Name:       name,
			LastActive: time.Now(),
		}
	} else {
		m.sessions[name].LastActive = time.Now()
	}
}

// RemoveSession removes a session from tracking.
func (m *Manager) RemoveSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}

// CloseAll closes all browser sessions.
func (m *Manager) CloseAll(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		sessions = append(sessions, name)
	}
	m.mu.Unlock()

	for _, name := range sessions {
		m.Run(ctx, name, "close")
		m.RemoveSession(name)
	}
	return nil
}

// Result is the parsed output of an agent-browser command.
type Result struct {
	Success bool            `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
	Warning string          `json:"warning,omitempty"`
	// RawStdout contains the raw stdout when --json parsing fails.
	RawStdout string `json:"-"`
	// RawStderr contains stderr output for diagnostics.
	RawStderr string `json:"-"`
}

// Run executes an agent-browser command with the given session and arguments.
// The command is run with --json for machine-readable output.
func (m *Manager) Run(ctx context.Context, session string, args ...string) (*Result, error) {
	return m.RunTimeout(ctx, session, time.Duration(m.cfg.DefaultTimeout)*time.Millisecond, args...)
}

// RunTimeout executes with an explicit timeout.
func (m *Manager) RunTimeout(ctx context.Context, session string, timeout time.Duration, args ...string) (*Result, error) {
	if session == "" {
		session = m.cfg.Session
	}

	cmdArgs := make([]string, 0, len(args)+10)

	// Session flags.
	if session != "" {
		cmdArgs = append(cmdArgs, "--session", session)
		m.TrackSession(session)
	}
	if m.cfg.SessionName != "" {
		cmdArgs = append(cmdArgs, "--session-name", m.cfg.SessionName)
	}
	if m.cfg.Profile != "" {
		cmdArgs = append(cmdArgs, "--profile", m.cfg.Profile)
	}
	if m.cfg.State != "" {
		cmdArgs = append(cmdArgs, "--state", m.cfg.State)
	}
	if m.cfg.Engine != "" {
		cmdArgs = append(cmdArgs, "--engine", m.cfg.Engine)
	}
	if m.cfg.Provider != "" {
		cmdArgs = append(cmdArgs, "-p", m.cfg.Provider)
	}
	if m.cfg.Headed {
		cmdArgs = append(cmdArgs, "--headed")
	}
	if m.cfg.ExecutablePath != "" {
		cmdArgs = append(cmdArgs, "--executable-path", m.cfg.ExecutablePath)
	}
	if m.cfg.Proxy != "" {
		cmdArgs = append(cmdArgs, "--proxy", m.cfg.Proxy)
	}

	// Always request JSON output for machine parsing.
	cmdArgs = append(cmdArgs, "--json")

	// Append the actual command arguments.
	cmdArgs = append(cmdArgs, args...)

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, m.cfg.AgentBrowserPath, cmdArgs...)

	stdout, err := cmd.Output()
	stderr := ""
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = string(exitErr.Stderr)
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("command timed out after %v: %s %s", timeout, m.cfg.AgentBrowserPath, strings.Join(cmdArgs, " "))
	}

	result := &Result{
		RawStdout: string(stdout),
		RawStderr: stderr,
	}

	// Try to parse JSON output.
	if len(stdout) > 0 {
		if parseErr := json.Unmarshal(stdout, result); parseErr != nil {
			// If JSON parsing fails, treat it as raw text success.
			result.Success = err == nil
			if !result.Success {
				result.Error = strings.TrimSpace(string(stdout))
			}
		}
	}

	if err != nil && result.Success {
		return result, nil
	}

	if err != nil {
		if result.Error == "" {
			result.Error = err.Error()
		}
		return result, fmt.Errorf("%s %s: %w", m.cfg.AgentBrowserPath, strings.Join(cmdArgs, " "), err)
	}

	return result, nil
}

// RunWithStdin is like Run but pipes stdin data for batch/JSON input.
func (m *Manager) RunWithStdin(ctx context.Context, session string, stdinData string, args ...string) (*Result, error) {
	if session == "" {
		session = m.cfg.Session
	}

	cmdArgs := make([]string, 0, len(args)+10)
	if session != "" {
		cmdArgs = append(cmdArgs, "--session", session)
		m.TrackSession(session)
	}
	if m.cfg.SessionName != "" {
		cmdArgs = append(cmdArgs, "--session-name", m.cfg.SessionName)
	}
	if m.cfg.Profile != "" {
		cmdArgs = append(cmdArgs, "--profile", m.cfg.Profile)
	}
	if m.cfg.State != "" {
		cmdArgs = append(cmdArgs, "--state", m.cfg.State)
	}
	if m.cfg.Engine != "" {
		cmdArgs = append(cmdArgs, "--engine", m.cfg.Engine)
	}
	if m.cfg.Provider != "" {
		cmdArgs = append(cmdArgs, "-p", m.cfg.Provider)
	}
	if m.cfg.Headed {
		cmdArgs = append(cmdArgs, "--headed")
	}
	cmdArgs = append(cmdArgs, "--json")
	cmdArgs = append(cmdArgs, args...)

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(m.cfg.DefaultTimeout)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, m.cfg.AgentBrowserPath, cmdArgs...)
	cmd.Stdin = strings.NewReader(stdinData)

	stdout, err := cmd.Output()
	stderr := ""
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = string(exitErr.Stderr)
	}

	result := &Result{
		RawStdout: string(stdout),
		RawStderr: stderr,
	}

	if len(stdout) > 0 {
		if parseErr := json.Unmarshal(stdout, result); parseErr != nil {
			result.Success = err == nil
			if !result.Success {
				result.Error = strings.TrimSpace(string(stdout))
			}
		}
	}

	if err != nil {
		if result.Error == "" {
			result.Error = err.Error()
		}
		if !result.Success {
			return result, fmt.Errorf("command failed: %w", err)
		}
	}

	return result, nil
}

// Text returns the result data as a plain string, trying raw first then JSON.
func (r *Result) Text() string {
	if r.RawStdout != "" && r.Data == nil {
		return strings.TrimSpace(r.RawStdout)
	}
	if r.Data != nil {
		return string(r.Data)
	}
	return strings.TrimSpace(r.RawStdout)
}
