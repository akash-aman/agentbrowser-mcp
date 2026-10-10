package devtools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
)

// Rect is a box in viewport CSS pixels, as agent-browser's get box reports it.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Target names an element: a CSS selector, or the box agent-browser reported
// for an @ref, which only agent-browser can resolve.
type Target struct {
	Selector string
	Box      *Rect
	Label    string // how the caller named it, for messages
}

// Element is a node the Elements features act on.
type Element struct {
	NodeID   int
	ObjectID string
	Label    string // e.g. button#buy.primary
}

// elementAtJS finds the element at the centre of an @ref's box, steps into
// shadow roots, then climbs to the ancestor whose box is the ref's box,
// because the point usually lands on a child such as a button's label.
const elementAtJS = `(() => {
  const [x, y, w, h] = [%g, %g, %g, %g];
  const cx = x + w / 2, cy = y + h / 2;
  let el = document.elementFromPoint(cx, cy);
  while (el && el.shadowRoot) {
    const inner = el.shadowRoot.elementFromPoint(cx, cy);
    if (!inner || inner === el) break;
    el = inner;
  }
  const near = (a, b) => Math.abs(a - b) < 1.5;
  for (let n = el; n; n = n.parentElement || (n.getRootNode() && n.getRootNode().host)) {
    const r = n.getBoundingClientRect();
    if (near(r.x, x) && near(r.y, y) && near(r.width, w) && near(r.height, h)) return n;
  }
  return el;
})()`

// describeJS names an element the way DevTools labels nodes.
const describeJS = `function() {
  let s = this.localName;
  if (this.id) s += "#" + this.id;
  for (const c of [...this.classList].slice(0, 3)) s += "." + c;
  return s;
}`

// ancestorsJS labels an element's ancestors like describeJS, parent first,
// crossing out of shadow roots to their hosts.
const ancestorsJS = `function() {
  const ancestors = [], up = (n) => n.parentElement || (n.getRootNode() && n.getRootNode().host);
  for (let n = up(this); n; n = up(n)) {
    let s = n.localName;
    if (n.id) s += "#" + n.id;
    for (const c of [...n.classList].slice(0, 3)) s += "." + c;
    ancestors.push(s);
  }
  return ancestors;
}`

// Element resolves a target to a DOM node.
func (p *Page) Element(ctx context.Context, t Target) (*Element, error) {
	if err := p.call(ctx, "DOM.enable", nil, nil); err != nil {
		return nil, err
	}
	if err := p.call(ctx, "DOM.getDocument", map[string]any{"depth": 0}, nil); err != nil {
		return nil, err
	}
	expr := fmt.Sprintf("document.querySelector(%s)", jsQuote(t.Selector))
	if t.Box != nil {
		expr = fmt.Sprintf(elementAtJS, t.Box.X, t.Box.Y, t.Box.Width, t.Box.Height)
	}
	obj, err := p.evaluate(ctx, expr, false)
	if err != nil {
		return nil, err
	}
	if obj.ObjectID == "" || obj.Subtype == "null" {
		if t.Box != nil {
			return nil, fmt.Errorf("nothing is at %s's position; it may be covered or off screen", t.Label)
		}
		return nil, fmt.Errorf("no element matches %s", t.Selector)
	}
	var node struct {
		NodeID int `json:"nodeId"`
	}
	if err := p.call(ctx, "DOM.requestNode", map[string]any{"objectId": obj.ObjectID}, &node); err != nil {
		return nil, err
	}
	el := &Element{NodeID: node.NodeID, ObjectID: obj.ObjectID, Label: t.Label}
	var name string
	if err := p.callOn(ctx, obj.ObjectID, describeJS, nil, &name); err == nil && name != "" {
		el.Label = name
	}
	return el, nil
}

// evaluate runs expr in the page and returns the result as a remote object,
// or by value when byValue is set.
func (p *Page) evaluate(ctx context.Context, expr string, byValue bool) (RemoteObject, error) {
	var res struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expr, "objectGroup": "agent-browser-mcp", "returnByValue": byValue}
	if err := p.call(ctx, "Runtime.evaluate", params, &res); err != nil {
		return RemoteObject{}, err
	}
	if res.ExceptionDetails != nil {
		return RemoteObject{}, res.ExceptionDetails
	}
	return res.Result, nil
}

// callOn calls fn with this bound to the object and decodes its value into out.
func (p *Page) callOn(ctx context.Context, objectID, fn string, args []any, out any) error {
	callArgs := make([]map[string]any, len(args))
	for i, a := range args {
		callArgs[i] = map[string]any{"value": a}
	}
	var res struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	params := map[string]any{"objectId": objectID, "functionDeclaration": fn, "arguments": callArgs, "returnByValue": true}
	if err := p.call(ctx, "Runtime.callFunctionOn", params, &res); err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return res.ExceptionDetails
	}
	if out == nil || len(res.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(res.Result.Value, out)
}

// cssSheet says where a style sheet comes from.
type cssSheet struct {
	URL       string
	StartLine int
	Origin    string
}

// enableCSS turns on the CSS domain, recording each style sheet's URL so
// rules can say where they live. It enables every time because stopping
// coverage turns the domain off again.
func (p *Page) enableCSS(ctx context.Context) error {
	p.mu.Lock()
	subscribed := p.cssSheets != nil
	if !subscribed {
		p.cssSheets = map[string]cssSheet{}
	}
	p.mu.Unlock()
	if subscribed {
		return p.call(ctx, "CSS.enable", nil, nil)
	}
	p.onSession("CSS.styleSheetAdded", func(params json.RawMessage) {
		var e struct {
			Header struct {
				StyleSheetID string  `json:"styleSheetId"`
				SourceURL    string  `json:"sourceURL"`
				StartLine    float64 `json:"startLine"`
				Origin       string  `json:"origin"`
			} `json:"header"`
		}
		if json.Unmarshal(params, &e) == nil {
			p.mu.Lock()
			p.cssSheets[e.Header.StyleSheetID] = cssSheet{URL: e.Header.SourceURL, StartLine: int(e.Header.StartLine), Origin: e.Header.Origin}
			p.mu.Unlock()
		}
	})
	return p.call(ctx, "CSS.enable", nil, nil)
}

type cssProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Important bool   `json:"important"`
	Implicit  bool   `json:"implicit"`
	Disabled  bool   `json:"disabled"`
	ParsedOk  *bool  `json:"parsedOk"`
	Text      string `json:"text"`
}

type cssStyle struct {
	StyleSheetID  string        `json:"styleSheetId"`
	CSSProperties []cssProperty `json:"cssProperties"`
	Range         *struct {
		StartLine int `json:"startLine"`
	} `json:"range"`
}

type ruleMatch struct {
	Rule struct {
		StyleSheetID string `json:"styleSheetId"`
		SelectorList struct {
			Selectors []struct {
				Text string `json:"text"`
			} `json:"selectors"`
			Text string `json:"text"`
		} `json:"selectorList"`
		Origin string   `json:"origin"`
		Style  cssStyle `json:"style"`
		Media  []struct {
			Text string `json:"text"`
		} `json:"media"`
	} `json:"rule"`
	MatchingSelectors []int `json:"matchingSelectors"`
}

type matchedStyles struct {
	InlineStyle     *cssStyle   `json:"inlineStyle"`
	AttributesStyle *cssStyle   `json:"attributesStyle"`
	MatchedCSSRules []ruleMatch `json:"matchedCSSRules"`
	Inherited       []struct {
		InlineStyle     *cssStyle   `json:"inlineStyle"`
		MatchedCSSRules []ruleMatch `json:"matchedCSSRules"`
	} `json:"inherited"`
}

// styleBlock is one rule, or the inline style, as the Styles pane lists it.
type styleBlock struct {
	title    string // selector, "element.style" or an attribute style
	where    string // file:line, empty for inline styles
	media    string
	origin   string
	props    []cssProperty
	overruns map[int]string // declaration index -> why it does not apply
}

// inheritedProperties are the properties an element takes from its parents,
// so an ancestor's rule can still decide its value.
var inheritedProperties = map[string]bool{
	"color": true, "font": true, "font-family": true, "font-size": true, "font-style": true, "font-variant": true,
	"font-weight": true, "font-stretch": true, "font-feature-settings": true, "line-height": true, "letter-spacing": true,
	"word-spacing": true, "text-align": true, "text-indent": true, "text-transform": true, "text-shadow": true,
	"white-space": true, "direction": true, "visibility": true, "cursor": true, "list-style": true, "list-style-type": true,
	"list-style-position": true, "quotes": true, "color-scheme": true, "accent-color": true, "caret-color": true,
	"word-break": true, "overflow-wrap": true, "hyphens": true, "writing-mode": true, "-webkit-font-smoothing": true,
}

// Styles lists the CSS that applies to the element in cascade order, as the
// Styles pane does: inline style, matching rules from highest precedence
// down, then inherited rules. Declarations that lose to another are marked.
func (p *Page) Styles(ctx context.Context, el *Element) (string, error) {
	if err := p.enableCSS(ctx); err != nil {
		return "", err
	}
	var m matchedStyles
	if err := p.call(ctx, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": el.NodeID}, &m); err != nil {
		return "", err
	}
	own := p.blocks(m.InlineStyle, m.MatchedCSSRules, m.AttributesStyle)
	markOverridden(own)
	var b strings.Builder
	fmt.Fprintf(&b, "Styles for %s, highest precedence first:", el.Label)
	var ua []styleBlock
	for _, blk := range own {
		if blk.origin == "user-agent" {
			ua = append(ua, blk)
			continue
		}
		writeBlock(&b, blk, "")
	}
	var ancestors []string // CDP lists inherited styles parent first
	if len(m.Inherited) > 0 {
		p.callOn(ctx, el.ObjectID, ancestorsJS, nil, &ancestors)
	}
	for i, inh := range m.Inherited {
		blocks := p.blocks(inh.InlineStyle, inh.MatchedCSSRules, nil)
		var kept []styleBlock
		for _, blk := range blocks {
			if blk.origin == "user-agent" {
				continue
			}
			blk.props = slices.DeleteFunc(blk.props, func(c cssProperty) bool {
				return !inheritedProperties[c.Name] && !strings.HasPrefix(c.Name, "--")
			})
			if len(blk.props) > 0 {
				kept = append(kept, blk)
			}
		}
		if len(kept) == 0 {
			continue
		}
		from := fmt.Sprintf("ancestor %d", i+1)
		if i < len(ancestors) {
			from = ancestors[i]
		}
		fmt.Fprintf(&b, "\nInherited from %s:", from)
		for _, blk := range kept {
			writeBlock(&b, blk, "  ")
		}
	}
	if len(ua) > 0 {
		var names []string
		for _, blk := range ua {
			if !slices.Contains(names, blk.title) {
				names = append(names, blk.title)
			}
		}
		fmt.Fprintf(&b, "\nUser agent styles: %s", strings.Join(names, "; "))
	}
	return b.String(), nil
}

