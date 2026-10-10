package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// clearEnv unsets every variable Load reads so tests see only what they set.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "AGENT_BROWSER_") {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)
	c, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "agent-browser-mcp" || c.AgentBrowserPath != "agent-browser" || c.DefaultTimeout != 60000 || c.MaxOutput != 40000 || c.IdleTimeout != 15*time.Minute {
		t.Fatalf("got %+v", c)
	}
	if !slices.Equal(c.Toolsets, AllToolsets) {
		t.Fatalf("all toolsets are on by default, got %v", c.Toolsets)
	}
	if c.Headed || c.AutoConnect || c.IgnoreHTTPSErrors || c.CloseOnExit {
		t.Fatalf("booleans default to false, got %+v", c)
	}
}

// field reads one string-ish setting back for comparison.
type field struct {
	env, flag, value string
	get              func(*Config) string
}

var stringFields = []field{
	{"AGENT_BROWSER_MCP_NAME", "name", "n", func(c *Config) string { return c.Name }},
	{"AGENT_BROWSER_MCP_PROJECT", "project", "p", func(c *Config) string { return c.Project }},
	{"AGENT_BROWSER_MCP_PURPOSE", "purpose", "u", func(c *Config) string { return c.Purpose }},
	{"AGENT_BROWSER_MCP_BROWSER_PATH", "agent-browser-path", "/ab", func(c *Config) string { return c.AgentBrowserPath }},
	{"AGENT_BROWSER_MCP_SESSION", "session", "s", func(c *Config) string { return c.Session }},
	{"AGENT_BROWSER_SESSION_NAME", "session-name", "sn", func(c *Config) string { return c.SessionName }},
	{"AGENT_BROWSER_PROFILE", "profile", "Default", func(c *Config) string { return c.Profile }},
	{"AGENT_BROWSER_STATE", "state", "/st.json", func(c *Config) string { return c.State }},
	{"AGENT_BROWSER_ENGINE", "engine", "lightpanda", func(c *Config) string { return c.Engine }},
	{"AGENT_BROWSER_PROVIDER", "provider", "kernel", func(c *Config) string { return c.Provider }},
	{"AGENT_BROWSER_EXECUTABLE_PATH", "executable-path", "/chrome", func(c *Config) string { return c.ExecutablePath }},
	{"AGENT_BROWSER_PROXY", "proxy", "http://p", func(c *Config) string { return c.Proxy }},
	{"AGENT_BROWSER_CDP", "cdp", "9222", func(c *Config) string { return c.CDP }},
	{"AGENT_BROWSER_ALLOWED_DOMAINS", "allowed-domains", "a.com", func(c *Config) string { return c.AllowedDomains }},
	{"AGENT_BROWSER_ACTION_POLICY", "action-policy", "/pol.json", func(c *Config) string { return c.ActionPolicy }},
	{"AGENT_BROWSER_EXTENSIONS", "extensions", "/e1,/e2", func(c *Config) string { return c.Extensions }},
	{"AGENT_BROWSER_ENABLE", "enable", "react-devtools", func(c *Config) string { return c.Enable }},
	{"AGENT_BROWSER_DOWNLOAD_PATH", "download-path", "/dl", func(c *Config) string { return c.DownloadPath }},
	{"AGENT_BROWSER_USER_AGENT", "user-agent", "UA", func(c *Config) string { return c.UserAgent }},
	{"AGENT_BROWSER_PROXY_BYPASS", "proxy-bypass", "localhost", func(c *Config) string { return c.ProxyBypass }},
	{"AGENT_BROWSER_INIT_SCRIPTS", "init-scripts", "/i.js", func(c *Config) string { return c.InitScripts }},
	{"AGENT_BROWSER_ARGS", "browser-args", "--no-sandbox", func(c *Config) string { return c.BrowserArgs }},
	{"AGENT_BROWSER_CONFIG", "config", "/ab.json", func(c *Config) string { return c.ConfigFile }},
	{"AGENT_BROWSER_MCP_LIGHTHOUSE_PATH", "lighthouse-path", "/lh", func(c *Config) string { return c.LighthousePath }},
}

func TestStringSettingsFromEnvAndFlag(t *testing.T) {
	for _, f := range stringFields {
		t.Run(f.flag, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(f.env, f.value)
			c, err := Load(nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := f.get(c); got != f.value {
				t.Fatalf("env: got %q, want %q", got, f.value)
			}

			c, err = Load([]string{"--" + f.flag, f.value + "-flag"})
			if err != nil {
				t.Fatal(err)
			}
			if got := f.get(c); got != f.value+"-flag" {
				t.Fatalf("flag must override env: got %q", got)
			}
		})
	}
}

func TestBoolSettings(t *testing.T) {
	bools := []struct {
		env, flag string
		get       func(*Config) bool
	}{
		{"AGENT_BROWSER_HEADED", "headed", func(c *Config) bool { return c.Headed }},
		{"AGENT_BROWSER_AUTO_CONNECT", "auto-connect", func(c *Config) bool { return c.AutoConnect }},
		{"AGENT_BROWSER_IGNORE_HTTPS_ERRORS", "ignore-https-errors", func(c *Config) bool { return c.IgnoreHTTPSErrors }},
		{"AGENT_BROWSER_NO_AUTO_DIALOG", "no-auto-dialog", func(c *Config) bool { return c.NoAutoDialog }},
		{"AGENT_BROWSER_CONTENT_BOUNDARIES", "content-boundaries", func(c *Config) bool { return c.ContentBoundaries }},
		{"AGENT_BROWSER_MCP_CLOSE_ON_EXIT", "close-on-exit", func(c *Config) bool { return c.CloseOnExit }},
	}
	for _, b := range bools {
		t.Run(b.flag, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(b.env, "true")
			c, err := Load(nil)
			if err != nil || !b.get(c) {
				t.Fatalf("env true: got %v, err %v", b.get(c), err)
			}
			c, err = Load([]string{"--" + b.flag + "=false"})
			if err != nil || b.get(c) {
				t.Fatalf("flag false must override env: got %v, err %v", b.get(c), err)
			}
		})
	}
}

