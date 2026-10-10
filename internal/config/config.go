// Package config loads agent-browser MCP server configuration from environment
// variables, with optional command-line flag overrides.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Toolsets that can be enabled with --toolsets.
const (
	ToolsetCore      = "core"
	ToolsetNetwork   = "network"
	ToolsetDevtools  = "devtools"
	ToolsetEmulation = "emulation"
	ToolsetStorage   = "storage"
)

// AllToolsets lists every toolset in registration order.
var AllToolsets = []string{ToolsetCore, ToolsetNetwork, ToolsetDevtools, ToolsetEmulation, ToolsetStorage}

// InputModes are the accepted input-mode values; empty keeps the CLI default.
var InputModes = []string{"", "instant", "smooth", "human"}

// Config holds all settings the MCP server needs.
type Config struct {
	// Server identity — describes what this instance is.
	Name    string
	Project string
	Purpose string

	// AgentBrowserPath is the agent-browser executable.
	AgentBrowserPath string

	// Session is the default session; empty uses the CLI's default session.
	Session string

	// Browser launch and connection settings, passed to every CLI call.
	SessionName       string // --session-name: auto-save/restore cookies and storage
	Profile           string // --profile: Chrome profile name or directory
	State             string // --state: storage state file to load
	Engine            string // --engine: chrome or lightpanda
	Provider          string // -p: cloud browser provider
	Headed            bool   // --headed: show the browser window
	ExecutablePath    string // --executable-path: custom browser binary
	Proxy             string // --proxy
	ProxyBypass       string // --proxy-bypass: hosts that skip the proxy
	AutoConnect       bool   // --auto-connect: attach to a running Chrome
	CDP               string // --cdp: attach on this CDP port or URL
	AllowedDomains    string // --allowed-domains: restrict navigation
	ActionPolicy      string // --action-policy: action policy JSON file
	Extensions        string // --extension, comma-separated
	InitScripts       string // --init-script, comma-separated
	Enable            string // --enable: built-in init scripts, e.g. react-devtools
	BrowserArgs       string // --args: extra browser launch arguments
	IgnoreHTTPSErrors bool   // --ignore-https-errors
	NoAutoDialog      bool   // --no-auto-dialog: leave alerts open for the dialog tool
	ContentBoundaries bool   // --content-boundaries: mark page output to resist prompt injection
	DownloadPath      string // --download-path
	UserAgent         string // --user-agent
	InputMode         string // --input-mode: pointer movement for every action (instant, smooth, human)
	ConfigFile        string // --config: agent-browser.json to use

	// LighthousePath is the Lighthouse CLI used by performance action lighthouse.
	LighthousePath string

	// CloseOnExit closes the browser sessions this server used when it stops.
	// Off by default: sessions are agent-browser daemons shared with other
	// clients, and an MCP reconnect must not kill the browser being worked in.
	CloseOnExit bool

	// IdleTimeout closes a session's browser after this long without
	// commands; 0 leaves it to agent-browser, which never closes a headed
	// one. Each open browser keeps rendering its page, so forgotten windows
	// add up to real CPU load.
	IdleTimeout time.Duration

	// DefaultTimeout for agent-browser commands in milliseconds.
	DefaultTimeout int

	// MaxOutput caps text returned to the model, in characters. 0 disables the cap.
	MaxOutput int

	// Toolsets enabled on the server.
	Toolsets []string
}

type stringSetting struct {
	target              *string
	env, flag, def, use string
}

type boolSetting struct {
	target         *bool
	env, flag, use string
}

type intSetting struct {
	target         *int
	env, flag, use string
	def            int
}

type durationSetting struct {
	target         *time.Duration
	env, flag, use string
	def            time.Duration
}

