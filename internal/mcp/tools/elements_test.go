package tools

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecdp"
)

var cssOverviewFixture = a{
	"elements": 42, "text": []any{[]any{"rgb(31, 35, 40)", 30}}, "backgrounds": []any{[]any{"rgb(255, 255, 255)", 5}},
	"borders": []any{}, "fonts": []any{[]any{"Inter 16px 400", 20}}, "distinct": a{"text": 3, "backgrounds": 2, "fonts": 4},
	"sheets": 2, "blocked": 1, "rules": 120, "unused": 40, "unusedExamples": []any{".old"}, "media": []any{"@media (max-width: 600px)"},
}

// elementsFixture gives the fake browser what the Elements features read:
// a button#buy.primary with matched rules, computed values, a box, an
// accessibility node and a background color.
func elementsFixture(s *fakecdp.Server) {
	r := func(result any) fakecdp.Reply { return fakecdp.Reply{Result: result} }
	decl := func(name, value string) a { return a{"name": name, "value": value, "text": name + ": " + value + ";"} }
	rule := func(sheet, origin, selector string, line int, props ...any) a {
		return a{"rule": a{"styleSheetId": sheet, "origin": origin, "selectorList": a{"text": selector, "selectors": []any{a{"text": selector}}},
			"style": a{"cssProperties": props, "range": a{"startLine": line}}}, "matchingSelectors": []any{0}}
	}
	hover := rule("css1", "regular", "button:hover", 7, decl("background", "#d65a3c"))
	hover["rule"].(a)["media"] = []any{a{"text": "(hover: hover)"}}
	s.Reply("CSS.getMatchedStylesForNode", r(a{
		"inlineStyle": a{"cssProperties": []any{decl("color", "red")}},
		"matchedCSSRules": []any{
			rule("ua", "user-agent", "button", 0, decl("padding", "1px 6px")),
			// background's longhands come without text, as CDP sends them.
			rule("css1", "regular", "button", 3, decl("background", "#e76f51"), decl("color", "white"),
				a{"name": "background-color", "value": "rgb(231, 111, 81)"}, a{"name": "background-image", "value": "initial"}),
			hover,
		},
		"inherited": []any{a{"matchedCSSRules": []any{rule("css1", "regular", ".card", 1, decl("padding", "16px"), decl("font-size", "15px"))}}},
	}))
	var computed []any
	for _, kv := range [][2]string{
		{"display", "flex"}, {"flex-direction", "row"}, {"color", "rgb(255, 255, 255)"}, {"font-family", "Inter"}, {"font-size", "15px"},
		{"font-weight", "600"}, {"width", "120px"}, {"container-type", "normal"},
		{"margin-top", "0px"}, {"margin-right", "0px"}, {"margin-bottom", "12px"}, {"margin-left", "0px"},
		{"padding-top", "10px"}, {"padding-right", "10px"}, {"padding-bottom", "10px"}, {"padding-left", "10px"},
	} {
		computed = append(computed, a{"name": kv[0], "value": kv[1]})
	}
	s.Reply("CSS.getComputedStyleForNode", r(a{"computedStyle": computed}))
	s.Reply("DOM.getBoxModel", r(a{"model": a{
		"margin":  []any{10, 20, 130, 20, 130, 72, 10, 72},
		"border":  []any{10, 20, 130, 20, 130, 60, 10, 60},
		"padding": []any{11, 21, 129, 21, 129, 59, 11, 59},
		"content": []any{21, 31, 119, 31, 119, 49, 21, 49},
		"width":   120, "height": 40,
	}}))
	s.Reply("Accessibility.getPartialAXTree", r(a{"nodes": []any{a{
		"role": a{"type": "role", "value": "button"},
		"name": a{"type": "computedString", "value": "Buy", "sources": []any{
			a{"type": "relatedElement", "attribute": "aria-labelledby"},
			a{"type": "contents", "value": a{"type": "computedString", "value": "Buy"}},
		}},
		"properties": []any{
			a{"name": "focusable", "value": a{"type": "booleanOrUndefined", "value": true}},
			a{"name": "level", "value": a{"type": "integer", "value": 2}},
		},
	}}}))
	s.Reply("Accessibility.queryAXTree", r(a{"nodes": []any{a{"backendDOMNodeId": 11}, a{"backendDOMNodeId": 12}}}))
	s.Reply("DOM.describeNode", r(a{"node": a{"backendNodeId": 12}}))
	s.Reply("CSS.getBackgroundColors", r(a{"backgroundColors": []any{"rgb(231, 111, 81)"}, "computedFontSize": "15px", "computedFontWeight": "600"}))
	s.Reply("DOM.requestNode", r(a{"nodeId": 9}))
	s.Reply("DOM.performSearch", r(a{"searchId": "s1", "resultCount": 2}))
	s.Reply("DOM.getSearchResults", r(a{"nodeIds": []any{9, 10}}))
	s.Reply("DOM.resolveNode", r(a{"object": a{"type": "object", "objectId": "node-1"}}))
	s.Handle("Runtime.callFunctionOn", func(p map[string]any) fakecdp.Reply {
		fn, _ := p["functionDeclaration"].(string)
		switch {
		case strings.Contains(fn, "JSON.stringify(this)"):
			if p["objectId"] == "obj-json" {
				return r(a{"result": a{"type": "string", "value": `{"a":1}`}})
			}
			return r(a{"result": a{"type": "string", "value": "{}"}})
		case strings.Contains(fn, "ancestors"):
			return r(a{"result": a{"type": "object", "value": []any{"div.card", "body"}}})
		case strings.Contains(fn, "classList"):
			return r(a{"result": a{"type": "string", "value": "button#buy.primary"}})
		case strings.Contains(fn, "data-testid"):
			return r(a{"result": a{"type": "object", "value": a{"css": "#buy", "xpath": "/html/body/button", "text": "Buy"}}})
		case strings.Contains(fn, "getOwnPropertyDescriptors"):
			return r(a{"result": a{"type": "object", "value": []any{[]any{"readyState", "1"}, []any{"url", `"ws://fake/live"`}}}})
		case strings.Contains(fn, "this.length"):
			return r(a{"result": a{"type": "number", "value": 3}})
		case strings.Contains(fn, "cssText"):
			return r(a{"result": a{"type": "string", "value": "color: red"}})
		}
		return r(a{"result": a{"type": "undefined"}})
	})
}

