package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

// SeparateContext returns the page's browser context when it is not the
// browser's default one, as for a window agent-browser opened with
// "window new": such a window has its own cookies and storage, and
// agent-browser does not set up downloads for it.
func (p *Page) SeparateContext(ctx context.Context) (string, bool) {
	var info struct {
		TargetInfo struct {
			BrowserContextID string `json:"browserContextId"`
		} `json:"targetInfo"`
	}
	var contexts struct {
		DefaultBrowserContextID string `json:"defaultBrowserContextId"`
	}
	if p.browserCall(ctx, "Target.getTargetInfo", map[string]any{"targetId": p.targetID}, &info) != nil ||
		p.browserCall(ctx, "Target.getBrowserContexts", nil, &contexts) != nil {
		return "", false
	}
	id := info.TargetInfo.BrowserContextID
	return id, id != "" && contexts.DefaultBrowserContextID != "" && id != contexts.DefaultBrowserContextID
}

// DownloadInContext saves the file that click starts in browser context
// contextID to path. agent-browser's download fails in a context it did not
// set up (the download is canceled, or saved where it does not look), so
// this sets the context to save into a temporary directory and waits for
// Chrome to finish.
func (p *Page) DownloadInContext(ctx context.Context, contextID, path string, click func() error, limit time.Duration) (string, error) {
	dir, err := os.MkdirTemp("", "agent-browser-mcp-download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)

	type result struct {
		guid, name, state string
		bytes             float64
	}
	var mu sync.Mutex
	names := map[string]string{} // guid -> suggested file name
	done := make(chan result, 1)
	stopBegin := p.conn.On("Browser.downloadWillBegin", func(ev cdp.Event) {
		var e struct {
			GUID              string `json:"guid"`
			SuggestedFilename string `json:"suggestedFilename"`
		}
		if ev.SessionID == "" && json.Unmarshal(ev.Params, &e) == nil {
			mu.Lock()
			names[e.GUID] = e.SuggestedFilename
			mu.Unlock()
		}
	})
	defer stopBegin()
	stopProgress := p.conn.On("Browser.downloadProgress", func(ev cdp.Event) {
		var e struct {
			GUID          string  `json:"guid"`
			State         string  `json:"state"`
			ReceivedBytes float64 `json:"receivedBytes"`
		}
		if ev.SessionID != "" || json.Unmarshal(ev.Params, &e) != nil || (e.State != "completed" && e.State != "canceled") {
			return
		}
		mu.Lock()
		name := names[e.GUID]
		mu.Unlock()
		select {
		case done <- result{e.GUID, name, e.State, e.ReceivedBytes}:
		default:
		}
	})
	defer stopProgress()

	behavior := map[string]any{"behavior": "allowAndName", "downloadPath": dir, "browserContextId": contextID, "eventsEnabled": true}
	if err := p.browserCall(ctx, "Browser.setDownloadBehavior", behavior, nil); err != nil {
		return "", err
	}
	defer p.browserCall(context.WithoutCancel(ctx), "Browser.setDownloadBehavior", map[string]any{"behavior": "default", "browserContextId": contextID}, nil)
	if err := click(); err != nil {
		return "", err
	}
	var got result
	select {
	case got = <-done:
	case <-time.After(limit):
		return "", fmt.Errorf("no download finished within %s of the click", limit)
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if got.state == "canceled" {
		return "", errors.New("the download was canceled")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := moveFile(filepath.Join(dir, got.guid), path); err != nil {
		return "", err
	}
	name := got.name
	if name == "" {
		name = "the file"
	}
	size := kb(got.bytes)
	if got.bytes < 1024 {
		size = fmt.Sprintf("%.0f B", got.bytes)
	}
	return fmt.Sprintf("downloaded %s to %s (%s)", name, path, size), nil
}

// moveFile renames src to dst, copying when they are on different volumes.
func moveFile(src, dst string) error {
	if os.Rename(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
