package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

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
	), r.handleReact)

	r.add(dev, mcp.NewTool("record",
		mcp.WithDescription("Record the session to a WebM video (start/stop/restart), or record the actions taken as a flow and export it as a Playwright test or Puppeteer script (flow_start, then act, then flow_export; flow_start again starts over), like DevTools' Recorder. Keeps cookies and storage."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("start", "stop", "restart", "flow_start", "flow_export")),
		mcp.WithString("format", mcp.Enum("playwright", "puppeteer"), mcp.Description("flow_export: script kind. Default playwright.")),
		mcp.WithString("path", mcp.Description("start/restart: output .webm path.")),
		mcp.WithString("url", mcp.Description("start/restart: URL to open. Default current page.")),
		mcp.WithBoolean("cursor", mcp.Description("start/restart: draw the pointer and clicks into the video.")),
		sessionParam(), mutating(),
	), r.handleRecord)

	r.add(dev, mcp.NewTool("diff",
		mcp.WithDescription("Compare page states: snapshot vs last snapshot or a baseline file, screenshot vs a baseline image, or two URLs (loaded in this tab, which ends on the second)."),
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
		mcp.WithDescription("Show rendering problems in the page with Rendering overlays (paint flashing heatmap, layout-shift regions, layer borders, FPS meter), list running animations and slow or pause them, open Chrome's DevTools window for the page (e.g. on the sources panel to watch the debugger pause), stream the viewport, or run the observability dashboard. Use rendering and animations for jank, slow scrolling, needless repaints or content that jumps."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(debugUIActions...)),
		mcp.WithString("panel", mcp.Enum(devtools.DevToolsPanels...), mcp.Description("open_devtools: panel to show first; resources is Application.")),
		mcp.WithBoolean("external", mcp.Description("open_devtools: instead return a DevTools URL to open in another browser.")),
		mcp.WithBoolean("paintFlashing", mcp.Description("rendering: flash repainted areas green.")),
		mcp.WithBoolean("layoutShifts", mcp.Description("rendering: flash layout-shift regions blue.")),
		mcp.WithBoolean("layerBorders", mcp.Description("rendering: outline compositor layers.")),
		mcp.WithBoolean("fpsMeter", mcp.Description("rendering: show the frame rate meter.")),
		mcp.WithBoolean("scrollBottlenecks", mcp.Description("rendering: mark regions that slow scrolling.")),
		mcp.WithNumber("port", mcp.Description("stream_enable/dashboard_start: port. Default automatic / 4848.")),
		mcp.WithNumber("playbackRate", mcp.Description("animations: speed for every animation, e.g. 0.1 slow motion, 0 pauses, 1 normal.")),
		sessionParam(), mutating(),
	), r.handleDebugUI)
}

var (
	reactActions   = []string{"tree", "inspect", "renders_start", "renders_stop", "suspense"}
	debugUIActions = []string{"open_devtools", "rendering", "animations", "stream_enable", "stream_disable", "stream_status", "dashboard_start", "dashboard_stop"}
)

// reactNotEnabled explains the missing hook in terms of this server: the
// CLI's error says to relaunch agent-browser with a flag the model cannot pass.
const reactNotEnabled = "React DevTools is not enabled in this browser. Start agent-browser-mcp with --enable react-devtools " +
	"(or AGENT_BROWSER_ENABLE=react-devtools), reconnect, close this session's browser so it relaunches with the hook, and reload the page."

