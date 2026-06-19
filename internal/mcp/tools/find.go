package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerFind(s *server.MCPServer, mgr *browser.Manager) {
	findActions := []string{"click", "fill", "type", "hover", "focus", "check", "uncheck", "text"}

	for _, action := range findActions {
		actionName := action
		actionDesc := map[string]string{
			"click":   "Click the element",
			"fill":    "Fill the element with a value (requires 'value' param)",
			"type":    "Type into the element (requires 'value' param)",
			"hover":   "Hover over the element",
			"focus":   "Focus the element",
			"check":   "Check the checkbox/radio",
			"uncheck": "Uncheck the checkbox",
			"text":    "Get the text content",
		}[action]

		s.AddTool(mcp.NewTool("find_by_text_"+actionName,
			mcp.WithDescription("Find an element by its text content and "+actionDesc),
			mcp.WithString("text", mcp.Required(), mcp.Description("Text to search for.")),
			mcp.WithString("value", mcp.Description("Value for fill/type actions.")),
			mcp.WithBoolean("exact", mcp.Description("Require exact text match instead of substring.")),
			mcp.WithString("session", mcp.Description("Session name.")),
		), handleFindText(mgr, actionName))
	}

	for _, action := range findActions {
		actionName := action
		actionDesc := map[string]string{
			"click": "Click the element",
		}[action]
		if actionDesc == "" {
			actionDesc = map[string]string{
				"fill":    "Fill the element with a value",
				"type":    "Type into the element",
				"hover":   "Hover over the element",
				"focus":   "Focus the element",
				"check":   "Check the checkbox/radio",
				"uncheck": "Uncheck the checkbox",
				"text":    "Get the text content",
			}[action]
		}

		s.AddTool(mcp.NewTool("find_by_role_"+actionName,
			mcp.WithDescription("Find an element by ARIA role and "+actionDesc),
			mcp.WithString("role", mcp.Required(), mcp.Description("ARIA role (e.g., button, link, textbox).")),
			mcp.WithString("name", mcp.Description("Accessible name to filter by.")),
			mcp.WithString("value", mcp.Description("Value for fill/type actions.")),
			mcp.WithString("session", mcp.Description("Session name.")),
		), handleFindRole(mgr, actionName))
	}

	for _, action := range findActions {
		actionName := action
		actionDesc := map[string]string{
			"click":   "Click the element",
			"fill":    "Fill the element with a value",
			"type":    "Type into the element",
			"hover":   "Hover over the element",
			"focus":   "Focus the element",
			"check":   "Check the checkbox/radio",
			"uncheck": "Uncheck the checkbox",
			"text":    "Get the text content",
		}[action]

		s.AddTool(mcp.NewTool("find_by_label_"+actionName,
			mcp.WithDescription("Find an element by its label text and "+actionDesc),
			mcp.WithString("label", mcp.Required(), mcp.Description("Label text to search for.")),
			mcp.WithString("value", mcp.Description("Value for fill/type actions.")),
			mcp.WithString("session", mcp.Description("Session name.")),
		), handleFindLabel(mgr, actionName))
	}

	for _, action := range findActions {
		actionName := action
		actionDesc := map[string]string{
			"click":   "Click the element",
			"fill":    "Fill the element with a value",
			"type":    "Type into the element",
			"hover":   "Hover over the element",
			"focus":   "Focus the element",
			"check":   "Check the checkbox/radio",
			"uncheck": "Uncheck the checkbox",
			"text":    "Get the text content",
		}[action]

		s.AddTool(mcp.NewTool("find_by_placeholder_"+actionName,
			mcp.WithDescription("Find an element by its placeholder text and "+actionDesc),
			mcp.WithString("placeholder", mcp.Required(), mcp.Description("Placeholder text to search for.")),
			mcp.WithString("value", mcp.Description("Value for fill/type actions.")),
			mcp.WithString("session", mcp.Description("Session name.")),
		), handleFindPlaceholder(mgr, actionName))
	}

	// Generic find tools
	s.AddTool(mcp.NewTool("find_first",
		mcp.WithDescription("Find the first element matching a CSS selector and click, fill, type, hover, focus, check, uncheck, or get text."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector.")),
		mcp.WithString("action", mcp.Required(), mcp.Description("Action: click, fill, type, hover, focus, check, uncheck, text.")),
		mcp.WithString("value", mcp.Description("Value for fill/type actions.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleFindFirst(mgr))

	s.AddTool(mcp.NewTool("find_all",
		mcp.WithDescription("Get text from all elements matching a CSS selector. Returns array of text content."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleFindAll(mgr))
}

func handleFindText(mgr *browser.Manager, action string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text, _ := request.RequireString("text")
		args := []string{"find", "text", text, action}
		if val := request.GetString("value", ""); val != "" {
			args = append(args, val)
		}
		if request.GetBool("exact", false) {
			args = append(args, "--exact")
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleFindRole(mgr *browser.Manager, action string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		role, _ := request.RequireString("role")
		args := []string{"find", "role", role, action}
		if name := request.GetString("name", ""); name != "" {
			args = append(args, "--name", name)
		}
		if val := request.GetString("value", ""); val != "" {
			args = append(args, val)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleFindLabel(mgr *browser.Manager, action string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		label, _ := request.RequireString("label")
		args := []string{"find", "label", label, action}
		if val := request.GetString("value", ""); val != "" {
			args = append(args, val)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleFindPlaceholder(mgr *browser.Manager, action string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		placeholder, _ := request.RequireString("placeholder")
		args := []string{"find", "placeholder", placeholder, action}
		if val := request.GetString("value", ""); val != "" {
			args = append(args, val)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleFindFirst(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		action, _ := request.RequireString("action")
		args := []string{"find", "first", selector, action}
		if val := request.GetString("value", ""); val != "" {
			args = append(args, val)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleFindAll(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "find", "all", selector, "text")
	}
}
