package tools

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecdp"
)

const fakePageURL = "http://fake/"

// withCDP starts a fake browser for CDP-backed tools and points the fake
// CLI's get cdp-url and get url at it.
func (e *env) withCDP() *fakecdp.Server {
	s := fakecdp.New(e.t, fakePageURL)
	browserFixture(s)
	e.fake.SetURL(fakePageURL)
	e.fake.Respond("get cdp-url", fmt.Sprintf(`{"cdpUrl":%q}`, s.WSURL()))
	e.t.Cleanup(e.reg.Close)
	e.cdp = s
	return s
}

type ev = fakecdp.Event

// pausedAt is the Debugger.paused event for a stop in onClick at app.js:line.
func pausedAt(line int) ev {
	frame := func(id, fn string, l int) a {
		return a{
			"callFrameId": id, "functionName": fn, "url": "http://fake/app.js",
			"location": a{"scriptId": "7", "lineNumber": l - 1, "columnNumber": 2},
			"scopeChain": []any{
				a{"type": "local", "object": a{"type": "object", "objectId": "scope-" + id}},
				a{"type": "global", "object": a{"type": "object", "objectId": "global"}},
			},
		}
	}
	return ev{Method: "Debugger.paused", Params: a{
		"reason": "other", "hitBreakpoints": []any{"bp-1"},
		"callFrames": []any{frame("f0", "onClick", line), frame("f1", "", 30)},
	}}
}

var resumed = ev{Method: "Debugger.resumed", Params: a{}}

// browserFixture gives the fake browser realistic replies for every method
// the CDP-backed tools use.
func browserFixture(s *fakecdp.Server) {
	r := func(result any, events ...ev) fakecdp.Reply { return fakecdp.Reply{Result: result, Events: events} }
	var src []string
	for i := 1; i <= 40; i++ {
		src = append(src, fmt.Sprintf("line %d", i))
	}
	s.Reply("Debugger.enable", r(a{"debuggerId": "D"}, ev{Method: "Debugger.scriptParsed", Params: a{"scriptId": "7", "url": "http://fake/app.js", "endLine": 39}}))
	s.Reply("Debugger.setBreakpointByUrl", r(a{"breakpointId": "bp-1", "locations": []any{a{"scriptId": "7", "lineNumber": 11, "columnNumber": 2}}}))
	s.Reply("Debugger.getScriptSource", r(a{"scriptSource": strings.Join(src, "\n")}))
	s.Reply("Debugger.pause", r(a{}, pausedAt(12)))
	for _, m := range []string{"Debugger.stepOver", "Debugger.stepInto", "Debugger.stepOut", "Debugger.continueToLocation"} {
		s.Reply(m, r(a{}, resumed, pausedAt(13)))
	}
	s.Reply("Debugger.resume", r(a{}, resumed))
	s.Reply("Debugger.evaluateOnCallFrame", r(a{"result": a{"type": "number", "value": 3, "description": "3"}}))
	s.Handle("Runtime.evaluate", func(p map[string]any) fakecdp.Reply {
		expr, _ := p["expression"].(string)
		switch {
		case expr == "location.origin":
			return r(a{"result": a{"type": "string", "value": "http://fake"}})
		case strings.Contains(expr, "querySelector"):
			return r(a{"result": a{"type": "object", "subtype": "node", "objectId": "node-1", "description": "button"}})
		case strings.Contains(expr, "serviceWorker"):
			return r(a{"result": a{"type": "string", "value": "http://fake/ activated http://fake/sw.js"}})
		}
		return r(a{"result": a{"type": "number", "value": 2, "description": "2"}})
	})
	s.Reply("Runtime.getProperties", r(a{"result": []any{
		a{"name": "total", "value": a{"type": "number", "value": 42, "description": "42"}},
		a{"name": "cart", "value": a{"type": "object", "className": "Cart", "objectId": "obj-2", "description": "Cart",
			"preview": a{"properties": []any{a{"name": "items", "type": "number", "value": "3"}}}}},
		a{"name": "t"},
	}}))
	s.Reply("DOMDebugger.getEventListeners", r(a{"listeners": []any{a{
		"type": "click", "useCapture": false, "passive": true, "once": false, "scriptId": "7", "lineNumber": 11, "columnNumber": 2,
		"handler": a{"type": "function", "description": "function onClick(e) {\n}"},
	}}}))
	s.Reply("DOM.getDocument", r(a{"root": a{"nodeId": 1}}))
	s.Reply("DOM.querySelector", r(a{"nodeId": 5}))
	s.Reply("Performance.getMetrics", r(a{"metrics": []any{
		a{"name": "Timestamp", "value": 1}, a{"name": "JSHeapUsedSize", "value": 1 << 20}, a{"name": "Nodes", "value": 42}, a{"name": "ScriptDuration", "value": 0.012},
		a{"name": "FirstMeaningfulPaint", "value": 225736.9}, a{"name": "ThreadTime", "value": 0.0008},
	}}))
	half := len(heapFixture) / 2
	s.Reply("HeapProfiler.takeHeapSnapshot", r(a{},
		ev{Method: "HeapProfiler.addHeapSnapshotChunk", Params: a{"chunk": heapFixture[:half]}},
		ev{Method: "HeapProfiler.addHeapSnapshotChunk", Params: a{"chunk": heapFixture[half:]}}))
	s.Reply("CSS.enable", r(a{}, ev{Method: "CSS.styleSheetAdded", Params: a{"header": a{"styleSheetId": "css1", "sourceURL": "http://fake/a.css", "length": 200}}}))
	s.Reply("Profiler.takePreciseCoverage", r(a{"result": []any{a{"url": "http://fake/app.js", "functions": []any{
		a{"ranges": []any{a{"startOffset": 0, "endOffset": 1000, "count": 1}, a{"startOffset": 200, "endOffset": 800, "count": 0}}},
	}}}}))
	s.Reply("CSS.stopRuleUsageTracking", r(a{"ruleUsage": []any{
		a{"styleSheetId": "css1", "startOffset": 0, "endOffset": 50, "used": true},
		a{"styleSheetId": "css1", "startOffset": 50, "endOffset": 200, "used": false},
	}}))
	s.Reply("Audits.enable", r(a{}, ev{Method: "Audits.issueAdded", Params: a{"issue": a{
		"code": "MixedContentIssue", "details": a{"mixedContentIssueDetails": a{"insecureURL": "http://cdn/x.js"}},
	}}}))
	s.Reply("IndexedDB.requestDatabaseNames", r(a{"databaseNames": []any{"shop"}}))
	s.Reply("IndexedDB.requestDatabase", r(a{"databaseWithObjectStores": a{"version": 2, "objectStores": []any{
		a{"name": "carts", "keyPath": a{"type": "string", "string": "id"}, "autoIncrement": false, "indexes": []any{a{"name": "byUser"}}},
	}}}))
	s.Reply("IndexedDB.requestData", r(a{"hasMore": true, "objectStoreDataEntries": []any{a{
		"key":   a{"type": "number", "value": 1, "description": "1"},
		"value": a{"type": "object", "className": "Object", "preview": a{"properties": []any{a{"name": "user", "type": "string", "value": "ann"}}}},
	}}}))
	s.Reply("CacheStorage.requestCacheNames", r(a{"caches": []any{a{"cacheId": "c1", "cacheName": "v1"}}}))
	s.Reply("CacheStorage.requestEntries", r(a{"returnCount": 1, "cacheDataEntries": []any{a{"requestURL": "http://fake/app.js", "requestMethod": "GET", "responseStatus": 200}}}))
	s.Reply("Page.getAppManifest", r(a{"url": "http://fake/manifest.json", "errors": []any{}, "data": `{"name": "Fake"}`}))
	s.Reply("Storage.getUsageAndQuota", r(a{"usage": 2048, "quota": 1 << 30, "usageBreakdown": []any{a{"storageType": "indexeddb", "usage": 2048}, a{"storageType": "cookies", "usage": 0}}}))
}