func TestIntSettings(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_BROWSER_MCP_TIMEOUT", "1234")
	t.Setenv("AGENT_BROWSER_MCP_MAX_OUTPUT", "99")
	c, err := Load(nil)
	if err != nil || c.DefaultTimeout != 1234 || c.MaxOutput != 99 {
		t.Fatalf("got %+v err %v", c, err)
	}
	c, err = Load([]string{"--timeout", "5", "--max-output", "0"})
	if err != nil || c.DefaultTimeout != 5 || c.MaxOutput != 0 {
		t.Fatalf("got %+v err %v", c, err)
	}
	t.Setenv("AGENT_BROWSER_MCP_TIMEOUT", "soon")
	if c, _ := Load(nil); c.DefaultTimeout != 60000 {
		t.Fatalf("invalid int env falls back to default, got %d", c.DefaultTimeout)
	}
}

func TestIdleTimeout(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_BROWSER_MCP_IDLE_TIMEOUT", "1h")
	c, err := Load(nil)
	if err != nil || c.IdleTimeout != time.Hour {
		t.Fatalf("got %v err %v", c.IdleTimeout, err)
	}
	c, err = Load([]string{"--idle-timeout", "0"})
	if err != nil || c.IdleTimeout != 0 {
		t.Fatalf("flag must override env: got %v err %v", c.IdleTimeout, err)
	}
	t.Setenv("AGENT_BROWSER_MCP_IDLE_TIMEOUT", "soon")
	if c, _ := Load(nil); c.IdleTimeout != 15*time.Minute {
		t.Fatalf("invalid duration env falls back to default, got %v", c.IdleTimeout)
	}
}

func TestTildeExpansion(t *testing.T) {
	clearEnv(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	c, err := Load([]string{"--executable-path", "~/c", "--action-policy", "~/p.json", "--download-path", "~/dl", "--state", "~/s.json", "--config", "~/ab.json"})
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		c.ExecutablePath: filepath.Join(home, "c"),
		c.ActionPolicy:   filepath.Join(home, "p.json"),
		c.DownloadPath:   filepath.Join(home, "dl"),
		c.State:          filepath.Join(home, "s.json"),
		c.ConfigFile:     filepath.Join(home, "ab.json"),
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestParseToolsets(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  string
	}{
		{"all", AllToolsets, ""},
		{"", []string{ToolsetCore}, ""},
		{"core", []string{ToolsetCore}, ""},
		{"network", []string{ToolsetCore, ToolsetNetwork}, ""},
		{" Storage , devtools ", []string{ToolsetCore, ToolsetDevtools, ToolsetStorage}, ""},
		{"core,all", AllToolsets, ""},
		{"core,bogus", nil, `unknown toolset "bogus"`},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseToolsets(c.in)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("want error %q, got %v", c.err, err)
				}
				return
			}
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatalf("got %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

func TestToolsetsFromEnvAndFlag(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_BROWSER_MCP_TOOLSETS", "network")
	c, err := Load(nil)
	if err != nil || !slices.Equal(c.Toolsets, []string{ToolsetCore, ToolsetNetwork}) {
		t.Fatalf("got %v %v", c.Toolsets, err)
	}
	c, err = Load([]string{"--toolsets", "emulation"})
	if err != nil || !c.HasToolset(ToolsetEmulation) || c.HasToolset(ToolsetNetwork) {
		t.Fatalf("flag must override env, got %v %v", c.Toolsets, err)
	}
	if _, err := Load([]string{"--toolsets", "nope"}); err == nil {
		t.Fatal("unknown toolset must fail")
	}
}

// TestInputMode is separate from the string table because the value is validated.
func TestInputMode(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_BROWSER_INPUT_MODE", "smooth")
	c, err := Load(nil)
	if err != nil || c.InputMode != "smooth" {
		t.Fatalf("env: got %q, %v", c.InputMode, err)
	}
	c, err = Load([]string{"--input-mode", "human"})
	if err != nil || c.InputMode != "human" {
		t.Fatalf("flag must override env: got %q, %v", c.InputMode, err)
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]struct {
		args []string
		err  string
	}{
		"zero timeout":      {[]string{"--timeout", "0"}, "timeout must be positive"},
		"negative output":   {[]string{"--max-output", "-1"}, "max-output must be >= 0"},
		"negative idle":     {[]string{"--idle-timeout", "-1m"}, "idle-timeout must be >= 0"},
		"empty binary path": {[]string{"--agent-browser-path", ""}, "agent-browser path must not be empty"},
		"cdp and auto":      {[]string{"--cdp", "9222", "--auto-connect"}, "mutually exclusive"},
		"bad input mode":    {[]string{"--input-mode", "robot"}, `input-mode must be instant, smooth or human, got "robot"`},
		"unknown flag":      {[]string{"--nope"}, "flag provided but not defined"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			_, err := Load(c.args)
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("want %q, got %v", c.err, err)
			}
		})
	}
}