func (c *Config) stringSettings() []stringSetting {
	return []stringSetting{
		{&c.Name, "AGENT_BROWSER_MCP_NAME", "name", "agent-browser-mcp", "Server identity name"},
		{&c.Project, "AGENT_BROWSER_MCP_PROJECT", "project", "", "Project this browser serves"},
		{&c.Purpose, "AGENT_BROWSER_MCP_PURPOSE", "purpose", "", "Purpose of this browser instance"},
		{&c.AgentBrowserPath, "AGENT_BROWSER_MCP_BROWSER_PATH", "agent-browser-path", "agent-browser", "Path to agent-browser binary"},
		{&c.Session, "AGENT_BROWSER_MCP_SESSION", "session", "", "Default session name"},
		{&c.SessionName, "AGENT_BROWSER_SESSION_NAME", "session-name", "", "Auto-save/restore state under this name"},
		{&c.Profile, "AGENT_BROWSER_PROFILE", "profile", "", "Chrome profile name or path"},
		{&c.State, "AGENT_BROWSER_STATE", "state", "", "Storage state file path"},
		{&c.Engine, "AGENT_BROWSER_ENGINE", "engine", "", "Browser engine: chrome, lightpanda"},
		{&c.Provider, "AGENT_BROWSER_PROVIDER", "provider", "", "Cloud provider: browserless, browserbase, etc."},
		{&c.ExecutablePath, "AGENT_BROWSER_EXECUTABLE_PATH", "executable-path", "", "Custom browser executable path"},
		{&c.Proxy, "AGENT_BROWSER_PROXY", "proxy", "", "Proxy server URL"},
		{&c.ProxyBypass, "AGENT_BROWSER_PROXY_BYPASS", "proxy-bypass", "", "Comma-separated hosts that bypass the proxy"},
		{&c.CDP, "AGENT_BROWSER_CDP", "cdp", "", "Connect to a browser on this CDP port or URL"},
		{&c.AllowedDomains, "AGENT_BROWSER_ALLOWED_DOMAINS", "allowed-domains", "", "Comma-separated domains navigation is restricted to"},
		{&c.ActionPolicy, "AGENT_BROWSER_ACTION_POLICY", "action-policy", "", "Action policy JSON file"},
		{&c.Extensions, "AGENT_BROWSER_EXTENSIONS", "extensions", "", "Comma-separated browser extension paths"},
		{&c.InitScripts, "AGENT_BROWSER_INIT_SCRIPTS", "init-scripts", "", "Comma-separated scripts to run before page scripts"},
		{&c.Enable, "AGENT_BROWSER_ENABLE", "enable", "", "Built-in init scripts to enable, e.g. react-devtools"},
		{&c.BrowserArgs, "AGENT_BROWSER_ARGS", "browser-args", "", "Extra browser launch args, comma-separated"},
		{&c.DownloadPath, "AGENT_BROWSER_DOWNLOAD_PATH", "download-path", "", "Default download directory"},
		{&c.UserAgent, "AGENT_BROWSER_USER_AGENT", "user-agent", "", "Browser User-Agent at launch"},
		{&c.InputMode, "AGENT_BROWSER_INPUT_MODE", "input-mode", "", "Pointer movement for every action: instant, smooth or human"},
		{&c.ConfigFile, "AGENT_BROWSER_CONFIG", "config", "", "agent-browser.json config file"},
		{&c.LighthousePath, "AGENT_BROWSER_MCP_LIGHTHOUSE_PATH", "lighthouse-path", "lighthouse", "Lighthouse CLI for performance audits"},
	}
}

func (c *Config) boolSettings() []boolSetting {
	return []boolSetting{
		{&c.Headed, "AGENT_BROWSER_HEADED", "headed", "Show browser window"},
		{&c.AutoConnect, "AGENT_BROWSER_AUTO_CONNECT", "auto-connect", "Attach to an already-running Chrome"},
		{&c.IgnoreHTTPSErrors, "AGENT_BROWSER_IGNORE_HTTPS_ERRORS", "ignore-https-errors", "Ignore HTTPS certificate errors"},
		{&c.NoAutoDialog, "AGENT_BROWSER_NO_AUTO_DIALOG", "no-auto-dialog", "Keep alert/beforeunload dialogs open for the dialog tool"},
		{&c.ContentBoundaries, "AGENT_BROWSER_CONTENT_BOUNDARIES", "content-boundaries", "Wrap page output in boundary markers"},
		{&c.CloseOnExit, "AGENT_BROWSER_MCP_CLOSE_ON_EXIT", "close-on-exit", "Close the browser sessions this server used when it stops"},
	}
}

