package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecdp"
	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecli"
)

// One test per bug found in the 1.x server.

func TestRegressionBackForwardUseSession(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"back", "forward"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.call("navigate", a{"action": action, "session": "work"})
			calls := e.fake.Calls()
			if !slices.ContainsFunc(calls, func(c []string) bool { return slices.Contains(c, action) }) ||
				slices.ContainsFunc(calls, func(c []string) bool { return !hasFlag(c, "--session", "work") }) {
				t.Fatalf("navigate %s must run in session work, got %q", action, calls)
			}
		})
	}
}

func TestRegressionUploadSendsEachFileSeparately(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("upload_file", a{"selector": "#f", "files": []any{"/a.txt", "/b.txt"}})
	got := e.fake.Commands()
	want := cmds(cmd("upload", "#f", "/a.txt", "/b.txt"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRegressionUserAgentKeepsCurrentPage: the user agent is set over CDP
// and the page reloaded. It once reopened about:blank, and later passed
// --user-agent, a launch option whose change relaunched the browser.
func TestRegressionUserAgentKeepsCurrentPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.fake.SetURL("https://app.example/dashboard")
	res := e.call("emulate", a{"userAgent": "Bot/1"})
	if res.IsError {
		t.Fatal(res.text())
	}
	if !strings.Contains(res.text(), "user agent Bot/1") || s.Params("Emulation.setUserAgentOverride")["userAgent"] != "Bot/1" {
		t.Fatalf("user agent not set over CDP: %q %v", res.text(), s.Params("Emulation.setUserAgentOverride"))
	}
	for _, c := range e.fake.Commands() {
		if slices.Contains(c, "about:blank") || slices.Contains(c, "--user-agent") {
			t.Fatalf("user agent change ran %q", c)
		}
	}
	if last := e.fake.Commands()[len(e.fake.Commands())-1]; last[0] != "reload" {
		t.Fatalf("expected a reload last, got %q", last)
	}
}

func TestRegressionTimeoutIsReported(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.DefaultTimeout = 200 })
	e.fake.SleepMS(3000, "click")
	res := e.call("click", a{"selector": "@e1"})
	if !res.IsError || !strings.Contains(res.text(), "timed out after") {
		t.Fatalf("want a timeout error, got isError=%v %q", res.IsError, res.text())
	}
}

func TestRegressionShutdownClosesDefaultSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("click", a{"selector": "@e1"})
	e.call("click", a{"selector": "@e1", "session": "other"})
	e.fake.Reset()

	e.mgr.CloseAll(context.Background())

	var closedDefault, closedOther bool
	for _, c := range e.fake.Calls() {
		if !reflect.DeepEqual(fakecli.Command(c), []string{"close"}) {
			continue
		}
		switch {
		case hasFlag(c, "--session", "other"):
			closedOther = true
		case !slices.Contains(c, "--session"):
			closedDefault = true
		}
	}
	if !closedDefault || !closedOther {
		t.Fatalf("closed default=%v other=%v; calls %q", closedDefault, closedOther, e.fake.Calls())
	}
}

func TestRegressionScreenshotSelectorIsPositional(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("screenshot", a{"selector": "#hero"})
	c := e.fake.Commands()[0]
	if slices.Contains(c, "--selector") || c[len(c)-1] != "#hero" {
		t.Fatalf("the CLI takes the screenshot selector positionally, got %q", c)
	}
}

func TestRegressionFindAllReturnsTexts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("find", a{"by": "all", "value": ".item"})
	if res.IsError || res.text() != `["one","two"]` {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}

func TestRegressionSnapshotIsPlainTree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("snapshot", nil)
	if got := res.text(); got != `- button "Go" [ref=e1]` {
		t.Fatalf("snapshot should be the bare tree without refs map or JSON escaping, got %q", got)
	}
}

func TestRegressionScreenshotReturnsImage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("screenshot", nil)
	if res.images() != 1 {
		t.Fatalf("want 1 image block, got %+v", res.Content)
	}
	img := res.Content[0]
	if img.MimeType != "image/jpeg" {
		t.Fatalf("mime = %q", img.MimeType)
	}
	if raw, _ := base64.StdEncoding.DecodeString(img.Data); !reflect.DeepEqual(raw, fakecli.TinyPNG()) {
		t.Fatal("image data does not match the file the CLI wrote")
	}
}

func TestRegressionGetURLIsPlain(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.SetURL("http://x/y")
	if got := e.call("get", a{"what": "url"}).text(); got != "url: http://x/y" {
		t.Fatalf("got %q", got)
	}
}

