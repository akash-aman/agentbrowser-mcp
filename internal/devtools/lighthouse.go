package devtools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// LighthouseCategories are the report categories Lighthouse can score.
var LighthouseCategories = []string{"performance", "accessibility", "best-practices", "seo"}

// LighthouseOptions configures one Lighthouse run against an existing browser.
type LighthouseOptions struct {
	Binary     string
	URL        string
	Port       int
	OutputPath string
	FormFactor string // "mobile" (default) or "desktop"
	Categories []string
}

// args builds the Lighthouse CLI arguments. --port reuses the agent-browser
// browser, so the audit sees the session's cookies and logins.
func (o LighthouseOptions) args() []string {
	args := []string{
		o.URL,
		fmt.Sprintf("--port=%d", o.Port),
		"--output=json",
		"--output-path=" + o.OutputPath,
		"--quiet",
	}
	if len(o.Categories) > 0 {
		args = append(args, "--only-categories="+strings.Join(o.Categories, ","))
	}
	if o.FormFactor == "desktop" {
		args = append(args, "--preset=desktop")
	}
	return args
}

// RunLighthouse runs an audit and returns a text summary of the JSON report.
func RunLighthouse(ctx context.Context, o LighthouseOptions) (string, error) {
	cmd := exec.CommandContext(ctx, o.Binary, o.args()...)
	out, err := cmd.CombinedOutput()
	if cmd.ProcessState == nil {
		return "", fmt.Errorf("cannot run %s (install with `npm install -g lighthouse` or set --lighthouse-path): %w", o.Binary, err)
	}
	if err != nil {
		return "", fmt.Errorf("lighthouse failed: %v: %s", err, lastLines(string(out), 5))
	}
	report, err := os.ReadFile(o.OutputPath)
	if err != nil {
		return "", fmt.Errorf("read lighthouse report: %w", err)
	}
	summary, err := SummarizeLighthouse(report, 10)
	if err != nil {
		return "", err
	}
	return summary + "\nfull report: " + o.OutputPath, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

type lighthouseReport struct {
	FinalURL   string `json:"finalDisplayedUrl"`
	Categories map[string]struct {
		Title     string `json:"title"`
		Score     *float64
		AuditRefs []struct {
			ID     string  `json:"id"`
			Weight float64 `json:"weight"`
		} `json:"auditRefs"`
	} `json:"categories"`
	Audits map[string]lighthouseAudit `json:"audits"`
}

type lighthouseAudit struct {
	Title            string   `json:"title"`
	Score            *float64 `json:"score"`
	ScoreDisplayMode string   `json:"scoreDisplayMode"`
	DisplayValue     string   `json:"displayValue"`
	// Details varies by audit type (tables, lists, debug data objects), so it
	// is decoded leniently in where.
	Details json.RawMessage `json:"details"`
}

// lighthouseItem is one row of an audit's details: a resource, an element,
// or a group of sub-rows.
type lighthouseItem struct {
	URL         string          `json:"url"`
	WastedBytes float64         `json:"wastedBytes"`
	WastedMs    float64         `json:"wastedMs"`
	Node        *lighthouseNode `json:"node"`
	Items       json.RawMessage `json:"items"`
}

// items decodes a details or group "items" field into rows, skipping rows of
// other shapes; anything that is not a list yields no rows.
func items(raw json.RawMessage) []lighthouseItem {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	rows := make([]lighthouseItem, 0, len(list))
	for _, r := range list {
		var it lighthouseItem
		if json.Unmarshal(r, &it) == nil {
			rows = append(rows, it)
		}
	}
	return rows
}

type lighthouseNode struct {
	Snippet   string `json:"snippet"`
	NodeLabel string `json:"nodeLabel"`
}

// maxItemsPerIssue is how many offending resources or elements each issue lists.
const maxItemsPerIssue = 3

// where lists the resources or elements behind an audit, e.g.
// "https://cdn/x.css (758 ms)" or "<a href=\"mailto:…\">".
func (a lighthouseAudit) where() []string {
	var details struct {
		Items json.RawMessage `json:"items"`
	}
	json.Unmarshal(a.Details, &details)
	var rows []lighthouseItem
	for _, it := range items(details.Items) {
		if sub := items(it.Items); it.URL == "" && it.Node == nil && len(sub) > 0 {
			rows = append(rows, sub...)
			continue
		}
		rows = append(rows, it)
	}
	var out []string
	for _, it := range rows {
		if len(out) == maxItemsPerIssue {
			break
		}
		if text := it.describe(); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func (it lighthouseItem) describe() string {
	var parts []string
	switch {
	case it.URL != "":
		parts = append(parts, shortURL(it.URL))
	case it.Node != nil && it.Node.Snippet != "":
		parts = append(parts, shorten(it.Node.Snippet, 120))
	case it.Node != nil && it.Node.NodeLabel != "":
		parts = append(parts, shorten(it.Node.NodeLabel, 120))
	default:
		return ""
	}
	if it.WastedMs >= 1 {
		parts = append(parts, fmt.Sprintf("%.0f ms", it.WastedMs))
	}
	if it.WastedBytes >= 1024 {
		parts = append(parts, fmt.Sprintf("%.0f KiB", it.WastedBytes/1024))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " (" + strings.Join(parts[1:], ", ") + ")"
}

// shortURL drops long query strings, which are noise in a summary.
func shortURL(u string) string {
	base, query, found := strings.Cut(u, "?")
	if found && len(query) > 40 {
		u = base + "?…"
	}
	return shorten(u, 120)
}

var lighthouseMetrics = []struct{ id, label string }{
	{"first-contentful-paint", "FCP"},
	{"largest-contentful-paint", "LCP"},
	{"total-blocking-time", "TBT"},
	{"cumulative-layout-shift", "CLS"},
	{"speed-index", "Speed Index"},
}

// SummarizeLighthouse renders category scores, key metrics, and the failing
// audits that matter most (by category weight), worst first.
func SummarizeLighthouse(report []byte, maxIssues int) (string, error) {
	var r lighthouseReport
	if err := json.Unmarshal(report, &r); err != nil {
		return "", fmt.Errorf("parse lighthouse report: %w", err)
	}
	lines := []string{"Lighthouse " + r.FinalURL, scoreLine(r)}
	if m := metricsLine(r); m != "" {
		lines = append(lines, m)
	}
	if issues := topIssues(r, maxIssues); len(issues) > 0 {
		lines = append(lines, "Top issues:")
		lines = append(lines, issues...)
	}
	return strings.Join(lines, "\n"), nil
}

func scoreLine(r lighthouseReport) string {
	var parts []string
	for _, id := range LighthouseCategories {
		c, ok := r.Categories[id]
		if !ok || c.Score == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d", c.Title, int(*c.Score*100+0.5)))
	}
	return strings.Join(parts, " | ")
}

func metricsLine(r lighthouseReport) string {
	var parts []string
	for _, m := range lighthouseMetrics {
		if a, ok := r.Audits[m.id]; ok && a.DisplayValue != "" {
			parts = append(parts, m.label+" "+a.DisplayValue)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Metrics: " + strings.Join(parts, ", ")
}

func topIssues(r lighthouseReport, limit int) []string {
	type issue struct {
		text   string
		score  float64
		weight float64
	}
	seen := map[string]bool{}
	var issues []issue
	for _, id := range LighthouseCategories {
		for _, ref := range r.Categories[id].AuditRefs {
			a, ok := r.Audits[ref.ID]
			if !ok || seen[ref.ID] || a.Score == nil || *a.Score >= 0.9 {
				continue
			}
			if a.ScoreDisplayMode != "binary" && a.ScoreDisplayMode != "numeric" && a.ScoreDisplayMode != "metricSavings" {
				continue
			}
			seen[ref.ID] = true
			text := "- " + a.Title
			if a.DisplayValue != "" {
				text += " — " + a.DisplayValue
			}
			for _, w := range a.where() {
				text += "\n    " + w
			}
			issues = append(issues, issue{text: text, score: *a.Score, weight: ref.Weight})
		}
	}
	slices.SortStableFunc(issues, func(a, b issue) int {
		return cmp.Or(cmp.Compare(b.weight, a.weight), cmp.Compare(a.score, b.score))
	})
	out := make([]string, 0, min(limit, len(issues)))
	for _, is := range issues[:min(limit, len(issues))] {
		out = append(out, is.text)
	}
	return out
}
