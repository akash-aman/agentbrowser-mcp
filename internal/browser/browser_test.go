package browser

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecli"
)

func TestMain(m *testing.M) {
	fakecli.MaybeRun()
	os.Exit(m.Run())
}

func newManager(t *testing.T, tweak ...func(*config.Config)) (*Manager, *fakecli.Fake) {
	t.Helper()
	fake := fakecli.Install(t)
	cfg := &config.Config{AgentBrowserPath: fake.Path, DefaultTimeout: 10000}
	for _, f := range tweak {
		f(cfg)
	}
	return NewManager(cfg), fake
}

func TestGlobalArgsEmpty(t *testing.T) {
	t.Parallel()
	m := NewManager(&config.Config{})
	if got := m.GlobalArgs(""); !slices.Equal(got, []string{"--json"}) {
		t.Fatalf("got %q", got)
	}
}

func TestGlobalArgsEveryField(t *testing.T) {
	t.Parallel()
	m := NewManager(&config.Config{
		SessionName:       "persist",
		Profile:           "Default",
		State:             "/s.json",
		Engine:            "lightpanda",
		Provider:          "browserbase",
		Headed:            true,
		ExecutablePath:    "/chrome",
		Proxy:             "http://p:1",
		AutoConnect:       true,
		CDP:               "9222",
		AllowedDomains:    "a.com,b.com",
		ActionPolicy:      "/policy.json",
		Extensions:        "/ext1, /ext2",
		Enable:            "react-devtools",
		IgnoreHTTPSErrors: true,
		DownloadPath:      "/dl",
		UserAgent:         "UA",
		InputMode:         "human",
		ProxyBypass:       "localhost",
		InitScripts:       "/i1.js,/i2.js",
		BrowserArgs:       "--no-sandbox",
		NoAutoDialog:      true,
		ContentBoundaries: true,
		ConfigFile:        "/ab.json",
	})
	want := []string{
		"--session", "s1",
		"--config", "/ab.json",
		"--session-name", "persist",
		"--profile", "Default",
		"--state", "/s.json",
		"--engine", "lightpanda",
		"-p", "browserbase",
		"--headed",
		"--executable-path", "/chrome",
		"--proxy", "http://p:1",
		"--proxy-bypass", "localhost",
		"--auto-connect",
		"--cdp", "9222",
		"--allowed-domains", "a.com,b.com",
		"--action-policy", "/policy.json",
		"--extension", "/ext1",
		"--extension", "/ext2",
		"--init-script", "/i1.js",
		"--init-script", "/i2.js",
		"--enable", "react-devtools",
		"--args", "--no-sandbox",
		"--ignore-https-errors",
		"--no-auto-dialog",
		"--content-boundaries",
		"--download-path", "/dl",
		"--user-agent", "UA",
		"--input-mode", "human",
		"--json",
	}
	if got := m.GlobalArgs("s1"); !slices.Equal(got, want) {
		t.Fatalf("got\n %q\nwant\n %q", got, want)
	}
}

func TestResolveSession(t *testing.T) {
	t.Parallel()
	m := NewManager(&config.Config{Session: "cfg"})
	if got := m.ResolveSession(""); got != "cfg" {
		t.Fatalf("empty uses configured default, got %q", got)
	}
	if got := m.ResolveSession("x"); got != "x" {
		t.Fatalf("explicit wins, got %q", got)
	}
}

func TestRunParsesEnvelope(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.SetURL("http://x/")
	res, err := m.Run(context.Background(), "s1", "get", "url")
	if err != nil {
		t.Fatal(err)
	}
	var data struct{ URL string }
	if !res.Success || !res.JSON || json.Unmarshal(res.Data, &data) != nil || data.URL != "http://x/" {
		t.Fatalf("got %+v", res)
	}
	call := fake.Calls()[0]
	if !slices.Equal(call, []string{"--session", "s1", "--json", "get", "url"}) {
		t.Fatalf("argv %q", call)
	}
}

func TestRunPlainTextOutput(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.RespondRaw("Usage: agent-browser", 0)
	res, err := m.Run(context.Background(), "", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.JSON || res.Text() != "Usage: agent-browser" {
		t.Fatalf("got %+v", res)
	}
}

func TestRunPlainTextFailure(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.RespondRaw("daemon not running", 2)
	_, err := m.Run(context.Background(), "", "click", "@e1")
	if err == nil || err.Error() != "agent-browser click: daemon not running" {
		t.Fatalf("got %v", err)
	}
}

func TestRunCLIErrorHidesArguments(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.FailOn("fill")
	res, err := m.Run(context.Background(), "", "fill", "#password", "hunter2")
	if err == nil {
		t.Fatal("want error")
	}
	if err.Error() != "agent-browser fill: fake failure" || strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error must name only the subcommand, got %q", err)
	}
	if res == nil || res.Error != "fake failure" {
		t.Fatalf("result should carry the CLI error, got %+v", res)
	}
}

func TestRunTimeout(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.SleepMS(3000)
	start := time.Now()
	_, err := m.RunTimeout(context.Background(), "", 200*time.Millisecond, "wait", "#x")
	if err == nil || !strings.Contains(err.Error(), "agent-browser wait timed out after 200ms") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout did not kill the command")
	}
}

func TestRunCallerCancellationIsNotATimeout(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	fake.SleepMS(3000)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, err := m.Run(ctx, "", "wait", "#x")
	if err == nil || strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("got %v", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	t.Parallel()
	m := NewManager(&config.Config{AgentBrowserPath: "/nonexistent/agent-browser", DefaultTimeout: 1000})
	_, err := m.Run(context.Background(), "", "get", "url")
	if err == nil || !strings.Contains(err.Error(), "cannot run /nonexistent/agent-browser") {
		t.Fatalf("got %v", err)
	}
}

func TestSessionTracking(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	ctx := context.Background()
	m.Run(ctx, "", "get", "url")
	m.Run(ctx, "a", "get", "url")
	m.Run(ctx, "a", "get", "url")
	if n := len(m.Sessions()); n != 2 {
		t.Fatalf("want default + a tracked, got %d", n)
	}

	fake.Reset()
	m.CloseAll(ctx)
	if len(m.Sessions()) != 0 {
		t.Fatalf("CloseAll must forget sessions, left %v", m.Sessions())
	}
	var closes [][]string
	for _, c := range fake.Calls() {
		if slices.Equal(fakecli.Command(c), []string{"close"}) {
			closes = append(closes, c)
		}
	}
	if len(closes) != 2 {
		t.Fatalf("want 2 close calls, got %q", fake.Calls())
	}
}

func TestParseResult(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		stdout  string
		exitOK  bool
		success bool
		json    bool
		data    string
		err     string
	}{
		{"null data", `{"success":true,"data":null,"error":null}`, true, true, true, "", ""},
		{"string error", `{"success":false,"data":null,"error":"nope"}`, false, false, true, "", "nope"},
		{"object error", `{"success":false,"error":{"code":1}}`, false, false, true, "", `{"code":1}`},
		{"not an envelope", `{"report":"x"}`, true, true, false, "", ""},
		{"text failure", "boom\n", false, false, false, "", "boom"},
		{"envelope wins over exit code", `{"success":true,"data":{"a":1}}`, false, true, true, `{"a":1}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := parseResult([]byte(c.stdout), "", c.exitOK)
			if r.Success != c.success || r.JSON != c.json || string(r.Data) != c.data || r.Error != c.err {
				t.Fatalf("got success=%v json=%v data=%q err=%q", r.Success, r.JSON, r.Data, r.Error)
			}
		})
	}
}