// agent-browser reports success for clicks, fills, typing and hovers on a
// display:none element even though nothing receives them, e.g. a desktop-only
// toggle after docked DevTools narrowed the page into its mobile layout.
func TestRegressionActionsOnHiddenElementsAreRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tool  string
		args  a
		doing string
	}{
		{"click", a{"selector": "@e26", "snapshot": "delta"}, "clicking it"},
		{"click", a{"selector": "@e26", "double": true}, "clicking it"},
		{"fill", a{"selector": "@e26", "value": "x"}, "filling it"},
		{"type", a{"selector": "@e26", "text": "x"}, "typing into it"},
		{"element_action", a{"selector": "@e26", "action": "hover"}, "hovering over it"},
	} {
		t.Run(c.tool+"_"+c.doing, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.fake.Respond("is visible", `{"visible":false}`)
			res := e.call(c.tool, c.args)
			want := "@e26 is not visible (display:none, visibility:hidden or zero size), so " + c.doing + " would do nothing"
			if !res.IsError || !strings.HasPrefix(res.text(), want) || !strings.Contains(res.text(), "take a fresh snapshot") {
				t.Fatalf("want a not-visible error, got isError=%v %q", res.IsError, res.text())
			}
			if got := e.fake.Commands(); !reflect.DeepEqual(got, cmds(visible("@e26"))) {
				t.Fatalf("a hidden target must not be acted on, got %q", got)
			}
		})
	}
}

// Actions that work on hidden elements, or have no element target, skip the check.
func TestActionsThatNeedNoVisibleTarget(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		tool string
		args a
		runs string // the CLI command the call makes; "" for none
	}{
		"new tab link":  {"click", a{"selector": "@e1", "newTab": true}, "click"},
		"keystrokes":    {"type", a{"text": "x", "mode": "keystrokes"}, "keyboard"},
		"check":         {"element_action", a{"selector": "@e1", "action": "check"}, "check"},
		"select":        {"select_option", a{"selector": "@e1", "values": []any{"b"}}, "select"},
		"invalid input": {"fill", a{"selector": "@e1"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.fake.Respond("is visible", `{"visible":false}`)
			e.call(c.tool, c.args)
			var ran []string
			for _, cmd := range e.fake.Commands() {
				ran = append(ran, cmd[0])
			}
			want := []string{}
			if c.runs != "" {
				want = []string{c.runs}
			}
			if !slices.Equal(ran, want) {
				t.Fatalf("%s: CLI commands %q, want %q with no visibility check", name, ran, want)
			}
		})
	}
}

func TestClickProceedsWhenVisibilityIsUnknown(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("is") // e.g. "Unknown ref": let click report it
	if res := e.call("click", a{"selector": "@e1"}); res.IsError {
		t.Fatalf("click must still run, got %q", res.text())
	}
	if got := e.fake.Commands(); !reflect.DeepEqual(got, cmds(visible("@e1"), cmd("click", "@e1"))) {
		t.Fatalf("got %q", got)
	}
}

// TestWaitHiddenNeverPassesState: --state is agent-browser's global storage
// state option, so "wait <sel> --state hidden" loaded a state file named
// hidden and relaunched the browser, losing the page.
func TestWaitHiddenNeverPassesState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("is visible", `{"visible":false}`)
	if got := e.call("wait", a{"for": "hidden", "value": "#spinner"}).text(); got != "#spinner is hidden" {
		t.Fatalf("got %q", got)
	}
	for _, c := range e.fake.Calls() {
		if slices.Contains(c, "--state") {
			t.Fatalf("passed --state: %q", c)
		}
	}
}

func TestWaitHiddenGoneOrTimedOut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.RespondRaw(`{"success":false,"data":null,"error":"Element not found: #x."}`, 1)
	if got := e.call("wait", a{"for": "hidden", "value": "#x"}).text(); got != "#x is gone" {
		t.Errorf("missing element: %q", got)
	}
	e2 := newEnv(t)
	e2.reg.waitHiddenFor = 300 * time.Millisecond
	res := e2.call("wait", a{"for": "hidden", "value": "#x"}) // the fake reports it visible
	if !res.IsError || !strings.Contains(res.text(), "#x is still visible after 300ms") {
		t.Errorf("visible element: isError=%v %q", res.IsError, res.text())
	}
}

// TestTabsListOneLinePerTab: the raw tab JSON repeated every field name.
func TestTabsListOneLinePerTab(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.SetURL("http://x/page")
	if got := e.call("tabs", a{}).text(); got != "* t1 Fake  http://x/page" {
		t.Fatalf("got %q", got)
	}
}

