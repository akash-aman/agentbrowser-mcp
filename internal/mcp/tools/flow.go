package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// locator is how an exported script finds an element: by role and name
// when they are unique, else by CSS selector.
type locator struct {
	role, name, css, ref string
	pw, pptr             string // ready-made Playwright locator and Puppeteer selector, from find
}

// flowStep is one recorded action, as DevTools' Recorder keeps them.
type flowStep struct {
	kind   string // goto back forward reload click dblclick fill type keys press select check uncheck hover focus scroll wait_text wait_url wait_element
	target locator
	value  string
	values []string
}

type flow struct {
	steps []flowStep
}

// flowTools are the tools whose calls a flow records.
var flowTools = map[string]bool{"navigate": true, "click": true, "fill": true, "type": true, "press_key": true,
	"select_option": true, "element_action": true, "scroll": true, "wait": true, "find": true}

func (r *Registry) activeFlow(session string) *flow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flows[r.mgr.ResolveSession(session)]
}

// recordFlow wraps an action tool so that, while a flow is being recorded,
// each successful call becomes a step. Element refs are turned into stable
// locators before the action, since the element may be gone after it.
func (r *Registry) recordFlow(name string, h server.ToolHandlerFunc) server.ToolHandlerFunc {
	if !flowTools[name] {
		return h
	}
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f := r.activeFlow(getSession(req))
		if f == nil {
			return h(ctx, req)
		}
		step, ok := r.flowStep(ctx, name, req)
		res, err := h(ctx, req)
		if ok && res != nil && !res.IsError {
			r.mu.Lock()
			f.steps = append(f.steps, step)
			r.mu.Unlock()
		}
		return res, err
	}
}

func (r *Registry) flowStep(ctx context.Context, tool string, req mcp.CallToolRequest) (flowStep, bool) {
	str := func(k string) string { return req.GetString(k, "") }
	target := func() locator { return r.locatorFor(ctx, req, str("selector")) }
	switch tool {
	case "navigate":
		switch a := req.GetString("action", "goto"); a {
		case "goto", "pushstate":
			return flowStep{kind: "goto", value: str("url")}, str("url") != ""
		default:
			return flowStep{kind: a}, true
		}
	case "click":
		if req.GetBool("double", false) {
			return flowStep{kind: "dblclick", target: target()}, true
		}
		return flowStep{kind: "click", target: target()}, true
	case "fill":
		return flowStep{kind: "fill", target: target(), value: str("value")}, true
	case "type":
		if str("selector") == "" || req.GetString("mode", "element") != "element" {
			return flowStep{kind: "keys", value: str("text")}, true
		}
		return flowStep{kind: "type", target: target(), value: str("text")}, true
	case "press_key":
		return flowStep{kind: "press", value: str("key")}, req.GetString("action", "press") == "press"
	case "select_option":
		return flowStep{kind: "select", target: target(), values: req.GetStringSlice("values", nil)}, true
	case "element_action":
		switch a := str("action"); a {
		case "check", "uncheck", "hover", "focus":
			return flowStep{kind: a, target: target()}, true
		}
	case "scroll":
		px := int(req.GetFloat("px", 300))
		dx, dy := 0, px
		switch req.GetString("direction", "down") {
		case "up":
			dy = -px
		case "left":
			dx, dy = -px, 0
		case "right":
			dx, dy = px, 0
		}
		return flowStep{kind: "scroll", value: fmt.Sprintf("%d, %d", dx, dy)}, true
	case "find":
		loc, ok := findLocator(req)
		if !ok {
			return flowStep{}, false
		}
		switch a := req.GetString("action", "click"); a {
		case "click", "hover", "check":
			return flowStep{kind: a, target: loc}, true
		case "fill":
			return flowStep{kind: a, target: loc, value: str("input")}, true
		}
	case "wait":
		switch str("for") {
		case "text":
			return flowStep{kind: "wait_text", value: str("value")}, true
		case "url":
			return flowStep{kind: "wait_url", value: str("value")}, true
		case "element":
			return flowStep{kind: "wait_element", target: target()}, true
		}
	}
	return flowStep{}, false
}

// locatorFor resolves a selector argument; an @ref is looked up in the page
// so the script does not depend on refs, which only this session knows.
func (r *Registry) locatorFor(ctx context.Context, req mcp.CallToolRequest, sel string) locator {
	if !strings.HasPrefix(sel, "@") {
		return locator{css: sel}
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return locator{ref: sel}
	}
	t, err := r.elementTarget(ctx, req)
	if err != nil {
		return locator{ref: sel}
	}
	el, err := page.Element(ctx, t)
	if err != nil {
		return locator{ref: sel}
	}
	role, name, css, err := page.StableSelector(ctx, el)
	if err != nil {
		return locator{ref: sel}
	}
	return locator{role: role, name: name, css: css, ref: sel}
}

