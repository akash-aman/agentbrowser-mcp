package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// realVitals is agent-browser 0.38.2's vitals output for docs/demo/site/feed.html.
const realVitals = `{"cls":{"entries":[{"startTime":940.2,"value":0.0556}],"score":0.06},"fcp":60,"hydratedComponents":[],"hydration":null,"inp":null,"lcp":{"element":"p","size":23906,"startTime":60,"url":null},"phases":[],"ttfb":2,"url":"http://localhost:8124/feed.html"}`

func TestFormatVitals(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ data, want string }{
		"good page": {realVitals, "Web Vitals for http://localhost:8124/feed.html, rated against Google's thresholds:\n" +
			"LCP 60 ms, good (p)\n" +
			"CLS 0.06, good (1 shift: 0.0556 at 940 ms)\n" +
			"INP not measured: it needs a click, tap or key press first\n" +
			"FCP 60 ms, good\n" +
			"TTFB 2 ms, good"},
		"poor page": {`{"cls":{"entries":[{"startTime":900,"value":0.05},{"startTime":1200,"value":0.3},{"startTime":2000,"value":0.1},{"startTime":2500,"value":0.01}],"score":0.46},"fcp":2100,"inp":{"value":350},"lcp":{"element":"img","startTime":5200,"url":"http://x/hero.jpg"},"ttfb":900}`,
			"Web Vitals, rated against Google's thresholds:\n" +
				"LCP 5200 ms, poor (img http://x/hero.jpg)\n" +
				"CLS 0.46, poor (4 shifts: 0.3 at 1200 ms, 0.1 at 2000 ms, 0.05 at 900 ms)\n" +
				"INP 350 ms, needs improvement\n" +
				"FCP 2100 ms, needs improvement\n" +
				"TTFB 900 ms, needs improvement"},
		"inp as a number, missing metrics": {`{"cls":{"entries":[],"score":0},"inp":120,"hydration":{"durationMs":40},"hydratedComponents":[{},{}]}`,
			"Web Vitals, rated against Google's thresholds:\n" +
				"LCP not measured\n" +
				"CLS 0, good\n" +
				"INP 120 ms, good\n" +
				"FCP not measured\n" +
				"TTFB not measured\n" +
				`Hydration: {"durationMs":40}` + "\n" +
				"Hydrated components: 2"},
	}
	for name, c := range cases {
		got, ok := formatVitals(json.RawMessage(c.data))
		if !ok || got != c.want {
			t.Errorf("%s: ok=%v\n got:\n%s\nwant:\n%s", name, ok, got, c.want)
		}
	}
	if _, ok := formatVitals(json.RawMessage(`{"url":"http://x/"}`)); ok {
		t.Error("data without any vital is not vitals output")
	}
}

func TestVitalsToolIsRated(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.Respond("vitals", realVitals)
	got := e.call("performance", a{"action": "vitals"}).text()
	if !strings.Contains(got, "CLS 0.06, good (1 shift: 0.0556 at 940 ms)") || strings.Contains(got, `"cls"`) {
		t.Fatalf("vitals should be rated lines, not JSON:\n%s", got)
	}
}
