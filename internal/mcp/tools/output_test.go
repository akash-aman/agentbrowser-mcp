package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// Results found too long or hard to read in a live test of every tool.

func TestRequestDetailIsReadable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("network request", `{"url":"http://x/api","method":"POST","status":500,"resourceType":"Fetch","mimeType":"application/json",`+
		`"headers":{"Content-Type":"application/json","Accept":"*/*"},"postData":"{\"id\":1}","responseHeaders":{"Date":"today"},"responseBody":"{\"error\":\"no\"}"}`)
	got := e.call("network", a{"action": "detail", "requestId": "1.2"}).text()
	want := "POST http://x/api → 500 (Fetch, application/json)\nrequest headers:\n  Accept: */*\n  Content-Type: application/json\n" +
		"request body: {\"id\":1}\nresponse headers:\n  Date: today\nresponse body: {\"error\":\"no\"}"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestURLDiffShowsChangedLines(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("diff url", `{"url1":"http://a/","url2":"http://b/","diff":{"changed":true},`+
		`"snapshot1":"- heading \"Shop\" [level=1, ref=e1]\n- button \"Buy\" [ref=e2]","snapshot2":"- heading \"Blog\" [level=1, ref=e7]\n- button \"Buy\" [ref=e8]"}`)
	got := e.call("diff", a{"kind": "urls", "urlA": "http://a/", "urlB": "http://b/"}).text()
	want := "http://a/ (-) vs http://b/ (+); this tab now shows http://b/\n- - heading \"Shop\" [level=1]\n+ - heading \"Blog\" [level=1]"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestLineDiff(t *testing.T) {
	t.Parallel()
	got := strings.Join(lineDiff([]string{"a", "b", "c", "d"}, []string{"a", "x", "c", "d", "e"}), "|")
	if got != "- b|+ x|+ e" {
		t.Fatalf("got %q", got)
	}
}

// TestGetStylesShowsCommonProperties: all ~400 computed values were 10 KB.
func TestGetStylesShowsCommonProperties(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("get styles", `{"styles":{"display":"block","color":"red","-webkit-locale":"en","margin-top":"0px","margin-right":"1px","margin-bottom":"2px","margin-left":"3px"}}`)
	got := e.call("get", a{"what": "styles", "selector": "h1"}).text()
	for _, want := range []string{"display: block", "color: red", "margin: 0px 1px 2px 3px", "elements computed"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "webkit") {
		t.Errorf("uncommon properties shown:\n%s", got)
	}
}

func TestFindHidesTheMarkerSelector(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("find", `{"clicked":"[data-agent-browser-located='true']"}`)
	if got := e.call("find", a{"by": "role", "value": "button", "name": "Sign up"}).text(); got != `clicked: the button named "Sign up"` {
		t.Errorf("role: %q", got)
	}
	if got := e.call("find", a{"by": "label", "value": "Email", "action": "fill", "input": "x"}).text(); got != `clicked: the element with label "Email"` {
		t.Errorf("label: %q", got)
	}
	// The CLI's own ref is not from the last snapshot.
	e.fake.Respond("find", `{"clicked":"@e1"}`)
	if got := e.call("find", a{"by": "role", "value": "button", "name": "Sign up"}).text(); got != `clicked: the button named "Sign up"` {
		t.Errorf("ref: %q", got)
	}
}

// TestStreamEnableWhenAlreadyStreaming: agent-browser 0.38 streams by default.
func TestStreamEnableWhenAlreadyStreaming(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailWith("stream enable", "Streaming is already enabled for this session")
	e.fake.Respond("stream status", `{"enabled":true,"port":9}`)
	res := e.call("debug_ui", a{"action": "stream_enable"})
	if res.IsError || !strings.Contains(res.text(), "already streaming:\nenabled: true\nport: 9") {
		t.Fatalf("isError=%v %q", res.IsError, res.text())
	}
}

func TestEmulateStepsAreLabelledOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("set offline", `{"offline":true}`)
	e.fake.Respond("set geo", `{"latitude":1,"longitude":2}`)
	got := e.call("emulate", a{"offline": true, "latitude": 1, "longitude": 2}).text()
	if !strings.Contains(got, "offline: true") || strings.Contains(got, "offline: offline") || !strings.Contains(got, "geo: latitude: 1, longitude: 2") {
		t.Fatalf("got %q", got)
	}
}

