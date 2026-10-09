package devtools

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

// Heatmap is main-thread activity over time: for each activity, the share of
// every time column it kept the main thread busy.
type Heatmap struct {
	BucketMs float64    `json:"bucketMs"`
	Rows     []HeatRow  `json:"rows"`
	Files    []FileCost `json:"scriptTimeByFile,omitempty"`
	Longest  []LongTask `json:"longestTasks"`
}

// HeatRow is one activity; Busy[i] is the fraction (0-1) of column i.
type HeatRow struct {
	Name string    `json:"name"`
	Busy []float64 `json:"busy"`
}

// FileCost is JavaScript self time attributed to one script URL.
type FileCost struct {
	URL string  `json:"url"`
	Ms  float64 `json:"ms"`
}

// LongTask is one main-thread task and when it started.
type LongTask struct {
	Ms   float64 `json:"ms"`
	AtMs float64 `json:"atMs"`
}

// activities maps trace event names to heatmap rows, in display order.
var activities = []struct {
	name   string
	events []string
}{
	{"Script", []string{"EvaluateScript", "v8.compile", "v8.compileModule", "v8.evaluateModule", "v8.run", "FunctionCall", "TimerFire", "EventDispatch", "FireAnimationFrame", "RunMicrotasks"}},
	{"Style", []string{"UpdateLayoutTree", "RecalculateStyles"}},
	{"Layout", []string{"Layout"}},
	{"Paint", []string{"PrePaint", "Paint", "PaintImage", "UpdateLayer", "Layerize", "Commit"}},
	{"Parse", []string{"ParseHTML", "ParseAuthorStyleSheet"}},
	{"GC", []string{"MajorGC", "MinorGC", "V8.GCScavenger", "V8.GCFinalizeMC", "BlinkGC.AtomicPhase"}},
}

// niceBuckets are the column widths the heatmap picks from.
var niceBuckets = []float64{10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 5000, 10000}

const maxColumns = 60

// BuildHeatmap reads a Chrome trace (agent-browser's trace or profiler
// output) and buckets main-thread activity into columns of bucketMs
// (0 = pick a width that gives at most 60 columns).
func BuildHeatmap(r io.Reader, bucketMs float64) (Heatmap, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Heatmap{}, err
	}
	events, err := traceEvents(data)
	if err != nil {
		return Heatmap{}, err
	}
	main := mainThreads(events)
	var onMain []traceEvent
	for _, e := range events {
		if e.Ph == "X" && main[[2]int{e.Pid, e.Tid}] {
			onMain = append(onMain, e)
		}
	}
	tasks := mainThreadTasks(events)
	if len(tasks) == 0 {
		return Heatmap{}, fmt.Errorf("the recording has no main-thread activity")
	}
	start, end := math.Inf(1), math.Inf(-1)
	for _, e := range tasks {
		start, end = min(start, e.Ts), max(end, e.Ts+e.Dur)
	}
	spanMs := (end - start) / 1000
	if bucketMs <= 0 {
		bucketMs = pickBucket(spanMs)
	}
	columns := int(math.Ceil(spanMs / bucketMs))
	h := Heatmap{BucketMs: bucketMs}
	h.Rows = append(h.Rows, HeatRow{Name: "Busy", Busy: coverage(tasks, start, bucketMs, columns)})
	for _, a := range activities {
		var evs []traceEvent
		for _, e := range onMain {
			if slices.Contains(a.events, e.Name) || (a.name == "GC" && strings.HasPrefix(e.Name, "V8.GC")) {
				evs = append(evs, e)
			}
		}
		h.Rows = append(h.Rows, HeatRow{Name: a.name, Busy: coverage(evs, start, bucketMs, columns)})
	}
	h.Files = scriptTimeByFile(events, 8)
	h.Longest = longestTasks(tasks, start, 5)
	return h, nil
}

func pickBucket(spanMs float64) float64 {
	for _, b := range niceBuckets {
		if spanMs/b <= maxColumns {
			return b
		}
	}
	return niceBuckets[len(niceBuckets)-1]
}

