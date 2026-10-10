package devtools

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestUsedBytes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		ranges []coverageRange
		want   int
	}{
		{"nothing ran", []coverageRange{{0, 100, 0}}, 0},
		{"all ran", []coverageRange{{0, 100, 1}}, 100},
		{"unrun block inside run function", []coverageRange{{0, 100, 1}, {40, 60, 0}}, 80},
		{"run function inside unrun script", []coverageRange{{0, 100, 0}, {10, 30, 2}}, 20},
		{"nested three deep", []coverageRange{{0, 100, 1}, {20, 80, 0}, {30, 40, 1}}, 50},
		{"adjacent ranges", []coverageRange{{0, 50, 1}, {50, 100, 0}}, 50},
		{"unsorted input", []coverageRange{{40, 60, 0}, {0, 100, 1}}, 80},
		{"inner block shares the start offset", []coverageRange{{0, 40, 0}, {0, 100, 1}}, 60},
		{"inner block shares the end offset", []coverageRange{{60, 100, 0}, {0, 100, 1}}, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := usedBytes(c.ranges); got != c.want {
				t.Fatalf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestJSCoverageGroupsByURL(t *testing.T) {
	t.Parallel()
	scripts := []scriptCoverage{
		{URL: "http://x/app.js"},
		{URL: "http://x/"},
		{URL: ""},
		{URL: "http://x/"},
	}
	scripts[0].Functions = append(scripts[0].Functions, struct {
		Ranges []coverageRange `json:"ranges"`
	}{Ranges: []coverageRange{{0, 100, 1}, {50, 100, 0}}})
	for _, i := range []int{1, 2, 3} {
		scripts[i].Functions = append(scripts[i].Functions, struct {
			Ranges []coverageRange `json:"ranges"`
		}{Ranges: []coverageRange{{0, 10, 1}}})
	}
	got := jsCoverage(scripts)
	want := []FileCoverage{{URL: "http://x/app.js", Kind: "js", Total: 100, Used: 50}, {URL: "http://x/", Kind: "js", Total: 20, Used: 20}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestCSSCoverage(t *testing.T) {
	t.Parallel()
	sheets := map[string]styleSheet{
		"1": {url: "http://x/a.css", length: 200},
		"2": {url: "http://x/", length: 50},
		"3": {url: "http://x/", length: 50},
		"4": {url: "", length: 10},
	}
	rules := []ruleUsage{
		{StyleSheetID: "1", StartOffset: 0, EndOffset: 60, Used: true},
		{StyleSheetID: "1", StartOffset: 60, EndOffset: 200, Used: false},
		{StyleSheetID: "2", StartOffset: 0, EndOffset: 50, Used: true},
	}
	got := cssCoverage(rules, sheets)
	want := []FileCoverage{{URL: "http://x/a.css", Kind: "css", Total: 200, Used: 60}, {URL: "http://x/", Kind: "css", Total: 100, Used: 50}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

const tinySnapshot = `{
 "snapshot": {"meta": {
   "node_fields": ["type","name","id","self_size","edge_count","trace_node_id","detachedness"],
   "node_types": [["hidden","array","string","object","code","closure","regexp","number","native","synthetic","concatenated string","sliced string","symbol","bigint","object shape"],"string","number","number","number","number","number"]
 }},
 "nodes": [
   3,0,1,100,0,0,0,
   3,0,2,50,0,0,0,
   5,1,3,40,0,0,0,
   2,2,4,30,0,0,0,
   8,3,5,20,0,0,2,
   8,3,6,20,0,0,1
 ],
 "edges": [],
 "strings": ["Widget","onClick","hello","HTMLDivElement"]
}`

func TestSummarizeHeap(t *testing.T) {
	t.Parallel()
	s, err := SummarizeHeap(strings.NewReader(tinySnapshot), 3)
	if err != nil {
		t.Fatal(err)
	}
	if s.Nodes != 6 || s.TotalSelfSize != 260 || s.DetachedDOMNodes != 1 {
		t.Fatalf("got %+v", s)
	}
	want := []HeapGroup{{"Widget", 2, 150}, {"(closure)", 1, 40}, {"HTMLDivElement", 2, 40}}
	if !slices.Equal(s.Top, want) {
		t.Fatalf("top = %+v", s.Top)
	}
	if _, err := SummarizeHeap(strings.NewReader(`{"snapshot":{"meta":{}}}`), 3); err == nil {
		t.Fatal("unknown format must fail")
	}
}

func TestSummarizeCPUProfile(t *testing.T) {
	t.Parallel()
	prof := `{"startTime":0,"endTime":100000,"samples":[1,2,2,3],"nodes":[
	 {"id":1,"hitCount":0,"callFrame":{"functionName":"(root)"}},
	 {"id":2,"hitCount":3,"callFrame":{"functionName":"render","url":"http://x/app.js","lineNumber":9}},
	 {"id":3,"hitCount":1,"callFrame":{"functionName":"","url":"http://x/lib.js","lineNumber":0}},
	 {"id":4,"hitCount":6,"callFrame":{"functionName":"(idle)"}}]}`
	got, err := SummarizeCPUProfile(strings.NewReader(prof), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"CPU profile: 100 ms", "30.0 ms  30.0%  render (http://x/app.js:10)", "10.0 ms  10.0%  (anonymous) (http://x/lib.js:1)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(idle)") {
		t.Error("idle time must be left out")
	}
}

func TestSummarizeCPUProfileFromTrace(t *testing.T) {
	t.Parallel()
	// agent-browser's profiler writes a trace: nodes and samples arrive in
	// ProfileChunk events, and each delta is the time since the previous sample.
	trace := `{"traceEvents":[
	 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
	 {"name":"AsyncTask","ph":"b","pid":1,"tid":7,"id":42},
	 {"name":"Flow","ph":"s","pid":1,"tid":7,"id2":{"local":"0x9"},"id":{"local":"0x9"}},
	 {"name":"thread_name","ph":"M","pid":1,"tid":50,"args":{"name":"v8:ProfEvntProc"}},
	 {"name":"Profile","ph":"P","id":"0x1","pid":1,"tid":7,"args":{"data":{"startTime":0}}},
	 {"name":"Profile","ph":"P","id":"0x9","pid":1,"tid":99,"args":{"data":{"startTime":0}}},
	 {"name":"ProfileChunk","ph":"P","id":"0x1","pid":1,"tid":50,"args":{"data":{
	   "cpuProfile":{"nodes":[{"id":1,"callFrame":{"functionName":"(root)"}},
	                          {"id":2,"callFrame":{"functionName":"render","url":"http://x/app.js","lineNumber":4}},
	                          {"id":3,"callFrame":{"functionName":"(idle)"}}],
	                 "samples":[2,2]},
	   "timeDeltas":[0,10000]}}},
	 {"name":"ProfileChunk","ph":"P","id":"0x1","pid":1,"tid":50,"args":{"data":{
	   "cpuProfile":{"nodes":[{"id":4,"callFrame":{"functionName":"","url":"http://x/lib.js","lineNumber":0}}],
	                 "samples":[3,4,2]},
	   "timeDeltas":[10000,30000,5000]}}},
	 {"name":"ProfileChunk","ph":"P","id":"0x9","pid":1,"tid":50,"args":{"data":{
	   "cpuProfile":{"nodes":[{"id":1,"callFrame":{"functionName":"workerOnly"}}],"samples":[1,1]},"timeDeltas":[0,50000]}}}]}`
	got, err := SummarizeCPUProfile(strings.NewReader(trace), 5)
	if err != nil {
		t.Fatal(err)
	}
	// render: 10ms within chunk 1 plus 10ms carried into chunk 2's first delta;
	// idle: 30ms (left out); lib.js: 5ms. Total wall time 55ms.
	for _, want := range []string{"CPU profile: 55 ms", "20.0 ms  36.4%  render (http://x/app.js:5)", "5.0 ms   9.1%  (anonymous) (http://x/lib.js:1)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "workerOnly") || strings.Contains(got, "(idle)") {
		t.Errorf("non-main-thread and idle samples must be left out:\n%s", got)
	}
}

func TestSummarizeTrace(t *testing.T) {
	t.Parallel()
	trace := `{"traceEvents":[
	 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
	 {"name":"thread_name","ph":"M","pid":1,"tid":8,"args":{"name":"Compositor"}},
	 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":1000000,"dur":120000},
	 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":1500000,"dur":30000},
	 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":2000000,"dur":80000},
	 {"name":"RunTask","ph":"X","pid":1,"tid":8,"ts":2000000,"dur":900000}]}`
	got, err := SummarizeTrace(strings.NewReader(trace), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 long tasks on the main thread, total blocking time 100 ms; longest task 120 ms of 3 tasks", "120 ms task at +0 ms", "80 ms task at +1000 ms"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Bare-array traces are accepted too.
	if _, err := SummarizeTrace(strings.NewReader(`[{"name":"RunTask","ph":"X","dur":1}]`), 5); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeTraceWithoutRunTask(t *testing.T) {
	t.Parallel()
	// agent-browser's trace only has the toplevel task event.
	trace := `{"traceEvents":[
	 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
	 {"name":"ThreadControllerImpl::RunTask","ph":"X","pid":1,"tid":7,"ts":0,"dur":30000},
	 {"name":"ThreadControllerImpl::RunTask","ph":"X","pid":1,"tid":7,"ts":100000,"dur":90000},
	 {"name":"ThreadController active","ph":"X","pid":1,"tid":7,"ts":0,"dur":900000}]}`
	got, err := SummarizeTrace(strings.NewReader(trace), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1 long tasks on the main thread, total blocking time 40 ms; longest task 90 ms of 2 tasks") {
		t.Fatalf("got %q", got)
	}
	if got, _ := SummarizeTrace(strings.NewReader(`{"traceEvents":[]}`), 5); got != "Trace: no main-thread task events recorded" {
		t.Fatalf("an empty trace must say so instead of reporting 0 long tasks, got %q", got)
	}
}

func TestSummarizeLighthouse(t *testing.T) {
	t.Parallel()
	report := map[string]any{
		"finalDisplayedUrl": "http://x/",
		"categories": map[string]any{
			"performance":   map[string]any{"title": "Performance", "score": 0.72, "auditRefs": []any{map[string]any{"id": "unused-javascript", "weight": 0}, map[string]any{"id": "largest-contentful-paint", "weight": 25}}},
			"accessibility": map[string]any{"title": "Accessibility", "score": 0.954, "auditRefs": []any{map[string]any{"id": "image-alt", "weight": 10}, map[string]any{"id": "color-contrast", "weight": 7}}},
		},
		"audits": map[string]any{
			"largest-contentful-paint": map[string]any{"title": "Largest Contentful Paint", "score": 0.4, "scoreDisplayMode": "numeric", "displayValue": "4.1 s"},
			"first-contentful-paint":   map[string]any{"title": "FCP", "score": 0.9, "scoreDisplayMode": "numeric", "displayValue": "1.2 s"},
			"unused-javascript":        map[string]any{"title": "Reduce unused JavaScript", "score": 0.5, "scoreDisplayMode": "metricSavings", "displayValue": "Potential savings of 120 KiB"},
			"image-alt":                map[string]any{"title": "Images have alt text", "score": 0, "scoreDisplayMode": "binary"},
			"color-contrast":           map[string]any{"title": "Contrast", "score": 1, "scoreDisplayMode": "binary"},
		},
	}
	data, _ := json.Marshal(report)
	got, err := SummarizeLighthouse(data, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := "Lighthouse http://x/\nPerformance 72 | Accessibility 95\nMetrics: FCP 1.2 s, LCP 4.1 s\nTop issues:\n" +
		"- Largest Contentful Paint — 4.1 s\n- Images have alt text\n- Reduce unused JavaScript — Potential savings of 120 KiB"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestLighthouseIssuesNameTheOffenders(t *testing.T) {
	t.Parallel()
	report := `{"finalDisplayedUrl":"http://x/","categories":{
	  "performance":{"title":"Performance","score":0.8,"auditRefs":[{"id":"render-blocking","weight":5}]},
	  "accessibility":{"title":"Accessibility","score":0.9,"auditRefs":[{"id":"link-name","weight":7}]}},
	 "audits":{
	  "render-blocking":{"title":"Render-blocking requests","score":0,"scoreDisplayMode":"metricSavings","displayValue":"Est savings of 1,510 ms",
	    "details":{"items":[
	      {"url":"https://use.typekit.net/kja6uqf.css","wastedMs":758.4},
	      {"url":"https://p.typekit.net/p.css?s=1&k=kja6uqf&ht=tk&f=24547.24548.38088.39110.39115&a=50872358","wastedMs":759},
	      {"url":"https://x/main.css","wastedMs":331,"wastedBytes":65536},
	      {"url":"https://x/fourth.css","wastedMs":10}]}},
	  "link-name":{"title":"Links do not have a discernible name","score":0,"scoreDisplayMode":"binary",
	    "details":{"items":[{"node":{"snippet":"<a href=\"mailto:hi@x.cx\">","nodeLabel":"a"}},"a stray string",{"items":[{"node":{"nodeLabel":"github link"}}]}]}},
	  "debug-data":{"title":"Diagnostics","score":null,"scoreDisplayMode":"informative","details":{"type":"debugdata","items":{"not":"a list"}}},
	  "odd-groups":{"title":"Odd","score":1,"scoreDisplayMode":"binary","details":{"items":[{"items":{"also":"an object"}}]}}}}`
	got, err := SummarizeLighthouse([]byte(report), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- Links do not have a discernible name\n    <a href=\"mailto:hi@x.cx\">\n    github link",
		"- Render-blocking requests — Est savings of 1,510 ms\n    https://use.typekit.net/kja6uqf.css (758 ms)",
		"    https://p.typekit.net/p.css?… (759 ms)",
		"    https://x/main.css (331 ms, 64 KiB)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fourth.css") {
		t.Errorf("each issue lists at most %d offenders:\n%s", maxItemsPerIssue, got)
	}
}

func TestLighthouseArgs(t *testing.T) {
	t.Parallel()
	o := LighthouseOptions{URL: "http://x/", Port: 9222, OutputPath: "/r.json", FormFactor: "desktop", Categories: []string{"performance", "seo"}}
	want := []string{"http://x/", "--port=9222", "--output=json", "--output-path=/r.json", "--quiet", "--only-categories=performance,seo", "--preset=desktop"}
	if got := o.args(); !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
	o = LighthouseOptions{URL: "http://x/", Port: 1, OutputPath: "/r.json", FormFactor: "mobile"}
	if got := o.args(); len(got) != 5 {
		t.Fatalf("mobile with all categories adds no flags, got %q", got)
	}
}

func TestThrottle(t *testing.T) {
	t.Parallel()
	p := NetworkProfiles["slow-3g"].networkParams()
	if p["latency"] != 2000.0 || p["downloadThroughput"] != 50000.0 || p["uploadThroughput"] != 50000.0 || p["offline"] != false {
		t.Fatalf("slow-3g params %v", p)
	}
	p = NetworkProfiles["none"].networkParams()
	if p["latency"] != 0.0 || p["downloadThroughput"] != -1.0 {
		t.Fatalf("none must disable limits, got %v", p)
	}
	rules := NetworkProfiles["slow-3g"].ruleParams()["matchedNetworkConditions"].([]any)
	if len(rules) != 1 || rules[0].(map[string]any)["urlPattern"] != "" || rules[0].(map[string]any)["latency"] != 2000.0 {
		t.Fatalf("slow-3g rule %v", rules)
	}
	if _, ok := rules[0].(map[string]any)["offline"]; ok {
		t.Fatal("offline is deprecated inside rules")
	}
	if rules := NetworkProfiles["none"].ruleParams()["matchedNetworkConditions"].([]any); len(rules) != 0 {
		t.Fatalf("none must clear the rules, got %v", rules)
	}
	if got := (Throttle{LatencyMs: 100, DownloadKbps: 800, CPUSlowdown: 4}).String(); got != "network latency 100ms, down 800kbps, up unlimited; CPU 4x slower" {
		t.Fatalf("got %q", got)
	}
	if got := (Throttle{}).String(); got != "network unthrottled; CPU unthrottled" {
		t.Fatalf("got %q", got)
	}
	if names := NetworkProfileNames(); !slices.Equal(names, []string{"fast-3g", "fast-4g", "none", "slow-3g", "slow-4g"}) {
		t.Fatalf("got %v", names)
	}
}

func TestRemoteObjectFull(t *testing.T) {
	t.Parallel()
	long := `"` + strings.Repeat("x", 300) + `"`
	obj := RemoteObject{Type: "string", Value: json.RawMessage(long)}
	if got := obj.String(); len(got) >= len(long) || !strings.HasSuffix(got, "…") {
		t.Fatalf("String is a preview and must shorten, got %d chars", len(got))
	}
	if got := obj.Full(); got != long {
		t.Fatalf("Full must keep the whole value, got %d chars", len(got))
	}
	fn := RemoteObject{Type: "function", Description: "function f() {\n}"}
	if fn.Full() != fn.String() {
		t.Fatal("non-primitive values render as String")
	}
}

func TestRemoteObjectString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		obj  RemoteObject
		want string
	}{
		{"number", RemoteObject{Type: "number", Value: json.RawMessage("42")}, "42"},
		{"string", RemoteObject{Type: "string", Value: json.RawMessage(`"hi"`)}, `"hi"`},
		{"undefined", RemoteObject{Type: "undefined"}, "undefined"},
		{"null", RemoteObject{Type: "object", Subtype: "null"}, "null"},
		{"NaN", RemoteObject{Type: "number", UnserializableValue: "NaN"}, "NaN"},
		{"function", RemoteObject{Type: "function", Description: "function onClick(e) {\n  go()\n}"}, "ƒ function onClick(e) {"},
		{"object preview", RemoteObject{Type: "object", ClassName: "Cart", Preview: &ObjectPreview{Properties: []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}{{"items", "number", "3"}, {"owner", "string", "ann"}}, Overflow: true}}, `Cart {items: 3, owner: "ann", …}`},
		{"function in a preview", RemoteObject{Type: "object", ClassName: "WebSocket", Preview: &ObjectPreview{Properties: []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}{{"readyState", "number", "1"}, {"onopen", "function", ""}}}}, "WebSocket {readyState: 1, onopen: ƒ}"},
		{"array preview", RemoteObject{Type: "object", Subtype: "array", Preview: &ObjectPreview{Subtype: "array", Description: "Array(2)", Properties: []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
		}{{"0", "number", "1"}, {"1", "number", "2"}}}}, "Array(2) [1, 2]"},
		{"plain object without preview", RemoteObject{Type: "object", Description: "HTMLButtonElement"}, "HTMLButtonElement"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.obj.String(); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestNumberLines(t *testing.T) {
	t.Parallel()
	src := "a\nb\nc\nd"
	if got := numberLines(src, 2, 3, 0); got != "     2  b\n     3  c" {
		t.Fatalf("got %q", got)
	}
	if got := numberLines(src, 0, 99, 0); strings.Count(got, "\n") != 3 {
		t.Fatalf("range must clamp to the file, got %q", got)
	}
	if got := markLine(numberLines(src, 1, 3, 0), 2); !strings.Contains(got, "►    2  b") {
		t.Fatalf("got %q", got)
	}
}

func TestFindAll(t *testing.T) {
	t.Parallel()
	src := "var a=1;var b=theme;\nfunction f(){return theme}"
	got := findAll(src, "theme", 10)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if l := got[0].Location; l.Line != 1 || l.Column != 15 {
		t.Fatalf("first match at %d:%d, want 1:15", l.Line, l.Column)
	}
	if l := got[1].Location; l.Line != 2 || l.Column != 21 {
		t.Fatalf("second match at %d:%d, want 2:21", l.Line, l.Column)
	}
	if !strings.Contains(got[0].Snippet, "b=theme;⏎function") {
		t.Fatalf("snippet %q", got[0].Snippet)
	}
	if got := findAll(src, "theme", 1); len(got) != 1 {
		t.Fatalf("limit ignored: %d matches", len(got))
	}
}

func TestSourceContext(t *testing.T) {
	t.Parallel()
	short := "a\nb\nc\nd\ne"
	if got := sourceContext(short, 3, 1, 1, 0); !strings.Contains(got, "►    3  c") || !strings.Contains(got, "     1  a") {
		t.Fatalf("short code shows numbered lines, got %q", got)
	}
	minified := strings.Repeat("x", 300) + "onClick:()=>void(toggle())" + strings.Repeat("y", 300)
	got := sourceContext("// header\n"+minified, 2, 313, 1, 0)
	if !strings.HasPrefix(got, "►    2:313  …") || !strings.Contains(got, "onClick:()=>▶void(toggle())") || !strings.HasSuffix(got, "…") {
		t.Fatalf("minified code shows a window around the column, got %q", got)
	}
	if sourceContext(short, 99, 1, 1, 0) != "" {
		t.Fatal("out-of-range line must render nothing")
	}
}

const heatmapTrace = `{"traceEvents":[
 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":0,"dur":60000},
 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":40000,"dur":60000},
 {"name":"FunctionCall","ph":"X","pid":1,"tid":7,"ts":0,"dur":50000},
 {"name":"EvaluateScript","ph":"X","pid":1,"tid":7,"ts":10000,"dur":20000},
 {"name":"Paint","ph":"X","pid":1,"tid":7,"ts":60000,"dur":10000},
 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":300000,"dur":20000},
 {"name":"UpdateLayoutTree","ph":"X","pid":1,"tid":7,"ts":300000,"dur":20000},
 {"name":"FunctionCall","ph":"X","pid":1,"tid":99,"ts":100000,"dur":90000},
 {"name":"Profile","ph":"P","id":"0x1","pid":1,"tid":7,"args":{"data":{}}},
 {"name":"ProfileChunk","ph":"P","id":"0x1","pid":1,"tid":50,"args":{"data":{
   "cpuProfile":{"nodes":[{"id":1,"callFrame":{"functionName":"render","url":"http://x/app.js"}},{"id":2,"callFrame":{"functionName":"track","url":"http://x/gtag.js"}}],
                 "samples":[1,1,2,1]},"timeDeltas":[0,30000,20000,5000]}}}]}`

func TestBuildHeatmap(t *testing.T) {
	t.Parallel()
	h, err := BuildHeatmap(strings.NewReader(heatmapTrace), 100)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, r := range h.Rows {
		var b strings.Builder
		for _, f := range r.Busy {
			b.WriteString(shade(f))
		}
		rows[r.Name] = b.String()
	}
	// Columns of 100 ms over 0-320 ms. Overlapping tasks 0-60 and 40-100 fill
	// column 0 once (not 120%); script events nest inside FunctionCall; the
	// worker-thread FunctionCall is ignored.
	want := map[string]string{"Busy": "█··░", "Script": "▓···", "Style": "···░", "Layout": "····", "Paint": "░···", "Parse": "····", "GC": "····"}
	for name, w := range want {
		if rows[name] != w {
			t.Errorf("%s = %q, want %q", name, rows[name], w)
		}
	}
	if h.Rows[0].Busy[0] != 1 || h.Rows[0].Busy[3] != 0.2 {
		t.Errorf("busy fractions %v", h.Rows[0].Busy)
	}
	// Each delta is the previous sample's time: app.js 30+20 ms, gtag.js 5 ms.
	if len(h.Files) != 2 || h.Files[0] != (FileCost{URL: "http://x/app.js", Ms: 50}) || h.Files[1] != (FileCost{URL: "http://x/gtag.js", Ms: 5}) {
		t.Errorf("files %+v", h.Files)
	}
	if h.Longest[0] != (LongTask{Ms: 60, AtMs: 0}) {
		t.Errorf("longest %+v", h.Longest)
	}
	text := h.Text()
	for _, w := range []string{"Main-thread heatmap: 400 ms in 100 ms columns", "Busy     █··░", "Script   ▓···", "50.0 ms  http://x/app.js", "Longest tasks: 60.0 ms at +0 ms"} {
		if !strings.Contains(text, w) {
			t.Errorf("text missing %q:\n%s", w, text)
		}
	}
	if !strings.Contains(h.JSON(), `"bucketMs":100,"rows":[{"name":"Busy","busy":[1,0,0,0.2]}`) {
		t.Errorf("json %s", h.JSON())
	}
}

func TestHeatmapPicksColumnWidth(t *testing.T) {
	t.Parallel()
	for span, want := range map[float64]float64{100: 10, 2400: 50, 6000: 100, 100000: 2000} {
		if got := pickBucket(span); got != want {
			t.Errorf("pickBucket(%v) = %v, want %v", span, got, want)
		}
	}
	h, err := BuildHeatmap(strings.NewReader(heatmapTrace), 0)
	if err != nil || h.BucketMs != 10 || len(h.Rows[0].Busy) != 32 {
		t.Fatalf("auto width: %v %v cols, %v", h.BucketMs, len(h.Rows[0].Busy), err)
	}
	if _, err := BuildHeatmap(strings.NewReader(`{"traceEvents":[]}`), 0); err == nil || !strings.Contains(err.Error(), "no main-thread activity") {
		t.Fatalf("got %v", err)
	}
}

// TestSourceContextForInlineScripts: an inline <script> starts partway down
// its HTML page, and pause positions count lines from the top of the page.
func TestSourceContextForInlineScripts(t *testing.T) {
	t.Parallel()
	src := "function later() {\n  return document.title;\n}\nlater();"
	got := sourceContext(src, 22, 3, 21, 8) // the script starts at page line 21
	for _, want := range []string{"    21  function later() {", "►   22    return document.title;", "    24  later();"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if got := numberLinesAt(src, 21, 23, 24, 0); got != "    23  }\n    24  later();" {
		t.Errorf("numberLinesAt: %q", got)
	}
	if sourceContext(src, 5, 1, 21, 0) != "" {
		t.Error("a line before the script has no context")
	}
}
