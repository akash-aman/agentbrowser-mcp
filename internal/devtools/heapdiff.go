package devtools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// HeapDiff compares two heap snapshots by constructor, like DevTools'
// Comparison view: what grew between them is where a leak lives.
func HeapDiff(before, after string, top int) (string, error) {
	a, err := summarizeFile(before)
	if err != nil {
		return "", err
	}
	b, err := summarizeFile(after)
	if err != nil {
		return "", err
	}
	type delta struct {
		name        string
		count, size int64
	}
	byName := map[string]*delta{}
	for _, g := range a.Top {
		byName[g.Name] = &delta{name: g.Name, count: -int64(g.Count), size: -g.SelfSize}
	}
	for _, g := range b.Top {
		d := byName[g.Name]
		if d == nil {
			d = &delta{name: g.Name}
			byName[g.Name] = d
		}
		d.count += int64(g.Count)
		d.size += g.SelfSize
	}
	var grew []delta
	for _, d := range byName {
		if d.size > 0 || d.count > 0 {
			grew = append(grew, *d)
		}
	}
	slices.SortFunc(grew, func(x, y delta) int {
		return cmp.Or(cmp.Compare(y.size, x.size), cmp.Compare(y.count, x.count), cmp.Compare(x.name, y.name))
	})
	verb := "grew by"
	switch change := b.TotalSelfSize - a.TotalSelfSize; {
	case change < 0:
		verb = "shrank by"
	case change == 0 && b.Nodes == a.Nodes:
		verb = "is unchanged:"
	}
	lines := []string{fmt.Sprintf("Heap %s %s (%+d objects) from %s to %s; detached DOM nodes %d → %d",
		verb, signedKB(b.TotalSelfSize-a.TotalSelfSize), b.Nodes-a.Nodes, before, after, a.DetachedDOMNodes, b.DetachedDOMNodes)}
	if len(grew) == 0 {
		return lines[0] + "\nnothing grew", nil
	}
	lines = append(lines, "grew most:")
	for _, d := range grew[:min(top, len(grew))] {
		lines = append(lines, fmt.Sprintf("  %10s %+7d  %s", signedKB(d.size), d.count, d.name))
	}
	lines = append(lines, "retainers on the after snapshot shows what holds a group, e.g. the first one")
	return strings.Join(lines, "\n"), nil
}

