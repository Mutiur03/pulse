// Package symbolicate maps minified JavaScript stack frames back to their
// original source with uploaded source maps.
package symbolicate

import (
	"container/list"
	"context"
	"errors"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mutiur03/pulse/internal/grouping"
	"github.com/Mutiur03/pulse/internal/sourcemap"
)

// RawFrame is one frame as the browser printed it. Line and Column are 1-based.
type RawFrame struct {
	Function string
	URL      string
	Line     int
	Column   int
}

var (
	reV8    = regexp.MustCompile(`^\s*at (?:(?:async )?(.+?) \()?(.+?):(\d+):(\d+)\)?$`)
	reGecko = regexp.MustCompile(`^\s*(.*?)@(.+?):(\d+):(\d+)$`)
)

// ParseJS reads V8 ("at fn (url:1:2)") and Firefox/Safari ("fn@url:1:2")
// stack lines, innermost first. Lines without a position are skipped.
func ParseJS(stack string) []RawFrame {
	var out []RawFrame
	for _, line := range strings.Split(strings.ReplaceAll(stack, "\r\n", "\n"), "\n") {
		var m []string
		if m = reV8.FindStringSubmatch(line); m == nil {
			if m = reGecko.FindStringSubmatch(line); m == nil {
				continue
			}
		}
		l, _ := strconv.Atoi(m[3])
		c, _ := strconv.Atoi(m[4])
		out = append(out, RawFrame{Function: strings.TrimPrefix(m[1], "new "), URL: m[2], Line: l, Column: c})
		if len(out) == 100 {
			break
		}
	}
	return out
}

// Frame is a frame after symbolication; it is stored as JSON with the error.
type Frame struct {
	Function    string   `json:"function,omitempty"`
	File        string   `json:"file"`
	Line        int      `json:"line,omitempty"`
	Column      int      `json:"column,omitempty"`
	InApp       bool     `json:"in_app"`
	Mapped      bool     `json:"mapped"`
	PreContext  []string `json:"pre_context,omitempty"`
	ContextLine string   `json:"context_line,omitempty"`
	PostContext []string `json:"post_context,omitempty"`
	// The frame as the browser printed it.
	RawFunction string `json:"raw_function,omitempty"`
	RawURL      string `json:"raw_url"`
	RawLine     int    `json:"raw_line,omitempty"`
	RawColumn   int    `json:"raw_column,omitempty"`
}

// ContextLines is how many lines of source surround each mapped frame.
const ContextLines = 5

// Artifacts finds the object key of the source map for a generated file.
type Artifacts interface {
	MapByDebugID(ctx context.Context, project int64, debugID string) (string, error)
	MapByURL(ctx context.Context, project int64, release, fileURL string) (string, error)
}

// ErrNoMap is returned by Artifacts when nothing matches.
var ErrNoMap = errors.New("no source map")

// Objects reads stored maps.
type Objects interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// Symbolicator resolves frames, keeping recently used parsed maps in memory.
type Symbolicator struct {
	Artifacts Artifacts
	Objects   Objects
	// CacheSize is how many parsed maps to keep; 0 means 64.
	CacheSize int
	// MissTTL is how long "no map for this file" is remembered; 0 means 30s.
	MissTTL time.Duration

	mu     sync.Mutex
	lru    *list.List
	maps   map[string]*list.Element
	misses map[string]time.Time
}

type entry struct {
	key string
	m   *sourcemap.Map
	mu  sync.Mutex // Map.Context fills a cache, so lookups on one map are serialized
}

func (s *Symbolicator) cached(key string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maps == nil {
		return nil
	}
	if el, ok := s.maps[key]; ok {
		s.lru.MoveToFront(el)
		return el.Value.(*entry)
	}
	return nil
}

func (s *Symbolicator) store(key string, m *sourcemap.Map) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maps == nil {
		s.maps, s.lru = map[string]*list.Element{}, list.New()
	}
	if el, ok := s.maps[key]; ok {
		return el.Value.(*entry)
	}
	e := &entry{key: key, m: m}
	s.maps[key] = s.lru.PushFront(e)
	size := s.CacheSize
	if size <= 0 {
		size = 64
	}
	for s.lru.Len() > size {
		old := s.lru.Back()
		s.lru.Remove(old)
		delete(s.maps, old.Value.(*entry).key)
	}
	return e
}

func (s *Symbolicator) missed(k string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.misses[k]
	return ok && now.Before(t)
}

func (s *Symbolicator) miss(k string, now time.Time) {
	ttl := s.MissTTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.misses == nil || len(s.misses) > 10000 {
		s.misses = map[string]time.Time{}
	}
	s.misses[k] = now.Add(ttl)
}

// Forget drops cached misses, e.g. after an upload. Safe on nil.
func (s *Symbolicator) Forget() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.misses = nil
	s.mu.Unlock()
}

