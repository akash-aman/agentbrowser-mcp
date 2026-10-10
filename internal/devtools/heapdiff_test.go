package devtools

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// snapshot builds a minimal heap snapshot of objects named after their
// constructors, with sizes.
func snapshot(t *testing.T, objects map[string][]int) string {
	t.Helper()
	strs := []string{""}
	var nodes []string
	for name, sizes := range objects {
		strs = append(strs, name)
		for _, sz := range sizes {
			nodes = append(nodes, "3,"+strconv.Itoa(len(strs)-1)+",1,"+strconv.Itoa(sz)+",0,0,0")
		}
	}
	data := `{"snapshot":{"meta":{"node_fields":["type","name","id","self_size","edge_count","trace_node_id","detachedness"],` +
		`"node_types":[["hidden","array","string","object"],"string","number","number","number","number","number"]}},` +
		`"nodes":[` + strings.Join(nodes, ",") + `],"strings":["` + strings.Join(strs, `","`) + `"]}`
	path := filepath.Join(t.TempDir(), "s.heapsnapshot")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHeapDiff(t *testing.T) {
	t.Parallel()
	before := snapshot(t, map[string][]int{"Widget": {100}, "Cache": {500}, "Old": {300}})
	after := snapshot(t, map[string][]int{"Widget": {100, 100, 100}, "Cache": {2500}, "Old": {300}})
	got, err := HeapDiff(before, after, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Heap grew by +2.1 KB (+2 objects)",
		"grew most:\n     +2.0 KB      +0  Cache\n     +0.2 KB      +2  Widget",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Old") {
		t.Errorf("unchanged groups are not growth:\n%s", got)
	}
	if got, _ := HeapDiff(after, after, 5); !strings.HasPrefix(got, "Heap is unchanged: +0.0 KB (+0 objects)") || !strings.Contains(got, "nothing grew") {
		t.Errorf("identical snapshots: %s", got)
	}
	if got, _ := HeapDiff(after, before, 5); !strings.HasPrefix(got, "Heap shrank by -2.1 KB (-2 objects)") {
		t.Errorf("smaller after: %s", got)
	}
}

func frameOf(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 360, 640))
	for y := range 640 {
		for x := range 360 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func TestComposeFilmstrip(t *testing.T) {
	t.Parallel()
	white, red := frameOf(color.White), frameOf(color.RGBA{220, 30, 30, 255})
	frames := []filmFrame{{0, white}, {100 * time.Millisecond, white}, {700 * time.Millisecond, red}}
	for i := range 20 { // more changes than fit, alternating
		f := white
		if i%2 == 0 {
			f = red
		}
		frames = append(frames, filmFrame{time.Duration(800+i*50) * time.Millisecond, f})
	}
	img, summary, err := composeFilmstrip(frames, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(summary, "Filmstrip: 21 distinct frames, showing 10; first change at 0.7s;") || !strings.Contains(summary, "load event at 1.5s") {
		t.Errorf("summary %q", summary)
	}
	decoded, err := png.Decode(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	// 10 thumbnails of 180 px in 5 columns, two rows.
	if b := decoded.Bounds(); b.Dx() != 5*(180+6)+6 || b.Dy() != 2*(320+16+6)+6 {
		t.Errorf("grid size %v", b)
	}
}

// TestRetainersRankByHeldMemory: an Array's items live in its "(array)"
// elements store, so window.leak (2000 items) must rank above a small one
// whose own object happens to be bigger.
func TestRetainersRankByHeldMemory(t *testing.T) {
	t.Parallel()
	const snap = `{"snapshot":{"meta":{
 "node_fields":["type","name","id","self_size","edge_count","trace_node_id","detachedness"],
 "node_types":[["hidden","array","string","object","code","closure","regexp","number","native","synthetic"],"string","number","number","number","number","number"],
 "edge_fields":["type","name_or_index","to_node"],
 "edge_types":[["context","element","property","internal","hidden","shortcut","weak"],"string_or_number","node"]}},
 "nodes":[9,1,1,0,1,0,0, 3,2,3,100,2,0,0, 3,3,5,32,1,0,0, 1,0,7,16000,0,0,0, 3,3,9,64,0,0,0],
 "edges":[1,1,7, 2,4,14, 2,5,28, 3,6,21],
 "strings":["","(root)","Window","Array","leak","tiny","elements"]}`
	path := filepath.Join(t.TempDir(), "a.heapsnapshot")
	if err := os.WriteFile(path, []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Retainers(path, "Array", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := "2 Array objects in " + path + "; how the largest are retained (root first):\n" +
		"  Array @5 (15.7 KB): Window -.leak-> Array\n" +
		"  Array @9 (0.1 KB): Window -.tiny-> Array"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestRetainersPreferTheWindowPath: Chrome often holds an object through an
// internal root too ("Pending activities"); that path is shorter but names
// nothing in the page, so the one from Window is shown.
func TestRetainersPreferTheWindowPath(t *testing.T) {
	t.Parallel()
	const snap = `{"snapshot":{"meta":{
 "node_fields":["type","name","id","self_size","edge_count","trace_node_id","detachedness"],
 "node_types":[["hidden","array","string","object","code","closure","regexp","number","native","synthetic"],"string","number","number","number","number","number"],
 "edge_fields":["type","name_or_index","to_node"],
 "edge_types":[["context","element","property","internal","hidden","shortcut","weak"],"string_or_number","node"]}},
 "nodes":[9,1,1,0,2,0,0, 9,2,3,0,1,0,0, 3,3,5,100,1,0,0, 3,4,7,50,1,0,0, 3,5,9,200,0,0,0],
 "edges":[1,1,7, 1,2,14, 3,8,28, 2,6,21, 3,7,28],
 "strings":["","(root)","(Pending activities)","Window","Map","Widget","cache","table","pending"]}`
	path := filepath.Join(t.TempDir(), "w.heapsnapshot")
	if err := os.WriteFile(path, []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Retainers(path, "Widget", 5)
	if err != nil {
		t.Fatal(err)
	}
	want := "1 Widget object in " + path + "; how the largest are retained (root first):\n  Widget @9 (0.2 KB): Window -.cache-> Map -(table)-> Widget"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
