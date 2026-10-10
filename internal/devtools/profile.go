package devtools

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// SummarizeCPUProfile lists the functions with the most self time, like the
// DevTools Bottom-Up view. It reads both .cpuprofile files and Chrome traces
// with ProfileChunk events (what agent-browser's profiler writes).
func SummarizeCPUProfile(r io.Reader, top int) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	var prof cpuProfile
	if json.Unmarshal(data, &prof) == nil && len(prof.Nodes) > 0 {
		t := prof.tree()
		return selfTimeSummary(t, (prof.EndTime-prof.StartTime)/1000, top), nil
	}
	events, err := traceEvents(data)
	if err != nil {
		return "", fmt.Errorf("parse cpu profile: %w", err)
	}
	t, totalMs := traceSelfTimes(events)
	return selfTimeSummary(t, totalMs, top), nil
}

// nodeKey names a profile node; traces can hold several profiles.
type nodeKey struct {
	profile string
	id      int
}

// callTree is a profile's nodes with their self time, for self and total
// (with callees) times per function.
type callTree struct {
	frames map[nodeKey]callFrame
	parent map[nodeKey]nodeKey
	self   map[nodeKey]float64 // ms
}

func newCallTree() callTree {
	return callTree{frames: map[nodeKey]callFrame{}, parent: map[nodeKey]nodeKey{}, self: map[nodeKey]float64{}}
}

// selfByFunction sums self time per function.
func (t callTree) selfByFunction() map[callFrame]float64 {
	out := map[callFrame]float64{}
	for k, ms := range t.self {
		out[t.frames[k]] += ms
	}
	return out
}

// totalByFunction gives each function the self time of every sample with it
// on the stack, once per sample even when it recurses, like the total time in
// DevTools' Bottom-Up and Call Tree views.
func (t callTree) totalByFunction() map[callFrame]float64 {
	out := map[callFrame]float64{}
	for k, ms := range t.self {
		if ms <= 0 {
			continue
		}
		seen := map[callFrame]bool{}
		for n, depth := k, 0; depth < 1000; depth++ {
			if f := t.frames[n]; !seen[f] {
				seen[f] = true
				out[f] += ms
			}
			p, ok := t.parent[n]
			if !ok {
				break
			}
			n = p
		}
	}
	return out
}

type callFrame struct {
	FunctionName string `json:"functionName"`
	URL          string `json:"url"`
	LineNumber   int    `json:"lineNumber"`
}

type profileNode struct {
	ID        int       `json:"id"`
	HitCount  int       `json:"hitCount"`
	CallFrame callFrame `json:"callFrame"`
	Children  []int     `json:"children"` // .cpuprofile files
	Parent    int       `json:"parent"`   // trace ProfileChunk nodes
}

type cpuProfile struct {
	Nodes     []profileNode `json:"nodes"`
	StartTime float64       `json:"startTime"`
	EndTime   float64       `json:"endTime"`
}

// tree spreads the profile's duration over nodes by hit count.
func (p cpuProfile) tree() callTree {
	t := newCallTree()
	hits := 0
	for _, n := range p.Nodes {
		hits += n.HitCount
		t.frames[nodeKey{id: n.ID}] = n.CallFrame
		for _, c := range n.Children {
			t.parent[nodeKey{id: c}] = nodeKey{id: n.ID}
		}
	}
	if hits == 0 {
		return t
	}
	msPerHit := (p.EndTime - p.StartTime) / 1000 / float64(hits)
	for _, n := range p.Nodes {
		t.self[nodeKey{id: n.ID}] += float64(n.HitCount) * msPerHit
	}
	return t
}

