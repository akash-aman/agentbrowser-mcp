// Package config loads agent-browser MCP server configuration from environment
// variables, with optional command-line flag overrides.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all settings the MCP server needs.
type Config struct {
	// Server identity — describes what this instance is.
	Name       string
	Project    string
	Purpose    string

	// agent-browser executable path (default: "agent-browser" from PATH).
	AgentBrowserPath string

	// Default session name. When set, all commands use --session <name>.
	Session string

	// Default session persistence name (--session-name flag).
	SessionName string

	// Default Chrome profile (--profile flag).
	Profile string

	// State file path to load on session start (--state flag).
	State string

	// Browser engine: "chrome" (default), "lightpanda".
	Engine string

	// Cloud provider: "browserless", "browserbase", "browseruse", "kernel", "agentcore", "ios".
	Provider string

	// Headed mode: show browser window.
	Headed bool

	// Executable path for custom browser binary.
	ExecutablePath string

	// Proxy URL.
	Proxy string

	// Default timeout for agent-browser commands in seconds.
	DefaultTimeout int
}

// Load reads configuration from environment variables and applies flag
// overrides from args.
func Load(args []string) (*Config, error) {
	c := &Config{
		Name:             envOr("AGENT_BROWSER_MCP_NAME", "agent-browser-mcp"),
		Project:          os.Getenv("AGENT_BROWSER_MCP_PROJECT"),
		Purpose:          os.Getenv("AGENT_BROWSER_MCP_PURPOSE"),
		AgentBrowserPath: envOr("AGENT_BROWSER_MCP_BROWSER_PATH", "agent-browser"),
		Session:          os.Getenv("AGENT_BROWSER_MCP_SESSION"),
		SessionName:      os.Getenv("AGENT_BROWSER_SESSION_NAME"),
		Profile:          os.Getenv("AGENT_BROWSER_PROFILE"),
		State:            os.Getenv("AGENT_BROWSER_STATE"),
		Engine:           os.Getenv("AGENT_BROWSER_ENGINE"),
		Provider:         os.Getenv("AGENT_BROWSER_PROVIDER"),
		Headed:           os.Getenv("AGENT_BROWSER_HEADED") == "true",
		ExecutablePath:   os.Getenv("AGENT_BROWSER_EXECUTABLE_PATH"),
		Proxy:            os.Getenv("AGENT_BROWSER_PROXY"),
		DefaultTimeout:   envInt("AGENT_BROWSER_MCP_TIMEOUT", 60000),
	}

	fs := flag.NewFlagSet("agent-browser-mcp", flag.ContinueOnError)
	fs.StringVar(&c.Name, "name", c.Name, "Server identity name")
	fs.StringVar(&c.Project, "project", c.Project, "Project this browser serves")
	fs.StringVar(&c.Purpose, "purpose", c.Purpose, "Purpose of this browser instance")
	fs.StringVar(&c.AgentBrowserPath, "agent-browser-path", c.AgentBrowserPath, "Path to agent-browser binary")
	fs.StringVar(&c.Session, "session", c.Session, "Default session name")
	fs.StringVar(&c.SessionName, "session-name", c.SessionName, "Default session persistence name")
	fs.StringVar(&c.Profile, "profile", c.Profile, "Chrome profile name or path")
	fs.StringVar(&c.State, "state", c.State, "Storage state file path")
	fs.StringVar(&c.Engine, "engine", c.Engine, "Browser engine: chrome, lightpanda")
	fs.StringVar(&c.Provider, "provider", c.Provider, "Cloud provider: browserless, browserbase, etc.")
	fs.BoolVar(&c.Headed, "headed", c.Headed, "Show browser window")
	fs.StringVar(&c.ExecutablePath, "executable-path", c.ExecutablePath, "Custom browser executable path")
	fs.StringVar(&c.Proxy, "proxy", c.Proxy, "Proxy server URL")
	fs.IntVar(&c.DefaultTimeout, "timeout", c.DefaultTimeout, "Default command timeout in ms")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Expand ~ in paths.
	for _, p := range []*string{&c.ExecutablePath} {
		if strings.HasPrefix(*p, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				*p = filepath.Join(home, (*p)[2:])
			}
		}
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate checks required settings.
func (c *Config) Validate() error {
	var missing []string
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
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
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}