func TestSingleListValueIsOneLine(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("select", `{"selected":["pro","team"]}`)
	if got := e.call("select_option", a{"selector": "#plan", "values": []any{"pro", "team"}}).text(); got != "selected: pro, team" {
		t.Fatalf("got %q", got)
	}
}

// TestCookiesStorageAndStatesReadAsLines: these came back as raw JSON.
func TestCookiesStorageAndStatesReadAsLines(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ data, want string }{
		{`{"cookies":[]}`, "no cookies"},
		{`{"cookies":[{"domain":"127.0.0.1","expires":-1,"httpOnly":false,"name":"theme","path":"/","secure":false,"session":true,"value":"dark"},` +
			`{"domain":".x.com","expires":1893456000,"httpOnly":true,"name":"sid","path":"/api","secure":true,"session":false,"sameSite":"Lax","value":"abc"}]}`,
			"2 cookies:\n  theme=dark  127.0.0.1/  session\n  sid=abc  .x.com/api  expires 2030-01-01 00:00 UTC, HttpOnly, Secure, SameSite=Lax"},
		{`{"data":{}}`, "(empty)"},
		{`{"data":{"cart":"[1,2]","a":"b"}}`, "a: b\ncart: [1,2]"},
		{`{"directory":"/s","files":[]}`, "no saved states in /s"},
		{`{"directory":"/s","files":["work.json"]}`, "1 saved state in /s:\n  work.json"},
		{`{"directory":"/s","files":[{"encrypted":false,"filename":"work.json","modified":1893456000,"path":"/s/work.json","size":36}]}`,
			"1 saved state in /s:\n  work.json  36 B, saved 2030-01-01 00:00 UTC"},
	} {
		if got := formatData(json.RawMessage(c.data)); got != c.want {
			t.Errorf("formatData(%s)\n got %q\nwant %q", c.data, got, c.want)
		}
	}
}

func TestDashboardStartSaysWhere(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("dashboard start", `{"access_urls":[],"pid":58222,"port":4848}`)
	if got := e.call("debug_ui", a{"action": "dashboard_start"}).text(); got != "dashboard running at http://localhost:4848 (pid 58222); dashboard_stop stops it" {
		t.Errorf("got %q", got)
	}
}

// TestElementScreenshotHasNoAnnotateTip: annotate numbers the page's
// elements, which a screenshot of one element does not show.
func TestElementScreenshotHasNoAnnotateTip(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if got := e.call("screenshot", a{"selector": "#hero", "inline": false}).text(); strings.Contains(got, "annotate") {
		t.Errorf("element screenshot: %q", got)
	}
	if got := e.call("screenshot", a{"inline": false}).text(); !strings.Contains(got, "annotate:true") {
		t.Errorf("page screenshot lost the tip: %q", got)
	}
}

// TestEmulateSaysWhatItSet: agent-browser answers media and headers with
// only "set: true".
func TestEmulateSaysWhatItSet(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("set media", `{"set":true}`)
	e.fake.Respond("set headers", `{"set":true}`)
	e.fake.Respond("set credentials", `{"set":true}`)
	for _, c := range []struct {
		args a
		want string
	}{
		{a{"colorScheme": "dark", "reducedMotion": true}, "media: color scheme dark, reduced motion on"},
		{a{"reducedMotion": false}, "media: reduced motion off"},
		{a{"headers": `{"X-Test":"1","Accept-Language":"de"}`}, "headers: Accept-Language, X-Test sent with every request"},
		{a{"headers": `{}`}, "headers: cleared"},
		{a{"username": "u", "password": "p"}, "credentials: HTTP basic auth as u for this session's requests"},
	} {
		if got := e.call("emulate", c.args).text(); got != c.want {
			t.Errorf("emulate %v: got %q, want %q", c.args, got, c.want)
		}
	}
}

