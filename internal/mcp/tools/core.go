package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

// shared helper: run agent-browser and return text
func runCmd(ctx context.Context, mgr *browser.Manager, session string, args ...string) (*mcp.CallToolResult, error) {
	result, err := mgr.Run(ctx, session, args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if result.Error != "" {
		return mcp.NewToolResultError(result.Error), nil
	}
	return mcp.NewToolResultText(result.Text()), nil
}

// getSession extracts the optional "session" parameter from a request.
func getSession(request mcp.CallToolRequest) string {
	return request.GetString("session", "")
}

// registerCore adds core interaction tools.
func registerCore(s *server.MCPServer, mgr *browser.Manager) {
	// navigate_page
	s.AddTool(mcp.NewTool("navigate_page",
		mcp.WithDescription("Navigate to a URL, or go back/forward/reload. Use 'url' type for new URLs, 'back', 'forward', or 'reload' for history navigation."),
		mcp.WithString("url", mcp.Description("Target URL. Required when type is 'url'.")),
		mcp.WithString("type", mcp.Description("Navigation type. Default 'url' if URL is provided. Also supports 'back', 'forward', 'reload'.")),
		mcp.WithString("session", mcp.Description("Session name for isolated browser instance.")),
		mcp.WithNumber("timeout", mcp.Description("Max wait time in milliseconds.")),
		mcp.WithBoolean("ignoreCache", mcp.Description("Whether to ignore cache on reload.")),
	), handleNavigatePage(mgr))

	// click
	s.AddTool(mcp.NewTool("click",
		mcp.WithDescription("Click an element on the page. Use refs (@e1, @e2) from a snapshot, or CSS selectors (#id, .class)."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector: @ref from snapshot, CSS selector, text= selector, or xpath= selector.")),
		mcp.WithString("session", mcp.Description("Session name for isolated browser instance.")),
		mcp.WithBoolean("newTab", mcp.Description("Open the link in a new tab instead of the current tab.")),
	), handleClick(mgr))

	// dblclick
	s.AddTool(mcp.NewTool("double_click",
		mcp.WithDescription("Double-click an element on the page."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector (ref, CSS, text=, xpath=).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDblClick(mgr))

	// fill
	s.AddTool(mcp.NewTool("fill",
		mcp.WithDescription("Clear and fill an input field. Use refs (@e3) from snapshot or CSS selectors."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Input element selector.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to fill into the element.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleFill(mgr))

	// type_text
	s.AddTool(mcp.NewTool("type_text",
		mcp.WithDescription("Type into an element (without clearing first). Use for incremental text input."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleType(mgr))

	// press_key
	s.AddTool(mcp.NewTool("press_key",
		mcp.WithDescription("Press a key or key combination (e.g., Enter, Tab, Control+a, Meta+Shift+R)."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key or combination to press. Modifiers: Control, Shift, Alt, Meta.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handlePressKey(mgr))

	// hover
	s.AddTool(mcp.NewTool("hover",
		mcp.WithDescription("Hover over an element on the page."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleHover(mgr))

	// focus
	s.AddTool(mcp.NewTool("focus",
		mcp.WithDescription("Focus an element on the page."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector to focus.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleFocus(mgr))

	// check
	s.AddTool(mcp.NewTool("check",
		mcp.WithDescription("Check a checkbox or radio button."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Checkbox/radio selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCheck(mgr))

	// uncheck
	s.AddTool(mcp.NewTool("uncheck",
		mcp.WithDescription("Uncheck a checkbox."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Checkbox selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleUncheck(mgr))

	// select_option
	s.AddTool(mcp.NewTool("select_option",
		mcp.WithDescription("Select a dropdown option by value."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Select element selector.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Option value to select.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSelect(mgr))

	// scroll
	s.AddTool(mcp.NewTool("scroll",
		mcp.WithDescription("Scroll the page or a specific element."),
		mcp.WithString("direction", mcp.Required(), mcp.Description("Scroll direction: up, down, left, right.")),
		mcp.WithNumber("px", mcp.Description("Pixels to scroll. Default: full page.")),
		mcp.WithString("selector", mcp.Description("Optional selector to scroll a specific element.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleScroll(mgr))

	// scroll_into_view
	s.AddTool(mcp.NewTool("scroll_into_view",
		mcp.WithDescription("Scroll an element into view."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector to scroll into view.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleScrollIntoView(mgr))

	// drag
	s.AddTool(mcp.NewTool("drag",
		mcp.WithDescription("Drag an element from one location to another."),
		mcp.WithString("source", mcp.Required(), mcp.Description("Source element selector.")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Target element selector.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDrag(mgr))

	// upload_file
	s.AddTool(mcp.NewTool("upload_file",
		mcp.WithDescription("Upload files through a file input element."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("File input element selector.")),
		mcp.WithString("files", mcp.Required(), mcp.Description("Comma-separated file paths to upload.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleUpload(mgr))

	// close
	s.AddTool(mcp.NewTool("close_browser",
		mcp.WithDescription("Close the browser session (optionally close all sessions)."),
		mcp.WithString("session", mcp.Description("Session name to close. Uses default session if omitted.")),
		mcp.WithBoolean("all", mcp.Description("Close ALL active browser sessions.")),
	), handleClose(mgr))

	// eval_script
	s.AddTool(mcp.NewTool("eval_script",
		mcp.WithDescription("Execute JavaScript in the browser context. Use for data extraction or custom interactions."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript code to execute. Use base64 mode (-b) for binary data.")),
		mcp.WithString("session", mcp.Description("Session name.")),
		mcp.WithBoolean("base64", mcp.Description("Encode the script as base64 (for scripts with special characters).")),
	), handleEval(mgr))
}

func handleNavigatePage(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		navType := request.GetString("type", "url")
		url := request.GetString("url", "")

		switch navType {
		case "back":
			return runCmd(ctx, mgr, session, "back")
		case "forward":
			return runCmd(ctx, mgr, session, "forward")
		case "reload":
			args := []string{"reload"}
			if request.GetBool("ignoreCache", false) {
				args = append(args, "--ignore-cache")
			}
			return runCmd(ctx, mgr, session, args...)
		default:
			if url == "" {
				return mcp.NewToolResultError("url is required when type is 'url'"), nil
			}
			args := []string{"open", url}
			timeout := request.GetFloat("timeout", 0)
			if timeout > 0 {
				args = append(args, fmt.Sprintf("--timeout=%d", int(timeout)))
			}
			return runCmd(ctx, mgr, session, args...)
		}
	}
}

func handleClick(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		args := []string{"click", selector}
		if request.GetBool("newTab", false) {
			args = append(args, "--new-tab")
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleDblClick(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "dblclick", selector)
	}
}

func handleFill(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		value, _ := request.RequireString("value")
		return runCmd(ctx, mgr, session, "fill", selector, value)
	}
}

func handleType(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		text, _ := request.RequireString("text")
		return runCmd(ctx, mgr, session, "type", selector, text)
	}
}

func handlePressKey(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key, _ := request.RequireString("key")
		return runCmd(ctx, mgr, session, "press", key)
	}
}

func handleHover(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "hover", selector)
	}
}

func handleFocus(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "focus", selector)
	}
}

func handleCheck(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "check", selector)
	}
}

func handleUncheck(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "uncheck", selector)
	}
}

func handleSelect(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		value, _ := request.RequireString("value")
		return runCmd(ctx, mgr, session, "select", selector, value)
	}
}

func handleScroll(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		direction, _ := request.RequireString("direction")
		args := []string{"scroll", direction}
		px := request.GetFloat("px", 0)
		if px > 0 {
			args = append(args, fmt.Sprintf("%d", int(px)))
		}
		if sel := request.GetString("selector", ""); sel != "" {
			args = append(args, "--selector", sel)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleScrollIntoView(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "scrollintoview", selector)
	}
}

func handleDrag(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		source, _ := request.RequireString("source")
		target, _ := request.RequireString("target")
		return runCmd(ctx, mgr, session, "drag", source, target)
	}
}

func handleUpload(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		files, _ := request.RequireString("files")
		return runCmd(ctx, mgr, session, "upload", selector, files)
	}
}

func handleClose(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		if request.GetBool("all", false) {
			mgr.CloseAll(ctx)
			return mcp.NewToolResultText("All browser sessions closed."), nil
		}
		return runCmd(ctx, mgr, session, "close")
	}
}

func handleEval(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		script, _ := request.RequireString("script")
		args := []string{"eval"}
		if request.GetBool("base64", false) {
			args = append(args, "-b")
		}
		args = append(args, script)
		return runCmd(ctx, mgr, session, args...)
	}
}
