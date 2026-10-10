package tools

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecdp"
)

// debuggerExtraFixture covers live edit, restart frame, function breakpoints
// and evaluating in an iframe.
func debuggerExtraFixture(s *fakecdp.Server) {
	r := func(result any, events ...ev) fakecdp.Reply { return fakecdp.Reply{Result: result, Events: events} }
	s.Reply("Debugger.setScriptSource", r(a{"status": "Ok"}))
	s.Reply("Debugger.restartFrame", r(a{}, resumed, pausedAt(12)))
	s.Reply("Debugger.setBreakpointOnFunctionCall", r(a{"breakpointId": "fbp-1"}))
	s.Reply("Page.getFrameTree", r(a{"frameTree": a{"frame": a{"id": "F1", "url": "http://fake/", "securityOrigin": "http://fake", "secureContextType": "InsecureScheme"}, "childFrames": []any{
		a{"frame": a{"id": "F2", "name": "ads", "url": "http://ads.fake/frame.html", "securityOrigin": "http://ads.fake", "secureContextType": "InsecureScheme"}},
	}}}))
	s.Reply("Runtime.enable", r(a{}, ev{Method: "Runtime.executionContextCreated", Params: a{"context": a{"id": 7, "auxData": a{"frameId": "F2", "isDefault": true}}}}))
}

func init() {
	cdpCases = append(cdpCases, debuggerExtraCases...)
}

var debuggerExtraCases = []cdpCase{
	{tool: "debugger", args: a{"action": "edit_source", "script": "http://fake/app.js", "find": "line 12", "replace": "line 12 fixed"},
		want:  []string{dom, overlay, enable, async, "Debugger.getScriptSource", "Fetch.enable"},
		text:  "edited http://fake/app.js at line 12; the edited file replaces the original until the browser closes or this server restarts (revert_source drops it)\nreloaded:",
		check: params("Fetch.enable", a{"patterns": []any{a{"urlPattern": "http://fake/app.js", "requestStage": "Request"}}})},
	{tool: "debugger", args: a{"action": "revert_source"}, prior: []toolCall{{"debugger", a{"action": "edit_source", "script": "http://fake/app.js", "find": "line 12", "replace": "x"}}},
		text: "reverted 1 edited script to the original\nreloaded:", check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			if m := s.Methods(); m[len(m)-1] != "Fetch.disable" {
				t.Errorf("no Fetch.disable after the last override went: %v", m)
			}
		}},
	{tool: "debugger", args: a{"action": "revert_source", "script": "http://fake/other.js"}, text: "no edited scripts to revert"},
	{tool: "debugger", args: a{"action": "edit_source", "script": "http://fake/app.js", "find": "line 1", "replace": "x"}, wantErr: "occurs 11 times"},
	{tool: "debugger", args: a{"action": "edit_source", "script": "http://fake/app.js", "find": "nope", "replace": ""}, wantErr: "find text not in http://fake/app.js"},
	{tool: "debugger", args: a{"action": "edit_source", "script": "http://fake/nope.js", "find": "x", "replace": ""}, wantErr: "no loaded script with URL http://fake/nope.js"},
	{tool: "debugger", args: a{"action": "restart_frame"}, paused: true, text: "restarted; Paused (breakpoint) on bp-1",
		check: params("Debugger.restartFrame", a{"callFrameId": "f0", "mode": "StepInto"})},
	{tool: "debugger", args: a{"action": "function_breakpoint", "expression": "app.save"}, text: "fbp-1: function calls to app.save (until the page reloads)",
		check: params("Debugger.setBreakpointOnFunctionCall", a{"objectId": "fn-1"})},
	{tool: "debugger", args: a{"action": "function_breakpoint", "expression": "app.save", "logMessage": "'saving'"},
		text: "if console.log('saving'), false", check: params("Debugger.setBreakpointOnFunctionCall", a{"condition": "console.log('saving'), false"})},
	{tool: "debugger", args: a{"action": "function_breakpoint", "expression": "count"}, wantErr: "count is 2, not a function"},
	{tool: "debugger", args: a{"action": "csp_breakpoint"}, text: "csp: Trusted Types CSP violations",
		check: params("DOMDebugger.setBreakOnCSPViolation", a{"violationTypes": []any{"trustedtype-sink-violation", "trustedtype-policy-violation"}})},
	{tool: "debugger", args: a{"action": "remove", "breakpointId": "csp"}, prior: []toolCall{{"debugger", a{"action": "csp_breakpoint"}}},
		text: "removed csp", check: params("DOMDebugger.setBreakOnCSPViolation", a{"violationTypes": []any{}})},
	{tool: "debugger", args: a{"action": "ignore", "patterns": []any{"node_modules", "vendor"}}, text: "skip scripts matching node_modules, vendor",
		check: params("Debugger.setBlackboxPatterns", a{"patterns": []any{"node_modules", "vendor"}})},
	{tool: "debugger", args: a{"action": "ignore"}, text: "ignore list cleared", check: params("Debugger.setBlackboxPatterns", a{"patterns": []any{}})},
	{tool: "debugger", args: a{"action": "status"}, prior: []toolCall{{"debugger", a{"action": "ignore", "patterns": []any{"vendor"}}}}, text: "ignoring vendor"},
	{tool: "eval_script", args: a{"script": "1+1", "frame": "ads"}, text: "in http://ads.fake/frame.html: 2", check: params("Runtime.evaluate", a{"contextId": 7.0})},
	{tool: "eval_script", args: a{"script": "1+1", "frame": "nowhere"}, wantErr: "no iframe named or at a URL containing \"nowhere\"; frames: ads (http://ads.fake/frame.html)"},
	{tool: "eval_script", args: a{"script": "1+1", "frame": "worker:sw.js"}, wantErr: "no worker or shared_worker or service_worker is running"},
}

