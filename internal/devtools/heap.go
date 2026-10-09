package devtools

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

// HeapSummary describes a heap snapshot without loading it in DevTools.
type HeapSummary struct {
	Path             string
	FileBytes        int64
	Nodes            int
	TotalSelfSize    int64
	DetachedDOMNodes int
	Top              []HeapGroup
}

// HeapGroup is the memory held by one constructor or object kind.
type HeapGroup struct {
	Name     string
	Count    int
	SelfSize int64
}

// HeapSnapshot writes a .heapsnapshot file (loadable in Chrome DevTools) to
// path and summarizes it.
func (p *Page) HeapSnapshot(ctx context.Context, path string, top int) (HeapSummary, error) {
	f, err := os.Create(path)
	if err != nil {
		return HeapSummary{}, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	var mu sync.Mutex
	var writeErr error
	unsubscribe := p.conn.On("HeapProfiler.addHeapSnapshotChunk", func(ev cdp.Event) {
		if ev.SessionID != p.sessionID {
			return
		}
		var e struct {
			Chunk string `json:"chunk"`
		}
		json.Unmarshal(ev.Params, &e)
		mu.Lock()
		defer mu.Unlock()
		if writeErr == nil {
			_, writeErr = w.WriteString(e.Chunk)
		}
	})
	defer unsubscribe()

	if err := p.call(ctx, "HeapProfiler.enable", nil, nil); err != nil {
		return HeapSummary{}, err
	}
	defer p.call(ctx, "HeapProfiler.disable", nil, nil)
	// Chunks arrive as events before this call returns.
	if err := p.call(ctx, "HeapProfiler.takeHeapSnapshot", map[string]any{"reportProgress": false}, nil); err != nil {
		return HeapSummary{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	if writeErr != nil {
		return HeapSummary{}, writeErr
	}
	if err := w.Flush(); err != nil {
		return HeapSummary{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return HeapSummary{}, err
	}
	summary, err := SummarizeHeap(f, top)
	if err != nil {
		return HeapSummary{}, fmt.Errorf("summarize %s: %w", path, err)
	}
	summary.Path = path
	if info, err := f.Stat(); err == nil {
		summary.FileBytes = info.Size()
	}
	return summary, nil
}

type heapFile struct {
	Snapshot struct {
		Meta struct {
			NodeFields []string          `json:"node_fields"`
			NodeTypes  []json.RawMessage `json:"node_types"`
		} `json:"meta"`
	} `json:"snapshot"`
	Nodes   []int64  `json:"nodes"`
	Strings []string `json:"strings"`
}

// detached is the "detachedness" value V8 gives DOM nodes no longer in the document.
const detached = 2

// SummarizeHeap groups a heap snapshot's nodes by constructor the way the
// DevTools Summary view does, and counts detached DOM nodes (a common leak).
func SummarizeHeap(r io.Reader, top int) (HeapSummary, error) {
	var h heapFile
	if err := json.NewDecoder(r).Decode(&h); err != nil {
		return HeapSummary{}, err
	}
	fields := h.Snapshot.Meta.NodeFields
	idx := func(name string) int { return slices.Index(fields, name) }
	typeIdx, nameIdx, sizeIdx, detIdx := idx("type"), idx("name"), idx("self_size"), idx("detachedness")
	if len(fields) == 0 || typeIdx < 0 || nameIdx < 0 || sizeIdx < 0 || len(h.Snapshot.Meta.NodeTypes) == 0 {
		return HeapSummary{}, fmt.Errorf("unrecognized heap snapshot format")
	}
	var typeNames []string
	if err := json.Unmarshal(h.Snapshot.Meta.NodeTypes[0], &typeNames); err != nil {
		return HeapSummary{}, fmt.Errorf("unrecognized node types: %w", err)
	}

	groups := map[string]*HeapGroup{}
	var s HeapSummary
	for i := 0; i+len(fields) <= len(h.Nodes); i += len(fields) {
		node := h.Nodes[i : i+len(fields)]
		name := groupName(at(typeNames, node[typeIdx]), at(h.Strings, node[nameIdx]))
		g := groups[name]
		if g == nil {
			g = &HeapGroup{Name: name}
			groups[name] = g
		}
		g.Count++
		g.SelfSize += node[sizeIdx]
		s.Nodes++
		s.TotalSelfSize += node[sizeIdx]
		if detIdx >= 0 && node[detIdx] == detached {
			s.DetachedDOMNodes++
		}
	}
	for _, g := range groups {
		s.Top = append(s.Top, *g)
	}
	slices.SortFunc(s.Top, func(a, b HeapGroup) int {
		return cmp.Or(cmp.Compare(b.SelfSize, a.SelfSize), cmp.Compare(a.Name, b.Name))
	})
	if top > 0 && len(s.Top) > top {
		s.Top = s.Top[:top]
	}
	return s, nil
}

func at(list []string, i int64) string {
	if i < 0 || int(i) >= len(list) {
		return ""
	}
	return list[i]
}

// groupName mirrors the DevTools Summary grouping: objects and native DOM
// objects by constructor name, everything else by kind.
func groupName(nodeType, name string) string {
	switch nodeType {
	case "object", "native":
		if name != "" {
			return name
		}
	case "closure":
		return "(closure)"
	case "string", "concatenated string", "sliced string":
		return "(string)"
	case "array":
		return "(array)"
	case "code":
		return "(compiled code)"
	case "hidden", "object shape":
		return "(system)"
	case "regexp":
		return "(regexp)"
	case "number", "bigint", "heap number":
		return "(number)"
	}
	return "(" + nodeType + ")"
}