func TestListsAndSavedStatesReadAsLines(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ data, want string }{
		{`[{"name":"core","description":"Core agent-browser usage guide. Read this first."},{"name":"slack","description":"Interact with Slack workspaces using browser automation."}]`,
			"2 skills (help topic:\"skills\" name:<name> loads one):\n- core: Core agent-browser usage guide\n- slack: Interact with Slack workspaces using browser automation"},
		{`[{"directory":"Default","name":"personal"},{"directory":"Profile 1","name":"work"}]`,
			"2 Chrome profiles (directory, name):\n  Default  personal\n  Profile 1  work"},
		{`{"profiles":[]}`, "no saved logins; save one in a terminal with agent-browser auth save <name>"},
		{`[{"content":"# core\nUse snapshot.","name":"core"}]`, "# core\nUse snapshot."},
		{`{"encrypted":false,"filename":"s.json","modified":1893456000,"path":"/tmp/s.json","size":444,"summary":"1 cookies, 1 origins",` +
			`"state":{"cookies":[{"domain":"x","expires":-1,"name":"theme","path":"/","session":true,"value":"dark"}],` +
			`"origins":[{"localStorage":[{"name":"cart","value":"[1,2]"}],"origin":"http://x","sessionStorage":[]}]}}`,
			"/tmp/s.json (444 B, saved 2030-01-01 00:00 UTC)\n1 cookie:\n  theme=dark  x/  session\nhttp://x: localStorage cart; sessionStorage (empty)"},
	} {
		if got := formatData(json.RawMessage(c.data)); got != c.want {
			t.Errorf("formatData(%s)\n got %q\nwant %q", c.data, got, c.want)
		}
	}
}

func TestReactWithoutTheHookSaysHowToEnableIt(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailWith("react tree", "Evaluation error: Error: React DevTools hook not installed - relaunch with --enable react-devtools")
	res := e.call("react", a{"action": "tree"})
	if !res.IsError || !strings.Contains(res.text(), "Start agent-browser-mcp with --enable react-devtools") {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	} // Without the hook renders_start claimed to record; nothing reaches the CLI.
	if res := e.call("react", a{"action": "renders_start"}); !res.IsError || len(e.fake.Commands()) != 0 {
		t.Fatalf("renders_start without the hook: %q, CLI calls %q", res.text(), e.fake.Commands())
	}
}

func TestScreenshotDiffInPlainNumbers(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ data, want string }{
		{`{"diffPath":"/tmp/d.png","differentPixels":824,"dimensionMismatch":null,"match":false,"mismatchPercentage":0.08046874999999999,"totalPixels":1.024e+06}`,
			"screenshots differ: 824 of 1024000 pixels (0.08%); changed pixels are marked in /tmp/d.png"},
		{`{"differentPixels":0,"dimensionMismatch":null,"match":true,"mismatchPercentage":0,"totalPixels":1.024e+06}`, "screenshots match (1024000 pixels compared)"},
	} {
		if got := screenshotDiff(json.RawMessage(c.data)); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestURLDiffOfSameTreesEndsCleanly(t *testing.T) {
	t.Parallel()
	got := urlDiff(json.RawMessage(`{"url1":"http://a/","url2":"http://b/","snapshot1":"- heading \"X\" [ref=e1]","snapshot2":"- heading \"X\" [ref=e7]",` +
		`"screenshot":{"match":true,"differentPixels":0,"totalPixels":100,"mismatchPercentage":0,"dimensionMismatch":null}}`))
	want := "http://a/ (-) vs http://b/ (+); this tab now shows http://b/\nthe accessibility trees are the same\nscreenshots match (100 pixels compared)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
