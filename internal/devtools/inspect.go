package devtools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Metrics returns Chrome's live page metrics (Performance.getMetrics):
// heap size, DOM nodes, listeners, layout and style recalc counts, script time.
// Counts and durations accumulate from the first call, which says so, so a
// reader does not mistake that call's zeros for an idle page.
func (p *Page) Metrics(ctx context.Context) (string, error) {
	if err := p.call(ctx, "Performance.enable", nil, nil); err != nil {
		return "", err
	}
	p.mu.Lock()
	first := p.perfSince.IsZero()
	if first {
		p.perfSince = time.Now()
	}
	since := time.Since(p.perfSince)
	p.mu.Unlock()
	var res struct {
		Metrics []struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		} `json:"metrics"`
	}
	if err := p.call(ctx, "Performance.getMetrics", nil, &res); err != nil {
		return "", err
	}
	lines := make([]string, 0, len(res.Metrics))
	for _, m := range res.Metrics {
		if timestampMetrics[m.Name] || strings.HasSuffix(m.Name, "Start") {
			continue
		}
		lines = append(lines, m.Name+": "+formatMetric(m.Name, m.Value))
	}
	header := fmt.Sprintf("Counts and durations since the first metrics call %.0fs ago:", since.Seconds())
	if first {
		header = "Counting starts now, so counts and durations are 0 on this first call; load or interact with the page, then call metrics again."
	}
	return header + "\n" + strings.Join(lines, "\n"), nil
}

// timestampMetrics are points in time (seconds since an arbitrary origin),
// not measurements, so they mean nothing to a reader.
var timestampMetrics = map[string]bool{"Timestamp": true, "FirstMeaningfulPaint": true, "DomContentLoaded": true}

func formatMetric(name string, v float64) string {
	switch {
	case strings.HasSuffix(name, "Size"):
		return fmt.Sprintf("%.1f MB", v/(1<<20))
	case strings.HasSuffix(name, "Duration") || strings.HasSuffix(name, "Time"):
		return fmt.Sprintf("%.1f ms", v*1000)
	}
	return fmt.Sprintf("%g", v)
}

// Issues returns the DevTools Issues panel entries (CORS, mixed content,
// cookie, deprecation, CSP, …) collected since the page was first inspected.
func (p *Page) Issues(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	started := p.issues != nil
	if !started {
		p.issues = &issueLog{}
	}
	log := p.issues
	p.mu.Unlock()

	if !started {
		p.onSession("Audits.issueAdded", func(params json.RawMessage) {
			var e struct {
				Issue struct {
					Code    string                     `json:"code"`
					Details map[string]json.RawMessage `json:"details"`
				} `json:"issue"`
			}
			if json.Unmarshal(params, &e) == nil {
				log.add(e.Issue.Code, e.Issue.Details)
			}
		})
		if err := p.call(ctx, "Audits.enable", nil, nil); err != nil {
			return nil, err
		}
		// Existing issues are reported as events right after enabling.
		time.Sleep(300 * time.Millisecond)
	}
	return log.list(), nil
}

type issueLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *issueLog) add(code string, details map[string]json.RawMessage) {
	var parts []string
	for _, d := range details {
		parts = append(parts, shorten(string(d), 300))
	}
	slices.Sort(parts)
	line := code
	if len(parts) > 0 {
		line += ": " + strings.Join(parts, " ")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !slices.Contains(l.lines, line) {
		l.lines = append(l.lines, line)
	}
}

func (l *issueLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.lines)
}

// origin returns the page's origin, which storage APIs are keyed by.
func (p *Page) origin(ctx context.Context) (string, error) {
	v, err := p.Evaluate(ctx, "location.origin", -1)
	if err != nil {
		return "", err
	}
	var origin string
	if json.Unmarshal(v.Value, &origin) != nil || origin == "" || origin == "null" {
		return "", fmt.Errorf("the page has no origin (about:blank or file URL)")
	}
	return origin, nil
}