func init() {
	cdpCases = append(cdpCases, elementsCases...)
	for _, v := range devtools.VisionDeficiencies {
		cdpCases = append(cdpCases, cdpCase{tool: "emulate", args: a{"visionDeficiency": v},
			want: []string{"Emulation.setEmulatedVisionDeficiency"}, check: params("Emulation.setEmulatedVisionDeficiency", a{"type": v})})
	}
}

const allOverrides = "overrides: page always focused, JavaScript off (reload to see the page without it), locale de-DE, timezone Asia/Tokyo, " +
	"print media, vision: deuteranopia, cache disabled (until the browser closes or this server restarts)"

var elementsCases = []cdpCase{
	{tool: "elements", args: a{"action": "styles", "selector": "#buy"},
		want: []string{"DOM.enable", "DOM.getDocument", "Runtime.evaluate", "DOM.requestNode", "Runtime.callFunctionOn", "CSS.enable", "CSS.getMatchedStylesForNode", "Runtime.callFunctionOn"},
		text: "Styles for button#buy.primary, highest precedence first:\n" +
			"element.style\n  color: red\n" +
			"@media (hover: hover) button:hover  a.css:8\n  background: #d65a3c\n" +
			"button  a.css:4\n  background: #e76f51   [overridden]\n  color: white   [overridden]\n" +
			"Inherited from div.card:\n  .card  a.css:2\n    font-size: 15px\n" +
			"User agent styles: button"},
	// Without properties, absent ones are skipped and flex layout values join the list.
	{tool: "elements", args: a{"action": "computed", "selector": "#buy"},
		text: "Computed style of button#buy.primary:\ndisplay: flex\nwidth: 120px\nmargin: 0px 0px 12px\npadding: 10px\ncolor: rgb(255, 255, 255)\nfont-family: Inter\nfont-size: 15px\nfont-weight: 600\nflex-direction: row"},
	{tool: "elements", args: a{"action": "computed", "selector": "#buy", "properties": []any{"font", "margin", "nope"}},
		text: "Computed style of button#buy.primary:\nfont-family: Inter\nfont-size: 15px\nfont-weight: 600\nmargin: 0px 0px 12px\nnope: (no such property)"},
	{tool: "elements", args: a{"action": "box", "selector": "#buy"},
		text: "size    120 × 40 (border box)\nmargin  0 0 12 0\nborder  1 1 1 1\npadding 10 10 10 10\ncontent 98 × 18"},
	{tool: "elements", args: a{"action": "a11y", "selector": "#buy"},
		text: "role: button\nname: \"Buy\" (from contents)\nstates: focusable\nproperties: level=2"},
	{tool: "elements", args: a{"action": "contrast", "selector": "#buy"},
		text: "Contrast of button#buy.primary: 3.09:1, rgb(255, 255, 255) on rgb(231, 111, 81), 15px 600 (normal text)\nWCAG AA FAILS (needs 4.5); AAA FAILS (needs 7)"},
	{tool: "elements", args: a{"action": "force_state", "selector": "#buy", "states": anys(devtools.PseudoStates)},
		text: "forcing :hover, :focus", check: params("CSS.forcePseudoState", a{"nodeId": 9.0, "forcedPseudoClasses": anys(devtools.PseudoStates)})},
	{tool: "elements", args: a{"action": "set_style", "selector": "#buy", "value": "color: red"}, text: `button#buy.primary style="color: red" (until the page reloads)`},
	{tool: "elements", args: a{"action": "set_attribute", "selector": "#buy", "name": "aria-label", "value": "Buy now"},
		text: "set aria-label on button#buy.primary", check: params("DOM.setAttributeValue", a{"nodeId": 9.0, "name": "aria-label", "value": "Buy now"})},
	{tool: "elements", args: a{"action": "set_html", "selector": "#buy", "value": "<b>x</b>"}, text: "replaced button#buy.primary",
		check: params("DOM.setOuterHTML", a{"outerHTML": "<b>x</b>"})},
	{tool: "elements", args: a{"action": "remove", "selector": "#buy"}, text: "removed button#buy.primary", check: params("DOM.removeNode", a{"nodeId": 9.0})},
	{tool: "elements", args: a{"action": "hide", "selector": "#buy"}, text: "hid button#buy.primary"},
	{tool: "elements", args: a{"action": "overlay", "selector": "#buy"}, text: "flex overlay on button#buy.primary", check: overlayFor("Overlay.setShowFlexOverlays", "flexNodeHighlightConfigs")},
	{tool: "elements", args: a{"action": "overlay_off"}, text: "grid, flex and container overlays off"},
	{tool: "elements", args: a{"action": "selector", "selector": "#buy"},
		text: "css: #buy\nxpath: /html/body/button\nplaywright: page.getByRole(\"button\", { name: \"Buy\" }).nth(1)  (2 elements share this role and name)"},
	{tool: "elements", args: a{"action": "search", "query": "Buy"}, text: "1 element matches \"Buy\":\n#buy  \"Buy\"",
		check: params("DOM.performSearch", a{"query": "Buy"})},
	{tool: "elements", args: a{"action": "css_overview"},
		text: "CSS overview of 42 elements, 2 style sheets (1 cross-origin, not readable), 120 rules:\ntext colors (3): rgb(31, 35, 40) ×30"},
	{tool: "elements", args: a{"action": "styles"}, wantErr: "selector is required for styles"},
	{tool: "elements", args: a{"action": "set_attribute", "selector": "#buy"}, wantErr: "name is required for set_attribute"},
	{tool: "elements", args: a{"action": "search"}, wantErr: "query is required for search"},

	{tool: "emulate", args: a{"focus": true, "javaScript": false, "locale": "de-DE", "timezone": "Asia/Tokyo", "mediaType": "print", "visionDeficiency": "deuteranopia", "cacheDisabled": true},
		want: []string{"Emulation.setFocusEmulationEnabled", "Emulation.setScriptExecutionDisabled", "Emulation.setLocaleOverride", "Emulation.setTimezoneOverride",
			"Emulation.setEmulatedMedia", "Emulation.setEmulatedVisionDeficiency", "Network.enable", "Network.setCacheDisabled"},
		text: allOverrides, check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			for method, want := range map[string]a{
				"Emulation.setFocusEmulationEnabled": {"enabled": true}, "Emulation.setScriptExecutionDisabled": {"value": true},
				"Emulation.setLocaleOverride": {"locale": "de-DE"}, "Emulation.setTimezoneOverride": {"timezoneId": "Asia/Tokyo"},
				"Emulation.setEmulatedMedia": {"media": "print"}, "Network.setCacheDisabled": {"cacheDisabled": true},
			} {
				params(method, want)(t, s)
			}
		}},
	{tool: "emulate", args: a{"mediaType": "screen", "locale": ""}, text: "overrides: none", check: func(t *testing.T, s *fakecdp.Server) {
		t.Helper()
		params("Emulation.setEmulatedMedia", a{"media": ""})(t, s)
		if _, set := s.Params("Emulation.setLocaleOverride")["locale"]; set || !slices.Contains(s.Methods(), "Emulation.setLocaleOverride") {
			t.Errorf("an empty locale must reset the override, got %v", s.Params("Emulation.setLocaleOverride"))
		}
	}},
	{tool: "emulate", args: a{"visionDeficiency": "sepia"}, wantErr: "visionDeficiency"},
	{tool: "emulate", args: a{"userAgent": "UA/1"}, want: []string{"Emulation.setUserAgentOverride"}, text: "user agent UA/1",
		check: params("Emulation.setUserAgentOverride", a{"userAgent": "UA/1"})},
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// overlayFor checks an overlay call was made for node 9.
func overlayFor(method, key string) func(*testing.T, *fakecdp.Server) {
	return func(t *testing.T, s *fakecdp.Server) {
		t.Helper()
		configs, _ := s.Params(method)[key].([]any)
		if len(configs) != 1 || fmt.Sprint(configs[0].(map[string]any)["nodeId"]) != "9" {
			t.Errorf("%s %s = %v, want one config for node 9", method, key, configs)
		}
	}
}

