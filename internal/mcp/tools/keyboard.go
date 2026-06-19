package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerKeyboard(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("keyboard_type",
		mcp.WithDescription("Type text with real keystrokes (simulates each key press/release). No selector needed — types at current focus."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type with real keystrokes.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleKeyboardType(mgr))

	s.AddTool(mcp.NewTool("keyboard_insert",
		mcp.WithDescription("Insert text without generating key events (faster, but won't trigger event handlers)."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to insert.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleKeyboardInsert(mgr))

	s.AddTool(mcp.NewTool("key_down",
		mcp.WithDescription("Hold a key down (for key combinations). Use key_up to release."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key to hold down.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleKeyDown(mgr))

	s.AddTool(mcp.NewTool("key_up",
		mcp.WithDescription("Release a held key."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key to release.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleKeyUp(mgr))
}

func handleKeyboardType(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text, _ := request.RequireString("text")
		return runCmd(ctx, mgr, session, "keyboard", "type", text)
	}
}

func handleKeyboardInsert(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text, _ := request.RequireString("text")
		return runCmd(ctx, mgr, session, "keyboard", "inserttext", text)
	}
}

func handleKeyDown(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key, _ := request.RequireString("key")
		return runCmd(ctx, mgr, session, "keydown", key)
	}
}

func handleKeyUp(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key, _ := request.RequireString("key")
		return runCmd(ctx, mgr, session, "keyup", key)
	}
}
