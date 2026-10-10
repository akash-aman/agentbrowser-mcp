package devtools

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// encodeVLQ is the inverse of decodeVLQ, to build mappings in tests.
func encodeVLQ(nums ...int) string {
	var b strings.Builder
	for _, n := range nums {
		v := n << 1
		if n < 0 {
			v = (-n << 1) | 1
		}
		for {
			d := v & 31
			v >>= 5
			if v > 0 {
				d |= 32
			}
			b.WriteByte(vlqChars[d])
			if v == 0 {
				break
			}
		}
	}
	return b.String()
}

func TestDecodeVLQ(t *testing.T) {
	t.Parallel()
	for _, nums := range [][]int{{0}, {1, -1}, {16, -16, 1000, -12345}, {0, 0, 0, 0, 0}} {
		got, err := decodeVLQ(encodeVLQ(nums...))
		if err != nil || !slices.Equal(got, nums) {
			t.Errorf("%v: got %v %v", nums, got, err)
		}
	}
	// Known values from the spec's examples.
	if got, _ := decodeVLQ("AAgBC"); !slices.Equal(got, []int{0, 0, 16, 1}) {
		t.Errorf("AAgBC = %v", got)
	}
	for _, bad := range []string{"A!", "g"} {
		if _, err := decodeVLQ(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func mapJSON(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSourceMapLookups(t *testing.T) {
	t.Parallel()
	// Generated line 0: col 0 → a.ts 0:0, col 10 → a.ts 1:4, col 20 → b.ts 5:2.
	// Generated line 1: col 4 → b.ts 6:0.
	line0 := strings.Join([]string{encodeVLQ(0, 0, 0, 0), encodeVLQ(10, 0, 1, 4), encodeVLQ(10, 1, 4, -2)}, ",")
	line1 := encodeVLQ(4, 0, 1, -2)
	m, err := ParseSourceMap(mapJSON(t, map[string]any{
		"version": 3, "sources": []string{"a.ts", "b.ts"}, "sourceRoot": "src", "mappings": line0 + ";" + line1,
	}), "http://x/assets/app.js.map")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Sources, []string{"http://x/assets/src/a.ts", "http://x/assets/src/b.ts"}) {
		t.Fatalf("sources %v", m.Sources)
	}
	cases := []struct {
		line, col   int
		src         string
		oLine, oCol int
		ok          bool
	}{
		{0, 0, "http://x/assets/src/a.ts", 0, 0, true},
		{0, 9, "http://x/assets/src/a.ts", 0, 0, true}, // before the next segment
		{0, 15, "http://x/assets/src/a.ts", 1, 4, true},
		{0, 99, "http://x/assets/src/b.ts", 5, 2, true},
		{1, 2, "", 0, 0, false}, // before the line's first segment
		{1, 4, "http://x/assets/src/b.ts", 6, 0, true},
		{7, 0, "", 0, 0, false},
	}
	for _, c := range cases {
		src, l, col, ok := m.Original(c.line, c.col)
		if ok != c.ok || ok && (src != c.src || l != c.oLine || col != c.oCol) {
			t.Errorf("Original(%d,%d) = %s %d:%d %v", c.line, c.col, src, l, col, ok)
		}
	}
	if l, c, ok := m.Generated(1, 5); !ok || l != 0 || c != 20 {
		t.Errorf("Generated(b.ts, 5) = %d:%d %v", l, c, ok)
	}
	if l, c, ok := m.Generated(1, 6); !ok || l != 1 || c != 4 {
		t.Errorf("Generated(b.ts, 6) = %d:%d %v", l, c, ok)
	}
	if l, _, ok := m.Generated(0, 1); !ok || l != 0 {
		t.Errorf("Generated(a.ts, 1) line %d %v", l, ok)
	}
	if _, _, ok := m.Generated(1, 50); ok {
		t.Error("a line past the end of b.ts has no code")
	}
	if i, ok := m.FindSource("src/b.ts"); !ok || i != 1 {
		t.Errorf("FindSource(src/b.ts) = %d %v", i, ok)
	}
	if _, ok := m.FindSource("c.ts"); ok {
		t.Error("c.ts is not in the map")
	}
}

// TestIndexSourceMap: Turbopack and some bundlers emit maps made of
// sections, each offset into the generated file.
func TestIndexSourceMap(t *testing.T) {
	t.Parallel()
	section := func(src string) map[string]any {
		return map[string]any{"version": 3, "sources": []string{src}, "mappings": encodeVLQ(0, 0, 3, 0)}
	}
	m, err := ParseSourceMap(mapJSON(t, map[string]any{"version": 3, "sections": []any{
		map[string]any{"offset": map[string]int{"line": 0, "column": 0}, "map": section("a.ts")},
		map[string]any{"offset": map[string]int{"line": 2, "column": 7}, "map": section("b.ts")},
	}}), "http://x/app.js.map")
	if err != nil {
		t.Fatal(err)
	}
	if src, line, _, ok := m.Original(2, 7); !ok || !strings.HasSuffix(src, "b.ts") || line != 3 {
		t.Errorf("section 2 lookup: %s %d %v", src, line, ok)
	}
	if _, _, _, ok := m.Original(2, 6); ok {
		t.Error("the column offset applies to the section's first line")
	}
}

func TestShortSource(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"webpack://shop/./src/cart.ts":       "src/cart.ts",
		"webpack:///./src/cart.ts":           "src/cart.ts",
		"turbopack:///[project]/src/cart.ts": "src/cart.ts",
		"http://localhost:5173/src/main.tsx": "src/main.tsx",
		"../../src/util.js":                  "../../src/util.js",
		"/abs/src/x.js":                      "abs/src/x.js",
		"file:///Users/me/app/src/index.ts":  "Users/me/app/src/index.ts",
		"vite:///@vite/client":               "@vite/client",
	} {
		if got := ShortSource(in); got != want {
			t.Errorf("ShortSource(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSourceMapInputs(t *testing.T) {
	t.Parallel()
	raw := `{"version":3,"sources":["a.js"],"mappings":"AAAA"}`
	if b, ok, err := sourceMapData("data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(raw))); !ok || err != nil || string(b) != raw {
		t.Errorf("base64 data URL: %q %v %v", b, ok, err)
	}
	if b, ok, err := sourceMapData("data:application/json," + strings.ReplaceAll(raw, " ", "%20")); !ok || err != nil || string(b) != raw {
		t.Errorf("plain data URL: %q %v %v", b, ok, err)
	}
	if _, ok, _ := sourceMapData("http://x/a.js.map"); ok {
		t.Error("http URLs are fetched, not decoded")
	}
	if _, err := ParseSourceMap([]byte(")]}'"+raw), "http://x/a.js.map"); err != nil {
		t.Errorf("XSSI prefix: %v", err)
	}
	if _, err := ParseSourceMap([]byte(`{"version":2,"sources":[],"mappings":""}`), ""); err == nil {
		t.Error("version 2 should be refused")
	}
	if got := resolveSource("http://x/js/app.js", "", "app.js.map"); got != "http://x/js/app.js.map" {
		t.Errorf("relative map URL: %s", got)
	}
}
