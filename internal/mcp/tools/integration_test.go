//go:build integration

package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/xcode-studio/agentbrowser-mcp/internal/browser"
)

const fixturePage = `<!doctype html><title>Fixture</title>
<h1 id="h">Hello Fixture</h1>
<p class="item">one</p><p class="item">two</p>
<label for="em">Email</label><input id="em" placeholder="you@x.com">
<button onclick="console.log('clicked go'); fetch('/api.json'); document.getElementById('h').textContent='Clicked'">Go</button>
<input type=file id=f multiple>
<a href="/second">Second page</a>
<script>console.log('hello log'); console.warn('slow warn');</script>`

// TestIntegration drives a real browser through every tool group against a
// local fixture site. Run with: go test -tags integration ./internal/mcp/tools/
func TestIntegration(t *testing.T) {
	path, err := exec.LookPath("agent-browser")
	if err != nil {
		t.Skip("agent-browser not on PATH")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		case "/second":
			fmt.Fprint(w, `<!doctype html><title>Second</title><p>second page</p>`)
		case "/broken":
			fmt.Fprint(w, `<!doctype html><title>Broken</title><img src="/missing.png"><script src="/missing.js"></script><script>setTimeout(() => lateError(), 50); notDefined()</script>`)
		case "/missing.png", "/missing.js":
			http.NotFound(w, r)
		default:
			fmt.Fprint(w, fixturePage)
		}
	}))
	defer site.Close()

	cfg := testConfig(path)
	cfg.DefaultTimeout = 30000
	cfg.IdleTimeout = 10 * time.Minute // the real CLI must take the flag the server sends
	mgr := browser.NewManager(cfg)
	srv := server.NewMCPServer("it", "0", server.WithToolCapabilities(false))
	reg := RegisterAll(srv, cfg, mgr)
	e := &env{t: t, srv: srv, cfg: cfg, mgr: mgr, reg: reg}
	session := fmt.Sprintf("abm-it-%d", os.Getpid())
	t.Cleanup(func() { e.call("close_browser", a{"session": session}) })

	call := func(tool string, args map[string]any) string {
		t.Helper()
		args["session"] = session
		res := e.call(tool, args)
		if res.IsError {
			t.Fatalf("%s %v: %s", tool, args, res.text())
		}
		if strings.Contains(res.text(), `"lifecycle"`) {
			t.Fatalf("%s: the CLI's lifecycle field leaked into the result:\n%s", tool, res.text())
		}
		return res.text()
	}
	expect := func(got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Fatalf("want %q in:\n%s", want, got)
		}
	}

	expect(call("navigate", a{"url": site.URL}), "Fixture")
	snap := call("snapshot", a{"interactive": true})
	expect(snap, "[ref=")
	if strings.Contains(snap, `"refs"`) {
		t.Fatal("snapshot still carries the refs map")
	}

	call("find", a{"by": "placeholder", "value": "you@x.com", "action": "fill", "input": "a@b.c"})
	expect(call("get", a{"what": "value", "selector": "#em"}), "a@b.c")
	expect(call("find", a{"by": "all", "value": ".item"}), `["one","two"]`)

	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644)
	}
	call("upload_file", a{"selector": "#f", "files": []any{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}})
	expect(call("eval_script", a{"script": "document.getElementById('f').files.length"}), "2")

	call("snapshot", a{})
	expect(call("click", a{"selector": "button", "snapshot": "diff"}), "Clicked")
	expect(call("wait", a{"for": "text", "value": "Clicked"}), "")
	expect(call("console", a{"pattern": "clicked"}), "[log] clicked go")
	expect(call("network", a{"filter": "api.json"}), "GET 200")

	// navigate reports what went wrong while the page loaded, and only that.
	health := call("navigate", a{"url": site.URL + "/broken"})
	expect(health, "Page problems during this load: 2 uncaught JS errors and 2 failed requests")
	expect(health, "- JS error: ReferenceError: notDefined is not defined")
	expect(health, "- JS error: ReferenceError: lateError is not defined")
	expect(health, "- 404 Image "+site.URL+"/missing.png")
	expect(health, "- 404 Script "+site.URL+"/missing.js")
	if got := call("navigate", a{"url": site.URL}); strings.Contains(got, "Page problems") {
		t.Fatalf("a healthy page got a health note:\n%s", got)
	}
	// The debugger being on (edit_source turns it on) does not silence it;
	// only something that can pause the load does.
	call("debugger", a{"action": "scripts"})
	debugHealth := call("navigate", a{"url": site.URL + "/broken"})
	expect(debugHealth, "Page problems during this load:")
	expect(debugHealth, "404 Image "+site.URL+"/missing.png")
	call("debugger", a{"action": "disable"})
	call("navigate", a{"url": site.URL})

	// Features that need agent-browser 0.38+: snapshot deltas and human-like
	// pointer movement.
	if v := mgr.CheckVersion(t.Context()); v.Warning != "" {
		t.Fatalf("installed CLI is not supported: %s", v)
	}
	expect(call("help", a{"topic": "doctor"}), "] Environment: CLI version")
	expect(call("snapshot", a{"interactive": true, "full": true}), "(full;")
	expect(call("snapshot", a{"interactive": true, "delta": true}), "unchanged since")
	call("eval_script", a{"script": `document.body.insertAdjacentHTML('beforeend', '<button id="added">Added</button>'); window.moves = 0; addEventListener('mousemove', () => moves++)`})
	expect(call("snapshot", a{"interactive": true, "delta": true}), `button "Added"`)
	expect(call("mouse", a{"action": "move", "x": 300, "y": 200, "human": true, "seed": 7}), "moved")
	if moves := call("eval_script", a{"script": "moves"}); moves == "0" || moves == "1" {
		t.Fatalf("human movement should send many mousemove events, got %s", moves)
	}
	expect(call("click", a{"selector": "#added", "human": true, "snapshot": "delta"}), "Snapshot revision")
	call("drag", a{"source": "#h", "target": "#em", "human": true})

	// agent-browser reports success for clicks on display:none elements; the
	// click tool must refuse instead of claiming a click that did nothing.
	call("eval_script", a{"script": `document.body.insertAdjacentHTML('beforeend', '<button id="ghost" style="display:none" onclick="window.ghostClicked = 1">Ghost</button>')`})
	if res := e.call("click", a{"selector": "#ghost", "session": session}); !res.IsError || !strings.Contains(res.text(), "#ghost is not visible") {
		t.Fatalf("click on a display:none element: isError=%v %q", res.IsError, res.text())
	}
	expect(call("eval_script", a{"script": "String(window.ghostClicked)"}), "undefined")

	shot := e.call("screenshot", a{"session": session, "annotate": true})
	if shot.IsError || shot.images() != 1 {
		t.Fatalf("screenshot: %+v", shot)
	}
	expect(shot.text(), "@e")

	batch := call("batch", a{"steps": []any{
		a{"tool": "navigate", "args": a{"url": site.URL + "/second"}},
		a{"tool": "get", "args": a{"what": "title"}},
	}})
	expect(batch, "Second")
	expect(call("navigate", a{"action": "back"}), site.URL)

	ua := call("emulate", a{"userAgent": "abm-it/1"})
	expect(ua, "user agent abm-it/1")
	expect(ua, "reloaded:")
	expect(call("eval_script", a{"script": "navigator.userAgent"}), "abm-it/1")
	// Resetting restores the browser's own user agent, also after a device's.
	call("emulate", a{"device": "iPhone 14"})
	call("emulate", a{"userAgent": ""})
	if got := call("eval_script", a{"script": "navigator.userAgent"}); strings.Contains(got, "iPhone") || strings.Contains(got, "abm-it") {
		t.Fatalf("user agent after reset: %s", got)
	}
	call("emulate", a{"width": 1280, "height": 800})

	expect(call("performance", a{"action": "timing"}), "navigation: TTFB ")
	expect(call("performance", a{"action": "memory"}), "JS heap: ")

	call("cookies", a{"action": "set", "name": "it", "value": "yes"})
	expect(call("cookies", a{}), "it")
	// A copied "Cookie:" request header imports as the cookies it names.
	cookieFile := filepath.Join(t.TempDir(), "cookies.txt")
	os.WriteFile(cookieFile, []byte("Cookie: ca=1; cb=two\n"), 0o644)
	call("cookies", a{"action": "import", "source": cookieFile, "domain": "127.0.0.1"})
	if got := call("cookies", a{}); !strings.Contains(got, "\n  ca=1 ") || strings.Contains(got, "Cookie:") {
		t.Fatalf("header import:\n%s", got)
	}
	call("storage", a{"action": "set", "key": "k", "value": "v"})
	expect(call("storage", a{"key": "k"}), "v")
	expect(call("tabs", a{}), "t1")

	// Fixes from a live test of every tool.
	before := call("get", a{"what": "url"})
	expect(call("wait", a{"for": "hidden", "value": "#nope"}), "#nope is gone")
	expect(call("get", a{"what": "url"}), before) // no browser relaunch
	styles := call("get", a{"what": "styles", "selector": "h1"})
	expect(styles, "display: block")
	expect(styles, "elements computed")
	if strings.Contains(styles, "-webkit") {
		t.Fatalf("get styles listed every property:\n%s", styles)
	}
	call("tabs", a{"action": "new_window", "url": site.URL + "/second"})
	expect(call("tabs", a{}), site.URL+"/second")
	call("tabs", a{"action": "close"})
	call("clipboard", a{"action": "write", "text": "abm clipboard"})
	expect(call("clipboard", a{"action": "read"}), "abm clipboard")
	// Copy and paste act as the shortcuts do: copy the selection, paste at
	// the focus. A script may declare the same const again, as in the Console.
	call("fill", a{"selector": "#em", "value": "copy me"})
	call("eval_script", a{"script": "const em = document.getElementById('em'); em.focus(); em.select()"})
	expect(call("clipboard", a{"action": "copy"}), `copied "copy me" from input#em`)
	call("fill", a{"selector": "#em", "value": ""})
	call("eval_script", a{"script": "const em = document.getElementById('em'); em.focus()"})
	expect(call("clipboard", a{"action": "paste"}), `pasted "copy me" into input#em`)
	expect(call("get", a{"what": "value", "selector": "#em"}), "copy me")
	expect(call("eval_script", a{"script": "({n: 1, s: 'x'})"}), `{"n":1,"s":"x"}`)
	// A snapshot diff after an action shows just what changed.
	call("navigate", a{"url": site.URL})
	call("snapshot", a{})
	changed := call("click", a{"selector": "button", "snapshot": "diff"})
	if !strings.Contains(changed, `+ - heading "Clicked"`) || !strings.Contains(changed, `- - heading "Hello Fixture"`) || strings.Contains(changed, "Second page") {
		t.Fatalf("snapshot diff after a click:\n%s", changed)
	}
	// Text in a plain paragraph, which compact snapshots leave out, counts too.
	call("eval_script", a{"script": "document.querySelector('.item').textContent = 'changed text'"})
	if got := call("diff", a{"kind": "snapshot"}); !strings.Contains(got, `StaticText "changed text"`) {
		t.Fatalf("paragraph change missing from the diff:\n%s", got)
	}

	// Control+a selects all on every OS (on macOS through the command).
	call("fill", a{"selector": "#em", "value": "select me"})
	call("element_action", a{"selector": "#em", "action": "focus"})
	call("press_key", a{"key": "Control+a"})
	call("press_key", a{"key": "Backspace"})
	if got := call("get", a{"what": "value", "selector": "#em"}); strings.TrimSpace(got) != "" {
		t.Fatalf("select all then Backspace left %q", got)
	}
	expect(call("tabs", a{}), "Fixture")

	// A browser that closed behind the server's back, as the idle timeout
	// closes one, is reported on the next call.
	if _, err := mgr.Run(context.Background(), session, "close"); err != nil {
		t.Fatal(err)
	}
	expect(call("navigate", a{"url": site.URL + "/second"}), "Note: this session's browser had closed since its last use (browsers close after 10m without commands)")
	if got := call("get", a{"what": "title"}); strings.Contains(got, "had closed") {
		t.Fatalf("the restart is reported once:\n%s", got)
	}
}
