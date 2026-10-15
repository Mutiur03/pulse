// Package grouping turns raw errors into stable issue fingerprints.
//
// The fingerprint is the exception type plus the top in-app stack frames,
// normalized so that line numbers, build hashes and ids do not split one bug
// into many issues. Errors without a usable stack fall back to the type plus
// a normalized message.
package grouping

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"regexp"
	"strings"
)

// Frame is one parsed stack frame.
type Frame struct {
	Function string
	File     string
	InApp    bool
	// Culprit is false when Function is not a real name (e.g. a source
	// line used as the key), so the culprit shows only the file.
	Culprit bool
}

// MaxFrames is how many in-app frames contribute to the fingerprint.
const MaxFrames = 3

var (
	// JavaScript (V8): "at fn (file:line:col)" or "at file:line:col"
	reV8 = regexp.MustCompile(`^\s*at (?:(?:async )?(.+?) \()?(.+?)(?::\d+)?(?::\d+)?\)?$`)
	// JavaScript (Firefox/Safari): "fn@file:line:col"
	reGecko = regexp.MustCompile(`^\s*(.*?)@(.+?)(?::\d+)?(?::\d+)?$`)
	// Python: File "path", line N, in fn
	rePython = regexp.MustCompile(`^\s*File "(.+?)", line \d+, in (.+)$`)
	// Java/Kotlin: "at pkg.Class.method(File.java:10)"
	reJava = regexp.MustCompile(`^\s*at ([\w$.<>]+)\((.*?)(?::\d+)?\)$`)
	// Go: "pkg.Func(...)" followed by "\t/path/file.go:42 +0x1d"
	reGoFunc = regexp.MustCompile(`^([\w./\-]+(?:\.\(?\*?[\w]+\)?)?\.[\w.\-]+)\(.*\)$`)
	reGoFile = regexp.MustCompile(`^\s+(.+\.go):\d+(?: \+0x[0-9a-f]+)?$`)

	// Build hashes in bundle names: app.3f2a1b9c.js, chunk-8XK2LQ.js
	reBundleHash = regexp.MustCompile(`[.\-][0-9a-zA-Z]{6,}(\.(?:m?js|cjs|css))$`)
)

var notInApp = []string{
	"node_modules/", "/site-packages/", "/dist-packages/", "/go/pkg/mod/", "/usr/lib/", "/usr/local/go/src/",
	"runtime/", "net/http.", "reflect.", "testing.", "<anonymous>", "native", "internal/",
	"chrome-extension://", "moz-extension://", "webpack/bootstrap", "java.", "javax.", "sun.", "jdk.", "kotlin.",
	"zone.js", "@opentelemetry/",
}

// InApp reports whether a frame is application code rather than a library,
// the runtime or the browser.
func InApp(fn, file string) bool { return inApp(fn, file) }

func inApp(fn, file string) bool {
	s := file + " " + fn
	if file == "" && fn == "" {
		return false
	}
	for _, p := range notInApp {
		if strings.Contains(s, p) || strings.HasPrefix(fn, p) {
			return false
		}
	}
	return true
}

// normalizeFile drops origins, query strings and bundle hashes and keeps the
// last two path segments, so the same code deployed elsewhere groups together.
func normalizeFile(f string) string {
	f = strings.TrimSpace(f)
	if i := strings.Index(f, "://"); i >= 0 {
		rest := f[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			f = rest[j:]
		}
	}
	if i := strings.IndexAny(f, "?#"); i >= 0 {
		f = f[:i]
	}
	f = reBundleHash.ReplaceAllString(f, "$1")
	dir, base := path.Split(strings.ReplaceAll(f, "\\", "/"))
	parent := path.Base(strings.TrimSuffix(dir, "/"))
	if parent == "." || parent == "/" || parent == "" {
		return base
	}
	return parent + "/" + base
}