// coverage returns, per column, the fraction of time covered by the union of
// the events' intervals (nested or overlapping events are not double counted).
func coverage(events []traceEvent, start, bucketMs float64, columns int) []float64 {
	type span struct{ from, to float64 } // ms since start
	spans := make([]span, 0, len(events))
	for _, e := range events {
		spans = append(spans, span{(e.Ts - start) / 1000, (e.Ts + e.Dur - start) / 1000})
	}
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.from, b.from) })
	var merged []span
	for _, s := range spans {
		if n := len(merged); n > 0 && s.from <= merged[n-1].to {
			merged[n-1].to = max(merged[n-1].to, s.to)
			continue
		}
		merged = append(merged, s)
	}
	busy := make([]float64, columns)
	for _, s := range merged {
		for c := int(s.from / bucketMs); c < columns && float64(c)*bucketMs < s.to; c++ {
			lo, hi := max(s.from, float64(c)*bucketMs), min(s.to, float64(c+1)*bucketMs)
			if hi > lo {
				busy[c] += (hi - lo) / bucketMs
			}
		}
	}
	for i := range busy {
		busy[i] = math.Round(min(busy[i], 1)*100) / 100
	}
	return busy
}

// scriptTimeByFile sums CPU-sample self time per script URL, when the
// recording has samples (profiler recordings do; plain traces do not).
func scriptTimeByFile(events []traceEvent, top int) []FileCost {
	self, _ := traceSelfTimes(events)
	byURL := map[string]float64{}
	for f, ms := range self {
		if f.URL != "" {
			byURL[f.URL] += ms
		}
	}
	files := make([]FileCost, 0, len(byURL))
	for u, ms := range byURL {
		files = append(files, FileCost{URL: u, Ms: math.Round(ms*10) / 10})
	}
	slices.SortFunc(files, func(a, b FileCost) int { return cmp.Or(cmp.Compare(b.Ms, a.Ms), cmp.Compare(a.URL, b.URL)) })
	return files[:min(top, len(files))]
}

func longestTasks(tasks []traceEvent, start float64, top int) []LongTask {
	sorted := slices.Clone(tasks)
	slices.SortFunc(sorted, func(a, b traceEvent) int { return cmp.Compare(b.Dur, a.Dur) })
	out := make([]LongTask, 0, top)
	for _, t := range sorted[:min(top, len(sorted))] {
		out = append(out, LongTask{Ms: math.Round(t.Dur/100) / 10, AtMs: math.Round((t.Ts - start) / 1000)})
	}
	return out
}

// shade maps a busy fraction to a block character.
func shade(f float64) string {
	switch {
	case f <= 0:
		return "·"
	case f < 0.25:
		return "░"
	case f < 0.5:
		return "▒"
	case f < 0.75:
		return "▓"
	}
	return "█"
}

// Text renders the heatmap as rows of shaded blocks with a time axis.
func (h Heatmap) Text() string {
	columns := len(h.Rows[0].Busy)
	var b strings.Builder
	fmt.Fprintf(&b, "Main-thread heatmap: %.0f ms in %.0f ms columns; share of each column busy: · 0%%  ░ <25%%  ▒ <50%%  ▓ <75%%  █ ≥75%%\n",
		float64(columns)*h.BucketMs, h.BucketMs)
	b.WriteString(axis(columns, h.BucketMs) + "\n")
	for _, row := range h.Rows {
		fmt.Fprintf(&b, "%-8s ", row.Name)
		for _, f := range row.Busy {
			b.WriteString(shade(f))
		}
		b.WriteString("\n")
	}
	if len(h.Files) > 0 {
		b.WriteString("Script time by file (CPU samples):\n")
		for _, f := range h.Files {
			fmt.Fprintf(&b, "  %8.1f ms  %s\n", f.Ms, shortURL(f.URL))
		}
	}
	var longest []string
	for _, t := range h.Longest {
		longest = append(longest, fmt.Sprintf("%.1f ms at +%.0f ms", t.Ms, t.AtMs))
	}
	b.WriteString("Longest tasks: " + strings.Join(longest, ", "))
	return b.String()
}

// axis labels every tenth column with its start time.
func axis(columns int, bucketMs float64) string {
	line := []rune(strings.Repeat(" ", 9+columns+12))
	for c := 0; c < columns; c += 10 {
		label := fmt.Sprintf("%.0fms", float64(c)*bucketMs)
		copy(line[9+c:], []rune(label))
	}
	return strings.TrimRight(string(line), " ")
}

// JSON renders the heatmap as compact JSON for building visuals.
func (h Heatmap) JSON() string {
	b, _ := json.Marshal(h)
	return string(b)
}