// traceSelfTimes rebuilds self time from ProfileChunk events. Each time delta
// is the time since the previous sample, so it is that previous sample's self
// time, even when the previous sample was in an earlier chunk. Chunks are
// written by V8's profiler thread; the "Profile" event with the same id names
// the thread being profiled, and only renderer main threads are counted when
// the trace names them.
func traceSelfTimes(events []traceEvent) (callTree, float64) {
	type chunk struct {
		CPUProfile struct {
			Nodes   []profileNode `json:"nodes"`
			Samples []int         `json:"samples"`
		} `json:"cpuProfile"`
		TimeDeltas []float64 `json:"timeDeltas"`
	}
	main := mainThreads(events)
	profiled := map[string][2]int{} // profile id -> thread being profiled
	for _, e := range events {
		if e.Name == "Profile" {
			profiled[string(e.ID)] = [2]int{e.Pid, e.Tid}
		}
	}
	t := newCallTree()
	last := map[string]int{} // profile id -> node of the previous sample
	totalUs := 0.0
	for _, e := range events {
		if e.Name != "ProfileChunk" {
			continue
		}
		profile := string(e.ID)
		thread, ok := profiled[profile]
		if !ok {
			thread = [2]int{e.Pid, e.Tid}
		}
		if len(main) > 0 && !main[thread] {
			continue
		}
		var args struct {
			Data chunk `json:"data"`
		}
		if json.Unmarshal(e.Args, &args) != nil {
			continue
		}
		for _, n := range args.Data.CPUProfile.Nodes {
			t.frames[nodeKey{profile, n.ID}] = n.CallFrame
			if n.Parent != 0 {
				t.parent[nodeKey{profile, n.ID}] = nodeKey{profile, n.Parent}
			}
		}
		deltas := args.Data.TimeDeltas
		for i, id := range args.Data.CPUProfile.Samples {
			if prev, ok := last[profile]; ok && i < len(deltas) {
				t.self[nodeKey{profile, prev}] += deltas[i] / 1000
				totalUs += deltas[i]
			}
			last[profile] = id
		}
	}
	return t, totalUs / 1000
}

// selfTimeSummary renders the top functions by self time, leaving out idle,
// then the top by total time (with the functions they call) where that
// tells something self time does not.
func selfTimeSummary(t callTree, totalMs float64, top int) string {
	self := ranked(t.selfByFunction())
	if len(self) == 0 || totalMs <= 0 {
		return fmt.Sprintf("CPU profile: %.0f ms, no samples", totalMs)
	}
	lines := []string{fmt.Sprintf("CPU profile: %.0f ms; top functions by self time:", totalMs)}
	selfMs := map[string]float64{}
	for _, f := range self {
		selfMs[f.name] = f.ms
	}
	for _, f := range self[:min(top, len(self))] {
		lines = append(lines, fmt.Sprintf("  %7.1f ms %5.1f%%  %s", f.ms, 100*f.ms/totalMs, f.name))
	}
	var callers []string
	for _, f := range ranked(t.totalByFunction()) {
		if len(callers) == top {
			break
		}
		if f.ms > selfMs[f.name]+0.05 { // spends time in callees
			callers = append(callers, fmt.Sprintf("  %7.1f ms %5.1f%%  %s", f.ms, 100*f.ms/totalMs, f.name))
		}
	}
	if len(callers) > 0 {
		lines = append(lines, "top by total time, including the functions they call:")
		lines = append(lines, callers...)
	}
	return strings.Join(lines, "\n")
}

type rankedFn struct {
	name string
	ms   float64
}

// ranked names functions as DevTools does and sorts them by time, leaving
// out idle time and the synthetic root.
func ranked(times map[callFrame]float64) []rankedFn {
	byName := map[string]float64{}
	for f, ms := range times {
		name := f.FunctionName
		if name == "(idle)" || name == "(root)" || name == "(program)" || ms <= 0 {
			continue
		}
		if name == "" {
			name = "(anonymous)"
		}
		if f.URL != "" {
			name += fmt.Sprintf(" (%s:%d)", f.URL, f.LineNumber+1)
		}
		byName[name] += ms
	}
	out := make([]rankedFn, 0, len(byName))
	for name, ms := range byName {
		out = append(out, rankedFn{name, ms})
	}
	slices.SortFunc(out, func(a, b rankedFn) int { return cmp.Or(cmp.Compare(b.ms, a.ms), cmp.Compare(a.name, b.name)) })
	return out
}

type traceEvent struct {
	Name string          `json:"name"`
	ID   json.RawMessage `json:"id"` // a string, number or object depending on the event
	Ph   string          `json:"ph"`
	Ts   float64         `json:"ts"`
	Dur  float64         `json:"dur"`
	Pid  int             `json:"pid"`
	Tid  int             `json:"tid"`
	Args json.RawMessage `json:"args"`
}

