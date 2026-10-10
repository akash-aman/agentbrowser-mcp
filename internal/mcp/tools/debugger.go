package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

func (r *Registry) registerDebugger() {
	r.add(config.ToolsetDevtools, mcp.NewTool("debugger",
		mcp.WithDescription("JavaScript debugger: line, conditional, function, DOM, XHR, event and CSP breakpoints, logpoints and pause on exceptions; then stack (with async frames and source-mapped original files), scope, watch, evaluate, step, restart a frame, edit a script and reload to try a fix (like Local Overrides), and an ignore list for library code. Use it to find why a feature misbehaves, a value is wrong or an exception is thrown, from runtime values rather than guesses from the source. Actions that hit a breakpoint return right away with the paused location."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(debuggerActions...)),
		mcp.WithString("url", mcp.Description("breakpoint/continue_to: script URL, or an original file such as src/cart.ts when the bundle has a source map.")),
		mcp.WithString("urlRegex", mcp.Description("breakpoint: script URL regex instead of url.")),
		mcp.WithNumber("line", mcp.Description("breakpoint/continue_to: 1-based line.")),
		mcp.WithNumber("column", mcp.Description("breakpoint: 1-based column.")),
		mcp.WithString("condition", mcp.Description("breakpoint/function_breakpoint: pause only when this JS is truthy.")),
		mcp.WithString("logMessage", mcp.Description("breakpoint/function_breakpoint: log these JS expressions instead of pausing (logpoint), e.g. 'total=', total.")),
		mcp.WithString("selector", mcp.Description("dom_breakpoint/listeners: CSS selector.")),
		mcp.WithString("change", mcp.Enum(devtools.DOMBreakpointTypes...), mcp.Description("dom_breakpoint: change to break on.")),
		mcp.WithString("urlContains", mcp.Description("xhr_breakpoint: pause on fetch/XHR whose URL contains this; empty = any.")),
		mcp.WithString("event", mcp.Description("event_breakpoint: event name, e.g. click.")),
		mcp.WithString("breakpointId", mcp.Description("remove: id from breakpoint or list.")),
		mcp.WithBoolean("all", mcp.Description("remove/unwatch: everything.")),
		mcp.WithString("mode", mcp.Enum(devtools.ExceptionModes...), mcp.Description("exceptions: when to pause.")),
		mcp.WithNumber("timeoutMs", mcp.Description("wait/resume/step: how long to wait for the next pause. Default 10000 for wait, 3000 otherwise.")),
		mcp.WithNumber("frame", mcp.Description("scope/evaluate/restart_frame: stack frame index. Default 0.")),
		mcp.WithString("objectId", mcp.Description("properties: object id shown in [brackets] by scope or properties.")),
		mcp.WithString("expression", mcp.Description("evaluate: JS to run in the frame (or globally when not paused). watch/unwatch: expression shown with every pause. function_breakpoint: the function, e.g. app.save.")),
		mcp.WithString("query", mcp.Description("search: text to find in loaded scripts.")),
		mcp.WithNumber("limit", mcp.Description("search: max matches. Default 20.")),
		mcp.WithString("filter", mcp.Description("scripts/search: script URL substring.")),
		mcp.WithString("script", mcp.Description("source/edit_source/revert_source: script URL (revert_source: omit for all); source also takes an id or an original file from a source map.")),
		mcp.WithString("find", mcp.Description("edit_source: exact text to replace; must occur once.")),
		mcp.WithString("replace", mcp.Description("edit_source: replacement text.")),
		mcp.WithArray("patterns", mcp.WithStringItems(), mcp.Description("ignore: script URL regexes stepping skips, e.g. node_modules; empty clears.")),
		mcp.WithNumber("from", mcp.Description("source: first line. Default 1.")),
		mcp.WithNumber("to", mcp.Description("source: last line. Default from+50.")),
		sessionParam(), mutating(),
	), r.handleDebugger)
}

var debuggerActions = []string{
	"breakpoint", "dom_breakpoint", "xhr_breakpoint", "event_breakpoint", "remove", "list", "exceptions",
	"pause", "resume", "step_over", "step_into", "step_out", "continue_to", "wait",
	"stack", "scope", "properties", "evaluate", "watch", "unwatch", "scripts", "search", "source", "listeners", "status", "disable",
	"edit_source", "revert_source", "restart_frame", "function_breakpoint", "csp_breakpoint", "ignore",
}

type debuggerAction func(ctx context.Context, b *argv, page *devtools.Page) (string, error)

