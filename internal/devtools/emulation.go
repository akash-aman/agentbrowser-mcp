package devtools

import (
	"context"
	"strings"
)

// Emulation holds DevTools emulation overrides beyond what agent-browser
// sets: a focused page, JavaScript off, locale, timezone, media type, vision
// deficiencies and a disabled cache. A nil field leaves that override as it
// is. They last as long as this CDP connection, like throttling.
type Emulation struct {
	Focus            *bool
	JavaScript       *bool // false stops scripts from running
	Locale           *string
	Timezone         *string
	MediaType        *string // print or screen
	VisionDeficiency *string
	CacheDisabled    *bool
	UserAgent        *string // empty restores the browser's own
}

// VisionDeficiencies are the values Rendering's "Emulate vision deficiencies" offers.
var VisionDeficiencies = []string{"none", "blurredVision", "reducedContrast", "achromatopsia", "deuteranopia", "protanopia", "tritanopia"}

// SetEmulation applies the overrides and describes all that are in effect.
func (p *Page) SetEmulation(ctx context.Context, e Emulation) (string, error) {
	set := func(method string, params map[string]any, apply func(*Emulation)) error {
		if err := p.call(ctx, method, params, nil); err != nil {
			return err
		}
		p.mu.Lock()
		apply(&p.emulation)
		p.mu.Unlock()
		return nil
	}
	if e.Focus != nil {
		if err := set("Emulation.setFocusEmulationEnabled", map[string]any{"enabled": *e.Focus}, func(s *Emulation) { s.Focus = e.Focus }); err != nil {
			return "", err
		}
	}
	if e.JavaScript != nil {
		if err := set("Emulation.setScriptExecutionDisabled", map[string]any{"value": !*e.JavaScript}, func(s *Emulation) { s.JavaScript = e.JavaScript }); err != nil {
			return "", err
		}
	}
	if e.Locale != nil {
		params := map[string]any{} // no locale resets the override
		if *e.Locale != "" {
			params["locale"] = *e.Locale
		}
		if err := set("Emulation.setLocaleOverride", params, func(s *Emulation) { s.Locale = e.Locale }); err != nil {
			return "", err
		}
	}
	if e.Timezone != nil {
		if err := set("Emulation.setTimezoneOverride", map[string]any{"timezoneId": *e.Timezone}, func(s *Emulation) { s.Timezone = e.Timezone }); err != nil {
			return "", err
		}
	}
	if e.MediaType != nil {
		media := *e.MediaType
		if media == "screen" {
			media = "" // the default
		}
		if err := set("Emulation.setEmulatedMedia", map[string]any{"media": media}, func(s *Emulation) { s.MediaType = e.MediaType }); err != nil {
			return "", err
		}
	}
	if e.VisionDeficiency != nil {
		if err := set("Emulation.setEmulatedVisionDeficiency", map[string]any{"type": *e.VisionDeficiency}, func(s *Emulation) { s.VisionDeficiency = e.VisionDeficiency }); err != nil {
			return "", err
		}
	}
	if e.UserAgent != nil {
		ua := *e.UserAgent
		if ua == "" {
			// Clearing this override left the one agent-browser sets for
			// "emulate device" (an Android phone's) in force, so restore
			// the browser's own user agent explicitly.
			var v struct {
				UserAgent string `json:"userAgent"`
			}
			if err := p.browserCall(ctx, "Browser.getVersion", nil, &v); err == nil {
				ua = v.UserAgent
			}
		}
		if err := set("Emulation.setUserAgentOverride", map[string]any{"userAgent": ua}, func(s *Emulation) { s.UserAgent = e.UserAgent }); err != nil {
			return "", err
		}
	}
	if e.CacheDisabled != nil {
		if err := p.call(ctx, "Network.enable", nil, nil); err != nil {
			return "", err
		}
		if err := set("Network.setCacheDisabled", map[string]any{"cacheDisabled": *e.CacheDisabled}, func(s *Emulation) { s.CacheDisabled = e.CacheDisabled }); err != nil {
			return "", err
		}
	}
	return p.emulationState(), nil
}

func (p *Page) emulationState() string {
	p.mu.Lock()
	e := p.emulation
	p.mu.Unlock()
	var on []string
	if e.Focus != nil && *e.Focus {
		on = append(on, "page always focused")
	}
	if e.JavaScript != nil && !*e.JavaScript {
		on = append(on, "JavaScript off (reload to see the page without it)")
	}
	if e.Locale != nil && *e.Locale != "" {
		on = append(on, "locale "+*e.Locale)
	}
	if e.Timezone != nil && *e.Timezone != "" {
		on = append(on, "timezone "+*e.Timezone)
	}
	if e.MediaType != nil && *e.MediaType == "print" {
		on = append(on, "print media")
	}
	if e.VisionDeficiency != nil && *e.VisionDeficiency != "none" {
		on = append(on, "vision: "+*e.VisionDeficiency)
	}
	if e.CacheDisabled != nil && *e.CacheDisabled {
		on = append(on, "cache disabled")
	}
	if e.UserAgent != nil && *e.UserAgent != "" {
		on = append(on, "user agent "+*e.UserAgent)
	}
	if len(on) == 0 {
		return "overrides: none"
	}
	return "overrides: " + strings.Join(on, ", ") + " (until the browser closes or this server restarts)"
}

// GrantClipboard lets pages read and write the clipboard without the
// permission prompt a fresh profile shows (or denies, headless).
func (p *Page) GrantClipboard(ctx context.Context) error {
	return p.browserCall(ctx, "Browser.grantPermissions", map[string]any{"permissions": []string{"clipboardReadWrite", "clipboardSanitizedWrite"}}, nil)
}

// EnsureFocused makes the page act focused, as some APIs (writing to the
// clipboard) wait for focus that a background browser window never gets.
// The returned function restores the previous state.
func (p *Page) EnsureFocused(ctx context.Context) (restore func(), err error) {
	p.mu.Lock()
	on := p.emulation.Focus != nil && *p.emulation.Focus
	p.mu.Unlock()
	if on {
		return func() {}, nil
	}
	if err := p.call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, nil); err != nil {
		return func() {}, err
	}
	return func() {
		p.call(context.WithoutCancel(ctx), "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": false}, nil)
	}, nil
}