// pauseOnEnable makes the page already paused once the debugger turns on.
func pauseOnEnable(s *fakecdp.Server) {
	s.Reply("Debugger.enable", fakecdp.Reply{Result: a{}, Events: []ev{
		{Method: "Debugger.scriptParsed", Params: a{"scriptId": "7", "url": "http://fake/app.js", "endLine": 39}},
		pausedAt(12),
	}})
}

const heapFixture = `{"snapshot":{"meta":{"node_fields":["type","name","id","self_size","edge_count","trace_node_id","detachedness"],
"node_types":[["hidden","array","string","object","code","closure","regexp","number","native"],"string","number","number","number","number","number"]}},
"nodes":[3,0,1,100,0,0,0,8,1,2,20,0,0,2],"edges":[],"strings":["Widget","HTMLDivElement"]}`

type toolCall struct {
	tool string
	args map[string]any
}

// cdpCase is a tool call against the fake browser: the CDP methods it must
// send (in order, including prior calls) and text its result must contain.
type cdpCase struct {
	tool    string
	args    map[string]any
	prior   []toolCall
	paused  bool
	setup   func(*env)
	want    []string
	text    string
	wantErr string
	check   func(*testing.T, *fakecdp.Server)
}

func params(method string, want map[string]any) func(*testing.T, *fakecdp.Server) {
	return func(t *testing.T, s *fakecdp.Server) {
		t.Helper()
		got := s.Params(method)
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s %s = %#v, want %#v", method, k, got[k], v)
			}
		}
	}
}

// rule checks the single global throttling rule sent to Chrome.
func rule(want map[string]any) func(*testing.T, *fakecdp.Server) {
	return func(t *testing.T, s *fakecdp.Server) {
		t.Helper()
		rules, _ := s.Params(byRule)["matchedNetworkConditions"].([]any)
		got := map[string]any{"rules": float64(len(rules))}
		if len(rules) == 1 {
			got = rules[0].(map[string]any)
		}
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("throttling rule %s = %#v, want %#v (rules %v)", k, got[k], v, rules)
			}
		}
	}
}

var (
	byRule     = "Network.emulateNetworkConditionsByRule"
	enable     = "Debugger.enable"
	setBP      = "Debugger.setBreakpointByUrl"
	priorBP    = []toolCall{{"debugger", a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12}}}
	priorStart = []toolCall{{"performance", a{"action": "coverage_start"}}}
)

