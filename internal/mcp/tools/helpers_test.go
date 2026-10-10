package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecdp"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecli"
)

func TestMain(m *testing.M) {
	fakecli.MaybeRun()
	os.Exit(m.Run())
}

// env is one MCP server wired to the fake CLI.
type env struct {
	t    *testing.T
	srv  *server.MCPServer
	fake *fakecli.Fake
	cfg  *config.Config
	mgr  *browser.Manager
	reg  *Registry
	cdp  *fakecdp.Server // set by withCDP
}

func testConfig(path string) *config.Config {
	return &config.Config{
		Name:             "test",
		AgentBrowserPath: path,
		DefaultTimeout:   10000,
		MaxOutput:        40000,
		Toolsets:         config.AllToolsets,
	}
}

func newEnv(t *testing.T, tweak ...func(*config.Config)) *env {
	t.Helper()
	fake := fakecli.Install(t)
	cfg := testConfig(fake.Path)
	for _, f := range tweak {
		f(cfg)
	}
	mgr := browser.NewManager(cfg)
	srv := server.NewMCPServer("test", "0", server.WithToolCapabilities(false))
	reg := RegisterAll(srv, cfg, mgr)
	reg.healthSettle = 0 // the fake has no late errors to wait for
	return &env{t: t, srv: srv, fake: fake, cfg: cfg, mgr: mgr, reg: reg}
}

// callResult is the wire shape of a tools/call result.
type callResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	} `json:"content"`
}

func (r callResult) text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r callResult) images() int {
	n := 0
	for _, c := range r.Content {
		if c.Type == "image" {
			n++
		}
	}
	return n
}

// call sends a tools/call JSON-RPC message through the server, so argument
// decoding and dispatch are exercised exactly as a client would hit them.
func (e *env) call(name string, args map[string]any) callResult {
	e.t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	msg, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		e.t.Fatal(err)
	}
	resp := e.srv.HandleMessage(context.Background(), msg)
	raw, err := json.Marshal(resp)
	if err != nil {
		e.t.Fatal(err)
	}
	var wire struct {
		Result *callResult `json:"result"`
		Error  any         `json:"error"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		e.t.Fatal(err)
	}
	if wire.Result == nil {
		e.t.Fatalf("%s: no result, got %s", name, raw)
	}
	return *wire.Result
}

// toolsList returns the tools/list result as raw JSON.
func (e *env) toolsList() json.RawMessage {
	e.t.Helper()
	resp := e.srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	raw, err := json.Marshal(resp)
	if err != nil {
		e.t.Fatal(err)
	}
	var wire struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		e.t.Fatal(err)
	}
	return wire.Result.Tools
}