// TestElementsFindsRefsByTheirBox: CDP cannot see agent-browser's @refs, so
// the ref is scrolled into view and found again at the centre of its box.
func TestElementsFindsRefsByTheirBox(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	s := e.withCDP()
	e.fake.Respond("get box", `{"x":10,"y":20,"width":100,"height":40}`)
	if res := e.call("elements", a{"action": "box", "selector": "@e3"}); res.IsError {
		t.Fatal(res.text())
	}
	var cli []string
	for _, c := range e.fake.Commands() {
		cli = append(cli, strings.Join(c, " "))
	}
	for _, want := range []string{"scrollintoview @e3", "get box @e3"} {
		if !slices.Contains(cli, want) {
			t.Errorf("CLI calls %q lack %q", cli, want)
		}
	}
	expr, _ := s.Params("Runtime.evaluate")["expression"].(string)
	if !strings.Contains(expr, "elementFromPoint") || !strings.Contains(expr, "[10, 20, 100, 40]") {
		t.Errorf("ref not looked up by its box:\n%s", expr)
	}
}

func TestElementsRefusesHiddenRefs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.fake.Respond("get box", `{"x":0,"y":0,"width":0,"height":0}`)
	res := e.call("elements", a{"action": "styles", "selector": "@e3"})
	if !res.IsError || !strings.Contains(res.text(), "@e3 has no box: it is hidden or not rendered") {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}
