// Package browser wraps agent-browser CLI execution and manages session state.
package browser

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

// Session tracks an active agent-browser session. An empty Name is the CLI's
// default session.
type Session struct {
	Name       string
	LastActive time.Time
}

// Manager executes agent-browser commands and manages named sessions.
type Manager struct {
	cfg      *config.Config
	sessions map[string]*Session
	mu       sync.RWMutex

	versionOnce sync.Once
	version     CLIVersion
}

// NewManager creates a browser manager.
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		cfg:      cfg,
		sessions: make(map[string]*Session),
	}
}

// Config returns the configuration the manager was built with.
func (m *Manager) Config() *config.Config {
	return m.cfg
}

// Sessions returns a copy of all active sessions.
func (m *Manager) Sessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, &Session{Name: s.Name, LastActive: s.LastActive})
	}
	return out
}

// TrackSession records an active session.
func (m *Manager) TrackSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[name]; ok {
		s.LastActive = time.Now()
		return
	}
	m.sessions[name] = &Session{Name: name, LastActive: time.Now()}
}

// RemoveSession removes a session from tracking.
func (m *Manager) RemoveSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}

// CloseAll closes every session this server has used, including the default one.
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.RLock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Run(ctx, name, "close")
		m.RemoveSession(name)
	}
}

// Result is the parsed output of an agent-browser command.
type Result struct {
	Success bool
	Data    json.RawMessage
	Error   string
	Warning string
	// JSON is true when stdout was the CLI's JSON envelope.
	JSON bool
	// RawStdout contains the raw stdout, used when the output is not a JSON envelope.
	RawStdout string
	// RawStderr contains stderr output for diagnostics.
	RawStderr string
}

// Text returns the result data as a plain string: the JSON data when present,
// otherwise the raw stdout.
func (r *Result) Text() string {
	if r.Data != nil {
		return string(r.Data)
	}
	return strings.TrimSpace(r.RawStdout)
}

// ResolveSession applies the configured default session to an empty name.
func (m *Manager) ResolveSession(session string) string {
	if session == "" {
		return m.cfg.Session
	}
	return session
}

// GlobalArgs returns the flags passed before every command for a resolved session.
func (m *Manager) GlobalArgs(session string) []string {
	c := m.cfg
	f := flags{}
	f.value("--session", session)
	f.value("--config", c.ConfigFile)
	f.value("--session-name", c.SessionName)
	f.value("--profile", c.Profile)
	f.value("--state", c.State)
	f.value("--engine", c.Engine)
	f.value("-p", c.Provider)
	f.toggle("--headed", c.Headed)
	f.value("--executable-path", c.ExecutablePath)
	f.value("--proxy", c.Proxy)
	f.value("--proxy-bypass", c.ProxyBypass)
	f.toggle("--auto-connect", c.AutoConnect)
	f.value("--cdp", c.CDP)
	f.value("--allowed-domains", c.AllowedDomains)
	f.value("--action-policy", c.ActionPolicy)
	f.each("--extension", c.Extensions)
	f.each("--init-script", c.InitScripts)
	f.value("--enable", c.Enable)
	f.value("--args", c.BrowserArgs)
	f.toggle("--ignore-https-errors", c.IgnoreHTTPSErrors)
	f.toggle("--no-auto-dialog", c.NoAutoDialog)
	f.toggle("--content-boundaries", c.ContentBoundaries)
	f.value("--download-path", c.DownloadPath)
	f.value("--user-agent", c.UserAgent)
	f.value("--input-mode", c.InputMode)
	// JSON output is what the tools parse.
	return append(f, "--json")
}

// flags accumulates CLI flags, skipping unset values.
type flags []string

func (f *flags) value(flag, v string) {
	if v != "" {
		*f = append(*f, flag, v)
	}
}

func (f *flags) toggle(flag string, on bool) {
	if on {
		*f = append(*f, flag)
	}
}

// each repeats flag for every item of a comma-separated list.
func (f *flags) each(flag, list string) {
	for item := range strings.SplitSeq(list, ",") {
		f.value(flag, strings.TrimSpace(item))
	}
}

// Run executes an agent-browser command with the given session and the
// configured default timeout.
func (m *Manager) Run(ctx context.Context, session string, args ...string) (*Result, error) {
	return m.RunTimeout(ctx, session, time.Duration(m.cfg.DefaultTimeout)*time.Millisecond, args...)
}

// RunTimeout executes with an explicit timeout. Errors name only the
// subcommand, never its arguments, so filled-in values do not leak into logs.
func (m *Manager) RunTimeout(ctx context.Context, session string, timeout time.Duration, args ...string) (*Result, error) {
	session = m.ResolveSession(session)
	m.TrackSession(session)
	name := commandName(args)

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := m.exec(cmdCtx, append(m.GlobalArgs(session), args...))

	switch {
	case errors.Is(cmdCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		return nil, fmt.Errorf("%s timed out after %v", name, timeout)
	case ctx.Err() != nil:
		return nil, fmt.Errorf("%s: %w", name, ctx.Err())
	case err != nil && !out.started:
		return nil, fmt.Errorf("cannot run %s: %w", m.cfg.AgentBrowserPath, err)
	}
	return checkResult(name, parseResult(out.stdout, out.stderr, err == nil), err)
}

func commandName(args []string) string {
	if len(args) == 0 {
		return "agent-browser"
	}
	return "agent-browser " + args[0]
}

type execOutput struct {
	stdout  []byte
	stderr  string
	started bool
}

func (m *Manager) exec(ctx context.Context, argv []string) (execOutput, error) {
	cmd := exec.CommandContext(ctx, m.cfg.AgentBrowserPath, argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	return execOutput{stdout: stdout, stderr: stderr.String(), started: cmd.ProcessState != nil}, err
}

// checkResult turns an unsuccessful result into an error naming only the subcommand.
func checkResult(name string, res *Result, runErr error) (*Result, error) {
	if res.Success {
		return res, nil
	}
	res.Error = cmp.Or(res.Error, strings.TrimSpace(res.RawStderr))
	if res.Error == "" && runErr != nil {
		res.Error = runErr.Error()
	}
	return res, fmt.Errorf("%s: %s", name, res.Error)
}

// parseResult decodes the CLI's JSON envelope ({"success","data","error"}).
// Output that is not an envelope is treated as plain text.
func parseResult(stdout []byte, stderr string, exitOK bool) *Result {
	r := &Result{RawStdout: string(stdout), RawStderr: stderr}
	var env struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   json.RawMessage `json:"error"`
		Warning string          `json:"warning"`
	}
	if json.Unmarshal(bytes.TrimSpace(stdout), &env) == nil && env.Success != nil {
		r.JSON = true
		r.Success = *env.Success
		if len(env.Data) > 0 && string(env.Data) != "null" {
			r.Data = env.Data
		}
		r.Error = errorText(env.Error)
		r.Warning = env.Warning
		return r
	}
	r.Success = exitOK
	if !exitOK {
		r.Error = strings.TrimSpace(string(stdout))
	}
	return r
}

func errorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
