package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

func (r *Registry) registerDevtools() {
	dev := config.ToolsetDevtools

	r.add(dev, mcp.NewTool("react",
		mcp.WithDescription("Inspect a React app: component tree, one fiber's props/hooks/state, render profiling to find slow or needless re-renders, Suspense boundaries. Needs the server started with --enable react-devtools."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(reactActions...)),
		mcp.WithString("fiberId", mcp.Description("inspect: id from tree.")),
		mcp.WithBoolean("onlyDynamic", mcp.Description("suspense: hide static boundaries.")),
		sessionParam(), mutating(),
	), r.cli(reactArgv))

	r.add(dev, mcp.NewTool("record",
		mcp.WithDescription("Record the session to a WebM video (start/stop/restart). Keeps cookies and storage."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("start", "stop", "restart")),
		mcp.WithString("path", mcp.Description("start/restart: output .webm path.")),
		mcp.WithString("url", mcp.Description("start/restart: URL to open. Default current page.")),
		sessionParam(), mutating(),
	), r.cli(recordArgv))

	r.add(dev, mcp.NewTool("diff",
		mcp.WithDescription("Compare page states: snapshot vs last snapshot or a baseline file, screenshot vs a baseline image, or two URLs."),
		mcp.WithString("kind", mcp.Required(), mcp.Enum("snapshot", "screenshot", "urls")),
		mcp.WithString("baseline", mcp.Description("snapshot: baseline file (default last snapshot). screenshot: baseline image (required).")),
		mcp.WithString("selector", mcp.Description("Scope to a CSS selector or @ref.")),
		mcp.WithString("output", mcp.Description("screenshot: diff image path.")),
		mcp.WithNumber("threshold", mcp.Description("screenshot: color threshold 0-1. Default 0.1.")),
		mcp.WithString("urlA", mcp.Description("urls: first URL.")),
		mcp.WithString("urlB", mcp.Description("urls: second URL.")),
		mcp.WithBoolean("screenshot", mcp.Description("urls: also diff screenshots.")),
		mcp.WithBoolean("fullPage", mcp.Description("screenshot/urls: full page.")),
		mcp.WithString("waitUntil", mcp.Enum("load", "domcontentloaded", "networkidle"), mcp.Description("urls: wait strategy.")),
		sessionParam(), mutating(),
	), r.handleDiff)

	r.add(dev, mcp.NewTool("debug_ui",
		mcp.WithDescription("Show rendering problems in the page with Rendering overlays (paint flashing heatmap, layout-shift regions, layer borders, FPS meter), open Chrome's DevTools window for the page (e.g. on the sources panel to watch the debugger pause), stream the viewport, or run the observability dashboard. Use rendering for jank, slow scrolling, needless repaints or content that jumps."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(debugUIActions...)),
		mcp.WithString("panel", mcp.Enum(devtools.DevToolsPanels...), mcp.Description("open_devtools: panel to show first; resources is Application.")),
		mcp.WithBoolean("external", mcp.Description("open_devtools: instead return a DevTools URL to open in another browser.")),
		mcp.WithBoolean("paintFlashing", mcp.Description("rendering: flash repainted areas green.")),
		mcp.WithBoolean("layoutShifts", mcp.Description("rendering: flash layout-shift regions blue.")),
		mcp.WithBoolean("layerBorders", mcp.Description("rendering: outline compositor layers.")),
		mcp.WithBoolean("fpsMeter", mcp.Description("rendering: show the frame rate meter.")),
		mcp.WithBoolean("scrollBottlenecks", mcp.Description("rendering: mark regions that slow scrolling.")),
		mcp.WithNumber("port", mcp.Description("stream_enable/dashboard_start: port. Default automatic / 4848.")),
		sessionParam(), mutating(),
	), r.handleDebugUI)
}

var (
	reactActions   = []string{"tree", "inspect", "renders_start", "renders_stop", "suspense"}
	debugUIActions = []string{"open_devtools", "rendering", "stream_enable", "stream_disable", "stream_status", "dashboard_start", "dashboard_stop"}
)

func reactArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "react")
	switch action := b.enum("action", "", reactActions...); action {
	case "inspect":
		b.add("inspect", b.requiredFor("fiberId", "inspect"))
	case "renders_start":
		b.add("renders", "start")
	case "renders_stop":
		b.add("renders", "stop")
	case "suspense":
		b.add("suspense").boolFlag("--only-dynamic", "onlyDynamic")
	default:
		b.add(action)
	}
	return b.done()
}

func recordArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "record")
	action := b.enum("action", "", "start", "stop", "restart")
	b.add(action)
	if action != "stop" {
		b.add(b.requiredFor("path", action)).opt("url")
	}
	return b.done()
}

func diffArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "diff")
	switch b.enum("kind", "", "snapshot", "screenshot", "urls") {
	case "snapshot":
		b.add("snapshot", "-c").flag("--baseline", "baseline")
	case "screenshot":
		b.add("screenshot", "--baseline", b.requiredFor("baseline", "kind=screenshot")).flag("--output", "output")
		if b.has("threshold") {
			b.add("--threshold", fmt.Sprint(req.GetFloat("threshold", 0.1)))
		}
		b.boolFlag("--full", "fullPage")
	case "urls":
		b.add("url", b.requiredFor("urlA", "kind=urls"), b.requiredFor("urlB", "kind=urls")).
			boolFlag("--screenshot", "screenshot").boolFlag("--full", "fullPage").flag("--wait-until", "waitUntil")
	}
	return b.flag("--selector", "selector").done()
}

func (r *Registry) handleDiff(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := diffArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if args[1] != "snapshot" {
		return r.run(ctx, req, args...), nil
	}
	out, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(formatSnapshotDiff(out.Data), r.cfg.MaxOutput), nil
}

// handleDebugUI opens DevTools in place over CDP; everything else, including
// external DevTools, goes through the CLI.
func (r *Registry) handleDebugUI(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	action := b.enum("action", "", debugUIActions...)
	if action == "rendering" {
		return r.handleRendering(ctx, req, b), nil
	}
	if action != "open_devtools" || b.boolean("external") {
		return r.cli(debugUIArgv)(ctx, req)
	}
	panel := ""
	if b.has("panel") {
		panel = b.enum("panel", "", devtools.DevToolsPanels...)
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.dt.Page(ctx, getSession(req))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	opened, err := page.OpenDevTools(ctx, panel, r.devtoolsSettle)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text := "opened Chrome DevTools for the active tab in the same browser"
	if panel != "" {
		text += " on the " + panel + " panel"
	}
	if note := opened.String(); note != "" {
		text += "\n" + note
	}
	return mcp.NewToolResultText(text), nil
}

// handleRendering switches DevTools Rendering overlays on the live page.
func (r *Registry) handleRendering(ctx context.Context, req mcp.CallToolRequest, b *argv) *mcp.CallToolResult {
	flag := func(key string) *bool {
		if !b.has(key) {
			return nil
		}
		v := b.boolean(key)
		return &v
	}
	opts := devtools.RenderingOptions{
		PaintFlashing: flag("paintFlashing"), LayoutShifts: flag("layoutShifts"), LayerBorders: flag("layerBorders"),
		FPSMeter: flag("fpsMeter"), ScrollBottlenecks: flag("scrollBottlenecks"),
	}
	if opts == (devtools.RenderingOptions{}) {
		b.fail(fmt.Errorf("set at least one of paintFlashing, layoutShifts, layerBorders, fpsMeter, scrollBottlenecks"))
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	text, err := page.SetRendering(ctx, opts)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return mcp.NewToolResultText(text)
}

func debugUIArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("action", "", debugUIActions...) {
	case "open_devtools":
		b.add("inspect")
	case "stream_enable":
		b.add("stream", "enable").intFlag("--port", "port")
	case "stream_disable":
		b.add("stream", "disable")
	case "stream_status":
		b.add("stream", "status")
	case "dashboard_start":
		b.add("dashboard", "start").intFlag("--port", "port")
	case "dashboard_stop":
		b.add("dashboard", "stop")
	}
	return b.done()
}
