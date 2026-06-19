package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerClipboard(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("clipboard_read",
		mcp.WithDescription("Read text from the system clipboard."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleClipboardRead(mgr))

	s.AddTool(mcp.NewTool("clipboard_write",
		mcp.WithDescription("Write text to the system clipboard."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to write to clipboard.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleClipboardWrite(mgr))

	s.AddTool(mcp.NewTool("clipboard_copy",
		mcp.WithDescription("Copy the current selection (Ctrl+C / Cmd+C)."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleClipboardCopy(mgr))

	s.AddTool(mcp.NewTool("clipboard_paste",
		mcp.WithDescription("Paste from clipboard (Ctrl+V / Cmd+V)."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleClipboardPaste(mgr))
}

func handleClipboardRead(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "clipboard", "read")
	}
}

func handleClipboardWrite(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text, _ := request.RequireString("text")
		return runCmd(ctx, mgr, session, "clipboard", "write", text)
	}
}

func handleClipboardCopy(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "clipboard", "copy")
	}
}

func handleClipboardPaste(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "clipboard", "paste")
	}
}
