//go:build integration

package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mark3labs/mcp-go/server"

	"github.com/xcode-studio/agentbrowser-mcp/internal/browser"
)

const devtoolsPage = `<!doctype html><title>Shop</title>
<link rel="stylesheet" href="/style.css">
<button id="buy" class="used">Buy</button><div id="cart"></div>
<script src="/app.js"></script>`

// appJS line numbers matter: the test sets breakpoints on them.
const appJS = `function total(items) {
  let sum = 0;
  for (const i of items) {
    sum += i;
  }
  return sum;
}
function unused() {
  return "never called";
}
document.getElementById("buy").addEventListener("click", function onBuy() {
  const t = total([1, 2, 3]);
  document.getElementById("cart").textContent = "Total " + t;
  fetch("/api.json");
});
const req = indexedDB.open("shop", 1);
req.onupgradeneeded = () => req.result.createObjectStore("carts", { keyPath: "id" });
req.onsuccess = () => req.result.transaction("carts", "readwrite").objectStore("carts").put({ id: 1, user: "ann" });
function later() {
  return document.title;
}
document.getElementById("cart").addEventListener("click", () => setTimeout(function tick() { later(); }, 0));
`

// cartTS is the original source of bundleJS: the bundle adds two header lines.
const cartTS = "function add(price: number) {\n  let total = 0;\n  total += price;\n  return total;\n}\n" +
	"document.getElementById(\"add\")!.addEventListener(\"click\", () => { document.title = \"t\" + add(5); });\n"

const bundleJS = "// bundled\n// do not edit\nfunction add(price) {\n  let total = 0;\n  total += price;\n  return total;\n}\n" +
	"document.getElementById(\"add\").addEventListener(\"click\", () => { document.title = \"t\" + add(5); });\n//# sourceMappingURL=/bundle.js.map\n"