// load finds and parses the map for a generated file.
func (s *Symbolicator) load(ctx context.Context, project int64, release, fileURL, debugID string) *entry {
	missKey := strconv.FormatInt(project, 10) + "|" + release + "|" + fileURL + "|" + debugID
	now := time.Now()
	if s.missed(missKey, now) {
		return nil
	}
	key, err := "", ErrNoMap
	if debugID != "" {
		key, err = s.Artifacts.MapByDebugID(ctx, project, debugID)
	}
	if errors.Is(err, ErrNoMap) && release != "" {
		key, err = s.Artifacts.MapByURL(ctx, project, release, fileURL)
	}
	if err != nil {
		if !errors.Is(err, ErrNoMap) {
			log.Printf("symbolicate: find map for %s: %v", fileURL, err)
			return nil // don't cache transient failures
		}
		s.miss(missKey, now)
		return nil
	}
	if e := s.cached(key); e != nil {
		return e
	}
	data, err := s.Objects.Get(ctx, key)
	if err != nil {
		log.Printf("symbolicate: read %s: %v", key, err)
		return nil
	}
	m, err := sourcemap.Parse(data)
	if err != nil {
		log.Printf("symbolicate: parse %s: %v", key, err)
		s.miss(missKey, now)
		return nil
	}
	return s.store(key, m)
}

// Result is a symbolicated stack.
type Result struct {
	Frames []Frame
	// Mapped counts frames resolved through a source map.
	Mapped int
}

// Resolve symbolicates a JavaScript stack. debugIDs maps generated file
// URLs to debug ids, as reported by the SDK. Frames without a map are kept
// as printed.
func (s *Symbolicator) Resolve(ctx context.Context, project int64, release string, debugIDs map[string]string, stack string) Result {
	raw := ParseJS(stack)
	out := Result{Frames: make([]Frame, len(raw))}
	pos := make([]*sourcemap.Position, len(raw))
	for i, r := range raw {
		f := Frame{Function: r.Function, File: displayURL(r.URL), Line: r.Line, Column: r.Column,
			RawFunction: r.Function, RawURL: r.URL, RawLine: r.Line, RawColumn: r.Column}
		if s != nil && r.Line > 0 && isFetchable(r.URL) {
			if e := s.load(ctx, project, release, r.URL, debugIDs[r.URL]); e != nil {
				e.mu.Lock()
				if p, ok := e.m.Lookup(r.Line, r.Column); ok {
					pos[i] = &p
					f.File, f.Line, f.Column, f.Mapped = sourcemap.CleanSource(p.Source), p.Line, p.Column, true
					f.PreContext, f.ContextLine, f.PostContext, _ = e.m.Context(p.SourceIndex, p.Line, ContextLines)
					out.Mapped++
				}
				e.mu.Unlock()
			}
		}
		f.InApp = grouping.InApp(f.Function, f.File)
		out.Frames[i] = f
	}
	// A frame's original function name is the name at its caller's call
	// site: the caller (the next frame out) calls it by that name.
	for i := range out.Frames {
		if !out.Frames[i].Mapped {
			continue
		}
		out.Frames[i].Function = ""
		if i+1 < len(pos) && pos[i+1] != nil && pos[i+1].Name != "" {
			out.Frames[i].Function = pos[i+1].Name
		}
		out.Frames[i].InApp = grouping.InApp(out.Frames[i].Function, out.Frames[i].File)
	}
	return out
}

func isFetchable(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "file://") || strings.HasPrefix(u, "app://")
}

// displayURL shows a URL without its origin and query.
func displayURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Path
}

// GroupingFrames converts symbolicated frames for fingerprinting. Mapped
// frames without a known function use their source line, which survives
// rebuilds as long as the code does.
func GroupingFrames(frames []Frame) []grouping.Frame {
	out := make([]grouping.Frame, 0, len(frames))
	for _, f := range frames {
		if !f.Mapped {
			// Parse the printed line again so unmapped frames group exactly
			// as they would without symbolication.
			loc := f.RawURL + ":" + strconv.Itoa(f.RawLine) + ":" + strconv.Itoa(f.RawColumn)
			line := "    at " + loc
			if f.RawFunction != "" {
				line = "    at " + f.RawFunction + " (" + loc + ")"
			}
			out = append(out, grouping.ParseStack(line)...)
			continue
		}
		g := grouping.Frame{Function: f.Function, File: f.File, InApp: f.InApp, Culprit: f.Function != ""}
		if g.Function == "" {
			g.Function = strings.Join(strings.Fields(f.ContextLine), " ")
			if len(g.Function) > 120 {
				g.Function = g.Function[:120]
			}
		}
		out = append(out, g)
	}
	return out
}