var cdpCases = []cdpCase{
	// debugger: breakpoints
	{tool: "debugger", args: a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12}, want: []string{enable, setBP},
		text: "bp-1: line http://fake/app.js:12 → http://fake/app.js:12:3", check: params(setBP, a{"url": "http://fake/app.js", "lineNumber": 11.0})},
	{tool: "debugger", args: a{"action": "breakpoint", "urlRegex": `app\.js$`, "line": 5, "column": 3, "condition": "total > 10"}, want: []string{enable, setBP},
		text: "if total > 10", check: params(setBP, a{"urlRegex": `app\.js$`, "lineNumber": 4.0, "columnNumber": 2.0, "condition": "total > 10"})},
	{tool: "debugger", args: a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12, "logMessage": "'total=', total"}, want: []string{enable, setBP},
		check: params(setBP, a{"condition": "console.log('total=', total), false"})},
	{tool: "debugger", args: a{"action": "breakpoint", "url": "http://fake/app.js"}, wantErr: "line is required for breakpoint"},
	{tool: "debugger", args: a{"action": "breakpoint", "line": 3}, wantErr: "url or urlRegex is required"},
	{tool: "debugger", args: a{"action": "dom_breakpoint", "selector": "#cart", "change": "subtree-modified"}, want: []string{enable, "DOM.getDocument", "DOM.querySelector", "DOMDebugger.setDOMBreakpoint"},
		text: "dom:subtree-modified:#cart", check: params("DOMDebugger.setDOMBreakpoint", a{"nodeId": 5.0, "type": "subtree-modified"})},
	{tool: "debugger", args: a{"action": "dom_breakpoint", "selector": "#cart", "change": "attribute-modified"}, want: []string{enable, "DOM.getDocument", "DOM.querySelector", "DOMDebugger.setDOMBreakpoint"}},
	{tool: "debugger", args: a{"action": "dom_breakpoint", "selector": "#cart", "change": "node-removed"}, want: []string{enable, "DOM.getDocument", "DOM.querySelector", "DOMDebugger.setDOMBreakpoint"}},
	{tool: "debugger", args: a{"action": "dom_breakpoint"}, wantErr: "selector is required for dom_breakpoint"},
	{tool: "debugger", args: a{"action": "xhr_breakpoint", "urlContains": "/api"}, want: []string{enable, "DOMDebugger.setXHRBreakpoint"},
		text: `request URL contains "/api"`, check: params("DOMDebugger.setXHRBreakpoint", a{"url": "/api"})},
	{tool: "debugger", args: a{"action": "event_breakpoint", "event": "click"}, want: []string{enable, "DOMDebugger.setEventListenerBreakpoint"},
		text: "event:click", check: params("DOMDebugger.setEventListenerBreakpoint", a{"eventName": "click"})},
	{tool: "debugger", args: a{"action": "remove", "breakpointId": "bp-1"}, prior: priorBP, want: []string{enable, setBP, "Debugger.removeBreakpoint"}, text: "removed bp-1"},
	{tool: "debugger", args: a{"action": "remove", "all": true}, prior: priorBP, want: []string{enable, setBP, "Debugger.removeBreakpoint"}, text: "removed 1 breakpoints"},
	{tool: "debugger", args: a{"action": "remove", "breakpointId": "nope"}, wantErr: `no breakpoint "nope"`},
	{tool: "debugger", args: a{"action": "list"}, prior: priorBP, want: []string{enable, setBP}, text: "bp-1: line http://fake/app.js:12"},
	{tool: "debugger", args: a{"action": "exceptions", "mode": "none"}, want: []string{enable, "Debugger.setPauseOnExceptions"}, check: params("Debugger.setPauseOnExceptions", a{"state": "none"})},
	{tool: "debugger", args: a{"action": "exceptions", "mode": "uncaught"}, want: []string{enable, "Debugger.setPauseOnExceptions"}, text: "pause on exceptions: uncaught"},
	{tool: "debugger", args: a{"action": "exceptions", "mode": "caught"}, want: []string{enable, "Debugger.setPauseOnExceptions"}, check: params("Debugger.setPauseOnExceptions", a{"state": "caught"})},
	{tool: "debugger", args: a{"action": "exceptions", "mode": "all"}, want: []string{enable, "Debugger.setPauseOnExceptions"}, check: params("Debugger.setPauseOnExceptions", a{"state": "all"})},

	// debugger: execution control and inspection
	{tool: "debugger", args: a{"action": "pause"}, want: []string{enable, "Debugger.pause", "Debugger.getScriptSource"}, text: "Paused (other) on bp-1\nat onClick (http://fake/app.js:12:3)"},
	{tool: "debugger", args: a{"action": "resume", "timeoutMs": 50}, paused: true, want: []string{enable, "Debugger.resume"}, text: "running"},
	{tool: "debugger", args: a{"action": "step_over"}, paused: true, want: []string{enable, "Debugger.stepOver", "Debugger.getScriptSource"}, text: "►   13  line 13"},
	{tool: "debugger", args: a{"action": "step_into"}, paused: true, want: []string{enable, "Debugger.stepInto", "Debugger.getScriptSource"}, text: "app.js:13:3"},
	{tool: "debugger", args: a{"action": "step_out"}, paused: true, want: []string{enable, "Debugger.stepOut", "Debugger.getScriptSource"}, text: "app.js:13:3"},
	{tool: "debugger", args: a{"action": "step_over"}, wantErr: "not paused"},
	{tool: "debugger", args: a{"action": "continue_to", "url": "http://fake/app.js", "line": 20}, paused: true, want: []string{enable, "Debugger.continueToLocation", "Debugger.getScriptSource"},
		check: params("Debugger.continueToLocation", a{"location": map[string]any{"scriptId": "7", "lineNumber": 19.0}})},
	{tool: "debugger", args: a{"action": "continue_to", "url": "http://fake/other.js", "line": 2}, paused: true, wantErr: "no loaded script with URL http://fake/other.js"},
	{tool: "debugger", args: a{"action": "wait"}, paused: true, want: []string{enable, "Debugger.getScriptSource"}, text: "Stack:\n  #0 onClick (http://fake/app.js:12:3)\n  #1 (anonymous) (http://fake/app.js:30:3)"},
	{tool: "debugger", args: a{"action": "wait", "timeoutMs": 30}, wantErr: "no pause within 30ms"},
	{tool: "debugger", args: a{"action": "stack"}, paused: true, want: []string{enable}, text: "#0 onClick (http://fake/app.js:12:3)"},
	{tool: "debugger", args: a{"action": "stack"}, wantErr: "not paused"},
	{tool: "debugger", args: a{"action": "scope"}, paused: true, want: []string{enable, "Runtime.getProperties"},
		text: "local:\n  total = 42\n  cart = Cart {items: 3}  [obj-2]\n  t = (uninitialized)", check: params("Runtime.getProperties", a{"objectId": "scope-f0"})},
	{tool: "debugger", args: a{"action": "scope", "frame": 1}, paused: true, check: params("Runtime.getProperties", a{"objectId": "scope-f1"})},
	{tool: "debugger", args: a{"action": "scope", "frame": 5}, paused: true, wantErr: "frame must be 0-1"},
	{tool: "debugger", args: a{"action": "properties", "objectId": "obj-2"}, want: []string{enable, "Runtime.getProperties"}, check: params("Runtime.getProperties", a{"objectId": "obj-2"})},
	{tool: "debugger", args: a{"action": "evaluate", "expression": "total + 1", "frame": 0}, paused: true, want: []string{enable, "Debugger.evaluateOnCallFrame"},
		text: "3", check: params("Debugger.evaluateOnCallFrame", a{"callFrameId": "f0", "expression": "total + 1"})},
	{tool: "debugger", args: a{"action": "evaluate", "expression": "1 + 1"}, want: []string{enable, "Runtime.evaluate"}, text: "2"},
	{tool: "debugger", args: a{"action": "evaluate", "expression": "JSON.stringify(state)"}, want: []string{enable, "Runtime.evaluate"},
		setup: func(e *env) {
			e.cdp.Reply("Runtime.evaluate", fakecdp.Reply{Result: a{"result": a{"type": "string", "value": strings.Repeat("x", 300) + "END"}}})
		}, text: "xEND\""},
	{tool: "debugger", args: a{"action": "scripts", "filter": "app"}, want: []string{enable}, text: "http://fake/app.js  id=7  40 lines"},
	{tool: "debugger", args: a{"action": "watch", "expression": "total"}, want: []string{enable}, text: "watching: total"},
	{tool: "debugger", args: a{"action": "watch", "expression": "total + 1"}, paused: true, want: []string{enable, "Debugger.evaluateOnCallFrame"}, text: "watching: total + 1\ntotal + 1 = 3"},
	{tool: "debugger", args: a{"action": "pause"}, prior: []toolCall{{"debugger", a{"action": "watch", "expression": "total"}}},
		want: []string{enable, "Debugger.pause", "Debugger.getScriptSource", "Debugger.evaluateOnCallFrame"}, text: "Watch:\n  total = 3\nStack:"},
	{tool: "debugger", args: a{"action": "unwatch", "expression": "total"}, prior: []toolCall{{"debugger", a{"action": "watch", "expression": "total"}}}, text: "no watch expressions"},
	{tool: "debugger", args: a{"action": "unwatch", "all": true}, prior: []toolCall{{"debugger", a{"action": "watch", "expression": "a"}}, {"debugger", a{"action": "watch", "expression": "b"}}}, text: "no watch expressions"},
	{tool: "debugger", args: a{"action": "unwatch"}, wantErr: "expression or all is required"},
	{tool: "debugger", args: a{"action": "search", "query": "line 12", "filter": "app"}, want: []string{enable, "Debugger.getScriptSource"},
		text: "http://fake/app.js:12:1  …", check: params("Debugger.getScriptSource", a{"scriptId": "7"})},
	{tool: "debugger", args: a{"action": "search", "query": "nowhere"}, want: []string{enable, "Debugger.getScriptSource"}, text: "(no matches)"},
	{tool: "debugger", args: a{"action": "search"}, wantErr: "query is required for search"},
	{tool: "debugger", args: a{"action": "source", "script": "http://fake/app.js", "from": 10, "to": 11}, want: []string{enable, "Debugger.getScriptSource"},
		text: "    10  line 10\n    11  line 11", check: params("Debugger.getScriptSource", a{"scriptId": "7"})},
	{tool: "debugger", args: a{"action": "listeners", "selector": "button"}, want: []string{enable, "Runtime.evaluate", "DOMDebugger.getEventListeners"},
		text: "click passive at http://fake/app.js:12:3: ƒ function onClick(e) {", check: func(t *testing.T, s *fakecdp.Server) {
			params("DOMDebugger.getEventListeners", a{"objectId": "node-1"})(t, s)
			params("Runtime.evaluate", a{"objectGroup": "agent-browser-mcp"})(t, s)
		}},
	{tool: "debugger", args: a{"action": "status"}, text: "debugger off"},
	{tool: "debugger", args: a{"action": "status"}, prior: priorBP, want: []string{enable, setBP}, text: "debugger on; 1 breakpoints; pause on exceptions: none\nrunning"},
	{tool: "debugger", args: a{"action": "disable"}, prior: priorBP, want: []string{enable, setBP, "Debugger.disable"}, text: "debugger off"},
	{tool: "debugger", args: a{"action": "disable"}, paused: true, prior: []toolCall{{"debugger", a{"action": "scripts"}}},
		want: []string{enable, "Debugger.resume", "Debugger.disable"}, text: "page resumed"},

	// performance over CDP
	{tool: "performance", args: a{"action": "metrics"}, want: []string{"Performance.enable", "Performance.getMetrics"}, text: "Counting starts now, so counts and durations are 0 on this first call; load or interact with the page, then call metrics again.\nJSHeapUsedSize: 1.0 MB\nNodes: 42\nScriptDuration: 12.0 ms\nThreadTime: 0.8 ms"},
	{tool: "performance", args: a{"action": "metrics"}, prior: []toolCall{{"performance", a{"action": "metrics"}}},
		want: []string{"Performance.enable", "Performance.getMetrics", "Performance.enable", "Performance.getMetrics"}, text: "Counts and durations since the first metrics call "}, // the seconds depend on test load
	{tool: "performance", args: a{"action": "heap_snapshot", "limit": 5}, want: []string{"HeapProfiler.enable", "HeapProfiler.takeHeapSnapshot", "HeapProfiler.disable"},
		text: "2 objects, 120 B self size, 1 detached DOM nodes"},
	{tool: "performance", args: a{"action": "coverage_start"}, want: []string{"Profiler.enable", "Profiler.startPreciseCoverage", "DOM.enable", "CSS.enable", "CSS.startRuleUsageTracking"}, text: "recording"},
	{tool: "performance", args: a{"action": "coverage_stop"}, prior: priorStart,
		want: []string{"Profiler.enable", "Profiler.startPreciseCoverage", "DOM.enable", "CSS.enable", "CSS.startRuleUsageTracking",
			"Profiler.takePreciseCoverage", "CSS.stopRuleUsageTracking", "Profiler.stopPreciseCoverage", "Profiler.disable", "CSS.disable", "DOM.disable"},
		text: "750 B of 1.2 KB unused (62%) across 2 files"},
	{tool: "performance", args: a{"action": "coverage_stop"}, wantErr: "coverage is not recording"},
	{tool: "performance", args: a{"action": "lighthouse", "formFactor": "desktop", "categories": []any{"performance", "seo"}},
		setup: func(e *env) { e.cfg.LighthousePath = e.fake.InstallLighthouse(e.t) }, text: "Performance 50 | SEO 100"},
	{tool: "performance", args: a{"action": "lighthouse", "categories": []any{"accessibility", "best-practices"}},
		setup: func(e *env) { e.cfg.LighthousePath = e.fake.InstallLighthouse(e.t) }, text: "Lighthouse http://fake/"},
	{tool: "performance", args: a{"action": "lighthouse", "formFactor": "mobile"},
		setup: func(e *env) { e.cfg.LighthousePath = e.fake.InstallLighthouse(e.t) }, text: "Largest Contentful Paint — 5.0 s\n    <img class=\"hero\" src=\"/hero.jpg\">"},
	{tool: "performance", args: a{"action": "lighthouse"}, setup: func(e *env) { e.cfg.LighthousePath = "/nonexistent/lighthouse" }, wantErr: "npm install -g lighthouse"},
	{tool: "performance", args: a{"action": "lighthouse"}, setup: func(e *env) {
		e.cfg.LighthousePath = e.fake.InstallLighthouse(e.t)
		e.fake.FailLighthouse()
	}, wantErr: "fake lighthouse failure"},

	// performance heatmap of the last recording
	{tool: "performance", args: a{"action": "heatmap"}, prior: []toolCall{{"performance", a{"action": "profiler_stop"}}},
		text: "Main-thread heatmap: 80 ms in 10 ms columns"},
	{tool: "performance", args: a{"action": "heatmap", "bucketMs": 40, "json": true}, prior: []toolCall{{"performance", a{"action": "trace_stop"}}},
		text: `{"bucketMs":40,"rows":[{"name":"Busy","busy":[1,1]}`},
	{tool: "performance", args: a{"action": "heatmap"}, wantErr: "no recording yet"},
	{tool: "performance", args: a{"action": "heatmap", "path": "/nonexistent/trace.json"}, wantErr: "no such file"},

	// debug_ui: Rendering overlays
	{tool: "debug_ui", args: a{"action": "rendering", "paintFlashing": true}, want: []string{"DOM.enable", "Overlay.enable", "Overlay.setShowPaintRects"},
		text: "rendering overlays on: paint flashing", check: params("Overlay.setShowPaintRects", a{"result": true})},
	{tool: "debug_ui", args: a{"action": "rendering", "paintFlashing": true, "layoutShifts": true, "layerBorders": true, "fpsMeter": true, "scrollBottlenecks": true},
		want: []string{"DOM.enable", "Overlay.enable", "Overlay.setShowPaintRects", "Overlay.setShowLayoutShiftRegions", "Overlay.setShowDebugBorders", "Overlay.setShowFPSCounter", "Overlay.setShowScrollBottleneckRects"},
		text: "paint flashing, layout shift regions, layer borders, FPS meter, scroll bottlenecks", check: func(t *testing.T, s *fakecdp.Server) {
			params("Overlay.setShowLayoutShiftRegions", a{"result": true})(t, s)
			params("Overlay.setShowFPSCounter", a{"show": true})(t, s)
		}},
	{tool: "debug_ui", args: a{"action": "rendering", "fpsMeter": false}, prior: []toolCall{{"debug_ui", a{"action": "rendering", "fpsMeter": true}}},
		text: "rendering overlays: all off", check: params("Overlay.setShowFPSCounter", a{"show": false})},
	{tool: "debug_ui", args: a{"action": "rendering"}, wantErr: "set at least one of paintFlashing"},

	// emulate throttling
	{tool: "emulate", args: a{"networkProfile": "slow-3g"}, want: []string{"Network.enable", byRule},
		text:  "throttling: network latency 2000ms, down 400kbps, up 400kbps; CPU unthrottled",
		check: rule(a{"urlPattern": "", "latency": 2000.0, "downloadThroughput": 50000.0, "uploadThroughput": 50000.0})},
	{tool: "emulate", args: a{"networkProfile": "fast-3g"}, want: []string{"Network.enable", byRule}, check: rule(a{"latency": 562.5})},
	{tool: "emulate", args: a{"networkProfile": "slow-4g"}, want: []string{"Network.enable", byRule}, check: rule(a{"downloadThroughput": 180000.0})},
	{tool: "emulate", args: a{"networkProfile": "fast-4g"}, want: []string{"Network.enable", byRule}, check: rule(a{"latency": 165.0})},
	{tool: "emulate", args: a{"networkProfile": "none"}, want: []string{"Network.enable", byRule},
		text: "network unthrottled", check: rule(a{"rules": 0.0})},
	{tool: "emulate", args: a{"networkProfile": "slow-3g", "latencyMs": 50, "downloadKbps": 800, "uploadKbps": 80}, want: []string{"Network.enable", byRule},
		check: rule(a{"latency": 50.0, "downloadThroughput": 100000.0, "uploadThroughput": 10000.0})},
	{tool: "emulate", args: a{"cpuSlowdown": 4}, want: []string{"Emulation.setCPUThrottlingRate"}, text: "CPU 4x slower",
		check: params("Emulation.setCPUThrottlingRate", a{"rate": 4.0})},
	{tool: "emulate", args: a{"cpuSlowdown": 0.5}, wantErr: "cpuSlowdown must be >= 1"},
	{tool: "emulate", args: a{"networkProfile": "dial-up"}, wantErr: "networkProfile must be one of"},

	// debug_ui: DevTools in the same browser
	{tool: "debug_ui", args: a{"action": "open_devtools"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "opened Chrome DevTools for the active tab in the same browser", check: func(t *testing.T, s *fakecdp.Server) {
			if p := s.Params("Target.openDevTools"); p["targetId"] != "T1" || p["panelId"] != nil {
				t.Errorf("params %v", p)
			}
		}},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "elements"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the elements panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "elements"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "console"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the console panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "console"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "network"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the network panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "network"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "sources"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the sources panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "sources"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "resources"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the resources panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "resources"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "performance"}, want: []string{"Page.getLayoutMetrics", "Target.openDevTools"},
		text: "on the performance panel", check: params("Target.openDevTools", a{"targetId": "T1", "panelId": "performance"})},
	{tool: "debug_ui", args: a{"action": "open_devtools", "panel": "lighthouse"}, wantErr: "panel must be one of"},
	{tool: "debug_ui", args: a{"action": "open_devtools"}, wantErr: "use external:true to get a DevTools URL", setup: func(e *env) {
		e.cdp.Reply("Target.openDevTools", fakecdp.Reply{Error: "'Target.openDevTools' wasn't found", Code: -32601})
	}},

	// console issues
	{tool: "console", args: a{"kind": "issues"}, want: []string{"Audits.enable"}, text: `1 of 1 issues` + "\n" + `MixedContentIssue: {"insecureURL":"http://cdn/x.js"}`},

	// application panel
	{tool: "application", args: a{"action": "indexeddb"}, want: []string{"Runtime.evaluate", "IndexedDB.enable", "IndexedDB.requestDatabaseNames", "IndexedDB.requestDatabase"},
		text: "shop (version 2)\n  store carts key=\"id\" autoIncrement=false indexes=[byUser]", check: params("IndexedDB.requestDatabaseNames", a{"securityOrigin": "http://fake"})},
	{tool: "application", args: a{"action": "indexeddb_read", "database": "shop", "store": "carts", "skip": 5, "limit": 1},
		want: []string{"Runtime.evaluate", "IndexedDB.enable", "IndexedDB.requestData"}, text: "1 => {user: \"ann\"}\n… more records; use skip=6",
		check: params("IndexedDB.requestData", a{"databaseName": "shop", "objectStoreName": "carts", "skipCount": 5.0, "pageSize": 1.0})},
	{tool: "application", args: a{"action": "indexeddb_read", "database": "shop"}, wantErr: "store is required for indexeddb_read"},
	{tool: "application", args: a{"action": "cache_storage"}, want: []string{"Runtime.evaluate", "CacheStorage.requestCacheNames", "CacheStorage.requestEntries"},
		text: "v1 (1 entries)\n  GET 200 http://fake/app.js"},
	{tool: "application", args: a{"action": "service_workers"}, want: []string{"Runtime.evaluate"}, text: "activated http://fake/sw.js"},
	{tool: "application", args: a{"action": "manifest"}, want: []string{"Page.getAppManifest"}, text: "manifest: http://fake/manifest.json\n{\"name\":\"Fake\"}"},
	{tool: "application", args: a{"action": "storage_usage"}, want: []string{"Runtime.evaluate", "Storage.getUsageAndQuota"},
		text: "http://fake: 2.0 KB used of 1.0 GB quota\n  indexeddb: 2.0 KB"},
	{tool: "application", args: a{"action": "clear_site_data"}, want: []string{"Runtime.evaluate", "Storage.clearDataForOrigin"},
		text: "cleared all site data for http://fake", check: params("Storage.clearDataForOrigin", a{"origin": "http://fake", "storageTypes": "all"})},
}