// IndexedDB lists databases and their object stores for the page's origin.
func (p *Page) IndexedDB(ctx context.Context) (string, error) {
	origin, err := p.origin(ctx)
	if err != nil {
		return "", err
	}
	if err := p.call(ctx, "IndexedDB.enable", nil, nil); err != nil {
		return "", err
	}
	var names struct {
		DatabaseNames []string `json:"databaseNames"`
	}
	if err := p.call(ctx, "IndexedDB.requestDatabaseNames", map[string]any{"securityOrigin": origin}, &names); err != nil {
		return "", err
	}
	if len(names.DatabaseNames) == 0 {
		return "(no IndexedDB databases for " + origin + ")", nil
	}
	var b strings.Builder
	for _, name := range names.DatabaseNames {
		var db struct {
			DatabaseWithObjectStores struct {
				Version      float64 `json:"version"`
				ObjectStores []struct {
					Name          string `json:"name"`
					AutoIncrement bool   `json:"autoIncrement"`
					KeyPath       struct {
						String string   `json:"string"`
						Array  []string `json:"array"`
					} `json:"keyPath"`
					Indexes []struct {
						Name string `json:"name"`
					} `json:"indexes"`
				} `json:"objectStores"`
			} `json:"databaseWithObjectStores"`
		}
		params := map[string]any{"securityOrigin": origin, "databaseName": name}
		if err := p.call(ctx, "IndexedDB.requestDatabase", params, &db); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s (version %g)\n", name, db.DatabaseWithObjectStores.Version)
		for _, s := range db.DatabaseWithObjectStores.ObjectStores {
			key := s.KeyPath.String
			if len(s.KeyPath.Array) > 0 {
				key = strings.Join(s.KeyPath.Array, ",")
			}
			var idx []string
			for _, i := range s.Indexes {
				idx = append(idx, i.Name)
			}
			fmt.Fprintf(&b, "  store %s key=%q autoIncrement=%v indexes=%v\n", s.Name, key, s.AutoIncrement, idx)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// IndexedDBRead returns up to limit records from an object store.
func (p *Page) IndexedDBRead(ctx context.Context, database, store string, skip, limit int) (string, error) {
	origin, err := p.origin(ctx)
	if err != nil {
		return "", err
	}
	if err := p.call(ctx, "IndexedDB.enable", nil, nil); err != nil {
		return "", err
	}
	var res struct {
		Entries []struct {
			Key   RemoteObject `json:"key"`
			Value RemoteObject `json:"value"`
		} `json:"objectStoreDataEntries"`
		HasMore bool `json:"hasMore"`
	}
	params := map[string]any{
		"securityOrigin": origin, "databaseName": database, "objectStoreName": store,
		"skipCount": skip, "pageSize": limit, // no indexName: read the store itself
	}
	if err := p.call(ctx, "IndexedDB.requestData", params, &res); err != nil {
		return "", err
	}
	lines := make([]string, 0, len(res.Entries)+1)
	for _, e := range res.Entries {
		lines = append(lines, e.Key.String()+" => "+e.Value.String())
	}
	if len(lines) == 0 {
		lines = append(lines, "(no records)")
	}
	if res.HasMore {
		lines = append(lines, fmt.Sprintf("… more records; use skip=%d", skip+limit))
	}
	return strings.Join(lines, "\n"), nil
}

// CacheStorage lists Cache Storage caches and their first entries.
func (p *Page) CacheStorage(ctx context.Context, limit int) (string, error) {
	origin, err := p.origin(ctx)
	if err != nil {
		return "", err
	}
	var caches struct {
		Caches []struct {
			CacheID   string `json:"cacheId"`
			CacheName string `json:"cacheName"`
		} `json:"caches"`
	}
	if err := p.call(ctx, "CacheStorage.requestCacheNames", map[string]any{"securityOrigin": origin}, &caches); err != nil {
		return "", err
	}
	if len(caches.Caches) == 0 {
		return "(no Cache Storage caches for " + origin + ")", nil
	}
	var b strings.Builder
	for _, c := range caches.Caches {
		var entries struct {
			Entries []struct {
				RequestURL     string `json:"requestURL"`
				RequestMethod  string `json:"requestMethod"`
				ResponseStatus int    `json:"responseStatus"`
			} `json:"cacheDataEntries"`
			ReturnCount int `json:"returnCount"`
		}
		params := map[string]any{"cacheId": c.CacheID, "skipCount": 0, "pageSize": limit}
		if err := p.call(ctx, "CacheStorage.requestEntries", params, &entries); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s (%d entries)\n", c.CacheName, entries.ReturnCount)
		for _, e := range entries.Entries {
			fmt.Fprintf(&b, "  %s %d %s\n", e.RequestMethod, e.ResponseStatus, e.RequestURL)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ServiceWorkers lists service worker registrations for the page.
func (p *Page) ServiceWorkers(ctx context.Context) (string, error) {
	const script = `(async () => {
  if (!navigator.serviceWorker) return "service workers are not available on this page";
  const regs = await navigator.serviceWorker.getRegistrations();
  if (!regs.length) return "(no service worker registrations)";
  return regs.map(r => {
    const w = r.active || r.waiting || r.installing;
    return r.scope + " " + (w ? w.state + " " + w.scriptURL : "no worker");
  }).join("\n");
})()`
	v, err := p.Evaluate(ctx, script, -1)
	if err != nil {
		return "", err
	}
	var text string
	json.Unmarshal(v.Value, &text)
	return text, nil
}

// Manifest returns the web app manifest URL, parse errors and contents.
func (p *Page) Manifest(ctx context.Context) (string, error) {
	var res struct {
		URL    string `json:"url"`
		Data   string `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := p.call(ctx, "Page.getAppManifest", nil, &res); err != nil {
		return "", err
	}
	if res.URL == "" {
		return "(no web app manifest)", nil
	}
	lines := []string{"manifest: " + res.URL}
	for _, e := range res.Errors {
		lines = append(lines, "error: "+e.Message)
	}
	if res.Data != "" {
		lines = append(lines, shorten(compactJSON(res.Data), 2000))
	}
	return strings.Join(lines, "\n"), nil
}

func compactJSON(s string) string {
	var v any
	if json.Unmarshal([]byte(s), &v) != nil {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// StorageUsage reports quota and usage per storage type for the page's origin.
func (p *Page) StorageUsage(ctx context.Context) (string, error) {
	origin, err := p.origin(ctx)
	if err != nil {
		return "", err
	}
	var res struct {
		Usage     float64 `json:"usage"`
		Quota     float64 `json:"quota"`
		Breakdown []struct {
			StorageType string  `json:"storageType"`
			Usage       float64 `json:"usage"`
		} `json:"usageBreakdown"`
	}
	if err := p.call(ctx, "Storage.getUsageAndQuota", map[string]any{"origin": origin}, &res); err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("%s: %s used of %s quota", origin, kb(res.Usage), kb(res.Quota))}
	for _, u := range res.Breakdown {
		if u.Usage > 0 {
			lines = append(lines, fmt.Sprintf("  %s: %s", u.StorageType, kb(u.Usage)))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// ClearSiteData deletes all stored data for the page's origin (cookies,
// storage, IndexedDB, caches, service workers).
func (p *Page) ClearSiteData(ctx context.Context) (string, error) {
	origin, err := p.origin(ctx)
	if err != nil {
		return "", err
	}
	if err := p.call(ctx, "Storage.clearDataForOrigin", map[string]any{"origin": origin, "storageTypes": "all"}, nil); err != nil {
		return "", err
	}
	return "cleared all site data for " + origin, nil
}

func kb(bytes float64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", bytes/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", bytes/(1<<20))
	}
	return fmt.Sprintf("%.1f KB", bytes/1024)
}
