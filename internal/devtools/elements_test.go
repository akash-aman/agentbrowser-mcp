package devtools

import (
	"math"
	"strings"
	"testing"
)

func TestContrastRatio(t *testing.T) {
	t.Parallel()
	white, black := rgba{255, 255, 255, 1}, rgba{0, 0, 0, 1}
	cases := []struct {
		name   string
		fg, bg rgba
		want   float64
	}{
		{"black on white", black, white, 21},
		{"same color", rgba{120, 40, 200, 1}, rgba{120, 40, 200, 1}, 1},
		{"WCAG example #767676 on white", rgba{118, 118, 118, 1}, white, 4.54},
		{"half-transparent black on white is rgb(127.5)", rgba{0, 0, 0, 0.5}, white, 3.98},
		{"transparent background counts as white", black, rgba{0, 0, 0, 0}, 21},
	}
	for _, c := range cases {
		if got := contrastRatio(c.fg, c.bg); math.Abs(got-c.want) > 0.01 {
			t.Errorf("%s: %.3f, want %.2f", c.name, got, c.want)
		}
	}
}

func TestParseColor(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]rgba{
		"rgb(255, 0, 10)":         {255, 0, 10, 1},
		"rgba(0, 0, 0, 0.5)":      {0, 0, 0, 0.5},
		"rgb(255 128 0 / 50%)":    {255, 128, 0, 0.5},
		" rgb(100%, 0%, 50%) ":    {255, 0, 127.5, 1},
		"rgba(10, 20, 30, 0.125)": {10, 20, 30, 0.125},
	} {
		got, err := parseColor(in)
		if err != nil || got != want {
			t.Errorf("parseColor(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "red", "#fff", "oklch(0.5 0.1 200)", "rgb(1, 2)"} {
		if _, err := parseColor(bad); err == nil {
			t.Errorf("parseColor(%q) should fail", bad)
		}
	}
}

// TestContrastReportThresholds: large text (24px, or 18.66px bold) needs 3:1
// for AA instead of 4.5:1.
func TestContrastReportThresholds(t *testing.T) {
	t.Parallel()
	gray := "rgb(130, 130, 130)" // 3.8:1 on white
	cases := []struct {
		size   float64
		weight int
		want   string
	}{
		{16, 400, "AA FAILS (needs 4.5)"},
		{24, 400, "AA passes (needs 3)"},
		{19, 700, "AA passes (needs 3)"},
		{19, 600, "AA FAILS (needs 4.5)"},
	}
	for _, c := range cases {
		got, err := contrastReport("p", gray, []string{"rgb(255, 255, 255)"}, c.size, c.weight)
		if err != nil || !strings.Contains(got, c.want) {
			t.Errorf("%gpx %d: %q, %v; want %q", c.size, c.weight, got, err, c.want)
		}
	}
	got, err := contrastReport("p", gray, []string{"rgb(255, 255, 255)", "rgb(200, 200, 200)"}, 16, 400)
	if err != nil || !strings.Contains(got, "on rgb(200, 200, 200)") {
		t.Errorf("the worst background should be reported: %q %v", got, err)
	}
	if got, _ := contrastReport("p", gray, nil, 16, 400); !strings.Contains(got, "cannot tell the background") {
		t.Errorf("no background colors: %q", got)
	}
}

func TestMarkOverridden(t *testing.T) {
	t.Parallel()
	no := false
	blocks := []styleBlock{
		{title: "element.style", props: []cssProperty{{Name: "color", Value: "red"}}},
		{title: ".a", props: []cssProperty{{Name: "color", Value: "blue", Important: true}, {Name: "margin", Value: "0"}}},
		{title: ".b", props: []cssProperty{{Name: "margin", Value: "4px"}, {Name: "width", Value: "x", ParsedOk: &no}, {Name: "top", Value: "0", Disabled: true}}},
	}
	markOverridden(blocks)
	want := []map[int]string{
		{0: "overridden"}, // !important in a lower rule beats the inline style
		{},
		{0: "overridden", 1: "invalid value", 2: "disabled"},
	}
	for i, b := range blocks {
		if len(b.overruns) != len(want[i]) {
			t.Errorf("%s: %v, want %v", b.title, b.overruns, want[i])
			continue
		}
		for k, v := range want[i] {
			if b.overruns[k] != v {
				t.Errorf("%s decl %d: %q, want %q", b.title, k, b.overruns[k], v)
			}
		}
	}
}

func TestEdges(t *testing.T) {
	t.Parallel()
	values := map[string]string{"margin-top": "1px", "margin-right": "2px", "margin-bottom": "3px", "margin-left": "4px", "border-top-width": "1px"}
	if got, ok := edges(values, "margin"); !ok || got != "1px 2px 3px 4px" {
		t.Errorf("margin: %q %v", got, ok)
	}
	if _, ok := edges(values, "border-width"); ok {
		t.Error("border-width with one side known should not be joined")
	}
	if _, ok := edges(values, "color"); ok {
		t.Error("color has no sides")
	}
}

func TestShorthand(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		sides []string
		want  string
	}{
		{[]string{"0px", "0px", "0px", "0px"}, "0px"},
		{[]string{"1px", "6px", "1px", "6px"}, "1px 6px"},
		{[]string{"0px", "0px", "12px", "0px"}, "0px 0px 12px"},
		{[]string{"1px", "2px", "3px", "4px"}, "1px 2px 3px 4px"},
		{[]string{"4px", "4px", "8px", "2px"}, "4px 4px 8px 2px"},
		{[]string{"8px", "8px"}, "8px"},
		{[]string{"8px", "4px"}, "8px 4px"},
	} {
		if got := shorthand(c.sides...); got != c.want {
			t.Errorf("shorthand(%v) = %q, want %q", c.sides, got, c.want)
		}
	}
}

// TestComputedPrefixHidesDefaults: font matched every font-* longhand,
// most of them at their defaults; an exact name is always shown.
func TestComputedPrefixHidesDefaults(t *testing.T) {
	t.Parallel()
	values := map[string]string{"font-family": "Arial", "font-size": "13px", "font-kerning": "auto", "font-variant-caps": "normal", "font-palette": "normal"}
	if got, want := FormatComputed(values, []string{"font"}), "font-family: Arial\nfont-size: 13px\n(3 more font-* at normal, none or auto; name one to read it)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := FormatComputed(values, []string{"font-kerning"}); got != "font-kerning: auto" {
		t.Errorf("exact name: %q", got)
	}
}