func TestCDPTools(t *testing.T) {
	t.Parallel()
	for i, c := range cdpCases {
		t.Run(caseName(i, argvCase{tool: c.tool, args: c.args, wantErr: c.wantErr}), func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			s := e.withCDP()
			if c.paused {
				pauseOnEnable(s)
			}
			if c.setup != nil {
				c.setup(e)
			}
			for _, p := range c.prior {
				if res := e.call(p.tool, p.args); res.IsError {
					t.Fatalf("prior %s: %s", p.tool, res.text())
				}
			}
			res := e.call(c.tool, c.args)

			if c.wantErr != "" {
				if !res.IsError || !strings.Contains(res.text(), c.wantErr) {
					t.Fatalf("want error %q, got isError=%v %q", c.wantErr, res.IsError, res.text())
				}
				return
			}
			if res.IsError {
				t.Fatalf("unexpected error: %s", res.text())
			}
			if c.want != nil && !slices.Equal(s.Methods(), c.want) {
				t.Errorf("CDP calls\n got %q\nwant %q", s.Methods(), c.want)
			}
			if !strings.Contains(res.text(), c.text) {
				t.Errorf("result missing %q:\n%s", c.text, res.text())
			}
			if c.check != nil {
				c.check(t, s)
			}
		})
	}
}