func summarizeFile(path string) (HeapSummary, error) {
	f, err := os.Open(path)
	if err != nil {
		return HeapSummary{}, err
	}
	defer f.Close()
	s, err := SummarizeHeap(f, 0)
	if err != nil {
		return HeapSummary{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func signedKB(bytes int64) string {
	sign := "+"
	if bytes < 0 {
		sign, bytes = "-", -bytes
	}
	return sign + kb(float64(bytes))
}

// heapGraph is a heap snapshot with its edges, enough to walk retainers.
type heapGraph struct {
	nodeFields, edgeFields []string
	nodeTypes, edgeTypes   []string
	nodes, edges           []int64
	strings                []string
	firstEdge              []int // per node, index of its first edge in edges
}

func loadHeapGraph(r io.Reader) (*heapGraph, error) {
	var raw struct {
		Snapshot struct {
			Meta struct {
				NodeFields []string          `json:"node_fields"`
				NodeTypes  []json.RawMessage `json:"node_types"`
				EdgeFields []string          `json:"edge_fields"`
				EdgeTypes  []json.RawMessage `json:"edge_types"`
			} `json:"meta"`
		} `json:"snapshot"`
		Nodes   []int64  `json:"nodes"`
		Edges   []int64  `json:"edges"`
		Strings []string `json:"strings"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	m := raw.Snapshot.Meta
	g := &heapGraph{nodeFields: m.NodeFields, edgeFields: m.EdgeFields, nodes: raw.Nodes, edges: raw.Edges, strings: raw.Strings}
	if len(m.NodeTypes) == 0 || len(m.EdgeTypes) == 0 || g.field("node", "edge_count") < 0 || g.field("edge", "to_node") < 0 {
		return nil, fmt.Errorf("heap snapshot without edges; take it with performance heap_snapshot")
	}
	if json.Unmarshal(m.NodeTypes[0], &g.nodeTypes) != nil || json.Unmarshal(m.EdgeTypes[0], &g.edgeTypes) != nil {
		return nil, fmt.Errorf("unrecognized heap snapshot types")
	}
	n, edgeCount := len(g.nodeFields), g.field("node", "edge_count")
	g.firstEdge = make([]int, len(g.nodes)/n+1)
	e := 0
	for i := 0; i*n < len(g.nodes); i++ {
		g.firstEdge[i] = e
		e += int(g.nodes[i*n+edgeCount]) * len(g.edgeFields)
	}
	g.firstEdge[len(g.firstEdge)-1] = e
	return g, nil
}

func (g *heapGraph) field(kind, name string) int {
	if kind == "node" {
		return slices.Index(g.nodeFields, name)
	}
	return slices.Index(g.edgeFields, name)
}

func (g *heapGraph) count() int { return len(g.nodes) / len(g.nodeFields) }

func (g *heapGraph) nodeValue(i int, field string) int64 {
	return g.nodes[i*len(g.nodeFields)+g.field("node", field)]
}

func (g *heapGraph) nodeName(i int) string {
	return groupName(at(g.nodeTypes, g.nodeValue(i, "type")), at(g.strings, g.nodeValue(i, "name")))
}

// ownSize is an object's size with the backing stores only it holds: an
// Array's own self size is a few bytes, its items live in "(array)"
// elements, so self size alone ranked a 2000-item array with the empty ones.
func (g *heapGraph) ownSize(i int) int64 {
	size := g.nodeValue(i, "self_size")
	typeField, nameField, toNode := g.field("edge", "type"), g.field("edge", "name_or_index"), g.field("edge", "to_node")
	for e := g.firstEdge[i]; e < g.firstEdge[i+1]; e += len(g.edgeFields) {
		if at(g.edgeTypes, g.edges[e+typeField]) != "internal" {
			continue
		}
		if name := at(g.strings, g.edges[e+nameField]); name == "elements" || name == "properties" || name == "table" {
			size += g.nodeValue(int(g.edges[e+toNode])/len(g.nodeFields), "self_size")
		}
	}
	return size
}

// edgeLabel names an edge as DevTools' Retainers view does.
func (g *heapGraph) edgeLabel(e int) string {
	kind := at(g.edgeTypes, g.edges[e+g.field("edge", "type")])
	v := g.edges[e+g.field("edge", "name_or_index")]
	switch kind {
	case "element", "hidden":
		return fmt.Sprintf("[%d]", v)
	case "context":
		return "(context) " + at(g.strings, v)
	case "internal", "shortcut":
		return "(" + at(g.strings, v) + ")"
	}
	return "." + at(g.strings, v)
}

// Retainers explains why objects of a constructor stay alive: for the
// largest few, the shortest chain of references from a GC root, like the
// Retainers pane in DevTools' Memory panel.
func Retainers(path, constructor string, limit int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	g, err := loadHeapGraph(f)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	// Reverse the graph, leaving out weak references, which do not retain.
	type ref struct{ from, edge int }
	retainers := make([][]ref, g.count())
	toNode, typeField := g.field("edge", "to_node"), g.field("edge", "type")
	for i := 0; i < g.count(); i++ {
		for e := g.firstEdge[i]; e < g.firstEdge[i+1]; e += len(g.edgeFields) {
			if at(g.edgeTypes, g.edges[e+typeField]) == "weak" {
				continue
			}
			to := int(g.edges[e+toNode]) / len(g.nodeFields)
			retainers[to] = append(retainers[to], ref{i, e})
		}
	}
	var targets []int
	for i := 0; i < g.count(); i++ {
		if g.nodeName(i) == constructor {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("no %s objects in %s", constructor, path)
	}
	slices.SortFunc(targets, func(a, b int) int { return cmp.Compare(g.ownSize(b), g.ownSize(a)) })
	lines := []string{fmt.Sprintf("%s in %s; how the largest are retained (root first):", plural(len(targets), constructor+" object"), path)}
	for _, t := range targets[:min(limit, len(targets))] {
		// Breadth-first from the object back toward node 0, the synthetic root.
		// Breadth-first toward a page's global object, or failing that the
		// synthetic root: the shortest path to the root often runs through
		// Chrome internals ("Pending activities"), while the Window path
		// names the variable that holds the object, as DevTools lists first.
		prev := map[int]ref{t: {-1, -1}}
		queue, found := []int{t}, -1
	search:
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, r := range retainers[n] {
				if _, seen := prev[r.from]; seen {
					continue
				}
				prev[r.from] = ref{n, r.edge}
				if strings.HasPrefix(g.nodeName(r.from), "Window") {
					found = r.from
					break search
				}
				if r.from == 0 && found < 0 {
					found = 0
					continue
				}
				queue = append(queue, r.from)
			}
		}
		line := fmt.Sprintf("  %s @%d (%s):", constructor, g.nodeValue(t, "id"), kb(float64(g.ownSize(t))))
		if found < 0 {
			lines = append(lines, line+" no path from a GC root (garbage waiting to be collected)")
			continue
		}
		type hop struct{ label, name string }
		var hops []hop
		for n := found; n != t; {
			r := prev[n]
			hops = append(hops, hop{shorten(g.edgeLabel(r.edge), 60), g.nodeName(r.from)})
			n = r.from
		}
		// Start at the page's global object when the path goes through it;
		// the hops before it are Chrome internals.
		root := "(GC roots)"
		if found != 0 {
			root = "Window"
		}
		for i := len(hops) - 1; i >= 0 && found == 0; i-- {
			if strings.HasPrefix(hops[i].name, "Window") {
				root, hops = "Window", hops[i+1:]
				break
			}
		}
		var path []string
		for _, h := range hops {
			path = append(path, " -"+h.label+"-> "+h.name)
		}
		if len(path) > 12 {
			path = append(path[:6], append([]string{" …"}, path[len(path)-5:]...)...)
		}
		lines = append(lines, line+" "+root+strings.Join(path, ""))
	}
	return strings.Join(lines, "\n"), nil
}

// QueryObjects counts the live objects made by a constructor, like
// queryObjects() in the console, after a garbage collection.
func (p *Page) QueryObjects(ctx context.Context, constructor string) (string, error) {
	if err := p.call(ctx, "HeapProfiler.enable", nil, nil); err == nil {
		p.call(ctx, "HeapProfiler.collectGarbage", nil, nil)
		p.call(ctx, "HeapProfiler.disable", nil, nil)
	}
	proto, err := p.evaluate(ctx, constructor+".prototype", false)
	if err != nil {
		return "", err
	}
	if proto.ObjectID == "" {
		return "", fmt.Errorf("%s has no prototype object", constructor)
	}
	var res struct {
		Objects RemoteObject `json:"objects"`
	}
	if err := p.call(ctx, "Runtime.queryObjects", map[string]any{"prototypeObjectId": proto.ObjectID, "objectGroup": "agent-browser-mcp"}, &res); err != nil {
		return "", err
	}
	defer p.call(ctx, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": "agent-browser-mcp"}, nil)
	var n int
	if err := p.callOn(ctx, res.Objects.ObjectID, "function() { return this.length; }", nil, &n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s after garbage collection; run it again after using the page to see whether they pile up", plural(n, "live "+constructor+" object")), nil
}
