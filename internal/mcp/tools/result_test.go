package tools

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestFormatData(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{"empty", ``, "ok"},
		{"null object", `{}`, "ok"},
		{"only origin", `{"origin":"http://x/"}`, "ok"},
		{"snapshot drops refs and origin", `{"origin":"http://x/","refs":{"e1":{}},"snapshot":"- a [ref=e1]"}`, "- a [ref=e1]"},
		{"content key unwrapped", `{"origin":"http://x/","text":"hello\nworld"}`, "hello\nworld"},
		{"eval array result", `{"origin":"o","result":[1,"a"]}`, `[1,"a"]`},
		{"eval number result", `{"result":2}`, "2"},
		{"report", `{"report":"# Vitals"}`, "# Vitals"},
		{"flat object as lines", `{"url":"http://x/","title":"T"}`, "title: T\nurl: http://x/"},
		{"single non-content key", `{"clicked":"@e1"}`, "clicked: @e1"},
		{"bool and null", `{"cleared":true,"x":null}`, "cleared: true\nx: null"},
		{"nested stays JSON", `{"a":{"b":1},"c":2}`, `{"a":{"b":1},"c":2}`},
		{"bare string", `"hi"`, "hi"},
		{"bare array", `[1,2]`, "[1,2]"},
		{"persistence off is noise", `{"closed":true,"restoreStatus":"not_configured","saveStatus":"not_attempted"}`, "closed: true"},
		{"persistence status kept when on", `{"closed":true,"saveStatus":"saved"}`, "closed: true\nsaveStatus: saved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := formatData(json.RawMessage(c.in)); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("abcdef", 0); got != "abcdef" {
		t.Fatalf("max 0 must not truncate, got %q", got)
	}
	if got := truncate("abcdef", 6); got != "abcdef" {
		t.Fatalf("exact length must not truncate, got %q", got)
	}
	got := truncate("abcdef", 4)
	if !strings.HasPrefix(got, "abcd\n") || !strings.Contains(got, "truncated 2 chars") {
		t.Fatalf("got %q", got)
	}
	// Counts runes, not bytes, so multi-byte text is never split mid-character.
	got = truncate("héllo wörld", 3)
	if !strings.HasPrefix(got, "hél\n") || !strings.Contains(got, "truncated 8 chars") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatSnapshotDiff(t *testing.T) {
	t.Parallel()
	changed := `{"changed":true,"diff":"--- before\n+++ after\n@@ -1 +1 @@\n-- a\n+- b\n\\ No newline at end of file\n"}`
	if got := formatSnapshotDiff(json.RawMessage(changed)); got != "@@ -1 +1 @@\n-- a\n+- b" {
		t.Fatalf("got %q", got)
	}
	if got := formatSnapshotDiff(json.RawMessage(`{"changed":false,"diff":""}`)); got != "(no changes since last snapshot)" {
		t.Fatalf("got %q", got)
	}
	if got := formatSnapshotDiff(json.RawMessage(`"x"`)); got != "x" {
		t.Fatalf("non-diff data falls back to formatData, got %q", got)
	}
}

func TestLineFilter(t *testing.T) {
	t.Parallel()
	lines := []string{"[log] a", "[error] api 500", "[log] b", "[warning] api slow"}
	if got := (lineFilter{limit: 10, noun: "messages"}).apply(nil); got != "(no messages)" {
		t.Fatalf("got %q", got)
	}
	if got := (lineFilter{limit: 2, noun: "messages"}).apply(lines); got != "2 of 4 messages\n[log] b\n[warning] api slow" {
		t.Fatalf("limit keeps the newest, got %q", got)
	}
	f := lineFilter{pattern: regexp.MustCompile("(?i)API"), noun: "messages"}
	if got := f.apply(lines); got != "2 of 4 messages (2 matched pattern)\n[error] api 500\n[warning] api slow" {
		t.Fatalf("got %q", got)
	}
}

func TestConsoleFormatting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if got := e.call("console", a{"pattern": "api", "limit": 1}).text(); got != "1 of 3 log (2 matched pattern)\n[warning] slow api" {
		t.Fatalf("got %q", got)
	}
	if got := e.call("console", a{"kind": "errors"}).text(); got != "1 of 1 errors\nError: boom (http://fake/app.js:3:7)" {
		t.Fatalf("got %q", got)
	}
}