// ParseStack extracts frames from JS, Python, Java and Go stack traces.
// Frames are returned innermost (most recent call) first.
func ParseStack(stack string) []Frame {
	lines := strings.Split(strings.ReplaceAll(stack, "\r\n", "\n"), "\n")
	var frames []Frame
	python := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Traceback (most recent call last)") {
			python = true
			continue
		}
		if m := rePython.FindStringSubmatch(line); m != nil {
			frames = append(frames, Frame{Function: m[2], File: normalizeFile(m[1]), InApp: inApp(m[2], m[1]), Culprit: true})
			continue
		}
		if m := reGoFunc.FindStringSubmatch(trimmed); m != nil && i+1 < len(lines) {
			if f := reGoFile.FindStringSubmatch(lines[i+1]); f != nil {
				frames = append(frames, Frame{Function: m[1], File: normalizeFile(f[1]), InApp: inApp(m[1], f[1]), Culprit: true})
				i++
				continue
			}
		}
		if m := reJava.FindStringSubmatch(line); m != nil && !strings.Contains(m[2], "/") {
			frames = append(frames, Frame{Function: m[1], File: m[2], InApp: inApp(m[1], m[1]), Culprit: true})
			continue
		}
		if m := reV8.FindStringSubmatch(line); m != nil {
			fn := strings.TrimPrefix(m[1], "new ")
			frames = append(frames, Frame{Function: fn, File: normalizeFile(m[2]), InApp: inApp(fn, m[2]), Culprit: true})
			continue
		}
		if strings.Contains(trimmed, "@") && !strings.Contains(trimmed, " ") {
			if m := reGecko.FindStringSubmatch(trimmed); m != nil {
				frames = append(frames, Frame{Function: m[1], File: normalizeFile(m[2]), InApp: inApp(m[1], m[2]), Culprit: true})
			}
		}
	}
	if python {
		// Python prints the outermost call first.
		for l, r := 0, len(frames)-1; l < r; l, r = l+1, r-1 {
			frames[l], frames[r] = frames[r], frames[l]
		}
	}
	return frames
}

var messageRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	{regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`), "<email>"},
	{regexp.MustCompile(`https?://\S+`), "<url>"},
	{regexp.MustCompile(`(?i)\b0x[0-9a-f]+\b`), "<hex>"},
	{regexp.MustCompile(`(?i)\b[0-9a-f]{12,}\b`), "<hex>"},
	{regexp.MustCompile(`\b\d+(?:\.\d+)*`), "<n>"},
	{regexp.MustCompile(`'[^']*'|"[^"]*"`), "<str>"},
	{regexp.MustCompile(`\s+`), " "},
}

// NormalizeMessage strips the parts of an error message that vary per
// occurrence: ids, numbers, hex, emails, URLs and quoted values.
func NormalizeMessage(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	for _, r := range messageRules {
		msg = r.re.ReplaceAllString(msg, r.repl)
	}
	return strings.TrimSpace(msg)
}

// Result is the grouping decision for one error.
type Result struct {
	Fingerprint string
	Title       string
	Culprit     string // top in-app frame, e.g. "pay (src/checkout.js)"
	Frames      []Frame
}

// Group computes the fingerprint, a readable title and the culprit frame.
func Group(excType, message, stack string) Result {
	return GroupFrames(excType, message, ParseStack(stack))
}

// GroupFrames groups by already parsed frames, innermost first. Symbolicated
// errors use it with original files and functions, so the fingerprint does
// not change when a new build renames or moves minified code.
func GroupFrames(excType, message string, frames []Frame) Result {
	if excType == "" {
		excType = "Error"
	}
	var keys []string
	culprit := ""
	for _, f := range frames {
		if !f.InApp {
			continue
		}
		k := f.Function + "@" + f.File
		if culprit == "" {
			culprit = f.File
			if f.Function != "" && f.Culprit {
				culprit = strings.TrimSpace(f.Function + " (" + f.File + ")")
			}
		}
		keys = append(keys, k)
		if len(keys) == MaxFrames {
			break
		}
	}
	norm := NormalizeMessage(message)
	parts := []string{excType}
	if len(keys) > 0 {
		parts = append(parts, keys...)
	} else {
		parts = append(parts, norm)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))

	title := excType
	if first := strings.SplitN(strings.TrimSpace(message), "\n", 2)[0]; first != "" {
		title += ": " + first
	}
	if len(title) > 200 {
		title = title[:200]
	}
	return Result{Fingerprint: hex.EncodeToString(sum[:16]), Title: title, Culprit: culprit, Frames: frames}
}
