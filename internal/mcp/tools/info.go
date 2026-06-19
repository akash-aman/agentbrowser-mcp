package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerInfo(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("take_snapshot",
		mcp.WithDescription("Get the accessibility tree of the current page with refs for interaction. This is the PRIMARY way to understand page structure for non-vision models. Use -i for interactive elements only, -c for compact, -d N for depth limit."),
		mcp.WithString("session", mcp.Description("Session name.")),
		mcp.WithBoolean("interactive", mcp.Description("Only show interactive elements (buttons, inputs, links). Recommended for most use cases.")),
		mcp.WithBoolean("compact", mcp.Description("Remove empty structural elements.")),
		mcp.WithNumber("depth", mcp.Description("Limit tree depth.")),
		mcp.WithString("selector", mcp.Description("Scope to a CSS selector.")),
		mcp.WithBoolean("urls", mcp.Description("Include href URLs for link elements.")),
	), handleSnapshot(mgr))

	s.AddTool(mcp.NewTool("get_text",
		mcp.WithDescription("Get the text content of an element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector (@ref, CSS, etc.).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetText(mgr))

	s.AddTool(mcp.NewTool("get_html",
		mcp.WithDescription("Get the innerHTML of an element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetHTML(mgr))

	s.AddTool(mcp.NewTool("get_value",
		mcp.WithDescription("Get the current value of an input element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Input element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetValue(mgr))

	s.AddTool(mcp.NewTool("get_attribute",
		mcp.WithDescription("Get an attribute value from an element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("attribute", mcp.Required(), mcp.Description("Attribute name (e.g., href, class, src).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetAttr(mgr))

	s.AddTool(mcp.NewTool("get_page_title",
		mcp.WithDescription("Get the current page title."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetTitle(mgr))

	s.AddTool(mcp.NewTool("get_current_url",
		mcp.WithDescription("Get the current page URL."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetURL(mgr))

	s.AddTool(mcp.NewTool("get_cdp_url",
		mcp.WithDescription("Get the Chrome DevTools Protocol WebSocket URL for direct CDP access."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetCDPURL(mgr))

	s.AddTool(mcp.NewTool("get_element_count",
		mcp.WithDescription("Count how many elements match a selector."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetCount(mgr))

	s.AddTool(mcp.NewTool("get_bounding_box",
		mcp.WithDescription("Get the bounding box of an element (x, y, width, height)."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetBox(mgr))

	s.AddTool(mcp.NewTool("get_computed_styles",
		mcp.WithDescription("Get computed CSS styles of an element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleGetStyles(mgr))

	s.AddTool(mcp.NewTool("is_visible",
		mcp.WithDescription("Check if an element is currently visible."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleIsVisible(mgr))

	s.AddTool(mcp.NewTool("is_enabled",
		mcp.WithDescription("Check if an element is enabled."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleIsEnabled(mgr))

	s.AddTool(mcp.NewTool("is_checked",
		mcp.WithDescription("Check if a checkbox/radio is checked."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Checkbox/radio selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleIsChecked(mgr))
}

func handleSnapshot(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"snapshot"}
		if request.GetBool("interactive", false) {
			args = append(args, "-i")
		}
		if request.GetBool("compact", false) {
			args = append(args, "-c")
		}
		if d := request.GetFloat("depth", 0); d > 0 {
			args = append(args, "-d", intToStr(int(d)))
		}
		if sel := request.GetString("selector", ""); sel != "" {
			args = append(args, "-s", sel)
		}
		if request.GetBool("urls", false) {
			args = append(args, "--urls")
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleGetText(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "text", selector)
	}
}

func handleGetHTML(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "html", selector)
	}
}

func handleGetValue(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "value", selector)
	}
}

func handleGetAttr(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		attr, _ := request.RequireString("attribute")
		return runCmd(ctx, mgr, getSession(request), "get", "attr", selector, attr)
	}
}

func handleGetTitle(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "get", "title")
	}
}

func handleGetURL(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "get", "url")
	}
}

func handleGetCDPURL(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "get", "cdp-url")
	}
}

func handleGetCount(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "count", selector)
	}
}

func handleGetBox(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "box", selector)
	}
}

func handleGetStyles(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "get", "styles", selector)
	}
}

func handleIsVisible(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "is", "visible", selector)
	}
}

func handleIsEnabled(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "is", "enabled", selector)
	}
}

func handleIsChecked(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, getSession(request), "is", "checked", selector)
	}
}

func intToStr(n int) string {
	if n < 0 {
		return "0"
	}
	return fmt.Sprintf("%d", n)
}
