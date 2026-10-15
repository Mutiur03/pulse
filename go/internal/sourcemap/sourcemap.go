// Package sourcemap parses Source Map v3 files and maps generated positions
// back to original source, file, line, column and name.
package sourcemap

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Map is a parsed source map.
type Map struct {
	File           string
	DebugID        string
	Sources        []string
	SourcesContent []*string
	Names          []string
	// lines[i] holds the segments of generated line i, sorted by column.
	lines [][]segment
	// split source contents, filled lazily.
	contentLines map[int][]string
}

type segment struct {
	genCol  int32
	source  int32 // -1 when the segment maps to nothing
	srcLine int32
	srcCol  int32
	name    int32 // -1 when unnamed
}

type raw struct {
	Version        int       `json:"version"`
	File           string    `json:"file"`
	SourceRoot     string    `json:"sourceRoot"`
	Sources        []string  `json:"sources"`
	SourcesContent []*string `json:"sourcesContent"`
	Names          []string  `json:"names"`
	Mappings       string    `json:"mappings"`
	DebugID        string    `json:"debugId"`
	DebugIDAlt     string    `json:"debug_id"`
	Sections       []any     `json:"sections"`
}

// ErrInvalid is returned for files that are not usable source maps.
var ErrInvalid = errors.New("invalid source map")

// Parse decodes a source map.
func Parse(data []byte) (*Map, error) {
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if r.Version != 3 {
		return nil, fmt.Errorf("%w: version %d", ErrInvalid, r.Version)
	}
	if len(r.Sections) > 0 {
		return nil, fmt.Errorf("%w: indexed maps with sections are not supported", ErrInvalid)
	}
	m := &Map{File: r.File, Sources: make([]string, len(r.Sources)), SourcesContent: r.SourcesContent, Names: r.Names}
	m.DebugID = NormalizeDebugID(r.DebugID)
	if m.DebugID == "" {
		m.DebugID = NormalizeDebugID(r.DebugIDAlt)
	}
	for i, s := range r.Sources {
		if r.SourceRoot != "" && !strings.Contains(s, "://") && !strings.HasPrefix(s, "/") {
			s = strings.TrimSuffix(r.SourceRoot, "/") + "/" + s
		}
		m.Sources[i] = s
	}
	if err := m.decode(r.Mappings); err != nil {
		return nil, err
	}
	return m, nil
}

const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var b64val = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = -1
	}
	for i := 0; i < len(b64); i++ {
		t[b64[i]] = int8(i)
	}
	return t
}()

// DecodeVLQ reads one base64 VLQ value from s starting at i and returns the
// value and the index after it.
func DecodeVLQ(s string, i int) (int, int, error) {
	result, shift := 0, 0
	for {
		if i >= len(s) {
			return 0, i, fmt.Errorf("%w: truncated VLQ", ErrInvalid)
		}
		v := b64val[s[i]]
		if v < 0 {
			return 0, i, fmt.Errorf("%w: bad base64 %q", ErrInvalid, s[i])
		}
		i++
		result += int(v&31) << shift
		if v&32 == 0 {
			break
		}
		shift += 5
		if shift > 30 {
			return 0, i, fmt.Errorf("%w: VLQ overflow", ErrInvalid)
		}
	}
	if result&1 == 1 {
		return -(result >> 1), i, nil
	}
	return result >> 1, i, nil
}

// EncodeVLQ appends the base64 VLQ of v; it is used by tests and tools.
func EncodeVLQ(b *strings.Builder, v int) {
	u := v << 1
	if v < 0 {
		u = (-v << 1) | 1
	}
	for {
		d := u & 31
		u >>= 5
		if u > 0 {
			d |= 32
		}
		b.WriteByte(b64[d])
		if u == 0 {
			return
		}
	}
}

