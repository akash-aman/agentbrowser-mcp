package devtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/cdp"
)

// animationsJS lists the running animations and transitions, like the
// Animations panel.
const animationsJS = `(() => document.getAnimations().slice(0, 50).map((a) => {
  const fx = a.effect, t = fx && fx.target, timing = fx ? fx.getComputedTiming() : {};
  let target = "";
  if (t) {
    target = t.localName + (t.id ? "#" + t.id : "") + [...t.classList].slice(0, 2).map((c) => "." + c).join("");
    if (fx.pseudoElement) target += fx.pseudoElement;
  }
  return { type: a.constructor.name, name: a.animationName || a.transitionProperty || a.id || "", target, state: a.playState,
    duration: timing.duration, iterations: timing.iterations === Infinity ? -1 : timing.iterations };
}))()`

// Animations lists the page's animations and, with rate, changes how fast
// they all play, as the Animations panel's speed buttons do (0 pauses).
func (p *Page) Animations(ctx context.Context, rate *float64) (string, error) {
	obj, err := p.evaluate(ctx, animationsJS, true)
	if err != nil {
		return "", err
	}
	var list []struct {
		Type, Name, Target, State string
		Duration                  any
		Iterations                float64
	}
	json.Unmarshal(obj.Value, &list)
	lines := []string{plural(len(list), "animation") + ":"}
	if len(list) == 0 {
		lines[0] = "no animations or transitions are running"
	}
	for _, a := range list {
		times := fmt.Sprintf("%g×", a.Iterations)
		if a.Iterations < 0 {
			times = "infinite"
		}
		lines = append(lines, fmt.Sprintf("  %s %s on %s: %s, %v ms, %s", a.Type, a.Name, a.Target, a.State, a.Duration, times))
	}
	if rate != nil {
		if err := p.call(ctx, "Animation.enable", nil, nil); err != nil {
			return "", err
		}
		if err := p.call(ctx, "Animation.setPlaybackRate", map[string]any{"playbackRate": *rate}, nil); err != nil {
			return "", err
		}
		switch *rate {
		case 0:
			lines = append(lines, "paused every animation; playbackRate 1 resumes")
		case 1:
			lines = append(lines, "animations play at normal speed")
		default:
			lines = append(lines, fmt.Sprintf("animations play at %g× speed; playbackRate 1 restores", *rate))
		}
	}
	return strings.Join(lines, "\n"), nil
}

// SetAuthenticator adds a virtual platform authenticator (passkeys, Touch ID
// style) that approves every request, or removes it, as the WebAuthn panel
// does.
func (p *Page) SetAuthenticator(ctx context.Context, on bool) (string, error) {
	p.mu.Lock()
	id := p.authenticator
	p.mu.Unlock()
	if !on {
		if id == "" {
			return "no virtual authenticator", nil
		}
		p.call(ctx, "WebAuthn.removeVirtualAuthenticator", map[string]any{"authenticatorId": id}, nil)
		p.mu.Lock()
		p.authenticator = ""
		p.mu.Unlock()
		return "removed the virtual authenticator", p.call(ctx, "WebAuthn.disable", nil, nil)
	}
	if id != "" {
		return "virtual authenticator already on", nil
	}
	if err := p.call(ctx, "WebAuthn.enable", map[string]any{"enableUI": false}, nil); err != nil {
		return "", err
	}
	var res struct {
		AuthenticatorID string `json:"authenticatorId"`
	}
	err := p.call(ctx, "WebAuthn.addVirtualAuthenticator", map[string]any{"options": map[string]any{
		"protocol": "ctap2", "transport": "internal", "hasResidentKey": true, "hasUserVerification": true,
		"isUserVerified": true, "automaticPresenceSimulation": true,
	}}, &res)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.authenticator = res.AuthenticatorID
	p.mu.Unlock()
	return "virtual passkey authenticator on: registrations and sign-ins succeed without a prompt; application credentials lists the passkeys", nil
}

// Credentials lists the passkeys stored in the virtual authenticator.
func (p *Page) Credentials(ctx context.Context) (string, error) {
	p.mu.Lock()
	id := p.authenticator
	p.mu.Unlock()
	if id == "" {
		return "", fmt.Errorf("no virtual authenticator; emulate authenticator:true adds one")
	}
	var res struct {
		Credentials []struct {
			CredentialID string `json:"credentialId"`
			RpID         string `json:"rpId"`
			UserHandle   string `json:"userHandle"`
			SignCount    int    `json:"signCount"`
			IsResident   bool   `json:"isResidentCredential"`
		} `json:"credentials"`
	}
	if err := p.call(ctx, "WebAuthn.getCredentials", map[string]any{"authenticatorId": id}, &res); err != nil {
		return "", err
	}
	if len(res.Credentials) == 0 {
		return "the virtual authenticator holds no passkeys yet", nil
	}
	lines := []string{plural(len(res.Credentials), "passkey") + ":"}
	for _, c := range res.Credentials {
		lines = append(lines, fmt.Sprintf("  %s for %s, user %s, used %d times, discoverable %v", shorten(c.CredentialID, 24), c.RpID, c.UserHandle, c.SignCount, c.IsResident))
	}
	return strings.Join(lines, "\n"), nil
}

// Raw sends any Chrome DevTools Protocol command, like the Protocol monitor,
// to the page or, with browser, to the browser. With events, it also
// collects events whose name starts with it for wait after the call.
func (p *Page) Raw(ctx context.Context, method string, params json.RawMessage, browser bool, events string, wait time.Duration) (string, error) {
	var mu sync.Mutex
	var seen []string
	if events != "" {
		stop := p.conn.On(events+"*", func(ev cdp.Event) {
			if !browser && ev.SessionID != p.sessionID {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) < 50 {
				seen = append(seen, ev.Method+" "+shorten(string(ev.Params), 400))
			}
		})
		defer stop()
	}
	var arg any
	if len(params) > 0 {
		arg = params
	}
	var result json.RawMessage
	var err error
	if browser {
		err = p.browserCall(ctx, method, arg, &result)
	} else {
		err = p.call(ctx, method, arg, &result)
	}
	if err != nil {
		return "", err
	}
	if events != "" && wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
	text := string(result)
	if text == "" || text == "null" {
		text = "{}"
	}
	mu.Lock()
	defer mu.Unlock()
	if events != "" {
		text += fmt.Sprintf("\n%d %s* events:", len(seen), events)
		for _, s := range seen[:min(10, len(seen))] {
			text += "\n  " + s
		}
		if len(seen) > 10 {
			text += fmt.Sprintf("\n  … %d more", len(seen)-10)
		}
	}
	return text, nil
}

// StableSelector picks how a test should find the element again: a unique
// role and name when it has them, else a unique CSS selector.
func (p *Page) StableSelector(ctx context.Context, el *Element) (role, name, css string, err error) {
	var info elementInfo
	if err := p.callOn(ctx, el.ObjectID, elementInfoJS, nil, &info); err != nil {
		return "", "", "", err
	}
	if n, err := p.axNode(ctx, el); err == nil && n.Role != nil && n.Name != nil {
		role, name = fmt.Sprint(n.Role.Value), fmt.Sprint(n.Name.Value)
		var res struct {
			Nodes []json.RawMessage `json:"nodes"`
		}
		unique := p.call(ctx, "Accessibility.queryAXTree", map[string]any{"nodeId": rootNode(ctx, p), "accessibleName": name, "role": role}, &res) == nil && len(res.Nodes) == 1
		if name == "" || role == "generic" || role == "none" || !unique {
			role, name = "", ""
		}
	}
	return role, name, info.CSS, nil
}
