// Package fakecli is a stand-in agent-browser binary for tests. The test
// binary re-executes itself as the CLI (the os/exec helper-process pattern),
// records each argv, and replies with canned JSON, so tools can be tested
// without a browser.
//
// Use it by calling MaybeRun first thing in TestMain, then Install in a test.
package fakecli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// binaryName is the name of the per-test symlink to the test binary; the
	// fake recognises itself by it and uses the symlink's directory as state.
	binaryName     = "agent-browser"
	lighthouseName = "lighthouse"
	markerFile     = ".fake-agent-browser"
)

// Fake controls the fake CLI for one test.
type Fake struct {
	// Path is the executable to use as AgentBrowserPath.
	Path string
	dir  string
}

// MaybeRun serves one CLI call and exits when the process was started as the
// fake. It must run before anything else in TestMain.
func MaybeRun() {
	dir, name := filepath.Split(os.Args[0])
	if _, err := os.Stat(filepath.Join(dir, markerFile)); err != nil {
		return
	}
	switch name {
	case binaryName:
		os.Exit(serve(dir, os.Args[1:]))
	case lighthouseName:
		os.Exit(serveLighthouse(dir, os.Args[1:]))
	}
}

// Install creates a fake CLI for one test: a symlink to the test binary in the
// test's own state directory. Nothing is set in the environment, so tests can
// run in parallel, and no new executable is created, which keeps macOS from
// security-scanning one per test.
func Install(t *testing.T) *Fake {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, binaryName)
	if err := os.Symlink(self, path); err != nil {
		t.Skipf("fake agent-browser needs symlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return &Fake{Path: path, dir: dir}
}

// InstallLighthouse adds a fake Lighthouse CLI next to the fake agent-browser
// and returns its path. It writes a canned JSON report to --output-path.
func (f *Fake) InstallLighthouse(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.dir, lighthouseName)
	if err := os.Symlink(self, path); err != nil {
		t.Fatal(err)
	}
	return path
}

// FailLighthouse makes the fake Lighthouse exit with an error.
func (f *Fake) FailLighthouse() { f.write("lighthouse-fail", "") }

// LighthouseCalls returns the argv of every fake Lighthouse run.
func (f *Fake) LighthouseCalls() [][]string { return f.readCalls("lighthouse-calls.jsonl") }

// Dir is the fake's working directory, used for screenshots and state.
func (f *Fake) Dir() string { return f.dir }

// SetURL sets the page URL the fake reports.
func (f *Fake) SetURL(url string) { f.write("url", url) }

// SetActiveTab sets the CDP target ID tab list reports as active (default T1,
// the fake browser's first page).
func (f *Fake) SetActiveTab(targetID string) { f.write("active-target", targetID) }

// SetVersion sets what `agent-browser --version` prints.
func (f *Fake) SetVersion(text string) { f.write("version", text) }

// FailOn makes a subcommand ("get") or subcommand pair ("get url") fail with
// a CLI error.
func (f *Fake) FailOn(cmd string) { f.write("fail", cmd) }

// FailWith makes a subcommand fail with this error message.
func (f *Fake) FailWith(cmd, msg string) {
	f.write("fail", cmd)
	f.write("fail-msg", msg)
}

// SleepMS delays responses to the given subcommands, or to every call when
// none are given.
func (f *Fake) SleepMS(ms int, cmds ...string) {
	f.write("sleep", strconv.Itoa(ms))
	f.write("sleep-cmds", strings.Join(cmds, ","))
}

// Respond replaces the data returned for a subcommand ("console") or a
// subcommand pair ("network requests").
func (f *Fake) Respond(cmd, dataJSON string) {
	f.write("resp-"+strings.ReplaceAll(cmd, " ", "_"), dataJSON)
}

// RespondRaw makes every call print raw text (not a JSON envelope) and exit
// with code.
func (f *Fake) RespondRaw(text string, code int) {
	f.write("raw", fmt.Sprintf("%d\n%s", code, text))
}

// Calls returns every recorded argv, including global flags.
func (f *Fake) Calls() [][]string { return f.readCalls("calls.jsonl") }

func (f *Fake) readCalls(name string) [][]string {
	file, err := os.Open(filepath.Join(f.dir, name))
	if err != nil {
		return nil
	}
	defer file.Close()
	var calls [][]string
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var argv []string
		if json.Unmarshal(sc.Bytes(), &argv) == nil {
			calls = append(calls, argv)
		}
	}
	return calls
}

// Commands returns every recorded argv with the global flags stripped, i.e.
// everything after --json.
func (f *Fake) Commands() [][]string {
	var out [][]string
	for _, c := range f.Calls() {
		out = append(out, Command(c))
	}
	return out
}