// TestTabsListShowsCurrentTitles: agent-browser's tab list keeps the title
// a tab had before its page loaded (the URL), so titles come from CDP.
func TestTabsListShowsCurrentTitles(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.cdp.SetTitle("T1", "Acme Mugs")
	if got := e.call("tabs", a{}).text(); got != "* t1 Acme Mugs  "+fakePageURL {
		t.Fatalf("got %q", got)
	}
	// A page without a title shows its URL as the title; that is left out.
	_, bare, _ := strings.Cut(fakePageURL, "://")
	e.cdp.SetTitle("T1", bare)
	if got := e.call("tabs", a{}).text(); got != "* t1  "+fakePageURL {
		t.Fatalf("untitled: %q", got)
	}
}

// TestDownloadInSeparateWindow: a window from tabs new_window is its own
// browser context, where agent-browser's download was canceled, so the file
// is saved over CDP. The default context keeps agent-browser's download.
func TestDownloadInSeparateWindow(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.cdp.Handle("Target.getBrowserContexts", func(map[string]any) fakecdp.Reply {
		return fakecdp.Reply{Result: a{"browserContextIds": []any{"CTX2"}, "defaultBrowserContextId": "DEFAULT"}}
	})
	if res := e.call("download", a{"selector": "#dl", "path": "/tmp/x.txt"}); res.IsError || !slices.ContainsFunc(e.fake.Commands(), func(c []string) bool { return len(c) > 0 && c[0] == "download" }) {
		t.Fatalf("default context should use agent-browser's download: %s", res.text())
	}

	e.cdp.SetContext("T1", "CTX2")
	e.cdp.Handle("Browser.setDownloadBehavior", func(p map[string]any) fakecdp.Reply {
		if p["behavior"] != "allowAndName" || p["browserContextId"] != "CTX2" {
			return fakecdp.Reply{}
		}
		dir, _ := p["downloadPath"].(string)
		os.WriteFile(filepath.Join(dir, "g1"), []byte("notes\n"), 0o644)
		return fakecdp.Reply{Events: []fakecdp.Event{
			{Method: "Browser.downloadWillBegin", Params: a{"guid": "g1", "suggestedFilename": "notes.txt"}},
			{Method: "Browser.downloadProgress", Params: a{"guid": "g1", "state": "completed", "receivedBytes": 6}},
		}}
	})
	path := filepath.Join(t.TempDir(), "saved", "notes.txt")
	res := e.call("download", a{"selector": "#dl", "path": path})
	if res.IsError || res.text() != "downloaded notes.txt to "+path+" (6 B)" {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "notes\n" {
		t.Fatalf("saved file: %q %v", b, err)
	}
	if got := e.fake.Commands(); !slices.ContainsFunc(got, func(c []string) bool { return slices.Equal(c, []string{"click", "#dl"}) }) {
		t.Errorf("no click on #dl in %v", got)
	}
	if p := e.cdp.Params("Browser.setDownloadBehavior"); p["behavior"] != "default" {
		t.Errorf("download behavior left as %v", p)
	}
}

// TestDebuggerIgnoresIsolatedWorlds: agent-browser's Web Vitals listeners
// run in an isolated world, so a click event breakpoint stopped in them
// before the page's own handler. DevTools ignores such scripts by default.
func TestDebuggerIgnoresIsolatedWorlds(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.Reply("Debugger.enable", fakecdp.Reply{Result: a{"debuggerId": "D"}, Events: []fakecdp.Event{
		{Method: "Debugger.scriptParsed", Params: a{"scriptId": "50", "url": "", "executionContextAuxData": a{"type": "isolated"}}},
		{Method: "Debugger.scriptParsed", Params: a{"scriptId": "51", "url": "", "executionContextAuxData": a{"type": "default"}}},
	}})
	e.call("debugger", a{"action": "event_breakpoint", "event": "click"})
	deadline := time.Now().Add(2 * time.Second)
	for s.Params("Debugger.setBlackboxedRanges") == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var ignored []any
	for _, c := range s.Calls() {
		if c.Method == "Debugger.setBlackboxedRanges" {
			ignored = append(ignored, c.Params["scriptId"])
		}
	}
	if !reflect.DeepEqual(ignored, []any{"50"}) {
		t.Fatalf("ignored scripts %v, want only the isolated one [50]", ignored)
	}
}

// TestWaitDownloadTimeoutPointsToDownload: agent-browser's wait for a
// download only sees one that starts after it, so click-then-wait missed a
// quick download and timed out with no hint why.
func TestWaitDownloadTimeoutPointsToDownload(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailWith("wait", "Operation timed out. The page may still be loading or the element may not exist.")
	res := e.call("wait", a{"for": "download", "value": "/tmp/f.txt"})
	if !res.IsError || !strings.Contains(res.text(), "use the download tool, which clicks and saves in one step") {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}

// TestCookieHeaderImport: agent-browser read "Cookie: a=1; b=2" as a cookie
// named "Cookie: a", so the header name is stripped first; other files go
// through as they are.
func TestCookieHeaderImport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	dir := t.TempDir()
	header, plain := filepath.Join(dir, "header.txt"), filepath.Join(dir, "plain.txt")
	os.WriteFile(header, []byte("Cookie: a=1; b=two\n"), 0o644)
	os.WriteFile(plain, []byte("a=1; b=two\n"), 0o644)

	e.call("cookies", a{"action": "import", "source": header})
	e.call("cookies", a{"action": "import", "source": plain})
	var sources []string
	for _, c := range e.fake.Commands() {
		if len(c) == 4 && c[1] == "set" && c[2] == "--curl" {
			sources = append(sources, c[3])
		}
	}
	if len(sources) != 2 || !strings.Contains(filepath.Base(sources[0]), "agent-browser-mcp-cookies-") || sources[1] != plain {
		t.Fatalf("imported from %q", sources)
	}
	if _, err := os.Stat(sources[0]); !os.IsNotExist(err) {
		t.Errorf("temporary cookie file left behind: %v", err)
	}
}

func TestEditingShortcut(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{
		"Meta+a": "selectAll", "Control+a": "selectAll", "ControlOrMeta+c": "copy", "Meta+x": "cut", "Control+v": "paste",
		"Meta+z": "undo", "Meta+Shift+z": "redo", "Control+y": "redo",
		"a": "", "Shift+a": "", "Alt+a": "", "Meta+b": "", "Meta+Shift+a": "", "Enter": "",
	} {
		if got, _, _ := editingShortcut(key); got != want {
			t.Errorf("editingShortcut(%q) = %q, want %q", key, got, want)
		}
	}
}

// TestSelectAllOnMac: on macOS "Meta+a" reached the page as keys and
// selected nothing, so the select-all command goes with it.
func TestSelectAllOnMac(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.reg.macEditing = true
	if got := e.call("press_key", a{"key": "Control+a"}).text(); got != "pressed: Control+a (selectAll)" {
		t.Fatalf("got %q", got)
	}
	var commands []any
	for _, c := range s.Calls() {
		if c.Method == "Input.dispatchKeyEvent" && c.Params["type"] == "keyDown" {
			commands, _ = c.Params["commands"].([]any)
		}
	}
	if !reflect.DeepEqual(commands, []any{"selectAll"}) {
		t.Fatalf("keyDown commands %v", commands)
	}

	other := newEnv(t)
	other.withCDP()
	other.reg.macEditing = false
	other.call("press_key", a{"key": "Control+a"})
	if got := other.fake.Commands(); !slices.ContainsFunc(got, func(c []string) bool { return slices.Equal(c, []string{"press", "Control+a"}) }) {
		t.Fatalf("elsewhere the CLI presses the keys: %q", got)
	}
}

// TestUserAgentResetRestoresTheBrowsers: clearing the override left the
// user agent of an earlier "emulate device" in force.
func TestUserAgentResetRestoresTheBrowsers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.Reply("Browser.getVersion", fakecdp.Reply{Result: a{"userAgent": "Mozilla/5.0 (Macintosh) Chrome/149"}})
	e.call("emulate", a{"userAgent": ""})
	if got := s.Params("Emulation.setUserAgentOverride")["userAgent"]; got != "Mozilla/5.0 (Macintosh) Chrome/149" {
		t.Fatalf("reset set the user agent to %q", got)
	}
}

