package tools

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// Result hints. Each is one line and only appears when it applies.
const (
	hintTruncated  = "…[truncated %d chars — narrow with selector, interactive, depth, pattern or limit]"
	hintAnnotate   = "Tip: annotate:true labels elements with their @refs so you can act by ref."
	hintMouseClick = "Tip: if the target has a @ref in snapshot, click @ref is more reliable than coordinates."
	hintRelaunched = "Note: agent-browser relaunched the browser for this command; earlier tabs, page state and @refs are gone."
)

// contentKeys are data fields whose value is the whole answer, returned without the key.
var contentKeys = []string{"text", "html", "result", "report", "snapshot", "diff", "value", "styles"}

// formatData renders CLI data for the model: plain text where possible,
// compact JSON otherwise. The "origin" and "lifecycle" fields the CLI adds to
// every result are dropped: they repeat on every call and rarely matter, so
// only a browser relaunch is reported. So are restore and save statuses that
// only say state persistence is off.
func formatData(data json.RawMessage) string {
	if len(data) == 0 {
		return "ok"
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		var s string
		if json.Unmarshal(data, &s) == nil {
			return s
		}
		return string(data)
	}
	note := relaunchNote(obj["lifecycle"])
	delete(obj, "origin")
	delete(obj, "lifecycle")
	for _, k := range []string{"restoreStatus", "saveStatus"} {
		if v := obj[k]; v == "not_configured" || v == "not_attempted" {
			delete(obj, k)
		}
	}
	return note + formatObject(obj)
}

// relaunchNote warns when the CLI had to relaunch the browser for this
// command, which loses the tabs and @refs the model is working with.
func relaunchNote(lifecycle any) string {
	if l, ok := lifecycle.(map[string]any); ok && l["relaunchedBrowser"] == true {
		return hintRelaunched + "\n"
	}
	return ""
}

func formatObject(obj map[string]any) string {
	switch snap := obj["snapshot"].(type) {
	case string:
		return snap
	case map[string]any:
		return formatSnapshotState(snap)
	}
	if len(obj) == 0 {
		return "ok"
	}
	if len(obj) == 1 {
		for k, v := range obj {
			if slices.Contains(contentKeys, k) {
				return scalarText(v)
			}
		}
	}
	if isFlat(obj) {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		lines := make([]string, 0, len(keys))
		for _, k := range keys {
			lines = append(lines, k+": "+scalarText(obj[k]))
		}
		return strings.Join(lines, "\n")
	}
	return compactJSON(obj)
}

// isFlat reports whether every value is a scalar, so key: value lines read well.
func isFlat(obj map[string]any) bool {
	for _, v := range obj {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

func scalarText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return "null"
	case float64, bool:
		return fmt.Sprint(t)
	default:
		return compactJSON(t)
	}
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// truncate caps text at max characters and appends a hint saying how to narrow
// the next call. max <= 0 disables the cap.
func truncate(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	runes := []rune(text)
	return string(runes[:max]) + "\n" + fmt.Sprintf(hintTruncated, len(runes)-max)
}

// textResult builds a successful text result capped at max characters.
func textResult(text string, max int) *mcp.CallToolResult {
	return mcp.NewToolResultText(truncate(text, max))
}

// appendText adds text to a result on a new line. It extends the last text
// block rather than adding another, because some clients (Claude Code among
// them) show consecutive text blocks with no separator between them.
func appendText(res *mcp.CallToolResult, text string) {
	if n := len(res.Content); n > 0 {
		if last, ok := res.Content[n-1].(mcp.TextContent); ok {
			last.Text += "\n" + text
			res.Content[n-1] = last
			return
		}
	}
	res.Content = append(res.Content, mcp.NewTextContent(text))
}

// resultText joins the text blocks of a result.
func resultText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(mcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// dataField returns one string field of CLI data, or "" if absent.
func dataField(data json.RawMessage, key string) string {
	var obj map[string]any
	if json.Unmarshal(data, &obj) != nil {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}

// formatSnapshotDiff renders `diff snapshot` data as just the changed lines.
func formatSnapshotDiff(data json.RawMessage) string {
	var d struct {
		Changed bool   `json:"changed"`
		Diff    string `json:"diff"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		return formatData(data)
	}
	if !d.Changed {
		return "(no changes since last snapshot)"
	}
	var out []string
	for line := range strings.SplitSeq(d.Diff, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, `\ No newline`) || line == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// formatSnapshotState renders `snapshot --delta` output: the whole tree, a
// note that nothing changed, or one line per @ref that changed.
func formatSnapshotState(snap map[string]any) string {
	rev := scalarText(snap["revision"])
	switch snap["kind"] {
	case "full":
		return fmt.Sprintf("Snapshot revision %s (full; delta snapshots after this return only changes):\n%s", rev, scalarText(snap["tree"]))
	case "unchanged":
		return fmt.Sprintf("Snapshot revision %s: unchanged since revision %s.", rev, scalarText(snap["baseRevision"]))
	case "delta":
		changes, _ := snap["changes"].([]any)
		lines := []string{fmt.Sprintf("Snapshot revision %s: %d changes since revision %s (+ added, ~ changed, - removed)",
			rev, len(changes), scalarText(snap["baseRevision"]))}
		for _, c := range changes {
			lines = append(lines, formatRefChange(c))
		}
		return strings.Join(lines, "\n")
	}
	return compactJSON(snap)
}

// formatRefChange renders one delta change, e.g. `+ @e5 link "Next"`,
// `~ @e3 name: "Renamed"` or `- @e9`.
func formatRefChange(change any) string {
	c, ok := change.(map[string]any)
	if !ok {
		return compactJSON(change)
	}
	ref := scalarText(c["ref"])
	switch c["op"] {
	case "add":
		node, _ := c["node"].(map[string]any)
		line := fmt.Sprintf("+ %s %s %s", ref, scalarText(node["role"]), compactJSON(node["name"]))
		var attrs []string
		for _, k := range slices.Sorted(maps.Keys(node)) {
			if k != "role" && k != "name" {
				attrs = append(attrs, k+"="+scalarText(node[k]))
			}
		}
		if len(attrs) > 0 {
			line += " [" + strings.Join(attrs, ", ") + "]"
		}
		return line
	case "replace":
		return fmt.Sprintf("~ %s %s: %s", ref, scalarText(c["field"]), compactJSON(c["value"]))
	case "remove":
		return "- " + ref
	}
	return "? " + compactJSON(c)
}
