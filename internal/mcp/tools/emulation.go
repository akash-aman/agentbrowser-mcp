package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerEmulation(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("set_viewport",
		mcp.WithDescription("Set the browser viewport size and optionally device pixel ratio for retina."),
		mcp.WithNumber("width", mcp.Required(), mcp.Description("Viewport width in pixels.")),
		mcp.WithNumber("height", mcp.Required(), mcp.Description("Viewport height in pixels.")),
		mcp.WithNumber("scale", mcp.Description("Device pixel ratio for retina displays (e.g., 2).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetViewport(mgr))

	s.AddTool(mcp.NewTool("set_device",
		mcp.WithDescription("Emulate a specific device (e.g., 'iPhone 14')."),
		mcp.WithString("device", mcp.Required(), mcp.Description("Device name to emulate.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetDevice(mgr))

	s.AddTool(mcp.NewTool("set_geolocation",
		mcp.WithDescription("Override browser geolocation."),
		mcp.WithNumber("latitude", mcp.Required(), mcp.Description("Latitude.")),
		mcp.WithNumber("longitude", mcp.Required(), mcp.Description("Longitude.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetGeo(mgr))

	s.AddTool(mcp.NewTool("set_offline",
		mcp.WithDescription("Toggle offline mode to simulate network disconnection."),
		mcp.WithBoolean("enabled", mcp.Required(), mcp.Description("Enable or disable offline mode.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetOffline(mgr))

	s.AddTool(mcp.NewTool("set_headers",
		mcp.WithDescription("Set extra HTTP headers for all requests (global scope)."),
		mcp.WithString("headers", mcp.Required(), mcp.Description("JSON string of header key-value pairs.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetHeaders(mgr))

	s.AddTool(mcp.NewTool("set_credentials",
		mcp.WithDescription("Set HTTP basic authentication credentials."),
		mcp.WithString("username", mcp.Required(), mcp.Description("HTTP basic auth username.")),
		mcp.WithString("password", mcp.Required(), mcp.Description("HTTP basic auth password.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetCredentials(mgr))

	s.AddTool(mcp.NewTool("set_color_scheme",
		mcp.WithDescription("Emulate dark or light mode preference."),
		mcp.WithString("scheme", mcp.Required(), mcp.Description("Color scheme: 'dark', 'light'.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetMedia(mgr))

	s.AddTool(mcp.NewTool("set_user_agent",
		mcp.WithDescription("Set a custom User-Agent string."),
		mcp.WithString("userAgent", mcp.Required(), mcp.Description("Custom User-Agent string.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSetUA(mgr))
}

func handleSetViewport(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		w := request.GetFloat("width", 0)
		h := request.GetFloat("height", 0)
		args := []string{"set", "viewport", fmt.Sprintf("%d", int(w)), fmt.Sprintf("%d", int(h))}
		if s := request.GetFloat("scale", 0); s > 0 {
			args = append(args, fmt.Sprintf("%d", int(s)))
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleSetDevice(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		device, _ := request.RequireString("device")
		return runCmd(ctx, mgr, session, "set", "device", device)
	}
}

func handleSetGeo(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		lat := request.GetFloat("latitude", 0)
		lng := request.GetFloat("longitude", 0)
		return runCmd(ctx, mgr, session, "set", "geo", fmt.Sprintf("%f", lat), fmt.Sprintf("%f", lng))
	}
}

func handleSetOffline(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		mode := "off"
		if request.GetBool("enabled", false) {
			mode = "on"
		}
		return runCmd(ctx, mgr, session, "set", "offline", mode)
	}
}

func handleSetHeaders(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		headers, _ := request.RequireString("headers")
		return runCmd(ctx, mgr, session, "set", "headers", headers)
	}
}

func handleSetCredentials(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		u, _ := request.RequireString("username")
		p, _ := request.RequireString("password")
		return runCmd(ctx, mgr, session, "set", "credentials", u, p)
	}
}

func handleSetMedia(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		scheme, _ := request.RequireString("scheme")
		return runCmd(ctx, mgr, session, "set", "media", scheme)
	}
}

func handleSetUA(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		ua, _ := request.RequireString("userAgent")
		return runCmd(ctx, mgr, session, "--user-agent", ua, "open", "about:blank")
	}
}
