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
		return selfTimeSummary(prof.selfTimes(), (prof.EndTime-prof.StartTime)/1000, top), nil
	}
	events, err := traceEvents(data)
	if err != nil {
		return "", fmt.Errorf("parse cpu profile: %w", err)
	}
	self, totalMs := traceSelfTimes(events)
	return selfTimeSummary(self, totalMs, top), nil
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
}

type cpuProfile struct {
	Nodes     []profileNode `json:"nodes"`
	StartTime float64       `json:"startTime"`
	EndTime   float64       `json:"endTime"`
}

// selfTimes spreads the profile's duration over nodes by hit count.
func (p cpuProfile) selfTimes() map[callFrame]float64 {
	hits := 0
	for _, n := range p.Nodes {
		hits += n.HitCount
	}
	self := map[callFrame]float64{}
	if hits == 0 {
		return self
	}
	msPerHit := (p.EndTime - p.StartTime) / 1000 / float64(hits)
	for _, n := range p.Nodes {
		self[n.CallFrame] += float64(n.HitCount) * msPerHit
	}
	return self
}

// traceSelfTimes rebuilds self time from ProfileChunk events. Each time delta
// is the time since the previous sample, so it is that previous sample's self
// time, even when the previous sample was in an earlier chunk. Chunks are
// written by V8's profiler thread; the "Profile" event with the same id names
// the thread being profiled, and only renderer main threads are counted when
// the trace names them.
func traceSelfTimes(events []traceEvent) (map[callFrame]float64, float64) {
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
	frames := map[string]map[int]callFrame{} // profile id -> node id -> frame
	last := map[string]int{}                 // profile id -> node of the previous sample
	self := map[callFrame]float64{}
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
		nodes := frames[profile]
		if nodes == nil {
			nodes = map[int]callFrame{}
			frames[profile] = nodes
		}
		for _, n := range args.Data.CPUProfile.Nodes {
			nodes[n.ID] = n.CallFrame
		}
		deltas := args.Data.TimeDeltas
		for i, id := range args.Data.CPUProfile.Samples {
			if prev, ok := last[profile]; ok && i < len(deltas) {
				self[nodes[prev]] += deltas[i] / 1000
				totalUs += deltas[i]
			}
			last[profile] = id
		}
	}
	return self, totalUs / 1000
}

// selfTimeSummary renders the top functions by self time, leaving out idle.
func selfTimeSummary(self map[callFrame]float64, totalMs float64, top int) string {
	type fn struct {
		name string
		ms   float64
	}
	byName := map[string]*fn{}
	for f, ms := range self {
		name := f.FunctionName
		if name == "(idle)" || name == "(root)" || ms <= 0 {
			continue
		}
		if name == "" {
			name = "(anonymous)"
		}
		if f.URL != "" {
			name += fmt.Sprintf(" (%s:%d)", f.URL, f.LineNumber+1)
		}
		if byName[name] == nil {
			byName[name] = &fn{name: name}
		}
		byName[name].ms += ms
	}
	if len(byName) == 0 || totalMs <= 0 {
		return fmt.Sprintf("CPU profile: %.0f ms, no samples", totalMs)
	}
	fns := make([]fn, 0, len(byName))
	for _, f := range byName {
		fns = append(fns, *f)
	}
	slices.SortFunc(fns, func(a, b fn) int { return cmp.Or(cmp.Compare(b.ms, a.ms), cmp.Compare(a.name, b.name)) })
	lines := []string{fmt.Sprintf("CPU profile: %.0f ms; top functions by self time:", totalMs)}
	for _, f := range fns[:min(top, len(fns))] {
		lines = append(lines, fmt.Sprintf("  %7.1f ms %5.1f%%  %s", f.ms, 100*f.ms/totalMs, f.name))
	}
	return strings.Join(lines, "\n")
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
	return strings.Join(lines, "\n"), nil
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
