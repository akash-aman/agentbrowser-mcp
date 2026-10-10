package devtools

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
)

// SourceMap maps positions in a bundled script back to the files the
// author wrote (source map v3, including index maps with sections).
type SourceMap struct {
	Sources []string  // as resolved against the map's URL and sourceRoot
	Content []*string // sourcesContent, when the map embeds it
	lines   [][]mapping
}

// mapping is one segment: a generated column and the original position it
// came from, all 0-based.
type mapping struct {
	genCol, src, line, col int
}

type rawSourceMap struct {
	Version        int       `json:"version"`
	Sources        []string  `json:"sources"`
	SourceRoot     string    `json:"sourceRoot"`
	SourcesContent []*string `json:"sourcesContent"`
	Mappings       string    `json:"mappings"`
	Sections       []struct {
		Offset struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"offset"`
		Map *rawSourceMap `json:"map"`
	} `json:"sections"`
}

// ParseSourceMap decodes a source map fetched from mapURL.
func ParseSourceMap(data []byte, mapURL string) (*SourceMap, error) {
	data = []byte(strings.TrimPrefix(string(data), ")]}'")) // XSSI guard some servers add
	var raw rawSourceMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not a source map: %w", err)
	}
	m := &SourceMap{}
	if err := m.add(&raw, mapURL, 0, 0); err != nil {
		return nil, err
	}
	for _, line := range m.lines {
		slices.SortStableFunc(line, func(a, b mapping) int { return a.genCol - b.genCol })
	}
	return m, nil
}

// add merges a map (or each section of an index map) shifted by a line and
// column offset; the column offset applies to the first line only.
func (m *SourceMap) add(raw *rawSourceMap, mapURL string, lineOff, colOff int) error {
	if len(raw.Sections) > 0 {
		for _, s := range raw.Sections {
			if s.Map == nil {
				return fmt.Errorf("index source map section without an inline map")
			}
			if err := m.add(s.Map, mapURL, lineOff+s.Offset.Line, s.Offset.Column); err != nil {
				return err
			}
		}
		return nil
	}
	if raw.Version != 3 {
		return fmt.Errorf("source map version %d, want 3", raw.Version)
	}
	base := len(m.Sources)
	for i, s := range raw.Sources {
		m.Sources = append(m.Sources, resolveSource(mapURL, raw.SourceRoot, s))
		var content *string
		if i < len(raw.SourcesContent) {
			content = raw.SourcesContent[i]
		}
		m.Content = append(m.Content, content)
	}
	src, line, col := 0, 0, 0
	for genLine, group := range strings.Split(raw.Mappings, ";") {
		genCol := 0
		for _, seg := range strings.Split(group, ",") {
			if seg == "" {
				continue
			}
			v, err := decodeVLQ(seg)
			if err != nil {
				return err
			}
			genCol += v[0]
			if len(v) < 4 {
				continue // a segment with no original position
			}
			src, line, col = src+v[1], line+v[2], col+v[3]
			at := lineOff + genLine
			c := genCol
			if genLine == 0 {
				c += colOff
			}
			for len(m.lines) <= at {
				m.lines = append(m.lines, nil)
			}
			m.lines[at] = append(m.lines[at], mapping{genCol: c, src: base + src, line: line, col: col})
		}
	}
	return nil
}

const vlqChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// decodeVLQ reads the base64 VLQ numbers of one mapping segment.
func decodeVLQ(seg string) ([]int, error) {
	var out []int
	value, shift := 0, 0
	for _, r := range seg {
		d := strings.IndexRune(vlqChars, r)
		if d < 0 {
			return nil, fmt.Errorf("bad source map segment %q", seg)
		}
		value += (d & 31) << shift
		if d&32 != 0 {
			shift += 5
			continue
		}
		n := value >> 1
		if value&1 != 0 {
			n = -n
		}
		out = append(out, n)
		value, shift = 0, 0
	}
	if shift != 0 {
		return nil, fmt.Errorf("truncated source map segment %q", seg)
	}
	return out, nil
}

// Original maps a 0-based generated position to the original file and
// 0-based position, using the closest segment at or before the column.
func (m *SourceMap) Original(line, col int) (source string, origLine, origCol int, ok bool) {
	if line < 0 || line >= len(m.lines) || len(m.lines[line]) == 0 {
		return "", 0, 0, false
	}
	segs := m.lines[line]
	i, _ := slices.BinarySearchFunc(segs, col, func(s mapping, c int) int { return s.genCol - c })
	if i == len(segs) || segs[i].genCol > col {
		if i == 0 {
			return "", 0, 0, false
		}
		i--
	}
	s := segs[i]
	return m.Sources[s.src], s.line, s.col, true
}

// Generated returns where a 0-based line of a source ends up in the bundle:
// the earliest generated position of that line, or of the nearest later
// line that has code.
func (m *SourceMap) Generated(src, line int) (genLine, genCol int, ok bool) {
	bestLine := 0
	for gl, segs := range m.lines {
		for _, s := range segs {
			if s.src != src || s.line < line {
				continue
			}
			if !ok || s.line < bestLine || s.line == bestLine && (gl < genLine || gl == genLine && s.genCol < genCol) {
				ok, bestLine, genLine, genCol = true, s.line, gl, s.genCol
			}
		}
	}
	return genLine, genCol, ok
}

// FindSource returns the index of the source whose path ends with p, as the
// model would name a file it saw in a stack (src/cart.ts).
func (m *SourceMap) FindSource(p string) (int, bool) {
	p = strings.TrimPrefix(p, "./")
	for i, s := range m.Sources {
		if s == p || ShortSource(s) == p || strings.HasSuffix(s, "/"+p) {
			return i, true
		}
	}
	return 0, false
}

// resolveSource makes a sources entry absolute the way DevTools does: URLs
// with a scheme stay as they are, others resolve against sourceRoot and the
// map's own URL.
func resolveSource(mapURL, root, s string) string {
	if strings.Contains(s, "://") || strings.HasPrefix(s, "data:") {
		return s
	}
	if root != "" {
		s = strings.TrimSuffix(root, "/") + "/" + s
		if strings.Contains(s, "://") {
			return s
		}
	}
	base, err := url.Parse(mapURL)
	if err != nil || base.Scheme == "data" {
		return path.Clean(s)
	}
	ref, err := url.Parse(s)
	if err != nil {
		return s
	}
	return base.ResolveReference(ref).String()
}

// ShortSource turns a resolved source into the path an author recognizes:
// webpack://app/./src/cart.ts and turbopack:///[project]/src/cart.ts become
// src/cart.ts, and a URL becomes its path.
func ShortSource(s string) string {
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok {
		return strings.TrimPrefix(path.Clean(s), "/")
	}
	switch scheme {
	case "http", "https", "file":
		if u, err := url.Parse(s); err == nil {
			return strings.TrimPrefix(u.Path, "/")
		}
	case "webpack":
		if _, after, ok := strings.Cut(rest, "/"); ok && !strings.HasPrefix(rest, "/") {
			rest = after // drop the namespace (the package name)
		}
	}
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimPrefix(rest, "[project]/")
	return strings.TrimPrefix(path.Clean(rest), "/")
}

// sourceMapData decodes a data: URL map; other URLs return ok=false.
func sourceMapData(u string) ([]byte, bool, error) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return nil, false, nil
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return nil, true, fmt.Errorf("malformed data URL source map")
	}
	if strings.HasSuffix(meta, ";base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		return b, true, err
	}
	s, err := url.PathUnescape(payload)
	return []byte(s), true, err
}
