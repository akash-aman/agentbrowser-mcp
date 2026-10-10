package tools

import (
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerDialog() {
	r.add(config.ToolsetCore, mcp.NewTool("dialog",
		mcp.WithDescription("Check, accept, or dismiss a JavaScript alert/confirm/prompt."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("status", "accept", "dismiss")),
		mcp.WithString("text", mcp.Description("Prompt answer for accept.")),
		sessionParam(), mutating(),
	), r.cli(dialogArgv))
}

func dialogArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "dialog")
	action := b.enum("action", "", "status", "accept", "dismiss")
	b.add(action)
	if action == "accept" {
		b.opt("text")
	}
	return b.done()
}
