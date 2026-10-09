package cdp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// TargetInfo describes a browser target such as a tab.
type TargetInfo struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Title    string `json:"title"`
}

// Pages lists the browser's page targets (tabs).
func (c *Conn) Pages(ctx context.Context) ([]TargetInfo, error) {
	var res struct {
		TargetInfos []TargetInfo `json:"targetInfos"`
	}
	if err := c.Call(ctx, "", "Target.getTargets", nil, &res); err != nil {
		return nil, err
	}
	var pages []TargetInfo
	for _, t := range res.TargetInfos {
		if t.Type == "page" {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// Target returns the current info for one target, e.g. its URL after navigation.
func (c *Conn) Target(ctx context.Context, targetID string) (TargetInfo, error) {
	var res struct {
		TargetInfo TargetInfo `json:"targetInfo"`
	}
	err := c.Call(ctx, "", "Target.getTargetInfo", map[string]any{"targetId": targetID}, &res)
	return res.TargetInfo, err
}

// Attach opens a flattened session on a target; pass the returned session ID
// to Call to talk to that target.
func (c *Conn) Attach(ctx context.Context, targetID string) (string, error) {
	var res struct {
		SessionID string `json:"sessionId"`
	}
	err := c.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, &res)
	return res.SessionID, err
}

// Port returns the TCP port of a CDP WebSocket URL such as
// ws://127.0.0.1:9222/devtools/browser/<id>.
func Port(wsURL string) (int, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return 0, fmt.Errorf("parse CDP URL: %w", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0, fmt.Errorf("CDP URL %q has no port", wsURL)
	}
	return port, nil
}
