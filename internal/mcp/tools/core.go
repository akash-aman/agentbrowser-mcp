package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
	"github.com/xcode-studio/agentbrowser-mcp/internal/devtools"
)

// registerCore adds navigation and element interaction tools.
func (r *Registry) registerCore() {
	core := config.ToolsetCore

	r.add(core, mcp.NewTool("navigate",
		mcp.WithDescription("Open a URL, or go back/forward/reload, or do SPA client-side navigation (pushstate). The result lists uncaught JS errors and failed requests from the load, if any."),
		mcp.WithString("url", mcp.Description("Target URL. Required for goto and pushstate.")),
		mcp.WithString("action", mcp.Enum(navigateActions...), mcp.Description("Default goto.")),
		mcp.WithString("headers", mcp.Description("goto: JSON object of HTTP headers sent only to this URL's origin, e.g. an Authorization bearer token.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.navigation(navigateArgv))

	r.add(core, mcp.NewTool("click",
		mcp.WithDescription("Click an element. Prefer @ref from snapshot; set snapshot:\"delta\" to see the effect in the same call."),
		selectorParam(true),
		mcp.WithBoolean("double", mcp.Description("Double-click.")),
		mcp.WithBoolean("newTab", mcp.Description("Open the link in a new tab.")),
		humanParam(),
		snapshotParam(), sessionParam(), mutating(),
	), r.visibleOnly(clickTarget, clickArgv))

	r.add(core, mcp.NewTool("fill",
		mcp.WithDescription("Clear an input and set its value. For several fields, put fills in one batch call."),
		selectorParam(true),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to set.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.visibleOnly(fillTarget, fillArgv))

	r.add(core, mcp.NewTool("type",
		mcp.WithDescription("Type text without clearing. mode element types into selector; keystrokes/insert type at current focus (insert skips key events)."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to type.")),
		selectorParam(false),
		mcp.WithString("mode", mcp.Enum("element", "keystrokes", "insert"), mcp.Description("Default element when selector is set, else keystrokes.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.visibleOnly(typeTarget, typeArgv))

	r.add(core, mcp.NewTool("press_key",
		mcp.WithDescription("Press a key or combo (Enter, Tab, Control+a), or hold/release one with down/up. Control or Meta with a, c, x, v, z or y selects all, copies, cuts, pastes, undoes or redoes on every OS."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Key or combination. Modifiers: Control, Shift, Alt, Meta.")),
		mcp.WithString("action", mcp.Enum("press", "down", "up"), mcp.Description("Default press.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.handlePressKey)

	r.add(core, mcp.NewTool("element_action",
		mcp.WithDescription("Hover, focus, check, uncheck, scroll into view, or highlight an element."),
		selectorParam(true),
		mcp.WithString("action", mcp.Required(), mcp.Enum(elementActions...)),
		snapshotParam(), sessionParam(), mutating(),
	), r.visibleOnly(hoverTarget, elementActionArgv))

	r.add(core, mcp.NewTool("select_option",
		mcp.WithDescription("Select one or more options in a <select> by value."),
		selectorParam(true),
		mcp.WithArray("values", mcp.Required(), mcp.WithStringItems(), mcp.Description("Option values to select.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.action(selectArgv))

	r.add(core, mcp.NewTool("scroll",
		mcp.WithDescription("Scroll the page or a scrollable element. To reach a known element, prefer element_action scroll_into_view."),
		mcp.WithString("direction", mcp.Enum("up", "down", "left", "right"), mcp.Description("Default down.")),
		mcp.WithNumber("px", mcp.Description("Pixels to scroll. Default 300.")),
		mcp.WithString("selector", mcp.Description("CSS selector of a scrollable container.")),
		sessionParam(), mutating(),
	), r.cli(scrollArgv))

	r.add(core, mcp.NewTool("drag",
		mcp.WithDescription("Drag one element onto another."),
		mcp.WithString("source", mcp.Required(), mcp.Description("Source @ref or selector.")),
		mcp.WithString("target", mcp.Required(), mcp.Description("Target @ref or selector.")),
		humanParam(),
		snapshotParam(), sessionParam(), mutating(),
	), r.action(dragArgv))

	r.add(core, mcp.NewTool("upload_file",
		mcp.WithDescription("Set files on a file input."),
		selectorParam(true),
		mcp.WithArray("files", mcp.Required(), mcp.WithStringItems(), mcp.Description("Absolute file paths.")),
		sessionParam(), mutating(),
	), r.cli(uploadArgv))

	r.add(core, mcp.NewTool("download",
		mcp.WithDescription("Click an element that triggers a download and save the file."),
		selectorParam(true),
		mcp.WithString("path", mcp.Required(), mcp.Description("Where to save the file.")),
		sessionParam(), mutating(),
	), r.handleDownload)

	r.add(core, mcp.NewTool("eval_script",
		mcp.WithDescription("Run JavaScript in the page, an iframe or a worker and return the result. Fallback — prefer get, find or page_text, which are cheaper and safer."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript expression or statements.")),
		mcp.WithString("frame", mcp.Description("Run in an iframe (name or part of its URL) or a worker (worker:<part of its URL>) instead of the page.")),
		sessionParam(), mutating(),
	), r.handleEval)

	r.add(core, mcp.NewTool("close_browser",
		mcp.WithDescription("Close the browser for a session, or every session with all:true."),
		mcp.WithBoolean("all", mcp.Description("Close every session.")),
		sessionParam(), destructive(),
	), r.handleClose)
}

var (
	navigateActions = []string{"goto", "back", "forward", "reload", "pushstate"}
	elementActions  = []string{"hover", "focus", "check", "uncheck", "scroll_into_view", "highlight"}
	keyCommands     = map[string]string{"press": "press", "down": "keydown", "up": "keyup"}
)

func navigateArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch action := b.enum("action", "goto", navigateActions...); action {
	case "back", "forward", "reload":
		b.add(action)
	case "pushstate":
		b.add("pushstate", b.requiredFor("url", action))
	default:
		b.add("open", b.requiredFor("url", action)).flag("--headers", "headers")
	}
	return b.done()
}

func clickArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	sel := b.required("selector")
	if b.boolean("double") {
		return b.add("dblclick", sel).boolFlag("--human", "human").done()
	}
	return b.add("click", sel).boolFlag("--new-tab", "newTab").boolFlag("--human", "human").done()
}

// visibleOnly makes an action refuse a target that is not visible:
// agent-browser reports success for clicks, double-clicks, fills, typing and
// hovers on display:none elements even though nothing receives them, e.g. a
// desktop-only button after the page switched to its mobile layout. target
// returns the selector to check and what the action does to it, or "" when
// the call needs no visible target. Invalid input is reported before the check.
func (r *Registry) visibleOnly(target func(mcp.CallToolRequest) (sel, doing string), fn argvFunc) server.ToolHandlerFunc {
	next := r.action(fn)
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if _, err := fn(req); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if sel, doing := target(req); sel != "" && r.hidden(ctx, req, sel) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"%s is not visible (display:none, visibility:hidden or zero size), so %s would do nothing; the layout may have changed, so take a fresh snapshot and use a visible element", sel, doing)), nil
		}
		return next(ctx, req)
	}
}

// Targets that must be visible. A link opened in a new tab is not clicked,
// and check, select and focus work on hidden elements, so they are not guarded.
func clickTarget(req mcp.CallToolRequest) (string, string) {
	if req.GetBool("newTab", false) {
		return "", ""
	}
	return req.GetString("selector", ""), "clicking it"
}

func fillTarget(req mcp.CallToolRequest) (string, string) {
	return req.GetString("selector", ""), "filling it"
}

func typeTarget(req mcp.CallToolRequest) (string, string) {
	if mode := req.GetString("mode", ""); mode != "" && mode != "element" {
		return "", ""
	}
	return req.GetString("selector", ""), "typing into it"
}

func hoverTarget(req mcp.CallToolRequest) (string, string) {
	if req.GetString("action", "") != "hover" {
		return "", ""
	}
	return req.GetString("selector", ""), "hovering over it"
}

// hidden reports whether selector matches an element that is not visible. It
// is false when the check cannot tell (the element is missing, the CLI fails,
// or the page is paused in the debugger and would block the check), leaving
// the action to report its own errors.
func (r *Registry) hidden(ctx context.Context, req mcp.CallToolRequest, selector string) bool {
	if page := r.dt.Existing(getSession(req)); page != nil && page.Paused() != nil {
		return false
	}
	out, err := r.mgr.Run(ctx, getSession(req), "is", "visible", selector)
	if err != nil {
		return false
	}
	var v struct {
		Visible *bool `json:"visible"`
	}
	return json.Unmarshal(out.Data, &v) == nil && v.Visible != nil && !*v.Visible
}

func fillArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "fill")
	return b.add(b.required("selector"), b.provided("value")).done()
}

func typeArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	text := b.required("text")
	defaultMode := "keystrokes"
	if b.str("selector") != "" {
		defaultMode = "element"
	}
	switch b.enum("mode", defaultMode, "element", "keystrokes", "insert") {
	case "keystrokes":
		b.add("keyboard", "type", text)
	case "insert":
		b.add("keyboard", "inserttext", text)
	default:
		b.add("type", b.requiredFor("selector", "mode element"), text)
	}
	return b.done()
}

// handlePressKey runs editing shortcuts as the commands they stand for on
// macOS, where "Meta+a" reached the page as keys but selected nothing.
// "Control+a" there moves to the line start, but a model sending it means
// select all, as on Linux and Windows, so both modifiers work.
func (r *Registry) handlePressKey(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	key := req.GetString("key", "")
	command, letter, mods := editingShortcut(key)
	if !r.macEditing || command == "" || req.GetString("action", "press") != "press" {
		return r.action(pressKeyArgv)(ctx, req)
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return r.action(pressKeyArgv)(ctx, req)
	}
	if err := page.KeyCommand(ctx, letter, mods, command); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	res := mcp.NewToolResultText("pressed: " + key + " (" + command + ")")
	r.appendSnapshot(ctx, req, res)
	return res, nil
}

// editingShortcut maps Control or Meta (with Shift for redo) plus a, c, x,
// v, z or y to its editing command, the letter and Meta-based modifiers.
func editingShortcut(key string) (command, letter string, mods int) {
	parts := strings.Split(key, "+")
	letter = strings.ToLower(parts[len(parts)-1])
	primary, shift := false, false
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "control", "ctrl", "meta", "cmd", "command", "controlormeta":
			primary = true
		case "shift":
			shift = true
		default:
			return "", "", 0
		}
	}
	if !primary {
		return "", "", 0
	}
	commands := map[string]string{"a": "selectAll", "c": "copy", "x": "cut", "v": "paste", "z": "undo", "y": "redo"}
	command = commands[letter]
	if shift {
		if letter != "z" {
			return "", "", 0
		}
		command = "redo"
	}
	mods = devtools.ModMeta
	if shift {
		mods |= devtools.ModShift
	}
	return command, letter, mods
}

func pressKeyArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	key := b.required("key")
	return b.add(keyCommands[b.enum("action", "press", "press", "down", "up")], key).done()
}

func elementActionArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	sel := b.required("selector")
	action := b.enum("action", "", elementActions...)
	if action == "scroll_into_view" {
		action = "scrollintoview"
	}
	return b.add(action, sel).done()
}

func selectArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "select")
	return b.add(b.required("selector")).add(b.list("values")...).done()
}

func scrollArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "scroll")
	b.add(b.enum("direction", "down", "up", "down", "left", "right"))
	if req.GetFloat("px", 0) > 0 {
		b.add(b.int("px"))
	}
	return b.flag("--selector", "selector").done()
}

func dragArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "drag")
	return b.add(b.required("source"), b.required("target")).boolFlag("--human", "human").done()
}

// uploadArgv passes each file as its own argument; the CLI does not split commas.
func uploadArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "upload")
	return b.add(b.required("selector")).add(b.list("files")...).done()
}

