package devtools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

// Throttle is the network and CPU throttling applied to a page. Zero values
// mean no throttling.
type Throttle struct {
	LatencyMs    float64
	DownloadKbps float64
	UploadKbps   float64
	CPUSlowdown  float64
}

// NetworkProfiles are Chrome DevTools' network presets. "none" removes throttling.
var NetworkProfiles = map[string]Throttle{
	"none":    {},
	"slow-3g": {LatencyMs: 2000, DownloadKbps: 400, UploadKbps: 400},
	"fast-3g": {LatencyMs: 562.5, DownloadKbps: 1440, UploadKbps: 675},
	"slow-4g": {LatencyMs: 562.5, DownloadKbps: 1440, UploadKbps: 675},
	"fast-4g": {LatencyMs: 165, DownloadKbps: 8100, UploadKbps: 1350},
}

// NetworkProfileNames lists the presets in a stable order.
func NetworkProfileNames() []string {
	names := make([]string, 0, len(NetworkProfiles))
	for name := range NetworkProfiles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ruleParams is the Network.emulateNetworkConditionsByRule request: one rule
// with an empty URL pattern matches every request; no rules removes throttling.
func (t Throttle) ruleParams() map[string]any {
	rules := []any{}
	if t.LatencyMs > 0 || t.DownloadKbps > 0 || t.UploadKbps > 0 {
		rule := t.networkParams()
		delete(rule, "offline")
		rule["urlPattern"] = ""
		rules = append(rules, rule)
	}
	return map[string]any{"matchedNetworkConditions": rules}
}

// networkParams converts kbps to the bytes/second CDP expects; -1 disables a
// limit. It is the deprecated Network.emulateNetworkConditions request, used
// on browsers too old for rules.
func (t Throttle) networkParams() map[string]any {
	return map[string]any{
		"offline":            false,
		"latency":            t.LatencyMs,
		"downloadThroughput": kbpsToBytes(t.DownloadKbps),
		"uploadThroughput":   kbpsToBytes(t.UploadKbps),
	}
}

func kbpsToBytes(kbps float64) float64 {
	if kbps <= 0 {
		return -1
	}
	return kbps * 1000 / 8
}

// SetNetwork applies network throttling to the page.
func (p *Page) SetNetwork(ctx context.Context, t Throttle) error {
	if err := p.call(ctx, "Network.enable", nil, nil); err != nil {
		return err
	}
	err := p.call(ctx, "Network.emulateNetworkConditionsByRule", t.ruleParams(), nil)
	if isMethodNotFound(err) {
		err = p.call(ctx, "Network.emulateNetworkConditions", t.networkParams(), nil)
	}
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.throttle.LatencyMs, p.throttle.DownloadKbps, p.throttle.UploadKbps = t.LatencyMs, t.DownloadKbps, t.UploadKbps
	return nil
}

// isMethodNotFound reports a CDP "method not found" error from an older browser.
func isMethodNotFound(err error) bool {
	var protoErr *cdp.Error
	return errors.As(err, &protoErr) && protoErr.Code == -32601
}

// SetCPU slows the page's CPU by rate (1 = no slowdown, 4 = mid-tier mobile).
func (p *Page) SetCPU(ctx context.Context, rate float64) error {
	if rate < 1 {
		return fmt.Errorf("cpuSlowdown must be >= 1, got %g", rate)
	}
	if err := p.call(ctx, "Emulation.setCPUThrottlingRate", map[string]any{"rate": rate}, nil); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.throttle.CPUSlowdown = rate
	return nil
}

// Throttle returns the throttling currently applied by this connection.
func (p *Page) Throttle() Throttle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.throttle
}

// String describes the throttling for a tool result.
func (t Throttle) String() string {
	var parts []string
	if t.LatencyMs > 0 || t.DownloadKbps > 0 || t.UploadKbps > 0 {
		parts = append(parts, fmt.Sprintf("network latency %gms, down %s, up %s", t.LatencyMs, rate(t.DownloadKbps), rate(t.UploadKbps)))
	} else {
		parts = append(parts, "network unthrottled")
	}
	if t.CPUSlowdown > 1 {
		parts = append(parts, fmt.Sprintf("CPU %gx slower", t.CPUSlowdown))
	} else {
		parts = append(parts, "CPU unthrottled")
	}
	return strings.Join(parts, "; ")
}

func rate(kbps float64) string {
	if kbps <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%gkbps", kbps)
}