func TestLighthouseRunsAgainstSessionBrowser(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.cfg.LighthousePath = e.fake.InstallLighthouse(t)
	report := filepath.Join(t.TempDir(), "r.json")
	res := e.call("performance", a{"action": "lighthouse", "path": report})
	if res.IsError {
		t.Fatal(res.text())
	}
	calls := e.fake.LighthouseCalls()
	port := strings.TrimPrefix(s.WSURL(), "ws://127.0.0.1:")
	port = port[:strings.Index(port, "/")]
	want := []string{fakePageURL, "--port=" + port, "--output=json", "--output-path=" + report, "--quiet"}
	if len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("lighthouse argv\n got %q\nwant %q", calls, want)
	}
	if !strings.Contains(res.text(), "full report: "+report) {
		t.Fatalf("result: %s", res.text())
	}
}

func TestThrottlingFallsBackOnOlderChrome(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.Reply(byRule, fakecdp.Reply{Error: "'Network.emulateNetworkConditionsByRule' wasn't found", Code: -32601})
	if res := e.call("emulate", a{"networkProfile": "fast-4g"}); res.IsError {
		t.Fatal(res.text())
	}
	want := []string{"Network.enable", byRule, "Network.emulateNetworkConditions"}
	if !slices.Equal(s.Methods(), want) {
		t.Fatalf("got %q", s.Methods())
	}
	params("Network.emulateNetworkConditions", a{"latency": 165.0, "offline": false})(t, s)
}

