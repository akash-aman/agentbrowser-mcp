package devtools

import (
	"strings"
	"testing"
)

// TestTraceInsights uses the event shapes Chrome 149 writes (taken from a
// real trace of docs/demo/site/feed.html).
func TestTraceInsights(t *testing.T) {
	t.Parallel()
	trace := `{"traceEvents":[
	 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
	 {"name":"RunTask","ph":"X","pid":1,"tid":7,"ts":1000000,"dur":120000},
	 {"name":"navigationStart","ph":"R","ts":1000000,"args":{"data":{"isOutermostMainFrame":true,"navigationId":"N1"}}},
	 {"name":"navigationStart","ph":"R","ts":900000,"args":{"data":{"isOutermostMainFrame":false,"navigationId":"F1"}}},
	 {"name":"ResourceSendRequest","ph":"I","ts":1001000,"args":{"data":{"requestId":"r1","url":"http://x/style.css","resourceType":"Stylesheet","renderBlocking":"blocking"}}},
	 {"name":"ResourceFinish","ph":"I","ts":1251000,"args":{"data":{"requestId":"r1"}}},
	 {"name":"ResourceSendRequest","ph":"I","ts":1002000,"args":{"data":{"requestId":"r2","url":"http://x/img.png","resourceType":"Image","renderBlocking":"non_blocking"}}},
	 {"name":"firstContentfulPaint","ph":"R","ts":1300000,"args":{"data":{"navigationId":"N1"}}},
	 {"name":"largestContentfulPaint::Candidate","ph":"R","ts":1300000,"args":{"data":{"candidateIndex":1,"isOutermostMainFrame":true,"navigationId":"N1","nodeName":"P","size":900,"type":"text"}}},
	 {"name":"largestContentfulPaint::Candidate","ph":"R","ts":2100000,"args":{"data":{"candidateIndex":2,"isOutermostMainFrame":true,"navigationId":"N1","nodeName":"IMG","size":120000,"type":"image"}}},
	 {"name":"LayoutShift","ph":"I","ts":1900000,"args":{"data":{"score":0.2,"had_recent_input":false,"is_main_frame":true,"frame_max_distance":208,"impacted_nodes":[{},{},{}]}}},
	 {"name":"LayoutShift","ph":"I","ts":2500000,"args":{"data":{"score":0.05,"had_recent_input":false,"is_main_frame":true,"frame_max_distance":20,"impacted_nodes":[{}]}}},
	 {"name":"LayoutShift","ph":"I","ts":2600000,"args":{"data":{"score":0.5,"had_recent_input":true,"is_main_frame":true}}}]}`
	got, err := SummarizeTrace(strings.NewReader(trace), 5)
	if err != nil {
		t.Fatal(err)
	}
	want := "Insights:\n" +
		"  FCP 300 ms after navigation\n" +
		"  LCP 1100 ms: an image element <img> of 120000 px²\n" +
		"  CLS 0.250 from 2 layout shifts; the largest (0.200 at 900 ms) moved 3 elements by up to 208 px\n" +
		"  requests that hold up the first render:\n" +
		"    http://x/style.css (Stylesheet, render-blocking, 250 ms)"
	if !strings.Contains(got, want) {
		t.Errorf("got:\n%s\nwant it to contain:\n%s", got, want)
	}
}