// TestEditedScriptIsServedInstead: after edit_source, a request for the
// script gets the edited text, and other requests go through untouched.
func TestEditedScriptIsServedInstead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	if res := e.call("debugger", a{"action": "edit_source", "script": "http://fake/app.js", "find": "line 12", "replace": "fixed()"}); res.IsError {
		t.Fatal(res.text())
	}
	s.Push(ev{Method: "Fetch.requestPaused", Params: a{"requestId": "r1", "request": a{"url": "http://fake/app.js#x"}}})
	s.Push(ev{Method: "Fetch.requestPaused", Params: a{"requestId": "r2", "request": a{"url": "http://fake/other.js"}}})
	deadline := time.Now().Add(2 * time.Second)
	for !slices.Contains(s.Methods(), "Fetch.continueRequest") || !slices.Contains(s.Methods(), "Fetch.fulfillRequest") {
		if time.Now().After(deadline) {
			t.Fatalf("requests not answered: %v", s.Methods())
		}
		time.Sleep(10 * time.Millisecond)
	}
	f := s.Params("Fetch.fulfillRequest")
	body, _ := base64.StdEncoding.DecodeString(f["body"].(string))
	if f["requestId"] != "r1" || !strings.Contains(string(body), "line 11\nfixed()\nline 13") {
		t.Errorf("fulfilled %v with %q", f["requestId"], body)
	}
	if c := s.Params("Fetch.continueRequest"); c["requestId"] != "r2" {
		t.Errorf("continued %v", c["requestId"])
	}
}

// TestStackShowsAsyncFrames: the code that scheduled the paused function
// (a timer here) is part of the story, as in DevTools' Call Stack.
func TestStackShowsAsyncFrames(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	paused := pausedAt(12)
	paused.Params.(a)["asyncStackTrace"] = a{"description": "setTimeout", "callFrames": []any{
		a{"functionName": "schedule", "scriptId": "7", "url": "http://fake/app.js", "lineNumber": 29, "columnNumber": 4},
	}}
	s.Reply("Debugger.pause", fakecdp.Reply{Result: a{}, Events: []ev{paused}})
	got := e.call("debugger", a{"action": "pause"}).text()
	if !strings.Contains(got, "  -- setTimeout --\n  #2 schedule (http://fake/app.js:30:5)") {
		t.Fatalf("no async frames:\n%s", got)
	}
}

// bundleMap is a source map for a bundle whose two header lines come before
// the code of src/cart.ts, so bundle line N+2 is cart.ts line N.
func bundleMap() string {
	m, _ := json.Marshal(map[string]any{
		"version": 3, "sources": []string{"webpack://shop/./src/cart.ts"},
		"sourcesContent": []string{"// cart.ts\nexport function add() {\n  let total = 0;\n  total += price;\n  return total;\n}\n"},
		"mappings":       ";;AAAA;AACA;AACA;AACA;AACA;AACA",
	})
	return "data:application/json;base64," + base64.StdEncoding.EncodeToString(m)
}

// TestSourceMappedDebugging: pauses, stacks and breakpoints use the files
// the author wrote when the bundle has a source map.
func TestSourceMappedDebugging(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.Reply("Debugger.enable", fakecdp.Reply{Result: a{}, Events: []ev{{Method: "Debugger.scriptParsed", Params: a{
		"scriptId": "9", "url": "http://fake/bundle.js", "endLine": 20, "sourceMapURL": bundleMap(),
	}}}})
	s.Reply("Debugger.pause", fakecdp.Reply{Result: a{}, Events: []ev{{Method: "Debugger.paused", Params: a{"reason": "other", "callFrames": []any{a{
		"callFrameId": "f0", "functionName": "add", "url": "http://fake/bundle.js", "location": a{"scriptId": "9", "lineNumber": 5, "columnNumber": 0},
	}}}}}})

	got := e.call("debugger", a{"action": "pause"}).text()
	if !strings.Contains(got, "at add (http://fake/bundle.js:6:1 → src/cart.ts:4:1)") {
		t.Errorf("pause not mapped to the original file:\n%s", got)
	}
	// The excerpt is the authored file, as DevTools shows it.
	if !strings.Contains(got, "src/cart.ts:\n     2  export function add() {\n     3    let total = 0;\n►    4    total += price;") {
		t.Errorf("pause does not show the authored source:\n%s", got)
	}
	if got := e.call("debugger", a{"action": "breakpoint", "url": "src/cart.ts", "line": 4}).text(); !strings.Contains(got, "src/cart.ts:4 (in the bundle at http://fake/bundle.js:6:1)") {
		t.Errorf("authored breakpoint: %s", got)
	}
	params("Debugger.setBreakpointByUrl", a{"url": "http://fake/bundle.js", "lineNumber": 5.0, "columnNumber": 0.0})(t, s)
	if got := e.call("debugger", a{"action": "source", "script": "src/cart.ts", "from": 3, "to": 4}).text(); got != "src/cart.ts (from its source map):\n     3    let total = 0;\n     4    total += price;" {
		t.Errorf("authored source: %q", got)
	}
	if got := e.call("debugger", a{"action": "scripts"}).text(); !strings.Contains(got, "http://fake/bundle.js  id=9  21 lines  (source map)") {
		t.Errorf("scripts: %s", got)
	} // continue_to takes the authored file too.
	if res := e.call("debugger", a{"action": "continue_to", "url": "src/cart.ts", "line": 5}); res.IsError {
		t.Fatalf("continue_to src/cart.ts: %s", res.text())
	}
	params("Debugger.continueToLocation", a{"location": a{"scriptId": "9", "lineNumber": 6.0, "columnNumber": 0.0}})(t, s)
}
