package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

var sentenceEnd = regexp.MustCompile(`[.!?](\s|$)`)

// TestDescriptionsAreShort keeps each description to at most two sentences:
// what the tool does, then when to prefer it or what to prefer instead.
func TestDescriptionsAreShort(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, st := range e.reg.Tools() {
		d := st.Tool.Description
		if d == "" {
			t.Errorf("%s has no description", st.Tool.Name)
			continue
		}
		n := len(sentenceEnd.FindAllString(strings.ReplaceAll(d, "e.g.", "eg"), -1))
		if n > 2 {
			t.Errorf("%s description has %d sentences: %q", st.Tool.Name, n, d)
		}
	}
}

// TestFallbacksPointToCheaperTools checks that tools the model should avoid
// name the cheaper alternative.
func TestFallbacksPointToCheaperTools(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"mouse":       "prefer click @ref",
		"screenshot":  "prefer snapshot",
		"eval_script": "prefer get, find or page_text",
		"wait":        "Prefer these over for=time",
		"page_text":   "Prefer over snapshot",
		"batch":       "Prefer this over one call per step",
		"scroll":      "prefer element_action scroll_into_view",
	}
	e := newEnv(t)
	for _, st := range e.reg.Tools() {
		if phrase, ok := want[st.Tool.Name]; ok && !strings.Contains(st.Tool.Description, phrase) {
			t.Errorf("%s description %q should contain %q", st.Tool.Name, st.Tool.Description, phrase)
		}
	}
}

func TestResultHints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tool string
		args map[string]any
		max  int
		hint string
		want bool
	}{
		{"screenshot plain", "screenshot", nil, 40000, hintAnnotate, true},
		{"screenshot annotated", "screenshot", a{"annotate": true}, 40000, hintAnnotate, false},
		{"mouse click", "mouse", a{"action": "click", "x": 1, "y": 2}, 40000, hintMouseClick, true},
		{"mouse move", "mouse", a{"action": "move", "x": 1, "y": 2}, 40000, hintMouseClick, false},
		{"truncated", "page_text", nil, 4, "…[truncated", true},
		{"not truncated", "page_text", nil, 40000, "…[truncated", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t, func(cfg *config.Config) { cfg.MaxOutput = c.max })
			got := strings.Contains(e.call(c.tool, c.args).text(), c.hint)
			if got != c.want {
				t.Fatalf("hint %q present = %v, want %v", c.hint, got, c.want)
			}
		})
	}
}

func TestAnnotatedScreenshotListsRefs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	text := e.call("screenshot", a{"annotate": true}).text()
	for _, s := range []string{`[1] @e1 button "Go"`, "[2] @e2 textbox"} {
		if !strings.Contains(text, s) {
			t.Errorf("legend missing %q:\n%s", s, text)
		}
	}
}