// handleDownload downloads through agent-browser, except in a window from
// tabs new_window: that window is a separate browser context, which
// agent-browser does not set up for downloads, so the file is saved over CDP.
func (r *Registry) handleDownload(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := downloadArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return r.cli(downloadArgv)(ctx, req)
	}
	contextID, separate := page.SeparateContext(ctx)
	if !separate {
		return r.cli(downloadArgv)(ctx, req)
	}
	path, err := filepath.Abs(args[2])
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	click := func() error {
		_, err := r.mgr.Run(ctx, getSession(req), "click", args[1])
		return err
	}
	text, err := page.DownloadInContext(ctx, contextID, path, click, time.Duration(r.cfg.DefaultTimeout)*time.Millisecond)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(text), nil
}

func downloadArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "download")
	return b.add(b.required("selector"), b.required("path")).done()
}

// handleEval runs scripts over CDP the way the Console does, in the page or
// in an iframe or worker, which the CLI cannot reach. The CLI's eval left a
// top-level const declared, so running a script again failed with "Identifier
// has already been declared"; it is the fallback when CDP is unavailable.
func (r *Registry) handleEval(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	script, err := evalArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	where := req.GetString("frame", "")
	page, err := r.livePage(ctx, req)
	if err != nil {
		if where == "" && !errors.Is(err, devtools.ErrPaused) {
			return r.cli(evalArgv)(ctx, req)
		}
		return mcp.NewToolResultError(err.Error()), nil
	}
	var text string
	if where == "" {
		text, err = page.EvaluateScript(ctx, script[1])
	} else {
		text, err = page.EvaluateIn(ctx, where, script[1])
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

func evalArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "eval")
	return b.add(b.required("script")).done()
}

func (r *Registry) handleClose(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !req.GetBool("all", false) {
		r.dt.Forget(getSession(req))
		res := r.run(ctx, req, "close")
		if !res.IsError {
			r.mgr.RemoveSession(r.mgr.ResolveSession(getSession(req)))
		}
		return res, nil
	}
	r.dt.Close()
	res := r.run(ctx, req, "close", "--all")
	if !res.IsError {
		for _, s := range r.mgr.Sessions() {
			r.mgr.RemoveSession(s.Name)
		}
	}
	return res, nil
}