// TestFramesListsOnlyThisPagesTargets: targets are browser-wide, so other
// tabs' workers and iframes showed under this page; a page in the
// back/forward cache repeats a worker, which is counted.
func TestFramesListsOnlyThisPagesTargets(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	s.AddTarget(map[string]any{"targetId": "W1", "type": "worker", "url": "http://fake/worker.js", "parentId": "T1"})
	s.AddTarget(map[string]any{"targetId": "W2", "type": "worker", "url": "http://fake/worker.js", "parentId": "T1"})
	s.AddTarget(map[string]any{"targetId": "W3", "type": "worker", "url": "http://other/worker.js", "parentId": "T9"})
	s.AddTarget(map[string]any{"targetId": "F9", "type": "iframe", "url": "http://other/frame", "parentId": "T9", "parentFrameId": "T9"})
	got := e.call("application", a{"action": "frames"}).text()
	if !strings.Contains(got, "  http://fake/worker.js  (worker) ×2") || strings.Contains(got, "http://other/") {
		t.Fatalf("got:\n%s", got)
	}
}

// TestStateRename: agent-browser 0.38's "state rename" fails with "Missing
// 'path' parameter" even for a file that exists, so the file is renamed in
// the directory "state list" reports.
func TestStateRename(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	dir := t.TempDir()
	e.fake.Respond("state list", fmt.Sprintf(`{"directory":%q,"files":[]}`, dir))
	os.WriteFile(filepath.Join(dir, "work.json"), []byte("{}"), 0o644)
	if got := e.call("state", a{"action": "rename", "path": "work", "newName": "home"}).text(); got != "renamed work.json to home.json in "+dir {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "home.json")); err != nil {
		t.Fatal(err)
	}
	if res := e.call("state", a{"action": "rename", "path": "work.json", "newName": "x"}); !res.IsError || !strings.Contains(res.text(), "no saved state work.json") {
		t.Fatalf("missing file: %q", res.text())
	}
	for _, c := range e.fake.Commands() {
		if len(c) > 1 && c[1] == "rename" {
			t.Fatalf("ran agent-browser's broken rename: %q", c)
		}
	}
}