var debuggerHandlers = map[string]debuggerAction{
	"breakpoint":          dbgBreakpoint,
	"dom_breakpoint":      dbgDOMBreakpoint,
	"xhr_breakpoint":      dbgXHRBreakpoint,
	"event_breakpoint":    dbgEventBreakpoint,
	"remove":              dbgRemove,
	"list":                dbgList,
	"exceptions":          dbgExceptions,
	"pause":               dbgControl,
	"resume":              dbgControl,
	"step_over":           dbgControl,
	"step_into":           dbgControl,
	"step_out":            dbgControl,
	"continue_to":         dbgContinueTo,
	"wait":                dbgWait,
	"stack":               dbgStack,
	"scope":               dbgScope,
	"properties":          dbgProperties,
	"evaluate":            dbgEvaluate,
	"watch":               dbgWatch,
	"unwatch":             dbgUnwatch,
	"scripts":             dbgScripts,
	"search":              dbgSearch,
	"source":              dbgSource,
	"listeners":           dbgListeners,
	"status":              dbgStatus,
	"disable":             dbgDisable,
	"edit_source":         dbgEditSource,
	"revert_source":       dbgRevertSource,
	"restart_frame":       dbgRestartFrame,
	"function_breakpoint": dbgFunctionBreakpoint,
	"csp_breakpoint":      dbgCSPBreakpoint,
	"ignore":              dbgIgnore,
}

func (r *Registry) handleDebugger(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	action := b.enum("action", "", debuggerActions...)
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.dt.Page(ctx, getSession(req))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	notice := r.dt.Notice(getSession(req))
	if notice != "" {
		notice += "\n"
	}
	if action != "status" && action != "disable" {
		if err := page.EnableDebugger(ctx); err != nil {
			return mcp.NewToolResultError(notice + err.Error()), nil
		}
	}
	text, err := debuggerHandlers[action](ctx, b, page)
	if err == nil {
		_, err = b.done()
	}
	if err != nil {
		return mcp.NewToolResultError(notice + err.Error()), nil
	}
	if (action == "edit_source" || action == "revert_source") && !strings.HasPrefix(text, "no ") {
		// The overrides apply to the next load; a breakpoint hit while
		// loading returns at once with the pause.
		res := r.run(ctx, req, "reload")
		text += "\nreloaded: " + resultText(res)
	}
	return textResult(notice+text, r.cfg.MaxOutput), nil
}

func dbgBreakpoint(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	if b.str("url") == "" && b.str("urlRegex") == "" {
		return "", fmt.Errorf("url or urlRegex is required for breakpoint")
	}
	if !b.has("line") {
		return "", errRequired("line", "breakpoint")
	}
	bp, err := page.SetBreakpoint(ctx, devtools.BreakpointSpec{
		URL: b.str("url"), URLRegex: b.str("urlRegex"),
		Line: int(b.req.GetFloat("line", 0)), Column: int(b.req.GetFloat("column", 0)),
		Condition: b.str("condition"), LogMessage: b.str("logMessage"),
	})
	if err != nil {
		return "", err
	}
	return describeBreakpoint(bp), nil
}

func describeBreakpoint(bp devtools.Breakpoint) string {
	text := fmt.Sprintf("%s: %s %s", bp.ID, bp.Kind, bp.Where)
	if bp.ID == bp.Kind { // "csp: csp Trusted Types..."
		text = bp.ID + ": " + bp.Where
	}
	if bp.Condition != "" {
		text += " if " + bp.Condition
	}
	if bp.Kind != "line" {
		return text
	}
	if len(bp.Resolved) == 0 {
		return text + " (binds when a matching script loads)"
	}
	locs := make([]string, 0, len(bp.Resolved))
	for _, l := range bp.Resolved {
		locs = append(locs, l.String())
	}
	return text + " → " + strings.Join(locs, ", ")
}

func dbgDOMBreakpoint(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	sel := b.requiredFor("selector", "dom_breakpoint")
	change := b.enum("change", "subtree-modified", devtools.DOMBreakpointTypes...)
	if _, err := b.done(); err != nil {
		return "", err
	}
	bp, err := page.SetDOMBreakpoint(ctx, sel, change)
	return describeBreakpoint(bp), err
}

func dbgXHRBreakpoint(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	bp, err := page.SetXHRBreakpoint(ctx, b.str("urlContains"))
	return describeBreakpoint(bp), err
}

func dbgEventBreakpoint(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	event := b.requiredFor("event", "event_breakpoint")
	if _, err := b.done(); err != nil {
		return "", err
	}
	bp, err := page.SetEventBreakpoint(ctx, event)
	return describeBreakpoint(bp), err
}

