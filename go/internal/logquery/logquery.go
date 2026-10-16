// Package logquery parses the search language of the logs explorer and log
// alerts and turns it into a ClickHouse condition on the logs table.
//
// A query is a list of terms that must all match:
//
//	timeout                 body contains "timeout" (case-insensitive)
//	"payment failed"        body contains the phrase
//	level:error             severity (trace, debug, info, warn, error, fatal);
//	                        level:warn+ means warn or worse
//	service:checkout        service name
//	trace:<id> span:<id>    trace or span id
//	order_id:1234           attribute equals the value
//	-term                   negates any term
package logquery

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Levels in increasing severity.
var Levels = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}

// Term is one condition of a query.
type Term struct {
	Field string // "" for the body, else level, service, trace, span or an attribute key
	Value string
	// OrWorse is set for level:x+.
	OrWorse bool
	Not     bool
}

// Query is a parsed search.
type Query struct{ Terms []Term }

// MaxLength caps the query text.
const MaxLength = 1000

// Parse reads a query. An empty query matches every log.
func Parse(s string) (Query, error) {
	if len(s) > MaxLength {
		return Query{}, fmt.Errorf("query must be at most %d characters", MaxLength)
	}
	var q Query
	toks, err := tokens(s)
	if err != nil {
		return q, err
	}
	for _, t := range toks {
		term := Term{}
		raw := t.text
		if !t.quoted && strings.HasPrefix(raw, "-") && len(raw) > 1 {
			term.Not, raw = true, raw[1:]
		}
		if k, v, ok := strings.Cut(raw, ":"); ok && !t.quoted && k != "" && validKey(k) {
			term.Field, term.Value = strings.ToLower(k), unquote(v)
			if term.Field != "level" && term.Field != "service" && term.Field != "trace" && term.Field != "span" {
				term.Field = k // attribute keys keep their case
			}
		} else {
			term.Value = unquote(raw)
		}
		if term.Field == "level" || term.Field == "severity" {
			term.Field = "level"
			v := strings.ToUpper(term.Value)
			if strings.HasSuffix(v, "+") {
				term.OrWorse, v = true, strings.TrimSuffix(v, "+")
			}
			if v == "WARNING" {
				v = "WARN"
			}
			if levelIndex(v) < 0 {
				return Query{}, fmt.Errorf("unknown level %q; use one of trace, debug, info, warn, error, fatal", term.Value)
			}
			term.Value = v
		}
		if term.Value == "" {
			return Query{}, fmt.Errorf("%q needs a value", t.text)
		}
		q.Terms = append(q.Terms, term)
	}
	return q, nil
}

type token struct {
	text   string
	quoted bool
}

// tokens splits on spaces, keeping "quoted phrases" (also as key:"a b").
func tokens(s string) ([]token, error) {
	var out []token
	var cur strings.Builder
	inQuote, quotedWhole := false, false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, token{cur.String(), quotedWhole})
		}
		cur.Reset()
		quotedWhole = false
	}
	for _, r := range s {
		switch {
		case r == '"':
			if !inQuote && cur.Len() == 0 {
				quotedWhole = true
			}
			inQuote = !inQuote
			cur.WriteRune(r)
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if inQuote {
		return nil, errors.New("unclosed quote")
	}
	flush()
	return out, nil
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func validKey(k string) bool {
	for _, r := range k {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func levelIndex(v string) int {
	for i, l := range Levels {
		if l == v {
			return i
		}
	}
	return -1
}

// Where returns the condition (joined with AND) and its arguments. It
// returns "1" for an empty query.
func (q Query) Where() (string, []interface{}) {
	if len(q.Terms) == 0 {
		return "1", nil
	}
	parts := make([]string, 0, len(q.Terms))
	var args []interface{}
	for _, t := range q.Terms {
		var c string
		switch t.Field {
		case "":
			c, args = "positionCaseInsensitive(body, ?) > 0", append(args, t.Value)
		case "level":
			if t.OrWorse {
				in := Levels[levelIndex(t.Value):]
				c = "severity IN (" + strings.TrimSuffix(strings.Repeat("?, ", len(in)), ", ") + ")"
				for _, l := range in {
					args = append(args, l)
				}
				if t.Value == "WARN" {
					c = "(" + c + " OR severity = 'WARNING')"
				}
			} else {
				c, args = "severity = ?", append(args, t.Value)
				if t.Value == "WARN" {
					c = "severity IN ('WARN', 'WARNING')"
					args = args[:len(args)-1]
				}
			}
		case "service":
			c, args = "service_name = ?", append(args, t.Value)
		case "trace":
			c, args = "trace_id = ?", append(args, strings.ToLower(t.Value))
		case "span":
			c, args = "span_id = ?", append(args, strings.ToLower(t.Value))
		default:
			// Attribute values are compared as text, so numbers match too.
			c, args = "JSONExtractRaw(attributes, ?) IN (?, ?)", append(args, t.Field, t.Value, `"`+t.Value+`"`)
		}
		if t.Not {
			c = "NOT (" + c + ")"
		}
		parts = append(parts, c)
	}
	return strings.Join(parts, " AND "), args
}

// String writes the query back in canonical form.
func (q Query) String() string {
	parts := make([]string, 0, len(q.Terms))
	for _, t := range q.Terms {
		v := t.Value
		if strings.ContainsAny(v, " \t") {
			v = `"` + v + `"`
		}
		s := v
		if t.Field != "" {
			if t.Field == "level" {
				v = strings.ToLower(v)
				if t.OrWorse {
					v += "+"
				}
			}
			s = t.Field + ":" + v
		}
		if t.Not {
			s = "-" + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}
