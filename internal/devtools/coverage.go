package devtools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

// ErrNoCoverage is returned when stopping coverage that was never started.
var ErrNoCoverage = errors.New("coverage is not recording; start it first")

// FileCoverage is how much of one script or stylesheet ran or applied.
type FileCoverage struct {
	URL   string
	Kind  string // "js" or "css"
	Total int
	Used  int
}

// Unused is the number of bytes that never ran or applied.
func (f FileCoverage) Unused() int { return f.Total - f.Used }

type styleSheet struct {
	url    string
	length int
}

type coverageRun struct {
	sheets      map[string]styleSheet
	unsubscribe func()
}

// StartCoverage begins recording which JS and CSS bytes are used. Interact
// with the page, then call StopCoverage.
func (p *Page) StartCoverage(ctx context.Context) error {
	run := &coverageRun{sheets: map[string]styleSheet{}}
	run.unsubscribe = p.conn.On("CSS.styleSheetAdded", func(ev cdp.Event) {
		if ev.SessionID != p.sessionID {
			return
		}
		var e struct {
			Header struct {
				StyleSheetID string  `json:"styleSheetId"`
				SourceURL    string  `json:"sourceURL"`
				Length       float64 `json:"length"`
			} `json:"header"`
		}
		if json.Unmarshal(ev.Params, &e) == nil {
			p.mu.Lock()
			run.sheets[e.Header.StyleSheetID] = styleSheet{url: e.Header.SourceURL, length: int(e.Header.Length)}
			p.mu.Unlock()
		}
	})
	for _, step := range []struct {
		method string
		params any
	}{
		{"Profiler.enable", nil},
		{"Profiler.startPreciseCoverage", map[string]any{"callCount": false, "detailed": true}},
		{"DOM.enable", nil},
		{"CSS.enable", nil},
		{"CSS.startRuleUsageTracking", nil},
	} {
		if err := p.call(ctx, step.method, step.params, nil); err != nil {
			run.unsubscribe()
			return err
		}
	}
	p.mu.Lock()
	p.coverage = run
	p.mu.Unlock()
	return nil
}

// StopCoverage ends recording and returns per-file usage, most unused first.
func (p *Page) StopCoverage(ctx context.Context) ([]FileCoverage, error) {
	p.mu.Lock()
	run := p.coverage
	p.coverage = nil
	p.mu.Unlock()
	if run == nil {
		return nil, ErrNoCoverage
	}
	defer run.unsubscribe()

	var js struct {
		Result []scriptCoverage `json:"result"`
	}
	if err := p.call(ctx, "Profiler.takePreciseCoverage", nil, &js); err != nil {
		return nil, err
	}
	var css struct {
		RuleUsage []ruleUsage `json:"ruleUsage"`
	}
	if err := p.call(ctx, "CSS.stopRuleUsageTracking", nil, &css); err != nil {
		return nil, err
	}
	for _, m := range []string{"Profiler.stopPreciseCoverage", "Profiler.disable", "CSS.disable", "DOM.disable"} {
		p.call(ctx, m, nil, nil)
	}

	p.mu.Lock()
	sheets := run.sheets
	p.mu.Unlock()
	files := append(jsCoverage(js.Result), cssCoverage(css.RuleUsage, sheets)...)
	slices.SortStableFunc(files, func(a, b FileCoverage) int { return cmp.Compare(b.Unused(), a.Unused()) })
	return files, nil
}

type coverageRange struct {
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
	Count       int `json:"count"`
}

type scriptCoverage struct {
	URL       string `json:"url"`
	Functions []struct {
		Ranges []coverageRange `json:"ranges"`
	} `json:"functions"`
}

type ruleUsage struct {
	StyleSheetID string  `json:"styleSheetId"`
	StartOffset  float64 `json:"startOffset"`
	EndOffset    float64 `json:"endOffset"`
	Used         bool    `json:"used"`
}

// jsCoverage totals used bytes per script URL. Inline scripts share the page
// URL and are summed; scripts without a URL (evals) are skipped.
func jsCoverage(scripts []scriptCoverage) []FileCoverage {
	byURL := map[string]*FileCoverage{}
	var order []string
	for _, s := range scripts {
		if s.URL == "" {
			continue
		}
		var ranges []coverageRange
		for _, f := range s.Functions {
			ranges = append(ranges, f.Ranges...)
		}
		total, used := scriptSize(ranges), usedBytes(ranges)
		f := byURL[s.URL]
		if f == nil {
			f = &FileCoverage{URL: s.URL, Kind: "js"}
			byURL[s.URL] = f
			order = append(order, s.URL)
		}
		f.Total += total
		f.Used += used
	}
	out := make([]FileCoverage, 0, len(order))
	for _, u := range order {
		out = append(out, *byURL[u])
	}
	return out
}

// scriptSize is the end of the outermost range, which spans the whole script.
func scriptSize(ranges []coverageRange) int {
	size := 0
	for _, r := range ranges {
		size = max(size, r.EndOffset)
	}
	return size
}

// usedBytes counts bytes whose innermost enclosing range ran at least once.
// V8 block coverage nests ranges: a function range with count 1 can contain a
// block with count 0 that never ran.
func usedBytes(ranges []coverageRange) int {
	type point struct {
		offset int
		start  bool
		r      coverageRange
	}
	points := make([]point, 0, 2*len(ranges))
	for _, r := range ranges {
		points = append(points, point{r.StartOffset, true, r}, point{r.EndOffset, false, r})
	}
	sort.SliceStable(points, func(i, j int) bool {
		a, b := points[i], points[j]
		if a.offset != b.offset {
			return a.offset < b.offset
		}
		if a.start != b.start {
			return !a.start // close ranges before opening new ones
		}
		// Ranges opening together: outer first, so the inner count is on top.
		// The order of ranges closing together does not change the count.
		return a.start && a.r.EndOffset-a.r.StartOffset > b.r.EndOffset-b.r.StartOffset
	})

	used, last := 0, 0
	var counts []int
	for _, p := range points {
		if len(counts) > 0 && counts[len(counts)-1] > 0 {
			used += p.offset - last
		}
		last = p.offset
		if p.start {
			counts = append(counts, p.r.Count)
		} else if len(counts) > 0 {
			counts = counts[:len(counts)-1]
		}
	}
	return used
}

// cssCoverage totals used rule bytes per stylesheet URL. Inline <style>
// sheets report the page URL and are summed with each other.
func cssCoverage(rules []ruleUsage, sheets map[string]styleSheet) []FileCoverage {
	used := map[string]int{}
	for _, r := range rules {
		if r.Used {
			used[r.StyleSheetID] += int(r.EndOffset - r.StartOffset)
		}
	}
	byURL := map[string]*FileCoverage{}
	var order []string
	ids := make([]string, 0, len(sheets))
	for id := range sheets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s := sheets[id]
		if s.url == "" || s.length == 0 {
			continue
		}
		f := byURL[s.url]
		if f == nil {
			f = &FileCoverage{URL: s.url, Kind: "css"}
			byURL[s.url] = f
			order = append(order, s.url)
		}
		f.Total += s.length
		f.Used += min(used[id], s.length)
	}
	out := make([]FileCoverage, 0, len(order))
	for _, u := range order {
		out = append(out, *byURL[u])
	}
	return out
}
