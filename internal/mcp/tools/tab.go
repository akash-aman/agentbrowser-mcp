package tools

import (
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerTabs() {
	r.add(config.ToolsetCore, mcp.NewTool("tabs",
		mcp.WithDescription("List, open, switch, or close tabs, or open a new window. Tab ids (t1, t2) and labels are interchangeable."),
		mcp.WithString("action", mcp.Enum(tabActions...), mcp.Description("Default list.")),
		mcp.WithString("tab", mcp.Description("Tab id or label for switch/close. close defaults to the active tab.")),
		mcp.WithString("url", mcp.Description("URL for new.")),
		mcp.WithString("label", mcp.Description("Label for new, e.g. docs.")),
		sessionParam(), mutating(),
	), r.cli(tabsArgv))
}

var tabActions = []string{"list", "new", "switch", "close", "new_window"}

func tabsArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("action", "list", tabActions...) {
	case "new":
		b.add("tab", "new").flag("--label", "label").opt("url")
	case "switch":
		b.add("tab", b.requiredFor("tab", "switch"))
	case "close":
		b.add("tab", "close").opt("tab")
	case "new_window":
		b.add("window", "new")
	default:
		b.add("tab", "list")
	}
	return b.done()
}
