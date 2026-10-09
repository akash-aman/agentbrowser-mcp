//go:build integration

package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
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
`

// TestIntegrationDevtools drives the CDP-backed tools against a real browser:
// debugger, coverage, heap, metrics, throttling, Application data, profiling,
// and Lighthouse when it is installed (set LIGHTHOUSE_PATH to point at it).
func TestIntegrationDevtools(t *testing.T) {
	path, err := exec.LookPath("agent-browser")
	if err != nil {
		t.Skip("agent-browser not on PATH")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, appJS)
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, ".used { color: red }\n.never-used { color: blue }\n#nothing { margin: 1px }\n")
		case "/api.json":
			fmt.Fprint(w, `{"ok":true}`)
		default:
			fmt.Fprint(w, devtoolsPage)
		}
	}))
	defer site.Close()

	cfg := testConfig(path)
	cfg.DefaultTimeout = 30000
	cfg.LighthousePath = os.Getenv("LIGHTHOUSE_PATH")
	mgr := browser.NewManager(cfg)
	srv := server.NewMCPServer("it", "0", server.WithToolCapabilities(false))
	reg := RegisterAll(srv, cfg, mgr)
	e := &env{t: t, srv: srv, cfg: cfg, mgr: mgr, reg: reg}
	session := fmt.Sprintf("abm-it-dt-%d", os.Getpid())
	t.Cleanup(func() {
		reg.Close()
		e.call("close_browser", a{"session": session})
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
		expect(call("tabs", a{}), `"tabId":"t1"`)
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