// handleReact refuses up front when this server launches browsers without
// the React DevTools hook: without it, renders_start reported recording and
// renders_stop then said no recording was active, and inspect failed with
// "No React renderer attached".
func (r *Registry) handleReact(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if _, err := reactArgv(req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if !strings.Contains(r.cfg.Enable, "react-devtools") {
		return mcp.NewToolResultError(reactNotEnabled), nil
	}
	res, err := r.cli(reactArgv)(ctx, req)
	if res != nil && res.IsError && strings.Contains(resultText(res), "React DevTools hook not installed") {
		return mcp.NewToolResultError(reactNotEnabled), nil
	}
	return res, err
}

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

// handleRecord records video through the CLI, or a flow of actions here.
func (r *Registry) handleRecord(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	switch req.GetString("action", "") {
	case "flow_start":
		return mcp.NewToolResultText(r.startFlow(ctx, req)), nil
	case "flow_export":
		code, err := r.exportFlow(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(code), nil
	}
	return r.cli(recordArgv)(ctx, req)
}

func recordArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "record")
	action := b.enum("action", "", "start", "stop", "restart")
	b.add(action)
	if action != "stop" {
		b.add(b.requiredFor("path", action)).opt("url").boolFlag("--cursor", "cursor")
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
	if args[1] == "snapshot" && req.GetString("baseline", "") == "" {
		text, err := r.diffSnapshot(ctx, req, req.GetString("selector", ""))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return textResult(text, r.cfg.MaxOutput), nil
	}
	out, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	switch args[1] {
	case "url":
		return textResult(urlDiff(out.Data), r.cfg.MaxOutput), nil
	case "screenshot":
		return textResult(screenshotDiff(out.Data), r.cfg.MaxOutput), nil
	case "snapshot":
	default:
		return textResult(body(out), r.cfg.MaxOutput), nil
	}
	return textResult(formatSnapshotDiff(out.Data), r.cfg.MaxOutput), nil
}

// screenshotDiff says how much of the picture changed in plain numbers: the
// CLI's mismatchPercentage carried 16 digits and totalPixels came out as
// 1.024e+06.
func screenshotDiff(data json.RawMessage) string {
	var d struct {
		Match             bool    `json:"match"`
		DifferentPixels   float64 `json:"differentPixels"`
		TotalPixels       float64 `json:"totalPixels"`
		MismatchPercent   float64 `json:"mismatchPercentage"`
		DiffPath          string  `json:"diffPath"`
		DimensionMismatch any     `json:"dimensionMismatch"`
	}
	if json.Unmarshal(data, &d) != nil || d.TotalPixels == 0 {
		return formatData(data)
	}
	if d.DimensionMismatch != nil {
		return fmt.Sprintf("the screenshots differ in size: %v", scalarText(d.DimensionMismatch))
	}
	if d.Match {
		return fmt.Sprintf("screenshots match (%.0f pixels compared)", d.TotalPixels)
	}
	text := fmt.Sprintf("screenshots differ: %.0f of %.0f pixels (%.2f%%)", d.DifferentPixels, d.TotalPixels, d.MismatchPercent)
	if d.DiffPath != "" {
		text += "; changed pixels are marked in " + d.DiffPath
	}
	return text
}

// snapshotLines is an accessibility tree without its refs, which change
// between snapshots of the same page.
func snapshotLines(s string) []string {
	lines := strings.Split(strings.TrimRight(refInLine.ReplaceAllString(s, ""), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

// diffSnapshot compares the page's accessibility tree with the last one this
// server saw for the session and scope. agent-browser 0.38's "diff snapshot"
// keeps no baseline: every line came back as added, even twice in a row.
// The full tree is compared: compact mode leaves out paragraphs and other
// text, so a changed status message did not show as a change.
func (r *Registry) diffSnapshot(ctx context.Context, req mcp.CallToolRequest, selector string) (string, error) {
	cur, err := r.fullSnapshot(ctx, req, selector)
	if err != nil {
		return "", err
	}
	prev, ok := r.swapSnapshot(req, selector, cur)
	if !ok {
		return fmt.Sprintf("no earlier snapshot to compare with; this one (%d lines) is the baseline for the next diff", len(cur)), nil
	}
	diff := lineDiff(prev, cur)
	if len(diff) == 0 {
		return "no changes since the last snapshot", nil
	}
	return strings.Join(diff, "\n"), nil
}

// fullSnapshot reads the page's whole accessibility tree, without refs.
func (r *Registry) fullSnapshot(ctx context.Context, req mcp.CallToolRequest, selector string) ([]string, error) {
	args := []string{"snapshot"}
	if selector != "" {
		args = append(args, "-s", selector)
	}
	out, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return nil, err
	}
	return snapshotLines(dataField(out.Data, "snapshot")), nil
}

// swapSnapshot stores a session's latest tree for a scope and returns the
// one before it.
func (r *Registry) swapSnapshot(req mcp.CallToolRequest, selector string, lines []string) ([]string, bool) {
	key := r.mgr.ResolveSession(getSession(req)) + "\x00" + selector
	r.mu.Lock()
	defer r.mu.Unlock()
	prev, ok := r.snapBase[key]
	r.snapBase[key] = lines
	return prev, ok
}

// urlDiff turns the CLI's two full snapshots into the lines that differ;
// refs are left out, since they never match across pages.
func urlDiff(data json.RawMessage) string {
	var d struct {
		Snapshot1  string          `json:"snapshot1"`
		Snapshot2  string          `json:"snapshot2"`
		URL1       string          `json:"url1"`
		URL2       string          `json:"url2"`
		Screenshot json.RawMessage `json:"screenshot"`
	}
	if json.Unmarshal(data, &d) != nil || d.URL1 == "" {
		return formatData(data)
	}
	diff := lineDiff(snapshotLines(d.Snapshot1), snapshotLines(d.Snapshot2))
	head := fmt.Sprintf("%s (-) vs %s (+); this tab now shows %s", d.URL1, d.URL2, d.URL2)
	if len(diff) == 0 {
		head += "\nthe accessibility trees are the same"
	}
	if len(d.Screenshot) > 0 && string(d.Screenshot) != "null" {
		head += "\n" + screenshotDiff(d.Screenshot)
	}
	return strings.Join(append([]string{head}, diff...), "\n")
}

var refInLine = regexp.MustCompile(` \[ref=e\d+\]|, ref=e\d+|\[ref=e\d+, |\[ref=e\d+\]`)

// lineDiff lists the lines removed (-) and added (+) between a and b, in
// order, from their longest common subsequence.
func lineDiff(a, b []string) []string {
	if len(a)*len(b) > 4_000_000 { // too big to align; compare as sets
		a, b = a[:min(len(a), 2000)], b[:min(len(b), 2000)]
	}
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out, i = append(out, "- "+a[i]), i+1
		default:
			out, j = append(out, "+ "+b[j]), j+1
		}
	}
	for ; i < len(a); i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < len(b); j++ {
		out = append(out, "+ "+b[j])
	}
	return out
}

// handleDebugUI opens DevTools in place over CDP; everything else, including
// external DevTools, goes through the CLI.
func (r *Registry) handleDebugUI(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	action := b.enum("action", "", debugUIActions...)
	if action == "rendering" {
		return r.handleRendering(ctx, req, b), nil
	}
	if action == "animations" {
		var rate *float64
		if b.has("playbackRate") {
			v := req.GetFloat("playbackRate", 1)
			rate = &v
		}
		page, err := r.livePage(ctx, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := page.Animations(ctx, rate)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return textResult(text, r.cfg.MaxOutput), nil
	}
	if action == "dashboard_start" {
		args, err := debugUIArgv(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out, err := r.mgr.Run(ctx, getSession(req), args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var d struct {
			Port       int      `json:"port"`
			PID        int      `json:"pid"`
			AccessURLs []string `json:"access_urls"`
		}
		if json.Unmarshal(out.Data, &d) != nil || d.Port == 0 {
			return textResult(body(out), r.cfg.MaxOutput), nil
		}
		text := fmt.Sprintf("dashboard running at http://localhost:%d (pid %d); dashboard_stop stops it", d.Port, d.PID)
		if len(d.AccessURLs) > 0 {
			text += "\nalso reachable at " + strings.Join(d.AccessURLs, ", ")
		}
		return mcp.NewToolResultText(text), nil
	}
	if action == "stream_enable" {
		res, _ := r.cli(debugUIArgv)(ctx, req)
		if res.IsError && strings.Contains(resultText(res), "already enabled") {
			// agent-browser 0.38 streams by default.
			return mcp.NewToolResultText("already streaming:\n" + resultText(r.run(ctx, req, "stream", "status"))), nil
		}
		return res, nil
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