// Reset forgets recorded calls.
func (f *Fake) Reset() { os.Remove(filepath.Join(f.dir, "calls.jsonl")) }

// Command strips global flags (everything up to and including --json).
func Command(argv []string) []string {
	if i := slices.Index(argv, "--json"); i >= 0 {
		return argv[i+1:]
	}
	return argv
}

func (f *Fake) write(name, content string) {
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		panic(err)
	}
}

func read(dir, name string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	return string(b), err == nil
}

func record(dir, name string, argv []string) {
	line, err := json.Marshal(argv)
	if err != nil {
		return
	}
	if fh, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fh.Write(append(line, '\n'))
		fh.Close()
	}
}

// recordingFixture is a tiny trace with main-thread tasks and CPU samples,
// written by trace/profiler stop.
const recordingFixture = `{"traceEvents":[
 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":0,"dur":80000},
 {"name":"FunctionCall","ph":"X","pid":1,"tid":7,"ts":0,"dur":60000},
 {"name":"Paint","ph":"X","pid":1,"tid":7,"ts":70000,"dur":5000},
 {"name":"Profile","ph":"P","id":"0x1","pid":1,"tid":7,"args":{"data":{}}},
 {"name":"ProfileChunk","ph":"P","id":"0x1","pid":1,"tid":50,"args":{"data":{
   "cpuProfile":{"nodes":[{"id":1,"callFrame":{"functionName":"render","url":"http://fake/app.js"}}],"samples":[1,1]},"timeDeltas":[0,40000]}}}]}`

// doctorReport is `agent-browser doctor --json` output (0.38).
const doctorReport = `{"checks":[` +
	`{"category":"Environment","id":"env.version","message":"CLI version 0.38.2 (macos aarch64)","status":"pass"},` +
	`{"category":"Chrome","id":"chrome.installed","message":"Chrome not found","status":"fail","fix":"agent-browser install"},` +
	`{"category":"Providers","id":"providers.kernel","message":"Kernel: KERNEL_API_KEY not set","status":"info"}],` +
	`"fixed":["installed Chrome"],"success":true,"summary":{"fail":1,"pass":1,"warn":0}}`

// lighthouseReport is a minimal Lighthouse JSON report.
const lighthouseReport = `{"finalDisplayedUrl":"http://fake/","categories":{
 "performance":{"title":"Performance","score":0.5,"auditRefs":[{"id":"largest-contentful-paint","weight":25}]},
 "seo":{"title":"SEO","score":1,"auditRefs":[]}},
 "audits":{"largest-contentful-paint":{"title":"Largest Contentful Paint","score":0.3,"scoreDisplayMode":"numeric","displayValue":"5.0 s",
   "details":{"items":[{"node":{"snippet":"<img class=\"hero\" src=\"/hero.jpg\">"}}]}}}}`

func serveLighthouse(dir string, argv []string) int {
	record(dir, "lighthouse-calls.jsonl", argv)
	if _, failing := read(dir, "lighthouse-fail"); failing {
		fmt.Fprintln(os.Stderr, "Runtime error encountered: fake lighthouse failure")
		return 1
	}
	for _, a := range argv {
		if path, ok := strings.CutPrefix(a, "--output-path="); ok {
			os.WriteFile(path, []byte(lighthouseReport), 0o644)
		}
	}
	return 0
}

// serve handles one invocation and returns the exit code.
func serve(dir string, argv []string) int {
	record(dir, "calls.jsonl", argv)
	cmd := Command(argv)
	name, sub := route(cmd)
	if ms, ok := read(dir, "sleep"); ok {
		if only, _ := read(dir, "sleep-cmds"); only == "" || slices.Contains(strings.Split(only, ","), name) {
			n, _ := strconv.Atoi(ms)
			time.Sleep(time.Duration(n) * time.Millisecond)
		}
	}
	if raw, ok := read(dir, "raw"); ok {
		code, text, _ := strings.Cut(raw, "\n")
		fmt.Print(text)
		n, _ := strconv.Atoi(code)
		return n
	}

	if slices.Equal(argv, []string{"--version"}) {
		v, ok := read(dir, "version")
		if !ok {
			v = "agent-browser 0.38.2"
		}
		fmt.Println(v)
		return 0
	}

	if fail, ok := read(dir, "fail"); ok && (fail == name || fail == name+" "+sub) {
		msg, ok := read(dir, "fail-msg")
		if !ok {
			msg = "fake failure"
		}
		b, _ := json.Marshal(msg)
		fmt.Printf(`{"success":false,"data":null,"error":%s}`, b)
		return 1
	}
	if slices.Contains(cmd, "--help") {
		fmt.Print("Usage: agent-browser (fake help)")
		return 0
	}
	if name == "doctor" {
		// Like the real CLI, doctor prints its own report instead of an envelope.
		fmt.Print(doctorReport)
		return 0
	}
	data := withLifecycle(respond(dir, cmd, name, sub))
	fmt.Printf(`{"success":true,"data":%s,"error":null}`, data)
	return 0
}