func (m *Map) decode(mappings string) error {
	var (
		line                       []segment
		src, srcLine, srcCol, name int
		genCol                     int
		i                          int
		vals                       [5]int
	)
	for i <= len(mappings) {
		if i == len(mappings) || mappings[i] == ';' {
			sort.SliceStable(line, func(a, b int) bool { return line[a].genCol < line[b].genCol })
			m.lines = append(m.lines, line)
			line, genCol = nil, 0
			i++
			continue
		}
		if mappings[i] == ',' {
			i++
			continue
		}
		n := 0
		for i < len(mappings) && mappings[i] != ',' && mappings[i] != ';' {
			if n == 5 {
				return fmt.Errorf("%w: segment with more than 5 fields", ErrInvalid)
			}
			v, next, err := DecodeVLQ(mappings, i)
			if err != nil {
				return err
			}
			vals[n], i = v, next
			n++
		}
		genCol += vals[0]
		seg := segment{genCol: int32(genCol), source: -1, name: -1}
		switch n {
		case 1:
		case 4, 5:
			src += vals[1]
			srcLine += vals[2]
			srcCol += vals[3]
			if src < 0 || src >= len(m.Sources) || srcLine < 0 || srcCol < 0 {
				return fmt.Errorf("%w: mapping out of range", ErrInvalid)
			}
			seg.source, seg.srcLine, seg.srcCol = int32(src), int32(srcLine), int32(srcCol)
			if n == 5 {
				name += vals[4]
				if name >= 0 && name < len(m.Names) {
					seg.name = int32(name)
				}
			}
		default:
			return fmt.Errorf("%w: segment with %d fields", ErrInvalid, n)
		}
		line = append(line, seg)
	}
	return nil
}

// Position is an original location. Line and Column are 1-based.
type Position struct {
	Source string
	Line   int
	Column int
	Name   string
	// SourceIndex finds the source's content.
	SourceIndex int
}

// Lookup maps a generated position (1-based line and column, as in JS stack
// traces) to the original position: the closest segment at or before the
// column on that line.
func (m *Map) Lookup(line, column int) (Position, bool) {
	l := line - 1
	if l < 0 || l >= len(m.lines) {
		return Position{}, false
	}
	segs := m.lines[l]
	col := int32(column - 1)
	i := sort.Search(len(segs), func(i int) bool { return segs[i].genCol > col }) - 1
	if i < 0 {
		return Position{}, false
	}
	s := segs[i]
	if s.source < 0 {
		return Position{}, false
	}
	p := Position{Source: m.Sources[s.source], Line: int(s.srcLine) + 1, Column: int(s.srcCol) + 1, SourceIndex: int(s.source)}
	if s.name >= 0 {
		p.Name = m.Names[s.name]
	}
	return p, true
}

// Context returns up to n lines before and after a 1-based line of a source,
// and the line itself. ok is false when the map has no content for it.
func (m *Map) Context(source, line, n int) (pre []string, ctx string, post []string, ok bool) {
	if source < 0 || source >= len(m.SourcesContent) || m.SourcesContent[source] == nil {
		return nil, "", nil, false
	}
	if m.contentLines == nil {
		m.contentLines = map[int][]string{}
	}
	lines, done := m.contentLines[source]
	if !done {
		lines = strings.Split(strings.ReplaceAll(*m.SourcesContent[source], "\r\n", "\n"), "\n")
		m.contentLines[source] = lines
	}
	i := line - 1
	if i < 0 || i >= len(lines) {
		return nil, "", nil, false
	}
	lo, hi := max(0, i-n), min(len(lines), i+n+1)
	clip := func(s []string) []string {
		out := make([]string, len(s))
		for k, v := range s {
			out[k] = clipLine(v)
		}
		return out
	}
	return clip(lines[lo:i]), clipLine(lines[i]), clip(lines[i+1 : hi]), true
}

func clipLine(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// CleanSource turns a map source like "webpack://app/./src/a.ts" or
// "../../src/a.ts" into a readable path such as "src/a.ts".
func CleanSource(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "/"); j >= 0 && !strings.HasPrefix(s, "/") {
			s = s[j+1:]
		}
	}
	s = path.Clean("/" + s)
	return strings.TrimPrefix(s, "/")
}

var (
	reDebugComment = regexp.MustCompile(`(?m)^//# debugId=([0-9a-fA-F-]{32,36})\s*$`)
	reUUID         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// NormalizeDebugID lower-cases a debug id and adds dashes; it returns "" for
// anything that is not a UUID.
func NormalizeDebugID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 32 && !strings.Contains(s, "-") {
		s = s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
	}
	if !reUUID.MatchString(s) {
		return ""
	}
	return s
}

// DebugIDFromSource reads the "//# debugId=" comment of a generated file.
func DebugIDFromSource(js []byte) string {
	m := reDebugComment.FindAllSubmatch(js, -1)
	if len(m) == 0 {
		return ""
	}
	return NormalizeDebugID(string(m[len(m)-1][1]))
}