// TestEvalThatPausesReturnsAtOnce: a script that stops at a breakpoint (or a
// requested pause) left eval_script waiting until the call timed out.
func TestEvalThatPausesReturnsAtOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.call("debugger", a{"action": "scripts"})
	// The script stops: the pause arrives, the result only after a resume.
	s.Reply("Runtime.evaluate", fakecdp.Reply{Events: []fakecdp.Event{pausedAt(12)}, Delay: 3 * time.Second,
		Result: a{"result": a{"type": "number", "value": 1, "description": "1"}}})
	start := time.Now()
	res := e.call("eval_script", a{"script": "pauseHere()"})
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("eval_script waited %v for a paused script", took)
	}
	if !res.IsError || !strings.Contains(res.text(), "the script paused in the debugger") || !strings.Contains(res.text(), "at onClick (http://fake/app.js:12:3)") {
		t.Fatalf("got %q", res.text())
	}
}

// TestSnapshotDiffComparesWithTheLastSnapshot: agent-browser 0.38's diff
// snapshot keeps no baseline, so every line came back as added.
func TestSnapshotDiffComparesWithTheLastSnapshot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("snapshot", `{"snapshot":"- heading \"Lab\" [ref=e1]\n- button \"Save\" [ref=e2]"}`)
	e.call("snapshot", a{})
	e.fake.Respond("snapshot", `{"snapshot":"- heading \"Lab\" [ref=e7]\n- button \"Saved\" [ref=e8]"}`)
	if got := e.call("diff", a{"kind": "snapshot"}).text(); got != "- - button \"Save\"\n+ - button \"Saved\"" {
		t.Fatalf("diff: %q", got)
	}
	if got := e.call("diff", a{"kind": "snapshot"}).text(); got != "no changes since the last snapshot" {
		t.Fatalf("second diff: %q", got)
	}
	if got := e.call("diff", a{"kind": "snapshot", "selector": "main"}).text(); !strings.HasPrefix(got, "no earlier snapshot to compare with") {
		t.Fatalf("new scope: %q", got)
	}
}

// TestScriptsForgetTheOldDocument: after a reload the debugger kept the old
// page's script IDs, and "source" asked Chrome for one it had dropped.
func TestScriptsForgetTheOldDocument(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.call("debugger", a{"action": "scripts"})
	parsed := func(id string, ctx int) fakecdp.Event {
		return fakecdp.Event{Method: "Debugger.scriptParsed", Params: a{"scriptId": id, "url": "http://fake/page.js", "endLine": 9,
			"executionContextId": ctx, "executionContextAuxData": a{"frameId": "F1", "isDefault": true, "type": "default"}}}
	}
	s.Push(parsed("71", 1))
	s.Push(parsed("72", 2)) // the same frame loaded a new document
	deadline := time.Now().Add(2 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		if got = e.call("debugger", a{"action": "scripts", "filter": "page.js"}).text(); strings.Contains(got, "id=72") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(got, "id=72") || strings.Contains(got, "id=71") {
		t.Fatalf("scripts after a reload:\n%s", got)
	}
}
