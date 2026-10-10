package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerCDP() {
	r.add(config.ToolsetDevtools, mcp.NewTool("cdp",
		mcp.WithDescription("Send any Chrome DevTools Protocol command to the page or browser and optionally collect the events that follow, like DevTools' Protocol monitor. Use it only for what no other tool covers, e.g. Layers, Media, WebAudio, Autofill, Preload, Storage buckets or device orientation."),
		mcp.WithString("method", mcp.Required(), mcp.Description("Domain.method, e.g. LayerTree.enable.")),
		mcp.WithString("params", mcp.Description("Parameters as a JSON object.")),
		mcp.WithString("target", mcp.Enum("page", "browser"), mcp.Description("Default page.")),
		mcp.WithString("events", mcp.Description("Collect events whose name starts with this, e.g. Media.")),
		mcp.WithNumber("waitMs", mcp.Description("How long to collect events after the call. Default 1000 with events.")),
		sessionParam(), destructive(),
	), r.handleCDP)
}

func (r *Registry) handleCDP(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	method := b.required("method")
	target := b.enum("target", "page", "page", "browser")
	var params json.RawMessage
	if raw := b.str("params"); raw != "" {
		if !json.Valid([]byte(raw)) {
			return mcp.NewToolResultError("params must be a JSON object"), nil
		}
		params = json.RawMessage(raw)
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.dt.Page(ctx, getSession(req))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	wait := time.Duration(req.GetFloat("waitMs", 1000)) * time.Millisecond
	text, err := page.Raw(ctx, method, params, target == "browser", b.str("events"), wait)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(text, r.cfg.MaxOutput), nil
}
