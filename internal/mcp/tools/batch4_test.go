package tools

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecdp"
)

// tinyJPEG is a solid-color frame for screencast replies.
func tinyJPEG(c color.Color) string {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := range 30 {
		for x := range 40 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

// retainersSnapshot: (root) -[1]-> Window -.cache-> Map -(table)-> Widget @7,
// and Widget @9 only held weakly.
const retainersSnapshot = `{"snapshot":{"meta":{
 "node_fields":["type","name","id","self_size","edge_count","trace_node_id","detachedness"],
 "node_types":[["hidden","array","string","object","code","closure","regexp","number","native","synthetic"],"string","number","number","number","number","number"],
 "edge_fields":["type","name_or_index","to_node"],
 "edge_types":[["context","element","property","internal","hidden","shortcut","weak"],"string_or_number","node"]}},
 "nodes":[9,1,1,0,2,0,0, 3,2,3,100,1,0,0, 3,3,5,50,1,0,0, 3,4,7,200,0,0,0, 3,4,9,100,0,0,0],
 "edges":[1,1,7, 6,0,28, 2,5,14, 3,6,21],
 "strings":["","(root)","Window","Map","Widget","cache","table"]}`

// retainersPath is a snapshot file of this test process's own, so parallel
// test runs cannot rewrite it while another reads it.
var retainersPath = func() string {
	f, err := os.CreateTemp("", "abm-retainers-*.heapsnapshot")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	f.WriteString(retainersSnapshot)
	return f.Name()
}()

func batch4Fixture(s *fakecdp.Server) {
	r := func(result any, events ...ev) fakecdp.Reply { return fakecdp.Reply{Result: result, Events: events} }
	frame := func(c color.Color) ev {
		return ev{Method: "Page.screencastFrame", Params: a{"data": tinyJPEG(c), "sessionId": 1}}
	}
	// Loading the page (not about:blank, where the filmstrip starts) paints twice.
	loaded := ev{Method: "Page.loadEventFired", Params: a{}}
	s.Handle("Page.navigate", func(p map[string]any) fakecdp.Reply {
		if p["url"] == "about:blank" {
			return r(a{}, loaded)
		}
		return r(a{}, frame(color.White), frame(color.RGBA{200, 0, 0, 255}), loaded)
	})
	s.Reply("Runtime.queryObjects", r(a{"objects": a{"type": "object", "subtype": "array", "objectId": "arr-1"}}))
	s.Reply("WebAuthn.addVirtualAuthenticator", r(a{"authenticatorId": "A1"}))
	s.Reply("WebAuthn.getCredentials", r(a{"credentials": []any{a{"credentialId": "cred123", "rpId": "fake.test", "userHandle": "dXNlcg", "signCount": 2, "isResidentCredential": true}}}))
	var logs []ev
	for i := range 12 {
		logs = append(logs, ev{Method: "Log.entryAdded", Params: a{"entry": a{"text": fmt.Sprintf("line %d", i)}}})
	}
	s.Reply("Log.enable", r(a{}, logs...))
	s.Reply("Media.enable", r(a{}, ev{Method: "Media.playerPropertiesChanged", Params: a{"playerId": "p1"}}, ev{Method: "Page.frameNavigated", Params: a{}}))
}

func init() {
	cdpCases = append(cdpCases, batch4Cases...)
}

var batch4Cases = []cdpCase{
	{tool: "performance", args: a{"action": "query_objects", "constructor": "Widget"}, text: "3 live Widget objects after garbage collection",
		check: params("Runtime.queryObjects", a{"prototypeObjectId": "proto-1"})},
	{tool: "performance", args: a{"action": "query_objects"}, wantErr: "constructor is required for query_objects"},
	{tool: "performance", args: a{"action": "filmstrip"}, text: "Filmstrip: 2 distinct frames, showing 2; first change at ",
		check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			var urls []any
			for _, c := range s.Calls() {
				if c.Method == "Page.navigate" {
					urls = append(urls, c.Params["url"])
				}
			}
			// From a blank page to the current one, so the load is visible.
			if len(urls) != 2 || urls[0] != "about:blank" || urls[1] != "http://fake/" {
				t.Errorf("navigations %v", urls)
			}
		}},
	{tool: "performance", args: a{"action": "heap_diff"}, prior: []toolCall{{"performance", a{"action": "heap_snapshot"}}, {"performance", a{"action": "heap_snapshot"}}},
		text: "nothing grew"},
	{tool: "performance", args: a{"action": "heap_diff"}, wantErr: "no heap snapshot yet"},
	{tool: "performance", args: a{"action": "retainers", "constructor": "Widget", "path": retainersPath},
		text: "2 Widget objects in " + retainersPath + "; how the largest are retained (root first):\n" +
			"  Widget @7 (0.2 KB): Window -.cache-> Map -(table)-> Widget\n" +
			"  Widget @9 (0.1 KB): no path from a GC root (garbage waiting to be collected)"},
	{tool: "performance", args: a{"action": "retainers", "constructor": "Gadget", "path": retainersPath}, wantErr: "no Gadget objects"},

	{tool: "record", args: a{"action": "flow_start"}, text: "recording this session's actions as a flow"},
	{tool: "record", args: a{"action": "flow_export", "format": "playwright"},
		prior: []toolCall{{"record", a{"action": "flow_start"}}, {"navigate", a{"url": "http://x/"}}, {"click", a{"selector": "#buy"}},
			{"fill", a{"selector": "#email", "value": "a@b.c"}}, {"press_key", a{"key": "Enter"}}, {"wait", a{"for": "text", "value": "Thanks"}}},
		// The flow starts on the current page, then follows the actions.
		text: "test('recorded flow', async ({ page }) => {\n  await page.goto(\"http://fake/\");\n  await page.goto(\"http://x/\");\n  await page.locator(\"#buy\").click();\n" +
			"  await page.locator(\"#email\").fill(\"a@b.c\");\n  await page.keyboard.press(\"Enter\");\n" +
			"  await expect(page.getByText(\"Thanks\").first()).toBeVisible();\n});"},
	{tool: "record", args: a{"action": "flow_export", "format": "puppeteer"},
		prior: []toolCall{{"record", a{"action": "flow_start"}}, {"navigate", a{"url": "http://x/"}}, {"click", a{"selector": "#buy"}}},
		text:  "const page = await browser.newPage();\nawait page.goto(\"http://fake/\");\nawait page.goto(\"http://x/\");\nawait page.locator(\"#buy\").click();\nawait browser.close();"},
	{tool: "record", args: a{"action": "flow_export"}, wantErr: "no flow is being recorded"},
	// Exporting keeps recording: the flow can be exported in both formats.
	{tool: "record", args: a{"action": "flow_export", "format": "puppeteer"},
		prior: []toolCall{{"record", a{"action": "flow_start"}}, {"click", a{"selector": "#buy"}}, {"record", a{"action": "flow_export"}}},
		text:  "await page.locator(\"#buy\").click();"},
	// find calls are recorded with Playwright's own locators.
	{tool: "record", args: a{"action": "flow_export"},
		prior: []toolCall{{"record", a{"action": "flow_start"}},
			{"find", a{"by": "label", "value": "Email", "action": "fill", "input": "a@b.c"}},
			{"find", a{"by": "role", "value": "button", "name": "Sign up", "exact": true}},
			{"find", a{"by": "text", "value": "Later", "action": "hover"}},
			{"find", a{"by": "nth", "value": "li", "index": 2}},
			{"find", a{"by": "all", "value": "li"}}},
		// find acts on the first match; .first() keeps Playwright's strict mode
		// from rejecting a locator that matches several elements.
		text: "  await page.getByLabel(\"Email\").first().fill(\"a@b.c\");\n  await page.getByRole(\"button\", { name: \"Sign up\", exact: true }).first().click();\n" +
			"  await page.getByText(\"Later\").first().hover();\n  await page.locator(\"li\").nth(2).click();\n});"},
	{tool: "record", args: a{"action": "flow_export", "format": "puppeteer"},
		prior: []toolCall{{"record", a{"action": "flow_start"}}, {"find", a{"by": "role", "value": "button", "name": "Sign up"}}, {"find", a{"by": "text", "value": "Later"}}},
		text:  "await page.locator(\"::-p-aria([name=\\\"Sign up\\\"][role=\\\"button\\\"])\").click();\nawait page.locator(\"::-p-text(Later)\").click();"},

	{tool: "debug_ui", args: a{"action": "animations"}, text: "1 animation:\n  CSSAnimation spin on div.loader: running, 1000 ms, infinite"},
	{tool: "debug_ui", args: a{"action": "animations", "playbackRate": 0.1}, text: "animations play at 0.1× speed",
		check: params("Animation.setPlaybackRate", a{"playbackRate": 0.1})},
	{tool: "debug_ui", args: a{"action": "animations", "playbackRate": 0}, text: "paused every animation"},

	{tool: "emulate", args: a{"authenticator": true}, text: "virtual passkey authenticator on",
		check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			opts, _ := s.Params("WebAuthn.addVirtualAuthenticator")["options"].(map[string]any)
			if opts["protocol"] != "ctap2" || opts["transport"] != "internal" || opts["isUserVerified"] != true {
				t.Errorf("authenticator options %v", opts)
			}
		}},
	{tool: "emulate", args: a{"authenticator": false}, text: "no virtual authenticator"},
	{tool: "application", args: a{"action": "credentials"}, prior: []toolCall{{"emulate", a{"authenticator": true}}},
		text: "1 passkey:\n  cred123 for fake.test, user dXNlcg, used 2 times, discoverable true"},
	{tool: "application", args: a{"action": "credentials"}, wantErr: "no virtual authenticator"},

	// Writing waits for page focus, so focus emulation is on just for the call.
	{tool: "clipboard", args: a{"action": "read"}, want: []string{"Browser.grantPermissions", "Emulation.setFocusEmulationEnabled", "Emulation.setFocusEmulationEnabled"}},
	{tool: "clipboard", args: a{"action": "write", "text": "hi"}, want: []string{"Browser.grantPermissions", "Emulation.setFocusEmulationEnabled", "Emulation.setFocusEmulationEnabled"},
		check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			var got []any
			for _, c := range s.Calls() {
				if c.Method == "Emulation.setFocusEmulationEnabled" {
					got = append(got, c.Params["enabled"])
				}
			}
			if len(got) != 2 || got[0] != true || got[1] != false {
				t.Errorf("focus emulation %v, want on then off", got)
			}
		}},
	// Copy and paste run Chrome's editing commands; a bare Ctrl+C key event
	// does not copy.
	{tool: "clipboard", args: a{"action": "copy"}, text: `copied "copied text" from input#email`,
		want:  []string{"Browser.grantPermissions", "Emulation.setFocusEmulationEnabled", "Runtime.evaluate", "Input.dispatchKeyEvent", "Input.dispatchKeyEvent", "Runtime.evaluate", "Emulation.setFocusEmulationEnabled"},
		check: params("Input.dispatchKeyEvent", a{"type": "keyUp", "key": "c"})},
	{tool: "clipboard", args: a{"action": "copy"}, check: func(t *testing.T, s *fakecdp.Server) {
		t.Helper()
		for _, c := range s.Calls() {
			if c.Method == "Input.dispatchKeyEvent" && c.Params["type"] == "keyDown" {
				if cmds, _ := c.Params["commands"].([]any); len(cmds) != 1 || cmds[0] != "copy" {
					t.Errorf("keyDown commands %v, want [copy]", c.Params["commands"])
				}
				return
			}
		}
		t.Error("no keyDown sent")
	}},
	// Focus emulation the model turned on itself stays on.
	{tool: "clipboard", args: a{"action": "paste"}, prior: []toolCall{{"emulate", a{"focus": true}}}, text: `pasted "copied text" into input#email`,
		want: []string{"Emulation.setFocusEmulationEnabled", "Browser.grantPermissions", "Runtime.evaluate", "Runtime.evaluate", "Input.dispatchKeyEvent", "Input.dispatchKeyEvent"}},
	{tool: "clipboard", args: a{"action": "copy"}, setup: unfocused, wantErr: "nothing is selected in p.note to copy"},
	{tool: "clipboard", args: a{"action": "paste"}, setup: unfocused, wantErr: "nothing editable is focused to paste into (focus is on p.note)"},

	// state rename moves the file itself; agent-browser's rename is broken.
	{tool: "state", args: a{"action": "rename", "path": "work", "newName": "home"}, text: "renamed work.json to home.json",
		setup: func(e *env) {
			dir := e.t.TempDir()
			e.fake.Respond("state list", fmt.Sprintf(`{"directory":%q,"files":[]}`, dir))
			os.WriteFile(filepath.Join(dir, "work.json"), []byte("{}"), 0o644)
		}},

	// eval_script runs like the Console: REPL mode, values as JSON.
	{tool: "eval_script", args: a{"script": "1+1"}, want: []string{"Runtime.evaluate", "Runtime.releaseObjectGroup"}, text: "2",
		check: params("Runtime.evaluate", a{"expression": "1+1", "replMode": true, "awaitPromise": true, "userGesture": true})},
	{tool: "eval_script", args: a{"script": "'hi'"}, text: "hi"},
	{tool: "eval_script", args: a{"script": "fetch('/x').then(r => r.status)"}, text: "204",
		check: params("Runtime.awaitPromise", a{"promiseObjectId": "promise-1"})},
	{tool: "eval_script", args: a{"script": "({a: 1})"}, text: `{"a":1}`},
	{tool: "eval_script", args: a{"script": "document.body"}, text: "body"},
	{tool: "eval_script", args: a{"script": "boom()"}, wantErr: "Error: boom"},
	// A paused page cannot run scripts; the CLI's eval would hang until resume.
	{tool: "eval_script", args: a{"script": "1+1"}, prior: []toolCall{{"debugger", a{"action": "pause"}}}, wantErr: "paused in the debugger"},
	{tool: "cdp", args: a{"method": "LayerTree.enable", "target": "page"}, want: []string{"LayerTree.enable"}, text: "{}"},
	{tool: "cdp", args: a{"method": "Media.enable", "events": "Media.", "waitMs": 10}, text: "{}\n1 Media.* events:\n  Media.playerPropertiesChanged {\"playerId\":\"p1\"}"},
	{tool: "cdp", args: a{"method": "Emulation.setDeviceMetricsOverride", "params": `{"width":300,"height":600,"deviceScaleFactor":1,"mobile":true}`},
		check: params("Emulation.setDeviceMetricsOverride", a{"width": 300.0, "mobile": true})},
	{tool: "cdp", args: a{"method": "Browser.getVersion", "target": "browser"}, text: "{}"},
	{tool: "cdp", args: a{"method": "Log.enable", "events": "Log.", "waitMs": 10}, text: "12 Log.* events:\n  Log.entryAdded {\"entry\":{\"text\":\"line 0\"}}"},
	{tool: "cdp", args: a{"method": "X.y", "params": "{nope"}, wantErr: "params must be a JSON object"},
}

// TestFilmstripReturnsAnImage: the frames come back as one PNG grid.
func TestFilmstripReturnsAnImage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	res := e.call("performance", a{"action": "filmstrip"})
	if res.IsError || res.images() != 1 {
		t.Fatalf("want one image, got isError=%v images=%d %q", res.IsError, res.images(), res.text())
	}
}

// TestFlowUsesStableLocatorsForRefs: a click on an @ref is exported with a
// selector that works without this session's refs.
func TestFlowUsesStableLocatorsForRefs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.fake.Respond("get box", `{"x":10,"y":20,"width":100,"height":40}`)
	e.call("record", a{"action": "flow_start"})
	if res := e.call("click", a{"selector": "@e1"}); res.IsError {
		t.Fatal(res.text())
	}
	got := e.call("record", a{"action": "flow_export"}).text()
	if !strings.Contains(got, `await page.locator("#buy").click();`) {
		t.Fatalf("ref not turned into a stable locator:\n%s", got)
	}
}

// unfocused: focus is on a paragraph with nothing selected.
func unfocused(e *env) {
	e.cdp.Reply("Runtime.evaluate", fakecdp.Reply{Result: a{"result": a{"type": "object", "value": a{"label": "p.note", "editable": false, "selected": false}}}})
}