func (c *Config) intSettings() []intSetting {
	return []intSetting{
		{&c.DefaultTimeout, "AGENT_BROWSER_MCP_TIMEOUT", "timeout", "Default command timeout in ms", 60000},
		{&c.MaxOutput, "AGENT_BROWSER_MCP_MAX_OUTPUT", "max-output", "Max characters of text returned per tool call (0 = unlimited)", 40000},
	}
}

func (c *Config) durationSettings() []durationSetting {
	return []durationSetting{
		{&c.IdleTimeout, "AGENT_BROWSER_MCP_IDLE_TIMEOUT", "idle-timeout", "Close a session's browser after this long without commands, e.g. 15m or 1h (0 = agent-browser's default, which keeps headed browsers open)", 15 * time.Minute},
	}
}

// Load reads configuration from environment variables, then applies flag
// overrides from args.
func Load(args []string) (*Config, error) {
	c := &Config{}
	toolsets := envOr("AGENT_BROWSER_MCP_TOOLSETS", "all")
	fs := flag.NewFlagSet("agent-browser-mcp", flag.ContinueOnError)
	c.bindFlags(fs)
	fs.StringVar(&toolsets, "toolsets", toolsets, "Comma-separated toolsets: all, "+strings.Join(AllToolsets, ", "))
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	c.expandHome()
	ts, err := ParseToolsets(toolsets)
	if err != nil {
		return nil, err
	}
	c.Toolsets = ts
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// bindFlags registers every setting as a flag whose default comes from its
// environment variable, so flags override env and env overrides built-ins.
func (c *Config) bindFlags(fs *flag.FlagSet) {
	for _, s := range c.stringSettings() {
		fs.StringVar(s.target, s.flag, envOr(s.env, s.def), s.use)
	}
	for _, s := range c.boolSettings() {
		fs.BoolVar(s.target, s.flag, os.Getenv(s.env) == "true", s.use)
	}
	for _, s := range c.intSettings() {
		fs.IntVar(s.target, s.flag, envInt(s.env, s.def), s.use)
	}
	for _, s := range c.durationSettings() {
		fs.DurationVar(s.target, s.flag, envDuration(s.env, s.def), s.use)
	}
}

func (c *Config) expandHome() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	for _, p := range []*string{&c.ExecutablePath, &c.ActionPolicy, &c.DownloadPath, &c.State, &c.ConfigFile} {
		if rest, ok := strings.CutPrefix(*p, "~/"); ok {
			*p = filepath.Join(home, rest)
		}
	}
}

// ParseToolsets turns "core,network" or "all" into a list of toolset names.
// Core is always included because every other toolset assumes it.
func ParseToolsets(s string) ([]string, error) {
	enabled := map[string]bool{ToolsetCore: true}
	for name := range strings.SplitSeq(s, ",") {
		name = strings.TrimSpace(strings.ToLower(name))
		switch {
		case name == "":
		case name == "all":
			for _, t := range AllToolsets {
				enabled[t] = true
			}
		case slices.Contains(AllToolsets, name):
			enabled[name] = true
		default:
			return nil, fmt.Errorf("unknown toolset %q (valid: all, %s)", name, strings.Join(AllToolsets, ", "))
		}
	}
	return slices.DeleteFunc(slices.Clone(AllToolsets), func(t string) bool { return !enabled[t] }), nil
}

// HasToolset reports whether a toolset is enabled.
func (c *Config) HasToolset(name string) bool {
	return slices.Contains(c.Toolsets, name)
}

// Validate checks settings that would otherwise fail later with a confusing error.
func (c *Config) Validate() error {
	switch {
	case c.AgentBrowserPath == "":
		return fmt.Errorf("agent-browser path must not be empty")
	case c.DefaultTimeout <= 0:
		return fmt.Errorf("timeout must be positive, got %d", c.DefaultTimeout)
	case c.MaxOutput < 0:
		return fmt.Errorf("max-output must be >= 0, got %d", c.MaxOutput)
	case c.IdleTimeout < 0:
		return fmt.Errorf("idle-timeout must be >= 0, got %v", c.IdleTimeout)
	case c.AutoConnect && c.CDP != "":
		return fmt.Errorf("auto-connect and cdp are mutually exclusive")
	case !slices.Contains(InputModes, c.InputMode):
		return fmt.Errorf("input-mode must be instant, smooth or human, got %q", c.InputMode)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return d
	}
	return def
}
