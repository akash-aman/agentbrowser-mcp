package tools

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecli"
)

// argvCase is one tool call and the exact CLI commands it must produce
// (global flags stripped). wantErr means the call must fail before or at the
// CLI; with want empty it must not reach the CLI at all.
type argvCase struct {
	tool    string
	args    map[string]any
	want    [][]string
	wantErr string
	setup   func(*fakecli.Fake)
}

type a = map[string]any

func cmds(c ...[]string) [][]string { return c }
func cmd(c ...string) []string      { return c }

// visible is the check click runs before clicking a target.
func visible(sel string) []string { return cmd("is", "visible", sel) }

// loads is what navigate runs: the page errors so far, the load, then the
// errors and failed requests since, for the page health note.
func loads(c []string) [][]string {
	return cmds(cmd("errors"), c, cmd("errors"), cmd("network", "requests", "--status", "400-599"))
}

var argvCases = []argvCase{
	// navigate
	{tool: "navigate", args: a{"url": "http://x/"}, want: loads(cmd("open", "http://x/"))},
	{tool: "navigate", args: a{"action": "goto", "url": "http://x/"}, want: loads(cmd("open", "http://x/"))},
	{tool: "navigate", args: a{"action": "back"}, want: loads(cmd("back"))},
	{tool: "navigate", args: a{"action": "forward"}, want: loads(cmd("forward"))},
	{tool: "navigate", args: a{"action": "reload"}, want: loads(cmd("reload"))},
	{tool: "navigate", args: a{"action": "pushstate", "url": "/next"}, want: loads(cmd("pushstate", "/next"))},
	{tool: "navigate", args: a{"url": "http://x/", "headers": `{"Authorization":"Bearer t"}`}, want: loads(cmd("open", "http://x/", "--headers", `{"Authorization":"Bearer t"}`))},
	{tool: "navigate", args: a{"action": "goto"}, wantErr: "url is required for goto"},
	{tool: "navigate", args: a{"action": "pushstate"}, wantErr: "url is required for pushstate"},
	{tool: "navigate", args: a{"action": "sideways"}, wantErr: "action must be one of"},

	// click
	{tool: "click", args: a{"selector": "@e1"}, want: cmds(visible("@e1"), cmd("click", "@e1"))},
	// newTab opens the link's href, so the target need not be visible.
	{tool: "click", args: a{"selector": "@e1", "newTab": true}, want: cmds(cmd("click", "@e1", "--new-tab"))},
	{tool: "click", args: a{"selector": "@e1", "double": true}, want: cmds(visible("@e1"), cmd("dblclick", "@e1"))},
	{tool: "click", args: a{"selector": "@e1", "human": true}, want: cmds(visible("@e1"), cmd("click", "@e1", "--human"))},
	{tool: "click", args: a{"selector": "@e1", "double": true, "human": true}, want: cmds(visible("@e1"), cmd("dblclick", "@e1", "--human"))},
	{tool: "click", args: a{}, wantErr: "selector is required"},

	// fill
	{tool: "fill", args: a{"selector": "#q", "value": "hi"}, want: cmds(visible("#q"), cmd("fill", "#q", "hi"))},
	{tool: "fill", args: a{"selector": "#q", "value": ""}, want: cmds(visible("#q"), cmd("fill", "#q", ""))},
	{tool: "fill", args: a{"selector": "#q"}, wantErr: "value is required"},

	// type
	{tool: "type", args: a{"selector": "#q", "text": "hi"}, want: cmds(visible("#q"), cmd("type", "#q", "hi"))},
	{tool: "type", args: a{"selector": "#q", "text": "hi", "mode": "element"}, want: cmds(visible("#q"), cmd("type", "#q", "hi"))},
	{tool: "type", args: a{"text": "hi"}, want: cmds(cmd("keyboard", "type", "hi"))},
	{tool: "type", args: a{"text": "hi", "mode": "keystrokes"}, want: cmds(cmd("keyboard", "type", "hi"))},
	{tool: "type", args: a{"text": "hi", "mode": "insert"}, want: cmds(cmd("keyboard", "inserttext", "hi"))},
	{tool: "type", args: a{"text": "hi", "mode": "element"}, wantErr: "selector is required for mode element"},
	{tool: "type", args: a{"selector": "#q"}, wantErr: "text is required"},

	// press_key
	{tool: "press_key", args: a{"key": "Enter"}, want: cmds(cmd("press", "Enter"))},
	{tool: "press_key", args: a{"key": "Enter", "action": "press"}, want: cmds(cmd("press", "Enter"))},
	{tool: "press_key", args: a{"key": "Shift", "action": "down"}, want: cmds(cmd("keydown", "Shift"))},
	{tool: "press_key", args: a{"key": "Shift", "action": "up"}, want: cmds(cmd("keyup", "Shift"))},

	// element_action
	{tool: "element_action", args: a{"selector": "@e2", "action": "hover"}, want: cmds(visible("@e2"), cmd("hover", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2", "action": "focus"}, want: cmds(cmd("focus", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2", "action": "check"}, want: cmds(cmd("check", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2", "action": "uncheck"}, want: cmds(cmd("uncheck", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2", "action": "scroll_into_view"}, want: cmds(cmd("scrollintoview", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2", "action": "highlight"}, want: cmds(cmd("highlight", "@e2"))},
	{tool: "element_action", args: a{"selector": "@e2"}, wantErr: "action must be one of"},

	// select_option
	{tool: "select_option", args: a{"selector": "#s", "values": []any{"a", "b"}}, want: cmds(cmd("select", "#s", "a", "b"))},
	{tool: "select_option", args: a{"selector": "#s", "values": []any{}}, wantErr: "values must not be empty"},

	// scroll
	{tool: "scroll", args: a{}, want: cmds(cmd("scroll", "down"))},
	{tool: "scroll", args: a{"direction": "up", "px": 500}, want: cmds(cmd("scroll", "up", "500"))},
	{tool: "scroll", args: a{"direction": "down", "selector": ".pane"}, want: cmds(cmd("scroll", "down", "--selector", ".pane"))},
	{tool: "scroll", args: a{"direction": "left"}, want: cmds(cmd("scroll", "left"))},
	{tool: "scroll", args: a{"direction": "right", "px": 40}, want: cmds(cmd("scroll", "right", "40"))},

	// drag
	{tool: "drag", args: a{"source": "@e1", "target": "@e2"}, want: cmds(cmd("drag", "@e1", "@e2"))},
	{tool: "drag", args: a{"source": "@e1", "target": "@e2", "human": true}, want: cmds(cmd("drag", "@e1", "@e2", "--human"))},
	{tool: "drag", args: a{"source": "@e1"}, wantErr: "target is required"},

	// upload_file
	{tool: "upload_file", args: a{"selector": "#f", "files": []any{"/a.txt", "/b.txt"}}, want: cmds(cmd("upload", "#f", "/a.txt", "/b.txt"))},
	{tool: "upload_file", args: a{"selector": "#f"}, wantErr: "files must not be empty"},

	// download
	// The tab list and CDP URL check for a separate window's browser context first.
	{tool: "download", args: a{"selector": "@e5", "path": "/tmp/r.pdf"}, want: cmds(cmd("tab", "list"), cmd("get", "cdp-url"), cmd("download", "@e5", "/tmp/r.pdf"))},
	{tool: "download", args: a{"selector": "@e5"}, wantErr: "path is required"},

	// eval_script
	// Scripts run over CDP; the CLI's eval is the fallback without it.
	{tool: "eval_script", args: a{"script": "1+1"}, want: cmds(cmd("tab", "list"), cmd("get", "cdp-url"), cmd("eval", "1+1"))},
	{tool: "eval_script", args: a{}, wantErr: "script is required"},

	// close_browser
	{tool: "close_browser", args: a{}, want: cmds(cmd("close"))},
	{tool: "close_browser", args: a{"all": true}, want: cmds(cmd("close", "--all"))},

	// snapshot
	// A plain snapshot also reads the full tree as the next diff's baseline.
	{tool: "snapshot", args: a{}, want: cmds(cmd("snapshot", "-c"), cmd("snapshot"))},
	{tool: "snapshot", args: a{"interactive": true, "compact": false, "depth": 3, "selector": "main", "urls": true},
		want: cmds(cmd("snapshot", "-i", "-d", "3", "-s", "main", "--urls"))},
	{tool: "snapshot", args: a{"interactive": true, "delta": true}, want: cmds(cmd("snapshot", "-i", "-c", "--delta"))},
	{tool: "snapshot", args: a{"delta": true, "full": true}, want: cmds(cmd("snapshot", "-c", "--delta", "--full"))},
	// full implies delta: the CLI answers --full in delta form either way.
	{tool: "snapshot", args: a{"full": true}, want: cmds(cmd("snapshot", "-c", "--delta", "--full"))},

	// page_text
	{tool: "page_text", args: a{}, want: cmds(cmd("get", "text", "body"))},
	{tool: "page_text", args: a{"selector": "@e2"}, want: cmds(cmd("get", "text", "@e2"))},

	// get
	{tool: "get", args: a{"what": "text", "selector": "@e1"}, want: cmds(cmd("get", "text", "@e1"))},
	{tool: "get", args: a{"what": "html", "selector": "@e1"}, want: cmds(cmd("get", "html", "@e1"))},
	{tool: "get", args: a{"what": "value", "selector": "@e1"}, want: cmds(cmd("get", "value", "@e1"))},
	{tool: "get", args: a{"what": "attr", "selector": "@e1", "attribute": "href"}, want: cmds(cmd("get", "attr", "@e1", "href"))},
	{tool: "get", args: a{"what": "title"}, want: cmds(cmd("get", "title"))},
	{tool: "get", args: a{"what": "url"}, want: cmds(cmd("get", "url"))},
	{tool: "get", args: a{"what": "count", "selector": "li"}, want: cmds(cmd("get", "count", "li"))},
	{tool: "get", args: a{"what": "box", "selector": "@e1"}, want: cmds(cmd("get", "box", "@e1"))},
	{tool: "get", args: a{"what": "styles", "selector": "@e1"}, want: cmds(cmd("get", "styles", "@e1"))},
	{tool: "get", args: a{"what": "cdp_url"}, want: cmds(cmd("get", "cdp-url"))},
	{tool: "get", args: a{"what": "visible", "selector": "@e1"}, want: cmds(cmd("is", "visible", "@e1"))},
	{tool: "get", args: a{"what": "enabled", "selector": "@e1"}, want: cmds(cmd("is", "enabled", "@e1"))},
	{tool: "get", args: a{"what": "checked", "selector": "@e1"}, want: cmds(cmd("is", "checked", "@e1"))},
	{tool: "get", args: a{"what": "text"}, wantErr: "selector is required for what=text"},
	{tool: "get", args: a{"what": "attr", "selector": "@e1"}, wantErr: "attribute is required"},

	// find: every locator and every action
	{tool: "find", args: a{"by": "role", "value": "button", "name": "Submit", "action": "click"}, want: cmds(cmd("find", "role", "button", "click", "--name", "Submit"))},
	{tool: "find", args: a{"by": "text", "value": "Sign in", "exact": true}, want: cmds(cmd("find", "text", "Sign in", "click", "--exact"))},
	{tool: "find", args: a{"by": "label", "value": "Email", "action": "fill", "input": "a@b.c"}, want: cmds(cmd("find", "label", "Email", "fill", "a@b.c"))},
	{tool: "find", args: a{"by": "placeholder", "value": "Search", "action": "fill", "input": "go"}, want: cmds(cmd("find", "placeholder", "Search", "fill", "go"))},
	// agent-browser's find has no type, focus or uncheck.
	{tool: "find", args: a{"by": "placeholder", "value": "Search", "action": "type", "input": "go"}, wantErr: "action must be one of"},
	{tool: "find", args: a{"by": "alt", "value": "Logo", "action": "hover"}, want: cmds(cmd("find", "alt", "Logo", "hover"))},
	{tool: "find", args: a{"by": "title", "value": "Close", "action": "hover"}, want: cmds(cmd("find", "title", "Close", "hover"))},
	{tool: "find", args: a{"by": "testid", "value": "agree", "action": "check", "exact": true}, want: cmds(cmd("find", "testid", "agree", "check"))},
	{tool: "find", args: a{"by": "first", "value": "input[type=checkbox]", "action": "check"}, want: cmds(cmd("find", "first", "input[type=checkbox]", "check"))},
	{tool: "find", args: a{"by": "last", "value": "li", "action": "text"}, want: cmds(cmd("find", "last", "li", "text"))},
	{tool: "find", args: a{"by": "nth", "value": ".card", "index": 2}, want: cmds(cmd("find", "nth", "2", ".card", "click"))},
	{tool: "find", args: a{"by": "all", "value": "li.item"}, want: cmds(cmd("eval", `Array.from(document.querySelectorAll("li.item"), e => e.innerText.trim())`))},
	{tool: "find", args: a{"by": "label", "value": "Email", "action": "fill"}, wantErr: "input is required for action fill"},
	{tool: "find", args: a{"by": "nth", "value": ".card"}, wantErr: "index is required"},
	{tool: "find", args: a{"by": "css", "value": "x"}, wantErr: "by must be one of"},

	// wait
	{tool: "wait", args: a{"for": "element", "value": "#done"}, want: cmds(cmd("wait", "#done"))},
	// Polled with "is visible": the CLI's --state hidden collides with its global --state.
	{tool: "wait", args: a{"for": "hidden", "value": "#spinner"}, want: cmds(cmd("is", "visible", "#spinner")),
		setup: func(f *fakecli.Fake) { f.Respond("is visible", `{"visible":false}`) }},
	{tool: "wait", args: a{"for": "text", "value": "Welcome"}, want: cmds(cmd("wait", "--text", "Welcome"))},
	{tool: "wait", args: a{"for": "url", "value": "**/home"}, want: cmds(cmd("wait", "--url", "**/home"))},
	{tool: "wait", args: a{"for": "load"}, want: cmds(cmd("wait", "--load", "load"))},
	{tool: "wait", args: a{"for": "load", "value": "networkidle"}, want: cmds(cmd("wait", "--load", "networkidle"))},
	{tool: "wait", args: a{"for": "function", "value": "window.ready"}, want: cmds(cmd("wait", "--fn", "window.ready"))},
	{tool: "wait", args: a{"for": "time", "value": "500"}, want: cmds(cmd("wait", "500"))},
	{tool: "wait", args: a{"for": "download"}, want: cmds(cmd("wait", "--download"))},
	{tool: "wait", args: a{"for": "download", "value": "/tmp/f.zip"}, want: cmds(cmd("wait", "--download", "/tmp/f.zip"))},
	{tool: "wait", args: a{"for": "time", "value": "soon"}, wantErr: "value must be milliseconds"},
	{tool: "wait", args: a{"for": "text"}, wantErr: "value is required for for=text"},

	// screenshot
	{tool: "screenshot", args: a{}, want: cmds(cmd("screenshot", "--screenshot-format", "jpeg", "--screenshot-quality", "70"))},
	{tool: "screenshot", args: a{"format": "jpeg", "quality": 40, "fullPage": true}, want: cmds(cmd("screenshot", "--screenshot-format", "jpeg", "--screenshot-quality", "40", "--full"))},
	{tool: "screenshot", args: a{"format": "png", "annotate": true}, want: cmds(cmd("screenshot", "--screenshot-format", "png", "--annotate"))},
	{tool: "screenshot", args: a{"selector": "#hero", "path": "/tmp/hero.png"}, want: cmds(cmd("screenshot", "--screenshot-format", "png", "#hero", "/tmp/hero.png"))},
	{tool: "screenshot", args: a{"quality": 101}, wantErr: "quality must be 1-100"},

	// save_pdf
	{tool: "save_pdf", args: a{"path": "/tmp/p.pdf"}, want: cmds(cmd("pdf", "/tmp/p.pdf"))},
	{tool: "save_pdf", args: a{}, wantErr: "path is required"},

	// mouse
	{tool: "mouse", args: a{"action": "click", "x": 10, "y": 20}, want: cmds(cmd("mouse", "move", "10", "20"), cmd("mouse", "down", "left"), cmd("mouse", "up", "left"))},
	{tool: "mouse", args: a{"action": "click", "x": 1, "y": 2, "button": "right"}, want: cmds(cmd("mouse", "move", "1", "2"), cmd("mouse", "down", "right"), cmd("mouse", "up", "right"))},
	{tool: "mouse", args: a{"action": "move", "x": 5, "y": 6}, want: cmds(cmd("mouse", "move", "5", "6"))},
	{tool: "mouse", args: a{"action": "move", "x": 5, "y": 6, "human": true, "seed": 0, "duration": 250},
		want: cmds(cmd("mouse", "move", "5", "6", "--human", "--seed", "0", "--duration", "250"))},
	{tool: "mouse", args: a{"action": "click", "x": 1, "y": 2, "human": true},
		want: cmds(cmd("mouse", "move", "1", "2", "--human"), cmd("mouse", "down", "left"), cmd("mouse", "up", "left"))},
	{tool: "mouse", args: a{"action": "down", "button": "left"}, want: cmds(cmd("mouse", "down", "left"))},
	{tool: "mouse", args: a{"action": "up", "button": "middle"}, want: cmds(cmd("mouse", "up", "middle"))},
	{tool: "mouse", args: a{"action": "wheel", "deltaY": 100, "deltaX": 5}, want: cmds(cmd("mouse", "wheel", "100", "5"))},
	{tool: "mouse", args: a{"action": "wheel", "deltaY": -50}, want: cmds(cmd("mouse", "wheel", "-50"))},
	{tool: "mouse", args: a{"action": "click", "x": 1}, wantErr: "x and y are required"},
	{tool: "mouse", args: a{"action": "wheel"}, wantErr: "deltaY or deltaX is required"},

	// tabs
	// Then the tab list again and the CDP URL, to read current titles over CDP.
	{tool: "tabs", args: a{}, want: cmds(cmd("tab", "list"), cmd("tab", "list"), cmd("get", "cdp-url"))},
	{tool: "tabs", args: a{"action": "list"}, want: cmds(cmd("tab", "list"), cmd("tab", "list"), cmd("get", "cdp-url"))},
	{tool: "tabs", args: a{"action": "new", "url": "http://d/", "label": "docs"}, want: cmds(cmd("tab", "new", "--label", "docs", "http://d/"))},
	{tool: "tabs", args: a{"action": "switch", "tab": "t2"}, want: cmds(cmd("tab", "t2"))},
	{tool: "tabs", args: a{"action": "close", "tab": "docs"}, want: cmds(cmd("tab", "close", "docs"))},
	{tool: "tabs", args: a{"action": "close"}, want: cmds(cmd("tab", "close"))},
	{tool: "tabs", args: a{"action": "new_window"}, want: cmds(cmd("window", "new"))},
	// "window new" takes no URL, so the new window opens it next.
	{tool: "tabs", args: a{"action": "new_window", "url": "http://x/"}, want: cmds(cmd("window", "new"), cmd("open", "http://x/"))},
	{tool: "tabs", args: a{"action": "switch"}, wantErr: "tab is required for switch"},

	// dialog
	{tool: "dialog", args: a{"action": "status"}, want: cmds(cmd("dialog", "status"))},
	{tool: "dialog", args: a{"action": "accept", "text": "yes"}, want: cmds(cmd("dialog", "accept", "yes"))},
	{tool: "dialog", args: a{"action": "dismiss"}, want: cmds(cmd("dialog", "dismiss"))},

	// console
	{tool: "console", args: a{}, want: cmds(cmd("console"))},
	{tool: "console", args: a{"kind": "log", "clear": true}, want: cmds(cmd("console"), cmd("console", "--clear"))},
	{tool: "console", args: a{"kind": "errors"}, want: cmds(cmd("errors"))},
	{tool: "console", args: a{"pattern": "("}, wantErr: "invalid pattern"},

	// batch (full behaviour in batch_test.go)
	{tool: "batch", args: a{"steps": []any{a{"tool": "click", "args": a{"selector": "@e1"}}}}, want: cmds(visible("@e1"), cmd("click", "@e1"))},
	{tool: "batch", args: a{"steps": []any{}}, wantErr: "steps must be a non-empty array"},

	// help
	{tool: "help", args: a{}, want: cmds(cmd("--help"))},
	{tool: "help", args: a{"topic": "discover", "command": "tab new"}, want: cmds(cmd("tab", "new", "--help"))},
	// doctor also reports the CLI version, checked once per server.
	{tool: "help", args: a{"topic": "doctor"}, want: cmds(cmd("doctor"), cmd("--version"))},
	{tool: "help", args: a{"topic": "doctor", "fix": true}, want: cmds(cmd("doctor", "--fix"), cmd("--version"))},
	{tool: "help", args: a{"topic": "skills"}, want: cmds(cmd("skills", "list"))},
	{tool: "help", args: a{"topic": "skills", "name": "core", "full": true}, want: cmds(cmd("skills", "get", "core", "--full"))},

	// network
	{tool: "network", args: a{}, want: cmds(cmd("network", "requests"))},
	{tool: "network", args: a{"action": "requests", "filter": "api", "type": "xhr,fetch", "method": "POST", "status": "4xx"},
		want: cmds(cmd("network", "requests", "--filter", "api", "--type", "xhr,fetch", "--method", "POST", "--status", "4xx"))},
	{tool: "network", args: a{"action": "requests", "clear": true}, want: cmds(cmd("network", "requests"), cmd("network", "requests", "--clear"))},
	{tool: "network", args: a{"action": "detail", "requestId": "1.2"}, want: cmds(cmd("network", "request", "1.2"))},
	{tool: "network", args: a{"action": "route", "url": "**/api", "abort": true, "resourceType": "script"}, want: cmds(cmd("network", "route", "**/api", "--abort", "--resource-type", "script"))},
	{tool: "network", args: a{"action": "route", "url": "**/data", "body": `{"ok":1}`}, want: cmds(cmd("network", "route", "**/data", "--body", `{"ok":1}`))},
	{tool: "network", args: a{"action": "unroute", "url": "**/api"}, want: cmds(cmd("network", "unroute", "**/api"))},
	{tool: "network", args: a{"action": "unroute"}, want: cmds(cmd("network", "unroute"))},
	{tool: "network", args: a{"action": "har_start"}, want: cmds(cmd("network", "har", "start"))},
	{tool: "network", args: a{"action": "har_stop", "path": "/tmp/a.har"}, want: cmds(cmd("network", "har", "stop", "/tmp/a.har"))},
	{tool: "network", args: a{"action": "har_stop"}, want: cmds(cmd("network", "har", "stop"))},
	{tool: "network", args: a{"action": "detail"}, wantErr: "requestId is required for detail"},
	{tool: "network", args: a{"action": "route"}, wantErr: "url is required for route"},

	// performance
	{tool: "performance", args: a{"action": "vitals"}, want: cmds(cmd("vitals"))},
	{tool: "performance", args: a{"action": "vitals", "url": "http://x/"}, want: cmds(cmd("vitals", "http://x/"))},
	{tool: "performance", args: a{"action": "timing", "limit": 3}, want: cmds(cmd("eval", fmt.Sprintf(timingScript, 3)))},
	{tool: "performance", args: a{"action": "memory"}, want: cmds(cmd("eval", memoryScript))},
	{tool: "performance", args: a{"action": "trace_start"}, want: cmds(cmd("trace", "start"))},
	{tool: "performance", args: a{"action": "trace_stop", "path": "/tmp/t.json"}, want: cmds(cmd("trace", "stop", "/tmp/t.json"))},
	{tool: "performance", args: a{"action": "profiler_start"}, want: cmds(cmd("profiler", "start"))},
	{tool: "performance", args: a{"action": "profiler_stop"}, want: cmds(cmd("profiler", "stop"))},

	// react
	{tool: "react", args: a{"action": "tree"}, want: cmds(cmd("react", "tree"))},
	{tool: "react", args: a{"action": "inspect", "fiberId": "12"}, want: cmds(cmd("react", "inspect", "12"))},
	{tool: "react", args: a{"action": "renders_start"}, want: cmds(cmd("react", "renders", "start"))},
	{tool: "react", args: a{"action": "renders_stop"}, want: cmds(cmd("react", "renders", "stop"))},
	{tool: "react", args: a{"action": "suspense"}, want: cmds(cmd("react", "suspense"))},
	{tool: "react", args: a{"action": "suspense", "onlyDynamic": true}, want: cmds(cmd("react", "suspense", "--only-dynamic"))},
	{tool: "react", args: a{"action": "inspect"}, wantErr: "fiberId is required"},

	// record
	{tool: "record", args: a{"action": "start", "path": "/tmp/r.webm", "url": "http://x/"}, want: cmds(cmd("record", "start", "/tmp/r.webm", "http://x/"))},
	{tool: "record", args: a{"action": "stop"}, want: cmds(cmd("record", "stop"))},
	{tool: "record", args: a{"action": "restart", "path": "/tmp/r2.webm"}, want: cmds(cmd("record", "restart", "/tmp/r2.webm"))},
	{tool: "record", args: a{"action": "start", "path": "/tmp/r.webm", "cursor": true}, want: cmds(cmd("record", "start", "/tmp/r.webm", "--cursor"))},
	{tool: "record", args: a{"action": "start"}, wantErr: "path is required for start"},

	// diff
	// The full tree: compact mode leaves out text such as status messages.
	{tool: "diff", args: a{"kind": "snapshot"}, want: cmds(cmd("snapshot"))},
	{tool: "diff", args: a{"kind": "snapshot", "selector": "main"}, want: cmds(cmd("snapshot", "-s", "main"))},
	{tool: "diff", args: a{"kind": "snapshot", "baseline": "/tmp/b.txt", "selector": "main"}, want: cmds(cmd("diff", "snapshot", "-c", "--baseline", "/tmp/b.txt", "--selector", "main"))},
	{tool: "diff", args: a{"kind": "screenshot", "baseline": "/tmp/b.png", "output": "/tmp/d.png", "threshold": 0.2, "fullPage": true},
		want: cmds(cmd("diff", "screenshot", "--baseline", "/tmp/b.png", "--output", "/tmp/d.png", "--threshold", "0.2", "--full"))},
	{tool: "diff", args: a{"kind": "urls", "urlA": "http://a/", "urlB": "http://b/", "screenshot": true, "waitUntil": "networkidle"},
		want: cmds(cmd("diff", "url", "http://a/", "http://b/", "--screenshot", "--wait-until", "networkidle"))},
	{tool: "diff", args: a{"kind": "urls", "urlA": "http://a/", "urlB": "http://b/", "waitUntil": "load", "fullPage": true},
		want: cmds(cmd("diff", "url", "http://a/", "http://b/", "--full", "--wait-until", "load"))},
	{tool: "diff", args: a{"kind": "urls", "urlA": "http://a/", "urlB": "http://b/", "waitUntil": "domcontentloaded"},
		want: cmds(cmd("diff", "url", "http://a/", "http://b/", "--wait-until", "domcontentloaded"))},
	{tool: "diff", args: a{"kind": "screenshot"}, wantErr: "baseline is required"},
	{tool: "diff", args: a{"kind": "urls", "urlA": "http://a/"}, wantErr: "urlB is required"},

	// debug_ui
	{tool: "debug_ui", args: a{"action": "open_devtools", "external": true}, want: cmds(cmd("inspect"))},
	{tool: "debug_ui", args: a{"action": "stream_enable", "port": 9333}, want: cmds(cmd("stream", "enable", "--port", "9333"))},
	{tool: "debug_ui", args: a{"action": "stream_disable"}, want: cmds(cmd("stream", "disable"))},
	{tool: "debug_ui", args: a{"action": "stream_status"}, want: cmds(cmd("stream", "status"))},
	{tool: "debug_ui", args: a{"action": "dashboard_start", "port": 4900}, want: cmds(cmd("dashboard", "start", "--port", "4900"))},
	{tool: "debug_ui", args: a{"action": "dashboard_stop"}, want: cmds(cmd("dashboard", "stop"))},

	// emulate
	{tool: "emulate", args: a{"width": 390, "height": 844, "scale": 3}, want: cmds(cmd("set", "viewport", "390", "844", "3"))},
	{tool: "emulate", args: a{"device": "iPhone 14"}, want: cmds(cmd("set", "device", "iPhone 14"))},
	{tool: "emulate", args: a{"latitude": 12.97, "longitude": 77.59}, want: cmds(cmd("set", "geo", "12.97", "77.59"))},
	{tool: "emulate", args: a{"offline": true}, want: cmds(cmd("set", "offline", "on"))},
	{tool: "emulate", args: a{"offline": false}, want: cmds(cmd("set", "offline", "off"))},
	{tool: "emulate", args: a{"headers": `{"X-A":"1"}`}, want: cmds(cmd("set", "headers", `{"X-A":"1"}`))},
	{tool: "emulate", args: a{"username": "u", "password": "p"}, want: cmds(cmd("set", "credentials", "u", "p"))},
	{tool: "emulate", args: a{"colorScheme": "dark"}, want: cmds(cmd("set", "media", "dark"))},
	{tool: "emulate", args: a{"colorScheme": "light", "reducedMotion": true}, want: cmds(cmd("set", "media", "light", "reduced-motion"))},
	{tool: "emulate", args: a{"colorScheme": "no-preference"}, wantErr: "colorScheme must be one of"},
	// agent-browser turns reduced motion off when it is not asked for.
	{tool: "emulate", args: a{"reducedMotion": false}, want: cmds(cmd("set", "media"))},
	{tool: "emulate", args: a{"reducedMotion": true}, want: cmds(cmd("set", "media", "reduced-motion"))},
	{tool: "emulate", args: a{"device": "Pixel 7", "offline": true, "colorScheme": "dark"},
		want: cmds(cmd("set", "device", "Pixel 7"), cmd("set", "offline", "on"), cmd("set", "media", "dark"))},
	{tool: "emulate", args: a{"width": 100}, wantErr: "width and height must be set together"},
	{tool: "emulate", args: a{"latitude": 1}, wantErr: "latitude and longitude must be set together"},
	{tool: "emulate", args: a{"username": "u"}, wantErr: "password is required"},
	{tool: "emulate", args: a{}, wantErr: "set at least one emulation setting"},
	{tool: "emulate", args: a{"colorScheme": "sepia"}, wantErr: "colorScheme must be one of"},

	// cookies
	{tool: "cookies", args: a{}, want: cmds(cmd("cookies", "get"))},
	{tool: "cookies", args: a{"action": "list"}, want: cmds(cmd("cookies", "get"))},
	{tool: "cookies", args: a{"action": "set", "name": "sid", "value": "abc", "url": "http://x/", "domain": ".x", "path": "/api",
		"sameSite": "Strict", "httpOnly": true, "secure": true, "expires": 1735689600},
		want: cmds(cmd("cookies", "set", "sid", "abc", "--url", "http://x/", "--domain", ".x", "--path", "/api", "--sameSite", "Strict", "--httpOnly", "--secure", "--expires", "1735689600"))},
	{tool: "cookies", args: a{"action": "set", "name": "a", "value": "1", "sameSite": "Lax"}, want: cmds(cmd("cookies", "set", "a", "1", "--sameSite", "Lax"))},
	{tool: "cookies", args: a{"action": "set", "name": "a", "value": "1", "sameSite": "None"}, want: cmds(cmd("cookies", "set", "a", "1", "--sameSite", "None"))},
	{tool: "cookies", args: a{"action": "clear"}, want: cmds(cmd("cookies", "clear"))},
	{tool: "cookies", args: a{"action": "import", "source": "/tmp/c.txt", "domain": "x.com"}, want: cmds(cmd("cookies", "set", "--curl", "/tmp/c.txt", "--domain", "x.com"))},
	{tool: "cookies", args: a{"action": "set"}, wantErr: "name is required for set"},
	{tool: "cookies", args: a{"action": "import"}, wantErr: "source is required for import"},

	// storage
	{tool: "storage", args: a{}, want: cmds(cmd("storage", "local"))},
	{tool: "storage", args: a{"area": "local", "action": "get", "key": "k"}, want: cmds(cmd("storage", "local", "get", "k"))},
	{tool: "storage", args: a{"area": "session", "action": "set", "key": "k", "value": "v"}, want: cmds(cmd("storage", "session", "set", "k", "v"))},
	{tool: "storage", args: a{"area": "session", "action": "clear"}, want: cmds(cmd("storage", "session", "clear"))},
	{tool: "storage", args: a{"action": "set"}, wantErr: "key is required for set"},

	// state
	{tool: "state", args: a{"action": "save", "path": "/tmp/s.json"}, want: cmds(cmd("state", "save", "/tmp/s.json"))},
	{tool: "state", args: a{"action": "load", "path": "/tmp/s.json"}, want: cmds(cmd("state", "load", "/tmp/s.json"))},
	{tool: "state", args: a{"action": "list"}, want: cmds(cmd("state", "list"))},
	{tool: "state", args: a{"action": "show", "path": "s.json"}, want: cmds(cmd("state", "show", "s.json"))},
	{tool: "state", args: a{"action": "clear"}, want: cmds(cmd("state", "clear"))},
	{tool: "state", args: a{"action": "clear", "all": true}, want: cmds(cmd("state", "clear", "--all"))},
	{tool: "state", args: a{"action": "clean", "olderThanDays": 7}, want: cmds(cmd("state", "clean", "--older-than", "7"))},
	{tool: "state", args: a{"action": "clean"}, wantErr: "olderThanDays is required for clean"},
	{tool: "state", args: a{"action": "save"}, wantErr: "path is required for save"},
	{tool: "state", args: a{"action": "rename", "path": "a"}, wantErr: "newName is required"},

	// clipboard
	{tool: "clipboard", args: a{"action": "write"}, wantErr: "text is required for write"},

	// session
	{tool: "session", args: a{}, want: cmds(cmd("session"), cmd("get", "url"))},
	{tool: "session", args: a{"action": "info"}, want: cmds(cmd("session"), cmd("get", "url"))},
	{tool: "session", args: a{"action": "list"}, want: cmds(cmd("session", "list"))},
	{tool: "session", args: a{"action": "profiles"}, want: cmds(cmd("profiles"))},
	{tool: "session", args: a{"action": "connect", "target": "9222"}, want: cmds(cmd("connect", "9222"))},
	{tool: "session", args: a{"action": "connect"}, wantErr: "target is required for connect"},

	// auth
	{tool: "auth", args: a{"action": "list"}, want: cmds(cmd("auth", "list"))},
	{tool: "auth", args: a{"action": "show", "name": "gh"}, want: cmds(cmd("auth", "show", "gh"))},
	{tool: "auth", args: a{"action": "login", "name": "gh"}, want: cmds(cmd("auth", "login", "gh"))},
	{tool: "auth", args: a{"action": "delete", "name": "gh"}, want: cmds(cmd("auth", "delete", "gh"))},
	{tool: "auth", args: a{"action": "login"}, wantErr: "name is required for login"},
}

func caseName(i int, c argvCase) string {
	var parts []string
	for _, k := range []string{"action", "by", "what", "for", "kind", "mode", "topic", "area", "format"} {
		if v, ok := c.args[k]; ok {
			parts = append(parts, fmt.Sprint(v))
		}
	}
	name := fmt.Sprintf("%03d_%s", i, c.tool)
	if len(parts) > 0 {
		name += "_" + strings.Join(parts, "_")
	}
	if c.wantErr != "" {
		name += "_error"
	}
	return name
}

func TestToolArgv(t *testing.T) {
	t.Parallel()
	for i, c := range argvCases {
		t.Run(caseName(i, c), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(cfg *config.Config) {
				if c.tool == "react" {
					cfg.Enable = "react-devtools"
				}
			})
			if c.setup != nil {
				c.setup(e.fake)
			}
			res := e.call(c.tool, c.args)
			got := e.fake.Commands()

			if c.wantErr != "" {
				if !res.IsError {
					t.Fatalf("want error containing %q, got success: %s", c.wantErr, res.text())
				}
				if !strings.Contains(res.text(), c.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", res.text(), c.wantErr)
				}
				if c.want == nil && len(got) != 0 {
					t.Fatalf("invalid input must not reach the CLI, got calls %q", got)
				}
				return
			}
			if res.IsError {
				t.Fatalf("unexpected error: %s", res.text())
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("CLI calls\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}

// TestSessionFlagOnEveryTool checks that each tool forwards its session
// argument to every CLI call it makes (the bug that hit navigate back/forward).
func TestSessionFlagOnEveryTool(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range argvCases {
		if c.wantErr != "" || seen[c.tool] || c.tool == "help" {
			continue
		}
		seen[c.tool] = true
		t.Run(c.tool, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(cfg *config.Config) { cfg.Enable = "react-devtools" })
			if c.setup != nil {
				c.setup(e.fake)
			}
			args := map[string]any{"session": "s1"}
			for k, v := range c.args {
				args[k] = v
			}
			if res := e.call(c.tool, args); res.IsError {
				t.Fatalf("unexpected error: %s", res.text())
			}
			for _, call := range e.fake.Calls() {
				if !hasFlag(call, "--session", "s1") {
					t.Fatalf("call %q is missing --session s1", call)
				}
			}
		})
	}
}

func hasFlag(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
