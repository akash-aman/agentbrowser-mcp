package tools

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
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
		var list []map[string]any
		if json.Unmarshal(data, &list) == nil {
			if text, ok := formatList(list); ok {
				return text
			}
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

// restartNote explains a browser that closed between two calls.
func restartNote(idle time.Duration) string {
	why := "e.g. idle or closed by hand"
	if idle > 0 {
		why = "browsers close after " + shortDuration(idle) + " without commands"
	}
	return "Note: this session's browser had closed since its last use (" + why + "), so this call started a fresh one; earlier tabs, page state and @refs are gone."
}

// shortDuration drops the zero units time.Duration prints: 15m, not 15m0s.
func shortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
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
	if text, ok := formatKnown(obj); ok {
		return text
	}
	if len(obj) == 1 {
		for k, v := range obj {
			if slices.Contains(contentKeys, k) {
				return scalarText(v)
			}
		}
	}
	if len(obj) == 1 {
		for k, v := range obj {
			if list, ok := v.([]any); ok && !slices.ContainsFunc(list, func(x any) bool { _, nested := x.(map[string]any); return nested }) {
				parts := make([]string, len(list))
				for i, x := range list {
					parts[i] = scalarText(x)
				}
				return k + ": " + strings.Join(parts, ", ")
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

// formatKnown renders CLI results that read badly as JSON: cookies, a
// storage area and the saved state files.
func formatKnown(obj map[string]any) (string, bool) {
	if cookies, ok := obj["cookies"].([]any); ok && len(obj) == 1 {
		return formatCookies(cookies), true
	}
	if data, ok := obj["data"].(map[string]any); ok && len(obj) == 1 {
		if len(data) == 0 {
			return "(empty)", true
		}
		keys := slices.Sorted(maps.Keys(data))
		lines := make([]string, len(keys))
		for i, k := range keys {
			lines[i] = k + ": " + shortText(scalarText(data[k]), 200)
		}
		return strings.Join(lines, "\n"), true
	}
	if profiles, ok := obj["profiles"].([]any); ok && len(obj) == 1 && len(profiles) == 0 {
		return "no saved logins; save one in a terminal with agent-browser auth save <name>", true
	}
	if st, ok := obj["state"].(map[string]any); ok && obj["filename"] != nil {
		return formatState(obj, st), true
	}
	if dir, ok := obj["directory"].(string); ok && len(obj) == 2 {
		files, ok := obj["files"].([]any)
		if !ok {
			return "", false
		}
		if len(files) == 0 {
			return "no saved states in " + dir, true
		}
		lines := []string{fmt.Sprintf("%s in %s:", plural(len(files), "saved state"), dir)}
		for _, f := range files {
			m, ok := f.(map[string]any)
			if !ok {
				lines = append(lines, "  "+scalarText(f))
				continue
			}
			line := "  " + scalarText(m["filename"])
			if size, ok := m["size"].(float64); ok {
				line += fmt.Sprintf("  %d B", int(size))
			}
			if mod, ok := m["modified"].(float64); ok {
				line += ", saved " + time.Unix(int64(mod), 0).UTC().Format("2006-01-02 15:04 UTC")
			}
			if m["encrypted"] == true {
				line += ", encrypted"
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n"), true
	}
	return "", false
}

// formatState summarizes a saved state file: its cookies and, per origin,
// the storage keys it restores.
func formatState(obj, st map[string]any) string {
	head := scalarText(obj["path"])
	if size, ok := obj["size"].(float64); ok {
		head += fmt.Sprintf(" (%d B", int(size))
		if mod, ok := obj["modified"].(float64); ok {
			head += ", saved " + time.Unix(int64(mod), 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		if obj["encrypted"] == true {
			head += ", encrypted"
		}
		head += ")"
	}
	lines := []string{head}
	if cookies, ok := st["cookies"].([]any); ok {
		lines = append(lines, formatCookies(cookies))
	}
	origins, _ := st["origins"].([]any)
	for _, o := range origins {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		line := scalarText(m["origin"]) + ":"
		for _, area := range []string{"localStorage", "sessionStorage"} {
			items, _ := m[area].([]any)
			names := make([]string, 0, len(items))
			for _, it := range items {
				if kv, ok := it.(map[string]any); ok {
					names = append(names, scalarText(kv["name"]))
				}
			}
			if len(names) == 0 {
				names = []string{"(empty)"}
			}
			line += " " + area + " " + strings.Join(names, ", ") + ";"
		}
		lines = append(lines, strings.TrimSuffix(line, ";"))
	}
	return strings.Join(lines, "\n")
}

// formatList renders the CLI's bare lists: bundled skills and Chrome
// profiles.
func formatList(list []map[string]any) (string, bool) {
	has := func(keys ...string) bool {
		for _, m := range list {
			for _, k := range keys {
				if _, ok := m[k].(string); !ok {
					return false
				}
			}
		}
		return len(list) > 0
	}
	var lines []string
	switch {
	case has("name", "content"): // one loaded skill: the guide itself
		var parts []string
		for _, m := range list {
			parts = append(parts, m["content"].(string))
		}
		return strings.Join(parts, "\n\n"), true
	case has("name", "description"):
		lines = append(lines, plural(len(list), "skill")+" (help topic:\"skills\" name:<name> loads one):")
		for _, m := range list {
			desc, _, _ := strings.Cut(m["description"].(string), ". ")
			lines = append(lines, "- "+m["name"].(string)+": "+shortText(strings.TrimSuffix(desc, "."), 160))
		}
	case has("name", "directory"):
		lines = append(lines, plural(len(list), "Chrome profile")+" (directory, name):")
		for _, m := range list {
			lines = append(lines, "  "+m["directory"].(string)+"  "+m["name"].(string))
		}
	default:
		return "", false
	}
	return strings.Join(lines, "\n"), true
}

// formatCookies writes one cookie per line: name=value, where it applies,
// then its flags.
func formatCookies(cookies []any) string {
	if len(cookies) == 0 {
		return "no cookies"
	}
	lines := []string{plural(len(cookies), "cookie") + ":"}
	for _, c := range cookies {
		m, ok := c.(map[string]any)
		if !ok {
			lines = append(lines, "  "+scalarText(c))
			continue
		}
		str := func(k string) string { s, _ := m[k].(string); return s }
		flags := []string{"session"}
		if exp, _ := m["expires"].(float64); exp > 0 && m["session"] != true {
			flags[0] = "expires " + time.Unix(int64(exp), 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		for _, f := range []string{"httpOnly", "secure"} {
			if m[f] == true {
				flags = append(flags, strings.ToUpper(f[:1])+f[1:])
			}
		}
		if ss := str("sameSite"); ss != "" {
			flags = append(flags, "SameSite="+ss)
		}
		lines = append(lines, fmt.Sprintf("  %s=%s  %s%s  %s", str("name"), shortText(str("value"), 80), str("domain"), str("path"), strings.Join(flags, ", ")))
	}
	return strings.Join(lines, "\n")
}

func shortText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
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
