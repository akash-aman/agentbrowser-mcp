package devtools

import (
	"strings"
	"testing"
)

// TestCallTreeTotals: a function that spends its time in callees has little
// self time but a large total, which the Call Tree view shows; recursion is
// counted once per sample.
func TestCallTreeTotals(t *testing.T) {
	t.Parallel()
	// (root) → onScroll (self 0) → rankPosts (self 60) → sort (self 30, recursing into sort)
	prof := `{"startTime":0,"endTime":100000,"nodes":[
	 {"id":1,"hitCount":0,"callFrame":{"functionName":"(root)"},"children":[2,6]},
	 {"id":2,"hitCount":0,"callFrame":{"functionName":"onScroll","url":"http://x/feed.js","lineNumber":27},"children":[3]},
	 {"id":3,"hitCount":6,"callFrame":{"functionName":"rankPosts","url":"http://x/feed.js","lineNumber":22},"children":[4]},
	 {"id":4,"hitCount":2,"callFrame":{"functionName":"sort"},"children":[5]},
	 {"id":5,"hitCount":1,"callFrame":{"functionName":"sort"}},
	 {"id":6,"hitCount":1,"callFrame":{"functionName":"(idle)"}}]}`
	got, err := SummarizeCPUProfile(strings.NewReader(prof), 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"top functions by self time:\n     60.0 ms  60.0%  rankPosts (http://x/feed.js:23)\n     30.0 ms  30.0%  sort",
		"top by total time, including the functions they call:\n     90.0 ms  90.0%  onScroll (http://x/feed.js:28)\n     90.0 ms  90.0%  rankPosts (http://x/feed.js:23)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// sort recurses: its total is its 30 ms of samples, not 40.
	if strings.Contains(got, "sort\n") && strings.Contains(got, "40.0 ms") {
		t.Errorf("recursion counted twice:\n%s", got)
	}
}

// TestCallTreeFromTraceParents: trace ProfileChunk nodes name their parent.
func TestCallTreeFromTraceParents(t *testing.T) {
	t.Parallel()
	trace := `{"traceEvents":[
	 {"name":"thread_name","ph":"M","pid":1,"tid":7,"args":{"name":"CrRendererMain"}},
	 {"name":"Profile","ph":"P","id":"0x1","pid":1,"tid":7,"args":{"data":{"startTime":0}}},
	 {"name":"ProfileChunk","ph":"P","id":"0x1","pid":1,"tid":50,"args":{"data":{
	   "cpuProfile":{"nodes":[{"id":1,"callFrame":{"functionName":"(root)"}},
	                          {"id":2,"parent":1,"callFrame":{"functionName":"handler","url":"http://x/a.js","lineNumber":1}},
	                          {"id":3,"parent":2,"callFrame":{"functionName":"work","url":"http://x/a.js","lineNumber":9}}],
	                 "samples":[3,3,3]},
	   "timeDeltas":[0,10000,10000]}}}]}`
	got, err := SummarizeCPUProfile(strings.NewReader(trace), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "including the functions they call:\n     20.0 ms 100.0%  handler (http://x/a.js:2)") {
		t.Errorf("no total for the caller:\n%s", got)
	}
}