// startFlow begins recording, starting from the current page.
func (r *Registry) startFlow(ctx context.Context, req mcp.CallToolRequest) string {
	f := &flow{}
	if out, err := r.mgr.Run(ctx, getSession(req), "get", "url"); err == nil {
		if u := dataField(out.Data, "url"); strings.HasPrefix(u, "http") {
			f.steps = append(f.steps, flowStep{kind: "goto", value: u})
		}
	}
	r.mu.Lock()
	r.flows[r.mgr.ResolveSession(getSession(req))] = f
	r.mu.Unlock()
	return "recording this session's actions as a flow from the current page; flow_export returns them as a test script"
}

// exportFlow renders the flow so far as a Playwright test or a Puppeteer
// script, like the Recorder's export. Recording goes on, so the flow can be
// exported in both formats or extended; flow_start starts over.
func (r *Registry) exportFlow(req mcp.CallToolRequest) (string, error) {
	key := r.mgr.ResolveSession(getSession(req))
	r.mu.Lock()
	f := r.flows[key]
	r.mu.Unlock()
	if f == nil {
		return "", fmt.Errorf("no flow is being recorded; start one with record action:flow_start, then act")
	}
	if len(f.steps) == 0 {
		return "", fmt.Errorf("the flow recorded no actions")
	}
	if req.GetString("format", "playwright") == "puppeteer" {
		return f.puppeteer(), nil
	}
	return f.playwright(), nil
}

func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// findLocator turns a find call into the matching Playwright locator and,
// where Puppeteer has one, a selector (its ::-p-aria and ::-p-text).
// find acts on the first match, so the Playwright locators say .first():
// three "Add to cart" buttons made getByRole fail Playwright's strict mode.
func findLocator(req mcp.CallToolRequest) (locator, bool) {
	l, ok := semanticLocator(req)
	if !ok {
		return l, false
	}
	switch req.GetString("by", "") {
	case "role", "text", "label", "placeholder", "alt", "title", "testid":
		l.pw += ".first()"
	}
	return l, ok
}

func semanticLocator(req mcp.CallToolRequest) (locator, bool) {
	value, name := req.GetString("value", ""), req.GetString("name", "")
	opts := ""
	if req.GetBool("exact", false) {
		opts = ", { exact: true }"
	}
	attr := func(a string) string { return "[" + a + "=" + q(value) + "]" }
	switch by := req.GetString("by", ""); by {
	case "role":
		if name == "" {
			return locator{pw: fmt.Sprintf("page.getByRole(%s)", q(value)), pptr: attr("role")}, true
		}
		exact := ""
		if opts != "" {
			exact = ", exact: true"
		}
		return locator{pw: fmt.Sprintf("page.getByRole(%s, { name: %s%s })", q(value), q(name), exact),
			pptr: fmt.Sprintf("::-p-aria([name=%s][role=%s])", q(name), q(value))}, true
	case "text":
		return locator{pw: fmt.Sprintf("page.getByText(%s%s)", q(value), opts), pptr: "::-p-text(" + value + ")"}, true
	case "label":
		return locator{pw: fmt.Sprintf("page.getByLabel(%s%s)", q(value), opts), pptr: "::-p-aria(" + value + ")"}, true
	case "placeholder":
		return locator{pw: fmt.Sprintf("page.getByPlaceholder(%s%s)", q(value), opts), pptr: attr("placeholder")}, true
	case "alt":
		return locator{pw: fmt.Sprintf("page.getByAltText(%s%s)", q(value), opts), pptr: attr("alt")}, true
	case "title":
		return locator{pw: fmt.Sprintf("page.getByTitle(%s%s)", q(value), opts), pptr: attr("title")}, true
	case "testid":
		return locator{pw: fmt.Sprintf("page.getByTestId(%s)", q(value)), pptr: attr("data-testid")}, true
	case "first":
		return locator{pw: fmt.Sprintf("page.locator(%s).first()", q(value)), pptr: value}, true
	case "last":
		return locator{pw: fmt.Sprintf("page.locator(%s).last()", q(value)), ref: "the last " + value}, true
	case "nth":
		i := int(req.GetFloat("index", 0))
		return locator{pw: fmt.Sprintf("page.locator(%s).nth(%d)", q(value), i), ref: fmt.Sprintf("match %d of %s", i, value)}, true
	}
	return locator{}, false
}

func (l locator) playwright() string {
	if l.pw != "" {
		return l.pw
	}
	switch {
	case l.role != "":
		return fmt.Sprintf("page.getByRole(%s, { name: %s })", q(l.role), q(l.name))
	case l.css != "":
		return fmt.Sprintf("page.locator(%s)", q(l.css))
	}
	return fmt.Sprintf("page.locator(%s) /* TODO: %s could not be resolved */", q("body"), l.ref)
}