func TestThrottlingSurvivesAcrossCalls(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.call("emulate", a{"networkProfile": "slow-3g"})
	res := e.call("emulate", a{"cpuSlowdown": 6})
	if !strings.Contains(res.text(), "network latency 2000ms") || !strings.Contains(res.text(), "CPU 6x slower") {
		t.Fatalf("the second call should report both settings on the same connection:\n%s", res.text())
	}
}

func TestActionReturnsWhenItHitsABreakpoint(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.call("debugger", a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12})
	e.fake.SleepMS(3000, "click") // the click would block until the debugger resumes
	go func() {
		// Pause once the click is running, not during the visibility check
		// before it, which can take a while under -race.
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if slices.ContainsFunc(e.fake.Commands(), func(c []string) bool { return c[0] == "click" }) {
				break
			}
		}
		s.Push(pausedAt(12))
	}()

	start := time.Now()
	res := e.call("click", a{"selector": "#buy"})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("click waited %v instead of returning on the pause", elapsed)
	}
	for _, want := range []string{"Paused (other) on bp-1", "► ", "(click finishes after you resume"} {
		if !strings.Contains(res.text(), want) {
			t.Errorf("missing %q in:\n%s", want, res.text())
		}
	}
}

func TestActionOnAlreadyPausedPageReturns(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	pauseOnEnable(s)
	e.reg.pausedWait = 50 * time.Millisecond
	e.call("debugger", a{"action": "status"})
	e.call("debugger", a{"action": "scripts"})
	e.fake.SleepMS(3000, "click")

	start := time.Now()
	res := e.call("click", a{"selector": "#buy"})
	if time.Since(start) > 2*time.Second || !strings.Contains(res.text(), "paused in the debugger, so click is waiting") {
		t.Fatalf("got %q after %v", res.text(), time.Since(start))
	}
	for _, c := range e.fake.Commands() {
		if c[0] == "is" {
			t.Fatalf("the visibility check would block on a paused page, but it ran: %q", c)
		}
	}
}

