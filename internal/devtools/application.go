package devtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BackForwardCache tests whether the page can be restored instantly from the
// back/forward cache, as the Application panel's test does: it navigates
// away and back, then reports the restore or Chrome's reasons it could not.
func (p *Page) BackForwardCache(ctx context.Context) (string, error) {
	if err := p.call(ctx, "Page.enable", nil, nil); err != nil {
		return "", err
	}
	var hist struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := p.call(ctx, "Page.getNavigationHistory", nil, &hist); err != nil {
		return "", err
	}
	if hist.CurrentIndex >= len(hist.Entries) {
		return "", fmt.Errorf("no navigation history")
	}
	home := hist.Entries[hist.CurrentIndex]
	if _, err := p.evaluate(ctx, "window.__abmBFCache = 1", true); err != nil {
		return "", err
	}
	reasons := make(chan []string, 1)
	stop := p.onSession("Page.backForwardCacheNotUsed", func(params json.RawMessage) {
		var e struct {
			NotRestoredExplanations []struct {
				Type    string `json:"type"`
				Reason  string `json:"reason"`
				Context string `json:"context"`
			} `json:"notRestoredExplanations"`
		}
		if json.Unmarshal(params, &e) != nil {
			return
		}
		var out []string
		for _, x := range e.NotRestoredExplanations {
			r := x.Reason + " (" + x.Type + ")"
			if x.Context != "" {
				r += " in " + x.Context
			}
			out = append(out, r)
		}
		select {
		case reasons <- out:
		default:
		}
	})
	defer stop()
	loaded := make(chan struct{}, 4)
	stopLoad := p.onSession("Page.loadEventFired", func(json.RawMessage) {
		select {
		case loaded <- struct{}{}:
		default:
		}
	})
	defer stopLoad()
	wait := func(d time.Duration) {
		select {
		case <-loaded:
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
	if err := p.call(ctx, "Page.navigate", map[string]any{"url": "data:text/html,<title>bfcache test</title>away"}, nil); err != nil {
		return "", err
	}
	wait(5 * time.Second)
	if err := p.call(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": home.ID}, nil); err != nil {
		return "", err
	}
	wait(5 * time.Second)
	time.Sleep(200 * time.Millisecond) // the not-used event follows the commit
	var why []string
	select {
	case why = <-reasons:
	default:
	}
	state, err := p.evaluate(ctx, "window.__abmBFCache === 1", true)
	restored := err == nil && string(state.Value) == "true"
	switch {
	case restored:
		return fmt.Sprintf("%s was restored from the back/forward cache: going back is instant and keeps page state", home.URL), nil
	case len(why) > 0:
		return fmt.Sprintf("%s was not restored from the back/forward cache, so going back reloads it. Chrome's reasons (PageSupportNeeded ones the page can fix):\n- %s", home.URL, strings.Join(why, "\n- ")), nil
	}
	return fmt.Sprintf("%s was not restored from the back/forward cache, and Chrome gave no reason (the cache may be off in this browser)", home.URL), nil
}

// Frames lists the page's frames and workers with their origin, secure
// context and cross-origin isolation, like the Application panel's Frames.
func (p *Page) Frames(ctx context.Context) (string, error) {
	var res struct {
		FrameTree json.RawMessage `json:"frameTree"`
	}
	if err := p.call(ctx, "Page.getFrameTree", nil, &res); err != nil {
		return "", err
	}
	type frameInfo struct {
		Frame struct {
			ID                             string `json:"id"`
			Name                           string `json:"name"`
			URL                            string `json:"url"`
			SecurityOrigin                 string `json:"securityOrigin"`
			SecureContextType              string `json:"secureContextType"`
			CrossOriginIsolatedContextType string `json:"crossOriginIsolatedContextType"`
			AdFrameStatus                  *struct {
				AdFrameType string `json:"adFrameType"`
			} `json:"adFrameStatus"`
		} `json:"frame"`
		ChildFrames []json.RawMessage `json:"childFrames"`
	}
	var lines []string
	ours := map[string]bool{p.targetID: true} // this page's target and frame ids
	var walk func(raw json.RawMessage, depth int)
	walk = func(raw json.RawMessage, depth int) {
		var f frameInfo
		if json.Unmarshal(raw, &f) != nil {
			return
		}
		ours[f.Frame.ID] = true
		line := strings.Repeat("  ", depth) + f.Frame.URL
		if f.Frame.Name != "" {
			line += fmt.Sprintf(" (name %q)", f.Frame.Name)
		}
		line += fmt.Sprintf("  origin %s, %s", f.Frame.SecurityOrigin, f.Frame.SecureContextType)
		if f.Frame.CrossOriginIsolatedContextType != "" && f.Frame.CrossOriginIsolatedContextType != "NotIsolated" {
			line += ", " + f.Frame.CrossOriginIsolatedContextType
		}
		if f.Frame.AdFrameStatus != nil && f.Frame.AdFrameStatus.AdFrameType != "none" {
			line += ", ad frame"
		}
		lines = append(lines, line)
		for _, c := range f.ChildFrames {
			walk(c, depth+1)
		}
	}
	walk(res.FrameTree, 0)
	var targets struct {
		TargetInfos []struct {
			Type          string `json:"type"`
			URL           string `json:"url"`
			ParentID      string `json:"parentId"`
			ParentFrameID string `json:"parentFrameId"`
		} `json:"targetInfos"`
	}
	// Targets are browser-wide: keep this page's, which other tabs' iframes
	// and workers joined. A page kept in the back/forward cache has its own
	// copy of a worker, so repeats are counted.
	var extra []string
	if p.browserCall(ctx, "Target.getTargets", nil, &targets) == nil {
		for _, t := range targets.TargetInfos {
			if internalURL(t.URL) || (t.ParentID != "" || t.ParentFrameID != "") && !ours[t.ParentID] && !ours[t.ParentFrameID] {
				continue
			}
			switch t.Type {
			case "iframe":
				extra = append(extra, "  "+t.URL+"  (cross-site iframe, own process)")
			case "worker", "shared_worker", "service_worker":
				extra = append(extra, "  "+t.URL+"  ("+strings.ReplaceAll(t.Type, "_", " ")+")")
			}
		}
	}
	counts := map[string]int{}
	for _, l := range extra {
		if counts[l]++; counts[l] == 1 {
			lines = append(lines, l)
		}
	}
	for i, l := range lines {
		if n := counts[l]; n > 1 {
			lines[i] = fmt.Sprintf("%s ×%d", l, n)
		}
	}
	return "Frames:\n" + strings.Join(lines, "\n"), nil
}

// Security reports the connection and certificate of the page, as the
// Security panel does.
func (p *Page) Security(ctx context.Context) (string, error) {
	got := make(chan json.RawMessage, 1)
	stop := p.onSession("Security.visibleSecurityStateChanged", func(params json.RawMessage) {
		select {
		case got <- params:
		default:
		}
	})
	defer stop()
	if err := p.call(ctx, "Security.enable", nil, nil); err != nil {
		return "", err
	}
	var raw json.RawMessage
	select {
	case raw = <-got:
	case <-time.After(3 * time.Second):
		return "", fmt.Errorf("Chrome did not report a security state")
	case <-ctx.Done():
		return "", ctx.Err()
	}
	var e struct {
		State struct {
			SecurityState string   `json:"securityState"`
			Issues        []string `json:"securityStateIssueIds"`
			Cert          *struct {
				Protocol                string   `json:"protocol"`
				KeyExchange             string   `json:"keyExchange"`
				KeyExchangeGroup        string   `json:"keyExchangeGroup"`
				Cipher                  string   `json:"cipher"`
				SubjectName             string   `json:"subjectName"`
				Issuer                  string   `json:"issuer"`
				ValidTo                 float64  `json:"validTo"`
				Certificate             []string `json:"certificate"`
				ObsoleteSSLProto        bool     `json:"obsoleteSslProtocol"`
				ObsoleteSSLCipher       bool     `json:"obsoleteSslCipher"`
				WeakSignature           bool     `json:"certificateHasWeakSignature"`
				CertificateNetworkError string   `json:"certificateNetworkError"`
			} `json:"certificateSecurityState"`
		} `json:"visibleSecurityState"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return "", err
	}
	s := e.State
	lines := []string{"security state: " + s.SecurityState}
	if c := s.Cert; c != nil {
		kx := c.KeyExchange
		if c.KeyExchangeGroup != "" {
			kx = strings.Trim(kx+" "+c.KeyExchangeGroup, " ")
		}
		lines = append(lines,
			fmt.Sprintf("connection: %s, %s, %s", c.Protocol, kx, c.Cipher),
			fmt.Sprintf("certificate: %s issued by %s, valid until %s", c.SubjectName, c.Issuer, time.Unix(int64(c.ValidTo), 0).UTC().Format("2006-01-02")))
		for _, w := range []struct {
			on   bool
			text string
		}{{c.ObsoleteSSLProto, "obsolete TLS protocol"}, {c.ObsoleteSSLCipher, "obsolete cipher"}, {c.WeakSignature, "weak certificate signature"}} {
			if w.on {
				lines = append(lines, "warning: "+w.text)
			}
		}
		if c.CertificateNetworkError != "" {
			lines = append(lines, "certificate error: "+c.CertificateNetworkError)
		}
	} else {
		lines = append(lines, "no TLS: the page was not served over HTTPS")
	}
	if len(s.Issues) > 0 {
		lines = append(lines, "issues: "+strings.Join(s.Issues, ", "))
	}
	return strings.Join(lines, "\n"), nil
}

func internalURL(u string) bool {
	for _, p := range []string{"chrome:", "chrome-untrusted:", "chrome-extension:", "devtools:"} {
		if strings.HasPrefix(u, p) {
			return true
		}
	}
	return false
}