func dbgRemove(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	if !b.boolean("all") {
		id := b.requiredFor("breakpointId", "remove")
		if _, err := b.done(); err != nil {
			return "", err
		}
		return "removed " + id, page.RemoveBreakpoint(ctx, id)
	}
	bps := page.Breakpoints()
	for _, bp := range bps {
		if err := page.RemoveBreakpoint(ctx, bp.ID); err != nil {
			return "", err
		}
	}
	return "removed " + plural(len(bps), "breakpoint"), nil
}

func dbgList(_ context.Context, _ *argv, page *devtools.Page) (string, error) {
	bps := page.Breakpoints()
	if len(bps) == 0 {
		return "(no breakpoints)", nil
	}
	lines := make([]string, 0, len(bps))
	for _, bp := range bps {
		lines = append(lines, describeBreakpoint(bp))
	}
	return strings.Join(lines, "\n"), nil
}

func dbgExceptions(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	mode := b.enum("mode", "uncaught", devtools.ExceptionModes...)
	if _, err := b.done(); err != nil {
		return "", err
	}
	return "pause on exceptions: " + mode, page.SetPauseOnExceptions(ctx, mode)
}

// dbgControl runs pause/resume/step and reports where execution stops next.
func dbgControl(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	action := b.str("action")
	next, stop := page.NextPause()
	defer stop()
	if err := page.Step(ctx, action); err != nil {
		return "", err
	}
	select {
	case pause := <-next:
		return page.Describe(ctx, pause), nil
	case <-time.After(b.timeout(3000)):
		if action == "pause" {
			return "pause requested; the page pauses when it next runs JavaScript (use wait)", nil
		}
		return "running", nil
	}
}

func dbgContinueTo(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	url := b.requiredFor("url", "continue_to")
	if !b.has("line") {
		b.fail(errRequired("line", "continue_to"))
	}
	if _, err := b.done(); err != nil {
		return "", err
	}
	next, stop := page.NextPause()
	defer stop()
	if err := page.ContinueTo(ctx, url, int(b.req.GetFloat("line", 0))); err != nil {
		return "", err
	}
	select {
	case pause := <-next:
		return page.Describe(ctx, pause), nil
	case <-time.After(b.timeout(3000)):
		return "running; the location was not reached yet", nil
	}
}

func dbgWait(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	pause, err := page.WaitForPause(ctx, b.timeout(10000))
	if err != nil {
		return "", err
	}
	return page.Describe(ctx, pause), nil
}

func (b *argv) timeout(defaultMs int) time.Duration {
	return time.Duration(b.req.GetFloat("timeoutMs", float64(defaultMs))) * time.Millisecond
}

func dbgStack(ctx context.Context, _ *argv, page *devtools.Page) (string, error) {
	pause := page.Paused()
	if pause == nil {
		return "", devtools.ErrNotPaused
	}
	return page.StackText(ctx, *pause, 50), nil
}

func dbgScope(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	return page.Scope(ctx, int(b.req.GetFloat("frame", 0)))
}

func dbgProperties(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	id := b.requiredFor("objectId", "properties")
	if _, err := b.done(); err != nil {
		return "", err
	}
	return page.Properties(ctx, id)
}

func dbgEvaluate(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	expr := b.requiredFor("expression", "evaluate")
	if _, err := b.done(); err != nil {
		return "", err
	}
	v, err := page.Evaluate(ctx, expr, int(b.req.GetFloat("frame", 0)))
	if err != nil {
		return "", err
	}
	text := v.Full()
	if v.ObjectID != "" && v.Type == "object" {
		text += "  [" + v.ObjectID + "]"
	}
	return text, nil
}

func dbgWatch(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	expr := b.requiredFor("expression", "watch")
	if _, err := b.done(); err != nil {
		return "", err
	}
	text := "watching: " + strings.Join(page.Watch(expr), ", ")
	if page.Paused() != nil {
		v, err := page.Evaluate(ctx, expr, 0)
		if err != nil {
			return text + fmt.Sprintf("\n%s: <%v>", expr, err), nil
		}
		text += "\n" + expr + " = " + v.String()
	}
	return text, nil
}

func dbgUnwatch(_ context.Context, b *argv, page *devtools.Page) (string, error) {
	expr := b.str("expression")
	if expr == "" && !b.boolean("all") {
		return "", fmt.Errorf("expression or all is required for unwatch")
	}
	left := page.Unwatch(expr)
	if len(left) == 0 {
		return "no watch expressions", nil
	}
	return "watching: " + strings.Join(left, ", "), nil
}

func dbgSearch(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	query := b.requiredFor("query", "search")
	if _, err := b.done(); err != nil {
		return "", err
	}
	matches, err := page.Search(ctx, query, b.str("filter"), int(b.req.GetFloat("limit", 20)))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "(no matches)", nil
	}
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("%s  …%s…", m.Location, m.Snippet))
	}
	return strings.Join(lines, "\n"), nil
}