func TestDebuggingSkipsTheCLI(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.call("debugger", a{"action": "scripts"})
	e.fake.Reset()
	if res := e.call("debugger", a{"action": "list"}); res.IsError {
		t.Fatal(res.text())
	}
	if calls := e.fake.Commands(); len(calls) != 0 {
		t.Fatalf("while debugging, tools must not wait on agent-browser (the page may pause), got %q", calls)
	}
}

func TestCDPToolsRefuseWhilePaused(t *testing.T) {
	t.Parallel()
	for _, c := range []toolCall{
		{"performance", a{"action": "metrics"}},
		{"application", a{"action": "storage_usage"}},
		{"console", a{"kind": "issues"}},
		{"emulate", a{"cpuSlowdown": 4}},
		{"debug_ui", a{"action": "rendering", "paintFlashing": true}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			s := e.withCDP()
			pauseOnEnable(s)
			e.call("debugger", a{"action": "scripts"})
			start := time.Now()
			res := e.call(c.tool, c.args)
			if !res.IsError || !strings.Contains(res.text(), "paused in the debugger") || time.Since(start) > time.Second {
				t.Fatalf("got isError=%v %q after %v", res.IsError, res.text(), time.Since(start))
			}
		})
	}
}

func TestCDPCallsTimeOut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	// The timeout covers the fake CLI calls before the CDP one too, which
	// start slowly under -race, so it cannot be very short.
	e.cfg.DefaultTimeout = 1500
	s := e.withCDP()
	block := make(chan struct{})
	defer close(block)
	s.Handle("Performance.getMetrics", func(map[string]any) fakecdp.Reply { <-block; return fakecdp.Reply{} })
	start := time.Now()
	res := e.call("performance", a{"action": "metrics"})
	if !res.IsError || !strings.Contains(res.text(), "deadline exceeded") || time.Since(start) > 6*time.Second {
		t.Fatalf("got isError=%v %q after %v", res.IsError, res.text(), time.Since(start))
	}
}

func TestActionsRunNormallyWhileDebuggerIsIdle(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.call("debugger", a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12})
	if got := e.call("click", a{"selector": "#buy"}).text(); got != "cmd: click\nok: true" {
		t.Fatalf("got %q", got)
	}
}

func TestCloseBrowserDropsCDPConnection(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.call("debugger", a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12})
	if e.reg.dt.Existing("") == nil {
		t.Fatal("expected an open CDP page")
	}
	e.call("close_browser", nil)
	if e.reg.dt.Existing("") != nil {
		t.Fatal("close_browser must drop the CDP connection")
	}
}