// blocks builds the Styles pane blocks: inline style first, then rules from
// highest precedence (CDP lists them lowest first), then attribute styles.
func (p *Page) blocks(inline *cssStyle, rules []ruleMatch, attrs *cssStyle) []styleBlock {
	var out []styleBlock
	if inline != nil && len(declared(inline.CSSProperties)) > 0 {
		out = append(out, styleBlock{title: "element.style", props: declared(inline.CSSProperties)})
	}
	for i := len(rules) - 1; i >= 0; i-- {
		r := rules[i].Rule
		var sel []string
		for _, n := range rules[i].MatchingSelectors {
			if n < len(r.SelectorList.Selectors) {
				sel = append(sel, r.SelectorList.Selectors[n].Text)
			}
		}
		title := cmp.Or(strings.Join(sel, ", "), r.SelectorList.Text)
		blk := styleBlock{title: title, origin: r.Origin, props: declared(r.Style.CSSProperties)}
		if len(r.Media) > 0 {
			blk.media = "@media " + r.Media[0].Text
		}
		if r.Style.Range != nil {
			p.mu.Lock()
			sh := p.cssSheets[r.StyleSheetID]
			p.mu.Unlock()
			if sh.URL != "" {
				blk.where = fmt.Sprintf("%s:%d", path.Base(strings.SplitN(sh.URL, "?", 2)[0]), sh.StartLine+r.Style.Range.StartLine+1)
			}
		}
		out = append(out, blk)
	}
	if attrs != nil && len(attributeDeclared(attrs.CSSProperties)) > 0 {
		out = append(out, styleBlock{title: "attribute style", props: attributeDeclared(attrs.CSSProperties)})
	}
	return out
}

// declared keeps what the author wrote: CDP also lists the longhands a
// shorthand expands to (background: red brings background-image: initial
// and the rest), without source text.
func declared(props []cssProperty) []cssProperty {
	var out []cssProperty
	for _, c := range props {
		if !c.Implicit && c.Text != "" {
			out = append(out, c)
		}
	}
	return out
}

// attributeDeclared keeps presentational attribute styles (width="10"),
// which have no source text.
func attributeDeclared(props []cssProperty) []cssProperty {
	var out []cssProperty
	for _, c := range props {
		if !c.Implicit && c.Value != "" {
			out = append(out, c)
		}
	}
	return out
}

// markOverridden marks, per property name, every declaration but the one
// that wins: the first in precedence order, unless a later one is
// !important and it is not. Disabled and invalid declarations never win.
func markOverridden(blocks []styleBlock) {
	type pos struct{ block, decl int }
	winner := map[string]pos{}
	important := map[string]bool{}
	for bi := range blocks {
		blocks[bi].overruns = map[int]string{}
		for di, c := range blocks[bi].props {
			switch {
			case c.Disabled:
				blocks[bi].overruns[di] = "disabled"
				continue
			case c.ParsedOk != nil && !*c.ParsedOk:
				blocks[bi].overruns[di] = "invalid value"
				continue
			}
			w, seen := winner[c.Name]
			switch {
			case !seen:
				winner[c.Name], important[c.Name] = pos{bi, di}, c.Important
			case c.Important && !important[c.Name]:
				blocks[w.block].overruns[w.decl] = "overridden"
				winner[c.Name], important[c.Name] = pos{bi, di}, true
			default:
				blocks[bi].overruns[di] = "overridden"
			}
		}
	}
}

func writeBlock(b *strings.Builder, blk styleBlock, indent string) {
	b.WriteString("\n" + indent)
	if blk.media != "" {
		b.WriteString(blk.media + " ")
	}
	b.WriteString(blk.title)
	if blk.where != "" {
		b.WriteString("  " + blk.where)
	}
	for i, c := range blk.props {
		line := fmt.Sprintf("\n%s  %s: %s", indent, c.Name, c.Value)
		if c.Important && !strings.Contains(c.Value, "important") {
			line += " !important"
		}
		if why := blk.overruns[i]; why != "" {
			line += "   [" + why + "]"
		}
		b.WriteString(line)
	}
}

// commonComputed are the computed values shown when no properties are asked for.
var commonComputed = []string{
	"display", "position", "top", "right", "bottom", "left", "z-index", "width", "height", "box-sizing",
	"margin", "padding", "border-width", "overflow-x", "overflow-y", "visibility", "opacity", "color",
	"background-color", "font-family", "font-size", "font-weight", "line-height", "text-align",
	"transform", "pointer-events", "cursor",
}

var layoutComputed = []string{"flex-direction", "flex-wrap", "justify-content", "align-items", "gap", "grid-template-columns", "grid-template-rows"}

// Computed returns the element's computed values: the given properties
// (exact names or prefixes such as "font"), or the common layout and text
// ones. margin, padding and border-width are shown as one line each.
func (p *Page) Computed(ctx context.Context, el *Element, props []string) (string, error) {
	values, err := p.computedMap(ctx, el)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Computed style of %s:\n%s", el.Label, FormatComputed(values, props)), nil
}