func TestNetworkFormatting(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	got := e.call("network", nil).text()
	want := "2 of 2 requests\n1.1 GET 200 Document http://fake/\n1.2 POST pending Fetch http://fake/api"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHelpReturnsPlainText(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if got := e.call("help", nil).text(); got != "Usage: agent-browser (fake help)" {
		t.Fatalf("got %q", got)
	}
}

func TestCLIErrorBecomesToolError(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("fill")
	res := e.call("fill", a{"selector": "#pw", "value": "hunter2"})
	if !res.IsError || res.text() != "agent-browser fill: fake failure" {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}

func TestImageMime(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{"a.jpg": "image/jpeg", "a.JPEG": "image/jpeg", "a.png": "image/png", "a.webp": "image/webp", "a": "image/png"} {
		if got := imageMime(path); got != want {
			t.Errorf("imageMime(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestJSString(t *testing.T) {
	t.Parallel()
	in := `a"b</script>`
	got := jsString(in)
	var back string
	if err := json.Unmarshal([]byte(got), &back); err != nil || back != in {
		t.Fatalf("jsString(%q) = %q does not round-trip", in, got)
	}
	if strings.Contains(got, "<") {
		t.Fatalf("jsString must escape <, got %q", got)
	}
}

func TestAppendTextExtendsLastTextBlock(t *testing.T) {
	t.Parallel()
	res := mcp.NewToolResultText("clicked: @e1")
	appendText(res, "Snapshot revision 2: unchanged since revision 1.")
	if len(res.Content) != 1 || res.Content[0].(mcp.TextContent).Text != "clicked: @e1\nSnapshot revision 2: unchanged since revision 1." {
		t.Fatalf("follow-up text must join the result on a new line, got %#v", res.Content)
	}
	res.Content = append(res.Content, mcp.NewImageContent("eA==", "image/png"))
	appendText(res, "legend")
	if len(res.Content) != 3 || res.Content[2].(mcp.TextContent).Text != "legend" {
		t.Fatalf("text after an image starts a new block, got %#v", res.Content)
	}
}

func TestFormatDataDropsLifecycle(t *testing.T) {
	t.Parallel()
	lifecycle := `"lifecycle":{"launched":false,"relaunchedBrowser":false,"reused":true}`
	if got := formatData(json.RawMessage(`{` + lifecycle + `,"url":"http://x/"}`)); got != "url: http://x/" {
		t.Fatalf("lifecycle must not reach the model, got %q", got)
	}
	if got := formatData(json.RawMessage(`{` + lifecycle + `,"origin":"o","result":"Probe"}`)); got != "Probe" {
		t.Fatalf("single content key must still unwrap, got %q", got)
	}
	relaunched := `{"lifecycle":{"relaunchedBrowser":true},"clicked":"@e1"}`
	if got := formatData(json.RawMessage(relaunched)); got != hintRelaunched+"\nclicked: @e1" {
		t.Fatalf("a relaunch must be reported, got %q", got)
	}
}

func TestFormatSnapshotDelta(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		{"full", `{"snapshot":{"kind":"full","revision":1,"refs":{},"tree":"- button \"Go\" [ref=e1]"}}`,
			"Snapshot revision 1 (full; delta snapshots after this return only changes):\n- button \"Go\" [ref=e1]"},
		{"unchanged", `{"snapshot":{"kind":"unchanged","baseRevision":1,"revision":2}}`,
			"Snapshot revision 2: unchanged since revision 1."},
		{"delta", `{"snapshot":{"kind":"delta","baseRevision":6,"revision":7,"changes":[
			{"op":"add","ref":"@e92","node":{"role":"link","name":"New"}},
			{"op":"add","ref":"@e93","node":{"role":"heading","name":"T","level":2}},
			{"op":"replace","ref":"@e10","field":"name","value":"Renamed"},
			{"op":"replace","ref":"@e11","field":"checked","value":true},
			{"op":"remove","ref":"@e90"},
			{"op":"move","ref":"@e5"}],
			"treeChange":{"startLine":4,"deleteCount":0,"lines":[]}}}`,
			"Snapshot revision 7: 6 changes since revision 6 (+ added, ~ changed, - removed)\n" +
				"+ @e92 link \"New\"\n" +
				"+ @e93 heading \"T\" [level=2]\n" +
				"~ @e10 name: \"Renamed\"\n" +
				"~ @e11 checked: true\n" +
				"- @e90\n" +
				`? {"op":"move","ref":"@e5"}`},
		{"unknown kind stays JSON", `{"snapshot":{"kind":"reset","revision":3}}`, `{"kind":"reset","revision":3}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := formatData(json.RawMessage(c.in)); got != c.want {
				t.Fatalf("got\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestSnapshotDeltaRoundTrip checks the delta flow end to end through the
// tool: a full baseline first, then only the changes.
func TestSnapshotDeltaRoundTrip(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first := e.call("snapshot", a{"interactive": true, "delta": true}).text()
	if !strings.HasPrefix(first, "Snapshot revision 1 (full") || !strings.Contains(first, `button "Go" [ref=e1]`) {
		t.Fatalf("first delta call must return the full tree, got %q", first)
	}
	second := e.call("click", a{"selector": "@e1", "snapshot": "delta"}).text()
	for _, want := range []string{"Snapshot revision 2: 3 changes since revision 1", `+ @e2 link "Next"`, `~ @e1 name: "Going"`, "- @e9"} {
		if !strings.Contains(second, want) {
			t.Fatalf("missing %q in %q", want, second)
		}
	}
	if strings.Contains(first+second, "lifecycle") {
		t.Fatal("lifecycle leaked into a result")
	}
}

func TestFormatDoctorIgnoresOtherOutput(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "plain text", `{"success":true,"data":{}}`, `{"checks":[]}`} {
		if got, ok := formatDoctor(in); ok {
			t.Errorf("formatDoctor(%q) = %q, want not a report", in, got)
		}
	}
}

func TestDoctorReportsCLIVersion(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	got := e.call("help", a{"topic": "doctor"}).text()
	want := "agent-browser 0.38.2 (supported: 0.38.0 or newer; tested with 0.38.2)\n\n" +
		"1 pass, 0 warn, 1 fail\n" +
		"[fail] Chrome: Chrome not found — fix: agent-browser install\n" +
		"[pass] Environment: CLI version 0.38.2 (macos aarch64)\n" +
		"[info] Providers: Kernel: KERNEL_API_KEY not set\n" +
		"fixed: installed Chrome"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	e = newEnv(t)
	e.fake.SetVersion("agent-browser 0.27.0")
	if got := e.call("help", a{"topic": "doctor"}).text(); !strings.Contains(got, "Warning: agent-browser 0.27.0 is older than 0.38.0") {
		t.Fatalf("got %q", got)
	}
}