const longTaskUs = 50_000

// taskEvents are the per-task trace events, best first: DevTools timeline
// traces have RunTask, while agent-browser's trace has only the toplevel
// ThreadControllerImpl::RunTask (both exist in some traces, nested).
var taskEvents = []string{"RunTask", "ThreadControllerImpl::RunTask"}

// SummarizeTrace reports long tasks (over 50 ms) on the page's main thread,
// the total blocking time they cause, and the longest task.
func SummarizeTrace(r io.Reader, top int) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	events, err := traceEvents(data)
	if err != nil {
		return "", err
	}
	tasks := mainThreadTasks(events)
	if len(tasks) == 0 {
		return "Trace: no main-thread task events recorded", nil
	}
	start := tasks[0].Ts
	for _, e := range tasks {
		start = min(start, e.Ts)
	}
	slices.SortFunc(tasks, func(a, b traceEvent) int { return cmp.Compare(b.Dur, a.Dur) })
	var long []traceEvent
	tbt := 0.0
	for _, e := range tasks {
		if e.Dur >= longTaskUs {
			long = append(long, e)
			tbt += e.Dur - longTaskUs
		}
	}
	lines := []string{fmt.Sprintf("Trace: %d long tasks on the main thread, total blocking time %.0f ms; longest task %.0f ms of %d tasks",
		len(long), tbt/1000, tasks[0].Dur/1000, len(tasks))}
	for _, e := range long[:min(top, len(long))] {
		lines = append(lines, fmt.Sprintf("  %6.0f ms task at +%.0f ms", e.Dur/1000, (e.Ts-start)/1000))
	}
	if insights := traceInsights(events); len(insights) > 0 {
		lines = append(lines, "Insights:")
		lines = append(lines, insights...)
	}
	return strings.Join(lines, "\n"), nil
}