// TestIntegrationDevtools drives the CDP-backed tools against a real browser:
// debugger, coverage, heap, metrics, throttling, Application data, profiling,
// and Lighthouse when it is installed (set LIGHTHOUSE_PATH to point at it).
func TestIntegrationDevtools(t *testing.T) {
	path, err := exec.LookPath("agent-browser")
	if err != nil {
		t.Skip("agent-browser not on PATH")
	}
	// A second origin on another site (localhost, not 127.0.0.1) runs its
	// iframe in its own process, as a separate target.
	otherSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Other site</title>`)
	}))
	defer otherSite.Close()
	other := strings.Replace(otherSite.URL, "127.0.0.1", "localhost", 1)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, appJS)
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, ".used { color: red }\n.never-used { color: blue }\n#nothing { margin: 1px }\nbutton { color: black; background: rgb(231, 111, 81) }\n")
		case "/api.json":
			// SameSite=None without Secure is refused, for the cookies action.
			w.Header().Set("Set-Cookie", "loose=1; SameSite=None")
			fmt.Fprint(w, `{"ok":true}`)
		case "/ws":
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			for {
				_, msg, err := c.Read(r.Context())
				if err != nil {
					return
				}
				c.Write(r.Context(), websocket.MessageText, append([]byte("echo:"), msg...))
			}
		case "/live":
			fmt.Fprint(w, `<!doctype html><title>Live</title><script>const ws = new WebSocket(location.origin.replace("http", "ws") + "/ws"); ws.onopen = () => ws.send("hello"); ws.onmessage = (e) => document.title = e.data;</script>`)
		case "/mapped":
			fmt.Fprintf(w, `<!doctype html><title>Mapped</title><button id="add">Add</button><script src="/bundle.js"></script>`+
				`<iframe name="ads" src="/frame"></iframe><iframe src="%s/frame"></iframe>`+
				"<button id=\"inl\" onclick=\"inlineFn()\">Inline</button>\n<script>\nfunction inlineFn() {\n  return 41 + 1;\n}\n</script>", other)
		case "/bundle.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, bundleJS)
		case "/bundle.js.map":
			m, _ := json.Marshal(map[string]any{"version": 3, "sources": []string{"webpack://shop/./src/cart.ts"},
				"sourcesContent": []string{cartTS}, "mappings": ";;AAAA;AACA;AACA;AACA;AACA;AACA"})
			w.Write(m)
		case "/frame":
			fmt.Fprint(w, `<!doctype html><title>Ad frame</title>`)
		default:
			fmt.Fprint(w, devtoolsPage)
		}
	}))
	defer site.Close()

	cfg := testConfig(path)
	cfg.DefaultTimeout = 30000
	cfg.LighthousePath = os.Getenv("LIGHTHOUSE_PATH")
	// A run that fails before cleanup once left its browser running for half
	// an hour; the idle timeout closes it.
	cfg.IdleTimeout = 5 * time.Minute
	mgr := browser.NewManager(cfg)
	srv := server.NewMCPServer("it", "0", server.WithToolCapabilities(false))
	reg := RegisterAll(srv, cfg, mgr)
	e := &env{t: t, srv: srv, cfg: cfg, mgr: mgr, reg: reg}
	session := fmt.Sprintf("abm-it-dt-%d", os.Getpid())
	t.Cleanup(func() {
		reg.Close()
		if res := e.call("close_browser", a{"session": session}); res.IsError {
			t.Errorf("close_browser left the test browser running: %s", res.text())
		}
	})

	// call and expect report failures on whichever test or subtest is running.
	var cur *testing.T
	call := func(tool string, args map[string]any) string {
		cur.Helper()
		args["session"] = session
		res := e.call(tool, args)
		if res.IsError {
			cur.Fatalf("%s %v: %s", tool, args, res.text())
		}
		return res.text()
	}
	expect := func(got string, wants ...string) {
		cur.Helper()
		for _, want := range wants {
			if !strings.Contains(got, want) {
				cur.Fatalf("want %q in:\n%s", want, got)
			}
		}
	}
	parent := t
	run := func(name string, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			cur, e.t = t, t
			defer func() { cur, e.t = parent, parent }()
			fn(t)
		})
	}
	appURL := site.URL + "/app.js"

	cur = t
	call("navigate", a{"url": site.URL})
	call("wait", a{"for": "load"})
	expect(call("performance", a{"action": "metrics"}), "Nodes:", "JSHeapUsedSize:")

	run("debugger", func(t *testing.T) {
		// Never leave the page paused for the next subtests, even on failure.
		defer e.call("debugger", a{"action": "disable", "session": session})
		expect(call("debugger", a{"action": "breakpoint", "url": appURL, "line": 12}), appURL+":12")
		expect(call("debugger", a{"action": "listeners", "selector": "#buy"}), "click", "onBuy")

		paused := call("click", a{"selector": "#buy"})
		expect(paused, "Paused", "onBuy ("+appURL+":12", "click finishes after you resume")
		expect(call("debugger", a{"action": "stack"}), "#0 onBuy")
		expect(call("debugger", a{"action": "scope"}), "local onBuy:", "t = (uninitialized)")
		expect(call("debugger", a{"action": "step_over"}), appURL+":13")
		expect(call("debugger", a{"action": "evaluate", "expression": "t * 2"}), "12")
		expect(call("debugger", a{"action": "source", "script": appURL, "from": 11, "to": 13}), "onBuy")
		expect(call("debugger", a{"action": "remove", "all": true}), "removed 1")
		call("debugger", a{"action": "resume"})
		expect(call("wait", a{"for": "text", "value": "Total 6"}), "")

		call("debugger", a{"action": "xhr_breakpoint", "urlContains": "/api.json"})
		expect(call("click", a{"selector": "#buy"}), "Paused (XHR)")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})

		call("debugger", a{"action": "exceptions", "mode": "uncaught"})
		call("eval_script", a{"script": `setTimeout(() => { throw new Error("boom") }, 50); 1`})
		expect(call("debugger", a{"action": "wait", "timeoutMs": 5000}), "Paused (exception): Error: boom")
		call("debugger", a{"action": "resume"})
		expect(call("debugger", a{"action": "disable"}), "debugger off")
	})

	run("debugged tab closed", func(t *testing.T) {
		expect(call("tabs", a{}), "* t1 ")
		call("tabs", a{"action": "new", "url": site.URL})
		call("tabs", a{"action": "switch", "tab": "t1"})
		call("debugger", a{"action": "breakpoint", "url": appURL, "line": 12})
		call("tabs", a{"action": "close", "tab": "t1"})
		// The pool must notice Chrome ended the session and move to the tab
		// that is now active instead of failing with "Session ... not found".
		var got string
		for range 20 {
			if got = call("debugger", a{"action": "list"}); strings.Contains(got, "was closed") {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		expect(got, "the tab being debugged was closed", "(no breakpoints)")
		call("debugger", a{"action": "disable"})
	})

	run("coverage", func(t *testing.T) {
		call("performance", a{"action": "coverage_start"})
		call("navigate", a{"action": "reload"})
		call("wait", a{"for": "load"})
		call("click", a{"selector": "#buy"})
		expect(call("performance", a{"action": "coverage_stop"}), "unused", "js", appURL, "css", site.URL+"/style.css")
	})

	run("heap and profiling", func(t *testing.T) {
		expect(call("performance", a{"action": "heap_snapshot", "path": t.TempDir() + "/h.heapsnapshot"}), "objects", "Top by self size")
		call("performance", a{"action": "profiler_start"})
		call("click", a{"selector": "#buy"})
		expect(call("performance", a{"action": "profiler_stop", "path": t.TempDir() + "/p.cpuprofile"}), "CPU profile:")
		call("performance", a{"action": "trace_start"})
		call("navigate", a{"action": "reload"})
		expect(call("performance", a{"action": "trace_stop", "path": t.TempDir() + "/t.json"}), "Trace:", "long tasks")
	})

	run("application", func(t *testing.T) {
		call("wait", a{"for": "function", "value": "new Promise(r => { const q = indexedDB.open('shop'); q.onsuccess = () => r(q.result.objectStoreNames.contains('carts')) })"})
		expect(call("application", a{"action": "indexeddb"}), "shop", "store carts")
		expect(call("application", a{"action": "indexeddb_read", "database": "shop", "store": "carts"}), `"ann"`)
		expect(call("application", a{"action": "storage_usage"}), "quota")
		expect(call("application", a{"action": "manifest"}), "no web app manifest")
		expect(call("application", a{"action": "service_workers"}), "service worker")
		expect(call("application", a{"action": "cache_storage"}), "Cache Storage")
		call("console", a{"kind": "issues"})
	})

	run("sources", func(t *testing.T) {
		defer e.call("debugger", a{"action": "disable", "session": session})
		call("navigate", a{"url": site.URL})
		call("debugger", a{"action": "breakpoint", "url": appURL, "line": 12})
		expect(call("click", a{"selector": "#buy"}), "Paused", appURL+":12")
		expect(call("debugger", a{"action": "restart_frame"}), "restarted; Paused", "onBuy")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})

		// An edited script replaces the original on the reload, like an override.
		expect(call("debugger", a{"action": "edit_source", "script": appURL, "find": `"Total " + t`, "replace": `"Sum " + t`}), "edited "+appURL+" at line 13", "reloaded:")
		call("click", a{"selector": "#buy"})
		call("wait", a{"for": "text", "value": "Sum 6"})
		expect(call("debugger", a{"action": "revert_source"}), "reverted 1 edited script")
		call("click", a{"selector": "#buy"})
		call("wait", a{"for": "text", "value": "Total 6"})

		// A function breakpoint reached through a timer shows the async frames.
		expect(call("debugger", a{"action": "function_breakpoint", "expression": "later"}), "function calls to later")
		expect(call("click", a{"selector": "#cart"}), "Paused", "at later (")
		expect(call("debugger", a{"action": "stack"}), "#1 tick (", "-- setTimeout --")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})

		// Source maps: a breakpoint on the authored file, pauses shown in it.
		call("navigate", a{"url": site.URL + "/mapped"})
		expect(call("debugger", a{"action": "breakpoint", "url": "src/cart.ts", "line": 3}), "src/cart.ts:3 (in the bundle at "+site.URL+"/bundle.js:5:")
		expect(call("click", a{"selector": "#add"}), "Paused", "bundle.js:5:3 → src/cart.ts:3:", "src/cart.ts:\n", "►    3    total += price;")
		expect(call("debugger", a{"action": "source", "script": "src/cart.ts", "from": 3, "to": 3}), "total += price;")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})
		// After a reload, source reads the new copy of the script, not an ID
		// Chrome dropped with the old page.
		call("navigate", a{"action": "reload"})
		expect(call("debugger", a{"action": "source", "script": site.URL + "/bundle.js", "from": 3, "to": 3}), "function add(price)")

		// A click event breakpoint stops in the page's listener, not in a
		// capturing listener from an isolated world (agent-browser's own
		// input listeners, or an extension's).
		frames := call("cdp", a{"method": "Page.getFrameTree"})
		frameID := regexp.MustCompile(`"id":"([^"]+)"`).FindStringSubmatch(frames)
		if frameID == nil {
			t.Fatalf("no frame id in %s", frames)
		}
		world := call("cdp", a{"method": "Page.createIsolatedWorld", "params": `{"frameId":"` + frameID[1] + `","worldName":"abm-it"}`})
		contextID := regexp.MustCompile(`"executionContextId":(\d+)`).FindStringSubmatch(world)
		if contextID == nil {
			t.Fatalf("no isolated world in %s", world)
		}
		call("cdp", a{"method": "Runtime.evaluate", "params": `{"contextId":` + contextID[1] + `,"expression":"addEventListener('click', function isolatedListener() {}, true)"}`})
		call("debugger", a{"action": "event_breakpoint", "event": "click"})
		expect(call("click", a{"selector": "#add"}), "Paused (EventListener)", "at (anonymous) ("+site.URL+"/bundle.js:")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})

		// A pause in an inline <script> shows its lines, numbered as in the page.
		call("debugger", a{"action": "function_breakpoint", "expression": "inlineFn"})
		expect(call("click", a{"selector": "#inl"}), "Paused", "return 41 + 1;", "►")
		call("debugger", a{"action": "remove", "all": true})
		call("debugger", a{"action": "resume"})

		expect(call("eval_script", a{"script": "document.title", "frame": "ads"}), site.URL+"/frame: Ad frame")
		expect(call("eval_script", a{"script": "document.title", "frame": other}), "iframe "+other+"/frame: Other site")
	})

	run("network capture", func(t *testing.T) {
		call("navigate", a{"url": site.URL})
		expect(call("network", a{"action": "capture"}), "capturing")
		call("click", a{"selector": "#buy"})
		call("wait", a{"for": "text", "value": "Total 6"})
		expect(call("network", a{"action": "initiator", "filter": "api.json"}), "GET "+site.URL+"/api.json", "initiator: script onBuy (")
		expect(call("network", a{"action": "cookies", "filter": "api.json"}), "Set-Cookie refused:", "loose=1: SameSiteNoneInsecure")
		expect(call("network", a{"action": "curl", "filter": "api.json"}), "curl '"+site.URL+"/api.json'")
		expect(call("network", a{"action": "replay", "filter": "api.json"}), "replayed GET "+site.URL+"/api.json: 200")

		call("navigate", a{"url": site.URL + "/live"})
		call("wait", a{"for": "function", "value": "document.title === 'echo:hello'"})
		expect(call("network", a{"action": "websockets", "filter": "/ws"}), "sent", "hello", "received", "echo:hello")
		expect(call("network", a{"action": "search", "query": "ws.onopen"}), site.URL+"/live", `ws.send("hello")`)

		// A WebSocket's state lives in getters on its prototype, handlers too.
		ws := call("debugger", a{"action": "evaluate", "expression": "ws"})
		expect(ws, "onopen: ƒ")
		id := regexp.MustCompile(`\[(\S+)\]$`).FindStringSubmatch(ws)
		if id == nil {
			t.Fatalf("no object id in %q", ws)
		}
		expect(call("debugger", a{"action": "properties", "objectId": id[1]}), "readyState = 1  (getter)", "onmessage = ƒ")
		call("debugger", a{"action": "disable"})
	})

	run("application extras", func(t *testing.T) {
		call("navigate", a{"url": site.URL + "/mapped"})
		expect(call("application", a{"action": "frames"}), site.URL+"/mapped", `(name "ads")`, other+"/frame  (cross-site iframe")
		expect(call("application", a{"action": "security"}), "security state:", "no TLS")
		call("navigate", a{"url": site.URL + "/frame"})
		expect(call("application", a{"action": "bfcache"}), site.URL+"/frame was", "back/forward cache")
	})

	run("performance and memory extras", func(t *testing.T) {
		call("navigate", a{"url": site.URL})
		shot := e.call("performance", a{"action": "filmstrip", "session": session})
		if shot.IsError || shot.images() != 1 || !strings.Contains(shot.text(), "Filmstrip:") {
			t.Fatalf("filmstrip: %+v", shot)
		}
		call("performance", a{"action": "trace_start"})
		call("navigate", a{"action": "reload"})
		expect(call("performance", a{"action": "trace_stop", "path": t.TempDir() + "/t.json"}), "Insights:", "FCP", "LCP", "requests that hold up the first render:", site.URL+"/style.css")

		dir := t.TempDir()
		call("performance", a{"action": "heap_snapshot", "path": dir + "/a.heapsnapshot"})
		call("eval_script", a{"script": "window.leak = Array.from({ length: 2000 }, () => document.createElement('div')); 1"})
		call("performance", a{"action": "heap_snapshot", "path": dir + "/b.heapsnapshot"})
		// Chrome names DOM nodes in snapshots by tag; detached ones are the leak.
		expect(call("performance", a{"action": "heap_diff"}), "Heap grew by", "detached DOM nodes 0 → 2000", "+2000  <div>")
		expect(call("performance", a{"action": "retainers", "constructor": "<div>"}), "how the largest are retained", "Window -.leak-> Array")
		expect(call("performance", a{"action": "query_objects", "constructor": "HTMLDivElement"}), "live HTMLDivElement objects")
	})

	run("animations, recorder, passkeys and raw CDP", func(t *testing.T) {
		call("navigate", a{"url": site.URL})
		expect(call("debug_ui", a{"action": "animations"}), "no animations")
		call("eval_script", a{"script": "document.getElementById('buy').animate([{ opacity: 0 }, { opacity: 1 }], { duration: 1000, iterations: Infinity }); 1"})
		expect(call("debug_ui", a{"action": "animations"}), "1 animation:", "Animation", "button#buy", "running", "infinite")
		expect(call("debug_ui", a{"action": "animations", "playbackRate": 0}), "paused every animation")
		call("debug_ui", a{"action": "animations", "playbackRate": 1})

		call("record", a{"action": "flow_start"})
		ref := regexp.MustCompile(`button "Buy" \[ref=(e\d+)\]`).FindStringSubmatch(call("snapshot", a{"interactive": true}))
		if ref == nil {
			t.Fatal("no ref for the Buy button")
		}
		call("click", a{"selector": "@" + ref[1]})
		call("wait", a{"for": "text", "value": "Total 6"})
		call("find", a{"by": "role", "value": "button", "name": "Buy"})
		buy := `await page.getByRole("button", { name: "Buy" }).click();`
		found := `await page.getByRole("button", { name: "Buy" }).first().click();` // find takes the first match
		expect(call("record", a{"action": "flow_export"}), `await page.goto("`+site.URL+`/");`,
			buy+"\n  "+`await expect(page.getByText("Total 6").first()).toBeVisible();`+"\n  "+found)

		// WebAuthn needs a domain, so this runs on the localhost origin.
		call("navigate", a{"url": other})
		expect(call("emulate", a{"authenticator": true}), "virtual passkey authenticator on")
		expect(call("eval_script", a{"script": `navigator.credentials.create({ publicKey: { challenge: new Uint8Array(16), rp: { name: "t" },
			user: { id: new Uint8Array(8), name: "ann", displayName: "Ann" }, pubKeyCredParams: [{ type: "public-key", alg: -7 }],
			authenticatorSelection: { residentKey: "required", userVerification: "required" } } }).then((c) => c.type)`}), "public-key")
		expect(call("application", a{"action": "credentials"}), "1 passkey:", "for localhost", "discoverable true")
		expect(call("emulate", a{"authenticator": false}), "removed the virtual authenticator")

		expect(call("cdp", a{"method": "Browser.getVersion", "target": "browser"}), "protocolVersion")
		expect(call("cdp", a{"method": "Runtime.evaluate", "params": `{"expression":"1+2","returnByValue":true}`}), `"value":3`)
	})

	run("elements", func(t *testing.T) {
		call("navigate", a{"url": site.URL})
		// .used beats the button rule on specificity, so its color wins.
		expect(call("elements", a{"action": "styles", "selector": "#buy"}),
			".used  style.css:1", "color: red", "button  style.css:4", "color: black   [overridden]", "background: rgb(231, 111, 81)")
		if s := call("elements", a{"action": "styles", "selector": "#buy"}); strings.Contains(s, "background-image") {
			t.Fatalf("styles listed the longhands of background:\n%s", s)
		}
		expect(call("elements", a{"action": "computed", "selector": "#buy", "properties": []any{"color"}}), "color: rgb(255, 0, 0)")
		expect(call("elements", a{"action": "contrast", "selector": "#buy"}), "rgb(255, 0, 0) on rgb(231, 111, 81)", "AA FAILS")

		ref := regexp.MustCompile(`button "Buy" \[ref=(e\d+)\]`).FindStringSubmatch(call("snapshot", a{"interactive": true}))
		if ref == nil {
			t.Fatal("no ref for the Buy button")
		}
		at := "@" + ref[1]
		expect(call("elements", a{"action": "a11y", "selector": at}), "role: button", `name: "Buy" (from contents)`)
		expect(call("elements", a{"action": "selector", "selector": at}), "css: #buy", `playwright: page.getByRole("button", { name: "Buy" })`)
		expect(call("elements", a{"action": "box", "selector": at}), "size", "padding")
		expect(call("elements", a{"action": "search", "query": "//button"}), "1 element matches", "#buy")

		call("elements", a{"action": "set_style", "selector": "#buy", "value": "color: rgb(0, 0, 0)"})
		expect(call("elements", a{"action": "contrast", "selector": "#buy"}), "rgb(0, 0, 0) on rgb(231, 111, 81)", "AA passes")
		expect(call("elements", a{"action": "css_overview"}), "CSS overview of", "rules matching no element right now: 2 of 4")

		expect(call("emulate", a{"timezone": "Asia/Tokyo", "locale": "de-DE"}), "locale de-DE, timezone Asia/Tokyo")
		expect(call("eval_script", a{"script": "Intl.DateTimeFormat().resolvedOptions().timeZone + ' ' + new Intl.NumberFormat().resolvedOptions().locale"}), "Asia/Tokyo de-DE")
		expect(call("emulate", a{"timezone": "", "locale": ""}), "overrides: none")
		call("navigate", a{"action": "reload"})
	})

	run("throttling", func(t *testing.T) {
		// Time a fresh fetch: the document itself may come from cache.
		fetchMs := func() float64 {
			out := call("eval_script", a{"script": "(async () => { const s = performance.now(); await fetch('/api.json?x=' + Math.random()); return performance.now() - s })()"})
			ms, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
			if err != nil {
				t.Fatalf("fetch timing %q: %v", out, err)
			}
			return ms
		}
		expect(call("emulate", a{"networkProfile": "slow-3g", "cpuSlowdown": 2}), "latency 2000ms", "CPU 2x slower")
		if ms := fetchMs(); ms < 1500 {
			t.Fatalf("slow-3g should add ~2s latency, fetch took %.0fms", ms)
		}
		expect(call("emulate", a{"networkProfile": "none", "cpuSlowdown": 1}), "network unthrottled", "CPU unthrottled")
		if ms := fetchMs(); ms > 1000 {
			t.Fatalf("throttling should be gone, fetch took %.0fms", ms)
		}
	})

	run("lighthouse", func(t *testing.T) {
		if cfg.LighthousePath == "" {
			t.Skip("set LIGHTHOUSE_PATH to run the Lighthouse audit")
		}
		expect(call("performance", a{"action": "lighthouse", "formFactor": "desktop", "categories": []any{"performance", "accessibility"}}),
			"Lighthouse "+site.URL, "Performance", "Accessibility", "full report:")
	})
}