// lifecycle is the launch report agent-browser 0.38+ adds to every object result.
const lifecycle = `{"effectiveLaunch":{"browserLaunched":true,"engine":"chrome","launchHash":1},"launched":false,"relaunchedBrowser":false,"restartedBackground":false,"restoreStatus":"not_configured","reused":true,"saveStatus":"not_attempted"}`

// withLifecycle adds the lifecycle field to object data, as the real CLI does,
// unless the canned response already has one.
func withLifecycle(data string) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(data), &obj) != nil {
		return data
	}
	if _, ok := obj["lifecycle"]; !ok {
		obj["lifecycle"] = json.RawMessage(lifecycle)
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// route returns the subcommand and its first argument, skipping per-command
// flags such as --user-agent that precede it.
func route(cmd []string) (name, sub string) {
	for i := 0; i < len(cmd); i++ {
		if cmd[i] == "--user-agent" {
			i++
			continue
		}
		name = cmd[i]
		if i+1 < len(cmd) {
			sub = cmd[i+1]
		}
		return name, sub
	}
	return "", ""
}

func respond(dir string, cmd []string, name, sub string) string {
	if d, ok := read(dir, "resp-"+name+"_"+sub); ok {
		return d
	}
	if d, ok := read(dir, "resp-"+name); ok {
		return d
	}
	url, _ := read(dir, "url")
	if url == "" {
		url = "about:blank"
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }

	switch name {
	case "open":
		url = cmd[len(cmd)-1]
		os.WriteFile(filepath.Join(dir, "url"), []byte(url), 0o644)
		return fmt.Sprintf(`{"title":"Fake","url":%s}`, q(url))
	case "back", "forward", "reload":
		return fmt.Sprintf(`{"url":%s}`, q(url))
	case "get":
		switch sub {
		case "url":
			return fmt.Sprintf(`{"url":%s}`, q(url))
		case "title":
			return `{"title":"Fake"}`
		}
		return fmt.Sprintf(`{"origin":%s,"text":"fake text"}`, q(url))
	case "tab":
		if sub == "list" {
			target, ok := read(dir, "active-target")
			if !ok {
				target = "T1"
			}
			return fmt.Sprintf(`{"tabs":[{"active":true,"label":null,"tabId":"t1","targetId":%s,"title":"Fake","type":"page","url":%s}]}`, q(target), q(url))
		}
	case "is":
		if sub == "visible" {
			return `{"visible":true}`
		}
	case "snapshot":
		if slices.Contains(cmd, "--delta") {
			return deltaSnapshot(dir, slices.Contains(cmd, "--full"))
		}
		return fmt.Sprintf(`{"origin":%s,"refs":{"e1":{"name":"Go","role":"button"}},"snapshot":"- button \"Go\" [ref=e1]"}`, q(url))
	case "diff":
		if sub == "snapshot" {
			return `{"additions":1,"changed":true,"diff":"--- before\n+++ after\n@@ -1 +1 @@\n-- button \"Go\" [ref=e1]\n+- button \"Gone\" [ref=e2]\n\\ No newline at end of file\n","removals":1,"unchanged":0}`
		}
		return `{"changed":false}`
	case "screenshot":
		return screenshot(dir, cmd)
	case "console":
		if slices.Contains(cmd, "--clear") {
			return `{"cleared":true}`
		}
		return `{"messages":[{"type":"log","text":"hello"},{"type":"error","text":"api failed 500"},{"type":"warning","text":"slow api"}]}`
	case "errors":
		if slices.Contains(cmd, "--clear") {
			return `{"cleared":true}`
		}
		if strings.Contains(url, "broken") {
			// A page whose script throws on load, reported like Chrome does:
			// no url, the location in the stack.
			return `{"errors":[{"text":"Error: boom","url":"http://fake/app.js","line":3,"column":7},` +
				`{"text":"TypeError: cart is undefined\n    at http://fake/broken.js:12:5","url":null,"line":0,"column":0}]}`
		}
		return `{"errors":[{"text":"Error: boom","url":"http://fake/app.js","line":3,"column":7}]}`
	case "network":
		if sub == "requests" {
			if slices.Contains(cmd, "--clear") {
				return `{"cleared":true}`
			}
			if strings.Contains(url, "broken") {
				// Ignores --status like a filter that matched everything, and
				// stamps the load's requests with the current time.
				now := time.Now().UnixMilli()
				return fmt.Sprintf(`{"requests":[`+
					`{"requestId":"0.9","method":"GET","status":500,"resourceType":"Fetch","url":"http://fake/old","timestamp":1}`+
					`,{"requestId":"2.1","method":"GET","status":200,"resourceType":"Document","url":%[2]s,"timestamp":%[1]d}`+
					`,{"requestId":"2.2","method":"GET","status":404,"resourceType":"Image","url":"http://fake/missing.png","timestamp":%[1]d}`+
					`,{"requestId":"2.3","method":"GET","status":500,"resourceType":"Fetch","url":"http://fake/api/cart","timestamp":%[1]d}`+
					`,{"requestId":"2.4","method":"GET","status":404,"resourceType":"Other","url":"http://fake/favicon.ico","timestamp":%[1]d}]}`, now, q(url))
			}
			return `{"requests":[{"requestId":"1.1","method":"GET","status":200,"resourceType":"Document","url":"http://fake/","headers":{"A":"b"}},{"requestId":"1.2","method":"POST","status":null,"resourceType":"Fetch","url":"http://fake/api","headers":{"A":"b"}}]}`
		}
	case "trace", "profiler":
		if sub == "stop" {
			path := filepath.Join(dir, name+".json")
			if last := cmd[len(cmd)-1]; strings.HasSuffix(last, ".json") {
				path = last
			}
			os.WriteFile(path, []byte(recordingFixture), 0o644)
			p, _ := json.Marshal(path)
			return fmt.Sprintf(`{"path":%s}`, p)
		}
	case "eval":
		return fmt.Sprintf(`{"origin":%s,"result":["one","two"]}`, q(url))
	case "session":
		if sub == "list" {
			return `{"sessions":["default"]}`
		}
		return `{"session":"default"}`
	}
	return fmt.Sprintf(`{"ok":true,"cmd":%s}`, q(name))
}

// deltaSnapshot mimics `snapshot --delta`: the first call (or --full) returns
// the whole tree, later ones the changes since the previous revision.
func deltaSnapshot(dir string, full bool) string {
	prev, ok := read(dir, "snapshot-revision")
	rev, _ := strconv.Atoi(prev)
	rev++
	os.WriteFile(filepath.Join(dir, "snapshot-revision"), []byte(strconv.Itoa(rev)), 0o644)
	if !ok || full {
		return fmt.Sprintf(`{"snapshot":{"kind":"full","revision":%d,"refs":{"e1":{"name":"Go","role":"button"}},"tree":"- button \"Go\" [ref=e1]"}}`, rev)
	}
	return fmt.Sprintf(`{"snapshot":{"kind":"delta","baseRevision":%d,"revision":%d,"changes":[`+
		`{"op":"add","ref":"@e2","node":{"role":"link","name":"Next"}},`+
		`{"op":"replace","ref":"@e1","field":"name","value":"Going"},`+
		`{"op":"remove","ref":"@e9"}],`+
		`"treeChange":{"startLine":1,"deleteCount":0,"lines":["- link \"Next\" [ref=e2]"]}}}`, rev-1, rev)
}

// tinyPNG is a valid 1x1 PNG.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

// TinyPNG returns the image bytes the fake writes for screenshots.
func TinyPNG() []byte { return slices.Clone(tinyPNG) }

func screenshot(dir string, cmd []string) string {
	ext := ".png"
	if i := slices.Index(cmd, "--screenshot-format"); i >= 0 && i+1 < len(cmd) && cmd[i+1] == "jpeg" {
		ext = ".jpg"
	}
	path := filepath.Join(dir, "shot"+ext)
	if last := cmd[len(cmd)-1]; strings.HasSuffix(last, ".png") || strings.HasSuffix(last, ".jpg") {
		path = last
	}
	os.WriteFile(path, tinyPNG, 0o644)
	p, _ := json.Marshal(path)
	if slices.Contains(cmd, "--annotate") {
		return fmt.Sprintf(`{"path":%s,"annotations":[{"number":1,"ref":"e1","role":"button","name":"Go","box":{"x":1,"y":2,"width":3,"height":4}},{"number":2,"ref":"e2","role":"textbox","box":{"x":1,"y":2,"width":3,"height":4}}]}`, p)
	}
	return fmt.Sprintf(`{"path":%s}`, p)
}