// FormatComputed picks computed values to show: the given properties (exact
// names or prefixes), or the common layout and text ones.
func FormatComputed(values map[string]string, props []string) string {
	want := props
	if len(want) == 0 {
		want = slices.Clone(commonComputed)
		if d := values["display"]; strings.Contains(d, "flex") || strings.Contains(d, "grid") {
			want = append(want, layoutComputed...)
		}
	}
	var lines []string
	for _, w := range want {
		if v, ok := edges(values, w); ok {
			lines = append(lines, w+": "+v)
			continue
		}
		if v, ok := values[w]; ok {
			lines = append(lines, w+": "+v)
			continue
		}
		var names []string
		for name := range values {
			if strings.HasPrefix(name, w+"-") {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		// Like the Computed pane without "Show all": a prefix such as font
		// matched 22 longhands, most of them at normal, none or auto.
		hidden := 0
		for _, name := range names {
			if v := values[name]; v == "normal" || v == "none" || v == "auto" {
				hidden++
				continue
			}
			lines = append(lines, name+": "+values[name])
		}
		if hidden > 0 {
			lines = append(lines, fmt.Sprintf("(%d more %s-* at normal, none or auto; name one to read it)", hidden, w))
		}
		if len(names) == 0 && len(props) > 0 {
			lines = append(lines, w+": (no such property)")
		}
	}
	return strings.Join(lines, "\n")
}

// edges joins the four sides of margin, padding or border-width.
func edges(values map[string]string, name string) (string, bool) {
	if name == "gap" {
		row, ok1 := values["row-gap"]
		col, ok2 := values["column-gap"]
		return shorthand(row, col), ok1 && ok2
	}
	format := map[string]string{"margin": "margin-%s", "padding": "padding-%s", "border-width": "border-%s-width"}[name]
	if format == "" {
		return "", false
	}
	var sides []string
	for _, s := range []string{"top", "right", "bottom", "left"} {
		v, ok := values[fmt.Sprintf(format, s)]
		if !ok {
			return "", false
		}
		sides = append(sides, v)
	}
	return shorthand(sides...), true
}

// shorthand writes sides the way CSS shorthand does, dropping repeats:
// "0px" for four equal sides, "1px 6px" when top/bottom and left/right match.
func shorthand(sides ...string) string {
	for len(sides) > 1 {
		n := len(sides)
		// left may repeat right, bottom top, and right top.
		if opposite := max(n-3, 0); sides[n-1] != sides[opposite] {
			break
		}
		sides = sides[:n-1]
	}
	return strings.Join(sides, " ")
}

func (p *Page) computedMap(ctx context.Context, el *Element) (map[string]string, error) {
	if err := p.enableCSS(ctx); err != nil {
		return nil, err
	}
	var res struct {
		ComputedStyle []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"computedStyle"`
	}
	if err := p.call(ctx, "CSS.getComputedStyleForNode", map[string]any{"nodeId": el.NodeID}, &res); err != nil {
		return nil, err
	}
	values := make(map[string]string, len(res.ComputedStyle))
	for _, c := range res.ComputedStyle {
		values[c.Name] = c.Value
	}
	return values, nil
}

// BoxModel reports the element's size and its margin, border and padding.
func (p *Page) BoxModel(ctx context.Context, el *Element) (string, error) {
	var res struct {
		Model struct {
			Content []float64 `json:"content"`
			Padding []float64 `json:"padding"`
			Border  []float64 `json:"border"`
			Margin  []float64 `json:"margin"`
			Width   float64   `json:"width"`
			Height  float64   `json:"height"`
		} `json:"model"`
	}
	if err := p.call(ctx, "DOM.getBoxModel", map[string]any{"nodeId": el.NodeID}, &res); err != nil {
		return "", fmt.Errorf("%s has no box: it is not rendered (%v)", el.Label, err)
	}
	m := res.Model
	if len(m.Content) < 8 || len(m.Padding) < 8 || len(m.Border) < 8 || len(m.Margin) < 8 {
		return "", fmt.Errorf("%s: incomplete box model", el.Label)
	}
	// Quads run clockwise from the top left: x1 y1 x2 y2 x3 y3 x4 y4.
	gap := func(outer, inner []float64) string {
		return fmt.Sprintf("%s %s %s %s", px(inner[1]-outer[1]), px(outer[2]-inner[2]), px(outer[5]-inner[5]), px(inner[0]-outer[0]))
	}
	return fmt.Sprintf("Box model of %s (top right bottom left):\nsize    %s × %s (border box)\nmargin  %s\nborder  %s\npadding %s\ncontent %s × %s",
		el.Label, px(m.Border[2]-m.Border[0]), px(m.Border[5]-m.Border[1]),
		gap(m.Margin, m.Border), gap(m.Border, m.Padding), gap(m.Padding, m.Content),
		px(m.Content[2]-m.Content[0]), px(m.Content[5]-m.Content[1])), nil
}

func px(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

type axValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type axNode struct {
	Ignored        bool `json:"ignored"`
	IgnoredReasons []struct {
		Name string `json:"name"`
	} `json:"ignoredReasons"`
	Role *axValue `json:"role"`
	Name *struct {
		Value   any `json:"value"`
		Sources []struct {
			Type       string   `json:"type"`
			Attribute  string   `json:"attribute"`
			Value      *axValue `json:"value"`
			Superseded bool     `json:"superseded"`
		} `json:"sources"`
	} `json:"name"`
	Description *axValue `json:"description"`
	Value       *axValue `json:"value"`
	Properties  []struct {
		Name  string  `json:"name"`
		Value axValue `json:"value"`
	} `json:"properties"`
}

func (p *Page) axNode(ctx context.Context, el *Element) (axNode, error) {
	var res struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := p.call(ctx, "Accessibility.getPartialAXTree", map[string]any{"nodeId": el.NodeID, "fetchRelatives": false}, &res); err != nil {
		return axNode{}, err
	}
	if len(res.Nodes) == 0 {
		return axNode{}, fmt.Errorf("%s has no accessibility node", el.Label)
	}
	return res.Nodes[0], nil
}

// Accessibility reports what assistive technology sees: role, computed name
// and where it came from, description, value and states.
func (p *Page) Accessibility(ctx context.Context, el *Element) (string, error) {
	n, err := p.axNode(ctx, el)
	if err != nil {
		return "", err
	}
	lines := []string{"Accessibility of " + el.Label + ":"}
	if n.Ignored {
		var why []string
		for _, r := range n.IgnoredReasons {
			why = append(why, r.Name)
		}
		lines = append(lines, "ignored by assistive technology: "+cmp.Or(strings.Join(why, ", "), "no reason given"))
	}
	if n.Role != nil {
		lines = append(lines, fmt.Sprintf("role: %v", n.Role.Value))
	}
	if n.Name != nil {
		name := fmt.Sprintf("name: %q", fmt.Sprint(n.Name.Value))
		for _, s := range n.Name.Sources {
			if s.Value != nil && !s.Superseded && fmt.Sprint(s.Value.Value) != "" {
				from := s.Type
				if s.Attribute != "" {
					from += " " + s.Attribute
				}
				name += " (from " + from + ")"
				break
			}
		}
		if fmt.Sprint(n.Name.Value) == "" {
			name = "name: none. Screen readers have nothing to announce for it"
		}
		lines = append(lines, name)
	}
	if n.Description != nil && fmt.Sprint(n.Description.Value) != "" {
		lines = append(lines, fmt.Sprintf("description: %v", n.Description.Value))
	}
	if n.Value != nil && fmt.Sprint(n.Value.Value) != "" {
		lines = append(lines, fmt.Sprintf("value: %v", n.Value.Value))
	}
	var states, props []string
	for _, pr := range n.Properties {
		switch v := pr.Value.Value.(type) {
		case bool:
			if v {
				states = append(states, pr.Name)
			}
		default:
			if s := fmt.Sprint(v); s != "" && s != "<nil>" {
				props = append(props, pr.Name+"="+s)
			}
		}
	}
	if len(states) > 0 {
		lines = append(lines, "states: "+strings.Join(states, ", "))
	}
	if len(props) > 0 {
		lines = append(lines, "properties: "+strings.Join(props, ", "))
	}
	return strings.Join(lines, "\n"), nil
}

// Contrast checks the element's text color against the background behind it,
// using WCAG 2 contrast ratios.
func (p *Page) Contrast(ctx context.Context, el *Element) (string, error) {
	values, err := p.computedMap(ctx, el)
	if err != nil {
		return "", err
	}
	var bg struct {
		BackgroundColors   []string `json:"backgroundColors"`
		ComputedFontSize   string   `json:"computedFontSize"`
		ComputedFontWeight string   `json:"computedFontWeight"`
	}
	if err := p.call(ctx, "CSS.getBackgroundColors", map[string]any{"nodeId": el.NodeID}, &bg); err != nil {
		return "", err
	}
	size := parsePx(cmp.Or(bg.ComputedFontSize, values["font-size"]))
	weight, _ := strconv.Atoi(cmp.Or(bg.ComputedFontWeight, values["font-weight"]))
	return contrastReport(el.Label, values["color"], bg.BackgroundColors, size, weight)
}

func parsePx(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
	return v
}

// contrastReport rates text against each background color, worst first.
func contrastReport(label, text string, backgrounds []string, size float64, weight int) (string, error) {
	fg, err := parseColor(text)
	if err != nil {
		return "", err
	}
	if len(backgrounds) == 0 {
		return fmt.Sprintf("%s: Chrome cannot tell the background color (an image, gradient or overlapping element), so check its contrast visually.", label), nil
	}
	large := size >= 24 || size >= 18.66 && weight >= 700
	aa, aaa := 4.5, 7.0
	if large {
		aa, aaa = 3, 4.5
	}
	worst, worstBg := math.Inf(1), ""
	for _, s := range backgrounds {
		c, err := parseColor(s)
		if err != nil {
			continue
		}
		if r := contrastRatio(fg, c); r < worst {
			worst, worstBg = r, s
		}
	}
	if worstBg == "" {
		return "", fmt.Errorf("cannot read the background colors %v", backgrounds)
	}
	verdict := func(need float64) string {
		if worst >= need {
			return fmt.Sprintf("passes (needs %g)", need)
		}
		return fmt.Sprintf("FAILS (needs %g)", need)
	}
	kind := "normal text"
	if large {
		kind = "large text"
	}
	return fmt.Sprintf("Contrast of %s: %.2f:1, %s on %s, %s %s (%s)\nWCAG AA %s; AAA %s",
		label, worst, text, worstBg, px(size)+"px", strconv.Itoa(weight), kind, verdict(aa), verdict(aaa)), nil
}

type rgba struct{ r, g, b, a float64 }

// parseColor reads the rgb()/rgba() colors computed styles report.
func parseColor(s string) (rgba, error) {
	s = strings.TrimSpace(s)
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open || !strings.HasPrefix(s, "rgb") {
		return rgba{}, fmt.Errorf("cannot read color %q", s)
	}
	parts := strings.FieldsFunc(s[open+1:end], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
	if len(parts) < 3 {
		return rgba{}, fmt.Errorf("cannot read color %q", s)
	}
	c := rgba{a: 1}
	for i, dst := range []*float64{&c.r, &c.g, &c.b, &c.a} {
		if i >= len(parts) {
			break
		}
		v, err := strconv.ParseFloat(strings.TrimSuffix(parts[i], "%"), 64)
		if err != nil {
			return rgba{}, fmt.Errorf("cannot read color %q", s)
		}
		if strings.HasSuffix(parts[i], "%") {
			v = v / 100
			if i < 3 {
				v *= 255
			}
		}
		*dst = v
	}
	return c, nil
}

// contrastRatio is the WCAG 2 ratio; a translucent foreground is blended
// onto the background, and a translucent background onto white.
func contrastRatio(fg, bg rgba) float64 {
	blend := func(top, under rgba) rgba {
		return rgba{top.r*top.a + under.r*(1-top.a), top.g*top.a + under.g*(1-top.a), top.b*top.a + under.b*(1-top.a), 1}
	}
	bg = blend(bg, rgba{255, 255, 255, 1})
	fg = blend(fg, bg)
	lum := func(c rgba) float64 {
		ch := func(v float64) float64 {
			v /= 255
			if v <= 0.03928 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
	}
	l1, l2 := lum(fg), lum(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// PseudoStates are the states force_state can hold an element in.
var PseudoStates = []string{"hover", "focus", "active", "focus-within", "focus-visible", "visited", "target"}

// ForceState holds the element in pseudo-class states, as DevTools' :hov
// toggles do; an empty list releases it.
func (p *Page) ForceState(ctx context.Context, el *Element, states []string) (string, error) {
	if err := p.enableCSS(ctx); err != nil {
		return "", err
	}
	if states == nil {
		states = []string{}
	}
	if err := p.call(ctx, "CSS.forcePseudoState", map[string]any{"nodeId": el.NodeID, "forcedPseudoClasses": states}, nil); err != nil {
		return "", err
	}
	if len(states) == 0 {
		return "released forced states on " + el.Label, nil
	}
	return fmt.Sprintf("forcing :%s on %s until released or the page reloads; styles or a screenshot shows the result", strings.Join(states, ", :"), el.Label), nil
}

// Edit changes the element live, as editing in the Elements panel does. The
// change lasts until the page reloads.
func (p *Page) Edit(ctx context.Context, el *Element, action, name, value string) (string, error) {
	var err error
	switch action {
	case "set_style":
		var style string
		err = p.callOn(ctx, el.ObjectID, `function(css) { this.style.cssText += ";" + css; return this.getAttribute("style"); }`, []any{value}, &style)
		if err == nil {
			return fmt.Sprintf("%s style=%q (until the page reloads)", el.Label, style), nil
		}
	case "set_attribute":
		err = p.call(ctx, "DOM.setAttributeValue", map[string]any{"nodeId": el.NodeID, "name": name, "value": value}, nil)
	case "set_html":
		err = p.call(ctx, "DOM.setOuterHTML", map[string]any{"nodeId": el.NodeID, "outerHTML": value}, nil)
	case "remove":
		err = p.call(ctx, "DOM.removeNode", map[string]any{"nodeId": el.NodeID}, nil)
	case "hide":
		err = p.callOn(ctx, el.ObjectID, `function() { this.style.setProperty("visibility", "hidden", "important"); }`, nil, nil)
	default:
		return "", fmt.Errorf("unknown edit %q", action)
	}
	if err != nil {
		return "", err
	}
	done := map[string]string{"set_attribute": "set " + name + " on", "set_html": "replaced", "remove": "removed", "hide": "hid"}[action]
	return fmt.Sprintf("%s %s (until the page reloads; take a new snapshot, refs may have changed)", done, el.Label), nil
}

// Overlay shows DevTools' grid, flex or container query overlay for the
// element, whichever its layout is.
func (p *Page) Overlay(ctx context.Context, el *Element) (string, error) {
	values, err := p.computedMap(ctx, el)
	if err != nil {
		return "", err
	}
	if err := p.call(ctx, "Overlay.enable", nil, nil); err != nil {
		return "", err
	}
	color := func(r, g, b int, a float64) map[string]any { return map[string]any{"r": r, "g": g, "b": b, "a": a} }
	display, container := values["display"], values["container-type"]
	switch {
	case strings.Contains(display, "grid"):
		cfg := map[string]any{"showGridExtensionLines": true, "showLineNames": true, "showTrackSizes": true,
			"gridBorderColor": color(255, 0, 255, 1), "cellBorderColor": color(128, 0, 128, 0.6), "rowGapColor": color(255, 0, 255, 0.15), "columnGapColor": color(255, 0, 255, 0.15)}
		err = p.call(ctx, "Overlay.setShowGridOverlays", map[string]any{"gridNodeHighlightConfigs": []any{map[string]any{"nodeId": el.NodeID, "gridHighlightConfig": cfg}}}, nil)
		return "grid overlay on " + el.Label + " (lines, gaps and track sizes); a screenshot shows it", err
	case strings.Contains(display, "flex"):
		cfg := map[string]any{"containerBorder": map[string]any{"color": color(128, 0, 255, 1), "pattern": "dashed"},
			"itemSeparator": map[string]any{"color": color(128, 0, 255, 0.6), "pattern": "dotted"}, "mainDistributedSpace": map[string]any{"fillColor": color(128, 0, 255, 0.15)}}
		err = p.call(ctx, "Overlay.setShowFlexOverlays", map[string]any{"flexNodeHighlightConfigs": []any{map[string]any{"nodeId": el.NodeID, "flexContainerHighlightConfig": cfg}}}, nil)
		return "flex overlay on " + el.Label + " (container, items and free space); a screenshot shows it", err
	case container != "" && container != "normal":
		cfg := map[string]any{"containerBorder": map[string]any{"color": color(0, 128, 255, 1), "pattern": "dashed"}}
		err = p.call(ctx, "Overlay.setShowContainerQueryOverlays", map[string]any{"containerQueryHighlightConfigs": []any{map[string]any{"nodeId": el.NodeID, "containerQueryContainerHighlightConfig": cfg}}}, nil)
		return "container query overlay on " + el.Label + "; a screenshot shows it", err
	}
	return "", fmt.Errorf("%s is display:%s, not a grid, flex or query container; element_action highlight outlines any element", el.Label, display)
}

// OverlayOff clears the grid, flex and container overlays.
func (p *Page) OverlayOff(ctx context.Context) (string, error) {
	for method, key := range map[string]string{"Overlay.setShowGridOverlays": "gridNodeHighlightConfigs", "Overlay.setShowFlexOverlays": "flexNodeHighlightConfigs", "Overlay.setShowContainerQueryOverlays": "containerQueryHighlightConfigs"} {
		if err := p.call(ctx, method, map[string]any{key: []any{}}, nil); err != nil {
			return "", err
		}
	}
	return "grid, flex and container overlays off", nil
}

// elementInfoJS returns a unique CSS selector, an XPath and the text of an
// element; test ids, ids and names win over structural paths.
const elementInfoJS = `function() {
  let el = this.nodeType === 1 ? this : this.parentElement;
  if (!el || (this.nodeType !== 1 && ["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE"].includes(el.tagName))) return null;
  const unique = (sel) => { try { const all = document.querySelectorAll(sel); return all.length === 1 && all[0] === el; } catch { return false; } };
  let css = null;
  for (const a of ["data-testid", "data-test", "data-cy", "id", "name", "aria-label"]) {
    const v = el.getAttribute(a);
    if (!v) continue;
    const sel = a === "id" ? "#" + CSS.escape(v) : el.localName + "[" + a + '="' + v.replace(/"/g, '\\"') + '"]';
    if (unique(sel)) { css = sel; break; }
  }
  if (!css) {
    const parts = [];
    for (let n = el; n && n.nodeType === 1 && n !== document.documentElement; n = n.parentElement) {
      if (n !== el && n.id && unique("#" + CSS.escape(n.id))) { parts.unshift("#" + CSS.escape(n.id)); break; }
      const same = n.parentElement ? [...n.parentElement.children].filter((c) => c.localName === n.localName) : [];
      parts.unshift(n.localName + (same.length > 1 ? ":nth-of-type(" + (same.indexOf(n) + 1) + ")" : ""));
      if (unique(parts.join(" > "))) break;
    }
    css = parts.join(" > ");
  }
  const xp = [];
  for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
    const same = n.parentElement ? [...n.parentElement.children].filter((c) => c.localName === n.localName) : [n];
    xp.unshift(n.localName + (same.length > 1 ? "[" + (same.indexOf(n) + 1) + "]" : ""));
  }
  const text = (el.innerText || el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 80);
  return { css, xpath: "/" + xp.join("/"), text };
}`

type elementInfo struct {
	CSS   string `json:"css"`
	XPath string `json:"xpath"`
	Text  string `json:"text"`
}

// Selector returns stable ways to find the element again, for tests: a
// unique CSS selector, an XPath, and a Playwright role locator when the
// element has a role and name.
func (p *Page) Selector(ctx context.Context, el *Element) (string, error) {
	var info elementInfo
	if err := p.callOn(ctx, el.ObjectID, elementInfoJS, nil, &info); err != nil {
		return "", err
	}
	lines := []string{"Selectors for " + el.Label + ":", "css: " + info.CSS, "xpath: " + info.XPath}
	if loc := p.roleLocator(ctx, el); loc != "" {
		lines = append(lines, "playwright: "+loc)
	}
	return strings.Join(lines, "\n"), nil
}

// roleLocator builds page.getByRole(...) for the element, with .nth() when
// the role and name are not unique.
func (p *Page) roleLocator(ctx context.Context, el *Element) string {
	n, err := p.axNode(ctx, el)
	if err != nil || n.Role == nil || n.Name == nil {
		return ""
	}
	role, name := fmt.Sprint(n.Role.Value), fmt.Sprint(n.Name.Value)
	if name == "" || role == "generic" || role == "none" || role == "StaticText" {
		return ""
	}
	loc := fmt.Sprintf("page.getByRole(%s, { name: %s })", jsQuote(role), jsQuote(name))
	var res struct {
		Nodes []struct {
			BackendDOMNodeID int `json:"backendDOMNodeId"`
		} `json:"nodes"`
	}
	if p.call(ctx, "Accessibility.queryAXTree", map[string]any{"nodeId": rootNode(ctx, p), "accessibleName": name, "role": role}, &res) != nil {
		return loc
	}
	same := res.Nodes
	if len(same) <= 1 {
		return loc
	}
	var self struct {
		Node struct {
			BackendNodeID int `json:"backendNodeId"`
		} `json:"node"`
	}
	if p.call(ctx, "DOM.describeNode", map[string]any{"nodeId": el.NodeID}, &self) == nil {
		for i, s := range same {
			if s.BackendDOMNodeID == self.Node.BackendNodeID {
				return fmt.Sprintf("%s.nth(%d)  (%d elements share this role and name)", loc, i, len(same))
			}
		}
	}
	return fmt.Sprintf("%s  (matches %d elements)", loc, len(same))
}

// rootNode returns the document's node id, or 0.
func rootNode(ctx context.Context, p *Page) int {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	p.call(ctx, "DOM.getDocument", map[string]any{"depth": 0}, &doc)
	return doc.Root.NodeID
}

// SearchDOM finds nodes by text, CSS selector or XPath, as Ctrl+F in the
// Elements panel does, including inside shadow DOM.
func (p *Page) SearchDOM(ctx context.Context, query string, limit int) (string, error) {
	if err := p.call(ctx, "DOM.enable", nil, nil); err != nil {
		return "", err
	}
	if err := p.call(ctx, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true}, nil); err != nil {
		return "", err
	}
	var found struct {
		SearchID    string `json:"searchId"`
		ResultCount int    `json:"resultCount"`
	}
	if err := p.call(ctx, "DOM.performSearch", map[string]any{"query": query, "includeUserAgentShadowDOM": false}, &found); err != nil {
		return "", err
	}
	defer p.call(ctx, "DOM.discardSearchResults", map[string]any{"searchId": found.SearchID}, nil)
	if found.ResultCount == 0 {
		return fmt.Sprintf("no elements match %q", query), nil
	}
	var res struct {
		NodeIDs []int `json:"nodeIds"`
	}
	if err := p.call(ctx, "DOM.getSearchResults", map[string]any{"searchId": found.SearchID, "fromIndex": 0, "toIndex": min(found.ResultCount, limit)}, &res); err != nil {
		return "", err
	}
	// Chrome counts the text node and its element (and script text) as
	// separate results; the header counts the elements listed.
	var lines []string
	seen := map[string]bool{}
	for _, id := range res.NodeIDs {
		var obj struct {
			Object RemoteObject `json:"object"`
		}
		if p.call(ctx, "DOM.resolveNode", map[string]any{"nodeId": id, "objectGroup": "agent-browser-mcp"}, &obj) != nil || obj.Object.ObjectID == "" {
			continue
		}
		var info *elementInfo
		if p.callOn(ctx, obj.Object.ObjectID, elementInfoJS, nil, &info) != nil || info == nil || seen[info.CSS] {
			continue
		}
		seen[info.CSS] = true
		line := info.CSS
		if info.Text != "" {
			line += "  " + strconv.Quote(info.Text)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return fmt.Sprintf("no elements match %q", query), nil
	}
	verb := "match"
	if len(lines) == 1 {
		verb = "matches"
	}
	lines = append([]string{fmt.Sprintf("%s %s %q:", plural(len(lines), "element"), verb, query)}, lines...)
	if found.ResultCount > limit {
		lines = append(lines, fmt.Sprintf("… %d more search results; raise limit or narrow the query", found.ResultCount-limit))
	}
	return strings.Join(lines, "\n"), nil
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// cssOverviewJS gathers what DevTools' CSS Overview shows: the colors and
// fonts in use, media queries, and rules whose selectors match nothing.
const cssOverviewJS = `(() => {
  const els = [...document.querySelectorAll("body, body *")].slice(0, 5000);
  const tally = (m, k) => { if (k) m[k] = (m[k] || 0) + 1; };
  const text = {}, bg = {}, border = {}, fonts = {};
  for (const el of els) {
    const s = getComputedStyle(el);
    if (s.display === "none") continue;
    if ([...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim())) {
      tally(text, s.color);
      tally(fonts, s.fontFamily.split(",")[0].replace(/["']/g, "").trim() + " " + s.fontSize + " " + s.fontWeight);
    }
    if (s.backgroundColor !== "rgba(0, 0, 0, 0)") tally(bg, s.backgroundColor);
    if (parseFloat(s.borderTopWidth) > 0) tally(border, s.borderTopColor);
  }
  let rules = 0, unused = 0, sheets = 0, blocked = 0;
  const media = new Set(), unusedExamples = [];
  const walk = (list) => {
    for (const r of list) {
      if (r.selectorText) {
        rules++;
        const sel = r.selectorText.replace(/::?[a-zA-Z-]+(\([^)]*\))?/g, "").trim();
        try {
          if (sel && !document.querySelector(sel)) { unused++; if (unusedExamples.length < 5) unusedExamples.push(r.selectorText); }
        } catch {}
      } else if (r.cssRules) {
        if (r.media) media.add("@media " + r.conditionText);
        walk(r.cssRules);
      }
    }
  };
  for (const sheet of document.styleSheets) {
    sheets++;
    try { walk(sheet.cssRules); } catch { blocked++; }
  }
  const top = (m, n) => Object.entries(m).sort((a, b) => b[1] - a[1]).slice(0, n);
  return { elements: els.length, text: top(text, 10), backgrounds: top(bg, 10), borders: top(border, 6), fonts: top(fonts, 10),
    distinct: { text: Object.keys(text).length, backgrounds: Object.keys(bg).length, fonts: Object.keys(fonts).length },
    sheets, blocked, rules, unused, unusedExamples, media: [...media].slice(0, 20) };
})()`

// CSSOverview summarizes the page's styling, as DevTools' CSS Overview does.
func (p *Page) CSSOverview(ctx context.Context) (string, error) {
	obj, err := p.evaluate(ctx, cssOverviewJS, true)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(obj.Value)
	var o struct {
		Elements    int            `json:"elements"`
		Text        [][]any        `json:"text"`
		Backgrounds [][]any        `json:"backgrounds"`
		Borders     [][]any        `json:"borders"`
		Fonts       [][]any        `json:"fonts"`
		Distinct    map[string]int `json:"distinct"`
		Sheets      int            `json:"sheets"`
		Blocked     int            `json:"blocked"`
		Rules       int            `json:"rules"`
		Unused      int            `json:"unused"`
		Examples    []string       `json:"unusedExamples"`
		Media       []string       `json:"media"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return "", fmt.Errorf("css overview: unexpected result %s", raw)
	}
	tallies := func(rows [][]any) string {
		var parts []string
		for _, r := range rows {
			if len(r) == 2 {
				parts = append(parts, fmt.Sprintf("%v ×%v", r[0], r[1]))
			}
		}
		return strings.Join(parts, "; ")
	}
	lines := []string{
		fmt.Sprintf("CSS overview of %d elements, %d style sheets (%d cross-origin, not readable), %d rules:", o.Elements, o.Sheets, o.Blocked, o.Rules),
		fmt.Sprintf("text colors (%d): %s", o.Distinct["text"], tallies(o.Text)),
		fmt.Sprintf("background colors (%d): %s", o.Distinct["backgrounds"], tallies(o.Backgrounds)),
		"border colors: " + tallies(o.Borders),
		fmt.Sprintf("fonts (%d family/size/weight combinations): %s", o.Distinct["fonts"], tallies(o.Fonts)),
		fmt.Sprintf("media queries (%d): %s", len(o.Media), strings.Join(o.Media, "; ")),
		fmt.Sprintf("rules matching no element right now: %d of %d", o.Unused, o.Rules),
	}
	if len(o.Examples) > 0 {
		lines = append(lines, "  e.g. "+strings.Join(o.Examples, ", "))
	}
	return strings.Join(lines, "\n"), nil
}