func dbgScripts(_ context.Context, b *argv, page *devtools.Page) (string, error) {
	scripts := page.Scripts(b.str("filter"))
	if len(scripts) == 0 {
		return "(no matching scripts)", nil
	}
	lines := make([]string, 0, len(scripts))
	for _, s := range scripts {
		line := fmt.Sprintf("%s  id=%s  %d lines", s.URL, s.ScriptID, s.Line)
		if page.HasSourceMap(s.ScriptID) {
			line += "  (source map)"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

func dbgSource(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	script := b.requiredFor("script", "source")
	if _, err := b.done(); err != nil {
		return "", err
	}
	from := int(b.req.GetFloat("from", 1))
	return page.Source(ctx, script, from, int(b.req.GetFloat("to", float64(from+50))))
}

func dbgListeners(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	sel := b.requiredFor("selector", "listeners")
	if _, err := b.done(); err != nil {
		return "", err
	}
	listeners, err := page.Listeners(ctx, sel)
	if err != nil {
		return "", err
	}
	if len(listeners) == 0 {
		return "(no listeners attached directly to " + sel + ")", nil
	}
	lines := make([]string, 0, len(listeners))
	for _, l := range listeners {
		flags := ""
		for _, f := range []struct {
			name string
			on   bool
		}{{" capture", l.Capture}, {" once", l.Once}, {" passive", l.Passive}} {
			if f.on {
				flags += f.name
			}
		}
		lines = append(lines, fmt.Sprintf("%s%s at %s: %s", l.Type, flags, l.Location, l.Handler))
	}
	return strings.Join(lines, "\n"), nil
}

func dbgStatus(ctx context.Context, _ *argv, page *devtools.Page) (string, error) {
	if !page.DebuggerOn() {
		return "debugger off", nil
	}
	state := "running"
	if pause := page.Paused(); pause != nil {
		state = page.Describe(ctx, *pause)
	}
	ignored := ""
	if bb := page.Blackbox(); len(bb) > 0 {
		ignored = "; ignoring " + strings.Join(bb, ", ")
	}
	return fmt.Sprintf("debugger on; %s; pause on exceptions: %s%s\n%s",
		plural(len(page.Breakpoints()), "breakpoint"), page.ExceptionMode(), ignored, state), nil
}

func dbgEditSource(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	script := b.requiredFor("script", "edit_source")
	find := b.requiredFor("find", "edit_source")
	replace := b.provided("replace")
	if _, err := b.done(); err != nil {
		return "", err
	}
	return page.EditSource(ctx, script, find, replace)
}

func dbgRevertSource(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	n, err := page.RemoveOverride(ctx, b.str("script"))
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "no edited scripts to revert", nil
	}
	return fmt.Sprintf("reverted %s to the original", plural(n, "edited script")), nil
}

// dbgRestartFrame re-runs a frame and reports where it pauses again.
func dbgRestartFrame(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	next, stop := page.NextPause()
	defer stop()
	if err := page.RestartFrame(ctx, int(b.req.GetFloat("frame", 0))); err != nil {
		return "", err
	}
	select {
	case pause := <-next:
		return "restarted; " + page.Describe(ctx, pause), nil
	case <-time.After(b.timeout(3000)):
		return "restart requested; the frame runs again from its first line (use wait)", nil
	}
}

func dbgFunctionBreakpoint(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	expr := b.requiredFor("expression", "function_breakpoint")
	if _, err := b.done(); err != nil {
		return "", err
	}
	bp, err := page.SetFunctionBreakpoint(ctx, expr, b.str("condition"), b.str("logMessage"))
	if err != nil {
		return "", err
	}
	return describeBreakpoint(bp) + " (until the page reloads)", nil
}

func dbgCSPBreakpoint(ctx context.Context, _ *argv, page *devtools.Page) (string, error) {
	bp, err := page.SetCSPBreakpoint(ctx)
	if err != nil {
		return "", err
	}
	return describeBreakpoint(bp), nil
}

func dbgIgnore(ctx context.Context, b *argv, page *devtools.Page) (string, error) {
	patterns := b.req.GetStringSlice("patterns", nil)
	if err := page.SetBlackbox(ctx, patterns); err != nil {
		return "", err
	}
	if len(patterns) == 0 {
		return "ignore list cleared", nil
	}
	return "stepping and pausing now skip scripts matching " + strings.Join(patterns, ", "), nil
}

func dbgDisable(ctx context.Context, _ *argv, page *devtools.Page) (string, error) {
	if !page.DebuggerOn() {
		return "debugger already off", nil
	}
	return "debugger off; breakpoints removed and page resumed", page.DisableDebugger(ctx)
}