// traceInsights reads what the Performance panel's Insights sidebar shows
// for a page load in the trace: paint timings and the LCP element, layout
// shifts, and render-blocking requests.
func traceInsights(events []traceEvent) []string {
	type data struct {
		Data struct {
			NavigationID         string  `json:"navigationId"`
			IsOutermostMainFrame bool    `json:"isOutermostMainFrame"`
			CandidateIndex       int     `json:"candidateIndex"`
			NodeName             string  `json:"nodeName"`
			Size                 float64 `json:"size"`
			Type                 string  `json:"type"`
			Score                float64 `json:"score"`
			HadRecentInput       bool    `json:"had_recent_input"`
			IsMainFrame          bool    `json:"is_main_frame"`
			MaxDistance          float64 `json:"frame_max_distance"`
			ImpactedNodes        []any   `json:"impacted_nodes"`
			URL                  string  `json:"url"`
			ResourceType         string  `json:"resourceType"`
			RenderBlocking       string  `json:"renderBlocking"`
			RequestID            string  `json:"requestId"`
		} `json:"data"`
	}
	parse := func(e traceEvent) data {
		var d data
		json.Unmarshal(e.Args, &d)
		return d
	}
	navStart := map[string]float64{}
	finished := map[string]float64{}
	for _, e := range events {
		switch e.Name {
		case "navigationStart":
			if d := parse(e); d.Data.IsOutermostMainFrame && d.Data.NavigationID != "" {
				navStart[d.Data.NavigationID] = e.Ts
			}
		case "ResourceFinish":
			finished[parse(e).Data.RequestID] = e.Ts
		}
	}
	var out []string
	since := func(id string, ts float64) (float64, bool) {
		start, ok := navStart[id]
		return (ts - start) / 1000, ok
	}
	// The paints of the last load in the trace, each timed from its own
	// navigation.
	var fcp, lcp *traceEvent
	for i, e := range events {
		switch e.Name {
		case "firstContentfulPaint":
			if _, ok := navStart[parse(e).Data.NavigationID]; ok && (fcp == nil || e.Ts > fcp.Ts) {
				fcp = &events[i]
			}
		case "largestContentfulPaint::Candidate":
			d := parse(e)
			if _, ok := navStart[d.Data.NavigationID]; ok && d.Data.IsOutermostMainFrame && (lcp == nil || e.Ts > lcp.Ts) {
				lcp = &events[i]
			}
		}
	}
	if fcp != nil {
		ms, _ := since(parse(*fcp).Data.NavigationID, fcp.Ts)
		out = append(out, fmt.Sprintf("  FCP %.0f ms after navigation", ms))
	}
	if lcp != nil {
		d := parse(*lcp)
		ms, _ := since(d.Data.NavigationID, lcp.Ts)
		article := "a"
		if strings.ContainsAny(d.Data.Type[:min(1, len(d.Data.Type))], "aeiou") {
			article = "an"
		}
		out = append(out, fmt.Sprintf("  LCP %.0f ms: %s %s element <%s> of %.0f px²", ms, article, d.Data.Type, strings.ToLower(d.Data.NodeName), d.Data.Size))
	}
	lastNav := ""
	if lcp != nil {
		lastNav = parse(*lcp).Data.NavigationID
	}
	cls, n := 0.0, 0
	var worst traceEvent
	worstScore := 0.0
	for _, e := range events {
		if e.Name != "LayoutShift" {
			continue
		}
		d := parse(e)
		if d.Data.HadRecentInput || !d.Data.IsMainFrame {
			continue
		}
		cls += d.Data.Score
		n++
		if d.Data.Score > worstScore {
			worst, worstScore = e, d.Data.Score
		}
	}
	if n > 0 {
		line := fmt.Sprintf("  CLS %.3f from %d layout %s", cls, n, map[bool]string{true: "shift", false: "shifts"}[n == 1])
		if worstScore > 0 {
			d := parse(worst)
			at := ""
			if ms, ok := since(lastNav, worst.Ts); ok {
				at = fmt.Sprintf(" at %.0f ms", ms)
			}
			line += fmt.Sprintf("; the largest (%.3f%s) moved %d elements by up to %.0f px", worstScore, at, len(d.Data.ImpactedNodes), d.Data.MaxDistance)
		}
		out = append(out, line)
	}
	var blocking []string
	for _, e := range events {
		if e.Name != "ResourceSendRequest" {
			continue
		}
		d := parse(e)
		kind := map[string]string{"blocking": "render-blocking", "in_body_parser_blocking": "parser-blocking"}[d.Data.RenderBlocking]
		if kind == "" {
			continue
		}
		item := fmt.Sprintf("%s (%s, %s", d.Data.URL, d.Data.ResourceType, kind)
		if end, ok := finished[d.Data.RequestID]; ok && end-e.Ts >= 1000 {
			item += fmt.Sprintf(", %.0f ms", (end-e.Ts)/1000)
		}
		blocking = append(blocking, item+")")
	}
	if len(blocking) > 0 {
		out = append(out, "  requests that hold up the first render:")
		for _, b := range blocking[:min(8, len(blocking))] {
			out = append(out, "    "+b)
		}
	}
	return out
}

// mainThreadTasks returns the task events on renderer main threads, using
// the first task event name the trace contains.
func mainThreadTasks(events []traceEvent) []traceEvent {
	main := mainThreads(events)
	for _, name := range taskEvents {
		var tasks []traceEvent
		for _, e := range events {
			if e.Ph == "X" && e.Name == name && main[[2]int{e.Pid, e.Tid}] {
				tasks = append(tasks, e)
			}
		}
		if len(tasks) > 0 {
			return tasks
		}
	}
	return nil
}

// traceEvents accepts both trace formats: {"traceEvents": [...]} and a bare array.
func traceEvents(data []byte) ([]traceEvent, error) {
	var wrapped struct {
		TraceEvents []traceEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.TraceEvents != nil {
		return wrapped.TraceEvents, nil
	}
	var bare []traceEvent
	if err := json.Unmarshal(data, &bare); err != nil {
		return nil, fmt.Errorf("parse trace: %w", err)
	}
	return bare, nil
}

// mainThreads finds renderer main threads from thread_name metadata.
func mainThreads(events []traceEvent) map[[2]int]bool {
	main := map[[2]int]bool{}
	for _, e := range events {
		if e.Ph != "M" || e.Name != "thread_name" {
			continue
		}
		var args struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(e.Args, &args) == nil && args.Name == "CrRendererMain" {
			main[[2]int{e.Pid, e.Tid}] = true
		}
	}
	return main
}
