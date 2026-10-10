package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerSession() {
	st := config.ToolsetStorage

	r.add(st, mcp.NewTool("session",
		mcp.WithDescription("Show the current session and URL, list sessions or Chrome profiles, or connect this session to a running browser over CDP (port or ws:// URL)."),
		mcp.WithString("action", mcp.Enum("info", "list", "profiles", "connect"), mcp.Description("Default info.")),
		mcp.WithString("target", mcp.Description("connect: CDP port (9222) or WebSocket URL.")),
		sessionParam(), mutating(),
	), r.handleSession)

	r.add(st, mcp.NewTool("auth",
		mcp.WithDescription("Use login profiles saved with `agent-browser auth save` (secrets never pass through the model): list, show metadata, log in, or delete."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("list", "show", "login", "delete")),
		mcp.WithString("name", mcp.Description("show/login/delete: profile name.")),
		sessionParam(), mutating(),
	), r.cli(authArgv))
}

func sessionArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("action", "info", "info", "list", "profiles", "connect") {
	case "list":
		b.add("session", "list")
	case "profiles":
		b.add("profiles")
	case "connect":
		b.add("connect", b.requiredFor("target", "connect"))
	default:
		b.add("session")
	}
	return b.done()
}

func (r *Registry) handleSession(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := sessionArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	res := r.run(ctx, req, args...)
	if res.IsError || args[0] != "session" || len(args) > 1 {
		return res, nil
	}
	if url, err := r.mgr.Run(ctx, getSession(req), "get", "url"); err == nil {
		appendText(res, "url: "+dataField(url.Data, "url"))
	}
	return res, nil
}

func authArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "auth")
	action := b.enum("action", "", "list", "show", "login", "delete")
	b.add(action)
	if action != "list" {
		b.add(b.requiredFor("name", action))
	}
	return b.done()
}
