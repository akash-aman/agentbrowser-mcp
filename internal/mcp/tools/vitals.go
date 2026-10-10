package tools

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// vitalLimits are Google's thresholds for each Web Vital
// (https://web.dev/articles/vitals): good up to good, poor above poor.
var vitalLimits = map[string]struct{ good, poor float64 }{
	"LCP":  {2500, 4000},
	"CLS":  {0.1, 0.25},
	"INP":  {200, 500},
	"FCP":  {1800, 3000},
	"TTFB": {800, 1800},
}

func rating(metric string, v float64) string {
	l := vitalLimits[metric]
	switch {
	case v <= l.good:
		return "good"
	case v <= l.poor:
		return "needs improvement"
	}
	return "poor"
}

type layoutShift struct {
	StartTime float64 `json:"startTime"`
	Value     float64 `json:"value"`
}

type vitalsData struct {
	URL string `json:"url"`
	LCP *struct {
		StartTime float64 `json:"startTime"`
		Element   string  `json:"element"`
		URL       string  `json:"url"`
	} `json:"lcp"`
	CLS *struct {
		Score   float64       `json:"score"`
		Entries []layoutShift `json:"entries"`
	} `json:"cls"`
	INP                json.RawMessage   `json:"inp"`
	FCP                *float64          `json:"fcp"`
	TTFB               *float64          `json:"ttfb"`
	Hydration          json.RawMessage   `json:"hydration"`
	HydratedComponents []json.RawMessage `json:"hydratedComponents"`
	Lifecycle          any               `json:"lifecycle"`
}

// formatVitals turns agent-browser's vitals JSON into one rated line per
// metric, naming the element behind LCP and the shifts behind CLS. It
// reports false when the data is not vitals JSON.
func formatVitals(data json.RawMessage) (string, bool) {
	var v vitalsData
	if json.Unmarshal(data, &v) != nil || v.LCP == nil && v.CLS == nil && v.FCP == nil && v.TTFB == nil {
		return "", false
	}
	var b strings.Builder
	b.WriteString(relaunchNote(v.Lifecycle))
	b.WriteString("Web Vitals")
	if v.URL != "" {
		b.WriteString(" for " + v.URL)
	}
	b.WriteString(", rated against Google's thresholds:")
	ms := func(name string, val *float64, detail string) {
		if val == nil {
			fmt.Fprintf(&b, "\n%s not measured", name)
			return
		}
		fmt.Fprintf(&b, "\n%s %s ms, %s%s", name, strconv.FormatFloat(*val, 'f', 0, 64), rating(name, *val), detail)
	}

	if v.LCP == nil {
		ms("LCP", nil, "")
	} else {
		what := v.LCP.Element
		if v.LCP.URL != "" {
			what += " " + v.LCP.URL
		}
		if what != "" {
			what = " (" + what + ")"
		}
		ms("LCP", &v.LCP.StartTime, what)
	}

	if v.CLS == nil {
		b.WriteString("\nCLS not measured")
	} else {
		fmt.Fprintf(&b, "\nCLS %s, %s", strconv.FormatFloat(v.CLS.Score, 'f', -1, 64), rating("CLS", v.CLS.Score))
		shifts := slices.Clone(v.CLS.Entries)
		slices.SortFunc(shifts, func(x, y layoutShift) int { return cmp.Compare(y.Value, x.Value) })
		var parts []string
		for _, s := range shifts[:min(len(shifts), 3)] {
			parts = append(parts, fmt.Sprintf("%s at %.0f ms", strconv.FormatFloat(s.Value, 'f', -1, 64), s.StartTime))
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, " (%s: %s)", plural(len(shifts), "shift"), strings.Join(parts, ", "))
		}
	}

	if inp, ok := inpValue(v.INP); ok {
		ms("INP", &inp, "")
	} else {
		b.WriteString("\nINP not measured: it needs a click, tap or key press first")
	}
	ms("FCP", v.FCP, "")
	ms("TTFB", v.TTFB, "")

	if len(v.Hydration) > 0 && string(v.Hydration) != "null" {
		b.WriteString("\nHydration: " + clip(string(v.Hydration), 300))
	}
	if n := len(v.HydratedComponents); n > 0 {
		fmt.Fprintf(&b, "\nHydrated components: %d", n)
	}
	return b.String(), true
}

// inpValue reads INP as a number or as an object with a value or duration.
func inpValue(raw json.RawMessage) (float64, bool) {
	var n float64
	if json.Unmarshal(raw, &n) == nil && len(raw) > 0 && string(raw) != "null" {
		return n, true
	}
	var o struct {
		Value    *float64 `json:"value"`
		Duration *float64 `json:"duration"`
	}
	if json.Unmarshal(raw, &o) != nil {
		return 0, false
	}
	switch {
	case o.Value != nil:
		return *o.Value, true
	case o.Duration != nil:
		return *o.Duration, true
	}
	return 0, false
}