func (l locator) cssOr() string {
	if l.pptr != "" {
		return q(l.pptr)
	}
	if l.css != "" {
		return q(l.css)
	}
	return fmt.Sprintf("%s /* TODO: %s could not be resolved */", q("body"), l.ref)
}

func (f *flow) playwright() string {
	var b strings.Builder
	b.WriteString("import { test, expect } from '@playwright/test';\n\ntest('recorded flow', async ({ page }) => {\n")
	for _, s := range f.steps {
		var line string
		switch s.kind {
		case "goto":
			line = fmt.Sprintf("await page.goto(%s);", q(s.value))
		case "back", "forward", "reload":
			line = fmt.Sprintf("await page.%s();", map[string]string{"back": "goBack", "forward": "goForward", "reload": "reload"}[s.kind])
		case "click", "dblclick", "check", "uncheck", "hover", "focus":
			line = fmt.Sprintf("await %s.%s();", s.target.playwright(), s.kind)
		case "fill":
			line = fmt.Sprintf("await %s.fill(%s);", s.target.playwright(), q(s.value))
		case "type":
			line = fmt.Sprintf("await %s.pressSequentially(%s);", s.target.playwright(), q(s.value))
		case "keys":
			line = fmt.Sprintf("await page.keyboard.type(%s);", q(s.value))
		case "press":
			line = fmt.Sprintf("await page.keyboard.press(%s);", q(s.value))
		case "select":
			vals, _ := json.Marshal(s.values)
			line = fmt.Sprintf("await %s.selectOption(%s);", s.target.playwright(), vals)
		case "scroll":
			line = fmt.Sprintf("await page.mouse.wheel(%s);", s.value)
		case "wait_text":
			line = fmt.Sprintf("await expect(page.getByText(%s).first()).toBeVisible();", q(s.value))
		case "wait_url":
			line = fmt.Sprintf("await page.waitForURL(%s);", q("**"+strings.TrimPrefix(s.value, "**")))
		case "wait_element":
			line = fmt.Sprintf("await expect(%s).toBeVisible();", s.target.playwright())
		}
		b.WriteString("  " + line + "\n")
	}
	b.WriteString("});\n")
	return b.String()
}

func (f *flow) puppeteer() string {
	var b strings.Builder
	b.WriteString("import puppeteer from 'puppeteer';\n\nconst browser = await puppeteer.launch();\nconst page = await browser.newPage();\n")
	for _, s := range f.steps {
		var line string
		switch s.kind {
		case "goto":
			line = fmt.Sprintf("await page.goto(%s);", q(s.value))
		case "back", "forward", "reload":
			line = fmt.Sprintf("await page.%s();", map[string]string{"back": "goBack", "forward": "goForward", "reload": "reload"}[s.kind])
		case "click":
			line = fmt.Sprintf("await page.locator(%s).click();", s.target.cssOr())
		case "dblclick":
			line = fmt.Sprintf("await page.locator(%s).click({ count: 2 });", s.target.cssOr())
		case "hover":
			line = fmt.Sprintf("await page.locator(%s).hover();", s.target.cssOr())
		case "focus":
			line = fmt.Sprintf("await page.focus(%s);", s.target.cssOr())
		case "check", "uncheck":
			line = fmt.Sprintf("await page.$eval(%s, (el) => { if (el.checked !== %v) el.click(); });", s.target.cssOr(), s.kind == "check")
		case "fill":
			line = fmt.Sprintf("await page.locator(%s).fill(%s);", s.target.cssOr(), q(s.value))
		case "type":
			line = fmt.Sprintf("await page.type(%s, %s);", s.target.cssOr(), q(s.value))
		case "keys":
			line = fmt.Sprintf("await page.keyboard.type(%s);", q(s.value))
		case "press":
			line = fmt.Sprintf("await page.keyboard.press(%s);", q(s.value))
		case "select":
			args := make([]string, len(s.values))
			for i, v := range s.values {
				args[i] = q(v)
			}
			line = fmt.Sprintf("await page.select(%s, %s);", s.target.cssOr(), strings.Join(args, ", "))
		case "scroll":
			dx, dy, _ := strings.Cut(s.value, ", ")
			line = fmt.Sprintf("await page.mouse.wheel({ deltaX: %s, deltaY: %s });", dx, dy)
		case "wait_text":
			line = fmt.Sprintf("await page.waitForFunction((t) => document.body.innerText.includes(t), {}, %s);", q(s.value))
		case "wait_url":
			line = fmt.Sprintf("await page.waitForFunction((u) => location.href.includes(u), {}, %s);", q(s.value))
		case "wait_element":
			line = fmt.Sprintf("await page.waitForSelector(%s, { visible: true });", s.target.cssOr())
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("await browser.close();\n")
	return b.String()
}