// dock models Chrome's docked DevTools in the fake browser: the page gets the
// window width minus a 20px frame and, once DevTools opens, minus DevTools'
// share of the window, a fixed width or a fraction of it.
type dock struct {
	mu           sync.Mutex
	left, window int
	open         bool
	fixed        int
	ratio        float64
}

func (d *dock) page() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	w := d.window - 20
	if d.open {
		w -= d.fixed + int(float64(d.window)*d.ratio)
	}
	return w
}

// install answers the fake browser's layout, DevTools and window calls from d.
func (d *dock) install(s *fakecdp.Server, windowState string) {
	s.Handle("Page.getLayoutMetrics", func(map[string]any) fakecdp.Reply {
		return fakecdp.Reply{Result: a{"cssLayoutViewport": a{"clientWidth": d.page()}}}
	})
	s.Handle("Target.openDevTools", func(map[string]any) fakecdp.Reply {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.open = true
		return fakecdp.Reply{}
	})
	s.Handle("Browser.getWindowForTarget", func(map[string]any) fakecdp.Reply {
		d.mu.Lock()
		defer d.mu.Unlock()
		return fakecdp.Reply{Result: a{"windowId": 7, "bounds": a{"left": d.left, "width": d.window, "windowState": windowState}}}
	})
	s.Handle("Browser.setWindowBounds", func(p map[string]any) fakecdp.Reply {
		d.mu.Lock()
		defer d.mu.Unlock()
		b := p["bounds"].(map[string]any)
		if w, ok := b["width"].(float64); ok {
			d.window = int(w)
		}
		if l, ok := b["left"].(float64); ok {
			d.left = int(l)
		}
		return fakecdp.Reply{}
	})
}

// openDevTools opens DevTools in a fake browser laid out by d, on a screen
// screen px wide (0: the page cannot report it), and returns the result text.
func openDevTools(t *testing.T, d *dock, windowState string, screen int, setup ...func(*fakecdp.Server)) (*fakecdp.Server, string) {
	t.Helper()
	e := newEnv(t)
	s := e.withCDP()
	e.reg.devtoolsSettle = 300 * time.Millisecond
	d.install(s, windowState)
	if screen > 0 {
		s.Reply("Runtime.evaluate", fakecdp.Reply{Result: a{"result": a{"value": a{"left": 0, "width": screen}}}})
	}
	for _, f := range setup {
		f(s)
	}
	res := e.call("debug_ui", a{"action": "open_devtools"})
	if res.IsError {
		t.Fatal(res.text())
	}
	return s, res.text()
}

func TestOpenDevToolsKeepsPageWidth(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		d      *dock
		screen int
		window float64 // final window width
	}{
		"fixed-width DevTools, screen unknown": {&dock{left: 40, window: 1300, fixed: 635}, 0, 1935},
		"DevTools keeps half the window":       {&dock{window: 1300, ratio: 0.5}, 4000, 2600},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, text := openDevTools(t, c.d, "normal", c.screen)
			want := fmt.Sprintf("the window was widened by %dpx to keep the page 1280px wide", int(c.window)-1300)
			if !strings.Contains(text, want) || c.d.page() != 1280 {
				t.Fatalf("page %dpx, result %q", c.d.page(), text)
			}
			params("Browser.setWindowBounds", a{"windowId": 7.0, "bounds": a{"width": c.window}})(t, s)
		})
	}
}

func TestOpenDevToolsNeverGrowsPastTheScreen(t *testing.T) {
	t.Parallel()
	d := &dock{left: 40, window: 1300, fixed: 635}
	s, text := openDevTools(t, d, "normal", 1728)
	// 1300 + 635 does not fit on a 1728px screen: the window fills it from x=0.
	params("Browser.setWindowBounds", a{"windowId": 7.0, "bounds": a{"width": 1728.0, "left": 0.0}})(t, s)
	want := "the window was widened by 428px, as far as the screen allows, but the page is 1073px wide instead of 1280px"
	if !strings.Contains(text, want) || !strings.Contains(text, "dragging the DevTools divider") {
		t.Fatalf("got %q", text)
	}
}

func TestOpenDevToolsReportsNarrowedPage(t *testing.T) {
	t.Parallel()
	want := "DevTools docked beside the page and narrowed it from 1280px to 645px; the site may switch to a narrower (e.g. mobile) layout and hide elements, so take a fresh snapshot before acting."
	for name, c := range map[string]struct {
		state string
		setup func(*fakecdp.Server)
	}{
		"maximized window": {"maximized", func(*fakecdp.Server) {}},
		"resize refused": {"normal", func(s *fakecdp.Server) {
			s.Reply("Browser.setWindowBounds", fakecdp.Reply{Error: "Browser window not found"})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, text := openDevTools(t, &dock{window: 1300, fixed: 635}, c.state, 1728, c.setup); !strings.Contains(text, want) {
				t.Fatalf("got %q", text)
			}
		})
	}
}

func TestOpenDevToolsUndockedLeavesWindowAlone(t *testing.T) {
	t.Parallel()
	s, text := openDevTools(t, &dock{window: 1300}, "normal", 1728) // a separate DevTools window takes no width
	if text != "opened Chrome DevTools for the active tab in the same browser" {
		t.Fatalf("got %q", text)
	}
	if slices.Contains(s.Methods(), "Browser.setWindowBounds") {
		t.Fatal("the window must not be resized when the page kept its width")
	}
}

// A page paused before the server restarted: agent-browser cannot read its
// URL, but tab list names it, so the debugger reattaches and sees the pause.
func TestDebuggerReachesPagePausedBeforeRestart(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.AddPage("chrome://newtab/") // another page target besides the paused tab
	pauseOnEnable(s)
	e.fake.FailOn("get url") // "CDP error (Runtime.evaluate): Promise was collected"
	res := e.call("debugger", a{"action": "stack"})
	if res.IsError || !strings.Contains(res.text(), "onClick") {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}
