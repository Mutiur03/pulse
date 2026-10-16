// Package insights turns trend query rows into chart series and evaluates
// series formulas such as "A / B * 100".
package insights

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Formula is a parsed arithmetic expression over series letters.
type Formula struct {
	root node
	src  string
}

type node interface {
	eval(v []float64) float64
}

type num float64

func (n num) eval([]float64) float64 { return float64(n) }

type ref int

func (r ref) eval(v []float64) float64 {
	if int(r) < len(v) {
		return v[r]
	}
	return 0
}

type neg struct{ x node }

func (n neg) eval(v []float64) float64 { return -n.x.eval(v) }

type bin struct {
	op   byte
	l, r node
}

func (b bin) eval(v []float64) float64 {
	l, r := b.l.eval(v), b.r.eval(v)
	switch b.op {
	case '+':
		return l + r
	case '-':
		return l - r
	case '*':
		return l * r
	}
	// Division by zero charts as zero rather than infinity.
	if r == 0 {
		return 0
	}
	return l / r
}

// MaxFormulaLength bounds the formula text.
const MaxFormulaLength = 200

// ParseFormula parses expr where A is the first series, B the second and so
// on up to series letters below n.
func ParseFormula(expr string, n int) (*Formula, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errors.New("formula is empty")
	}
	if len(expr) > MaxFormulaLength {
		return nil, errors.New("formula is too long")
	}
	p := &parser{s: expr, n: n}
	root, err := p.sum()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("unexpected %q in formula", p.s[p.i:])
	}
	return &Formula{root: root, src: expr}, nil
}

// Eval computes the formula for one set of series values.
func (f *Formula) Eval(v []float64) float64 { return f.root.eval(v) }

func (f *Formula) String() string { return f.src }

type parser struct {
	s     string
	i     int
	n     int
	depth int
}

func (p *parser) space() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

func (p *parser) peek() byte {
	p.space()
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *parser) sum() (node, error) {
	l, err := p.product()
	if err != nil {
		return nil, err
	}
	for {
		c := p.peek()
		if c != '+' && c != '-' {
			return l, nil
		}
		p.i++
		r, err := p.product()
		if err != nil {
			return nil, err
		}
		l = bin{op: c, l: l, r: r}
	}
}

func (p *parser) product() (node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		c := p.peek()
		if c != '*' && c != '/' {
			return l, nil
		}
		p.i++
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = bin{op: c, l: l, r: r}
	}
}

func (p *parser) unary() (node, error) {
	if p.peek() == '-' {
		p.i++
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return neg{x}, nil
	}
	return p.atom()
}

func (p *parser) atom() (node, error) {
	c := p.peek()
	switch {
	case c == '(':
		p.depth++
		if p.depth > 20 {
			return nil, errors.New("formula is nested too deeply")
		}
		p.i++
		x, err := p.sum()
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, errors.New("missing ) in formula")
		}
		p.i++
		p.depth--
		return x, nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.i
		for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
			p.i++
		}
		v, err := strconv.ParseFloat(p.s[start:p.i], 64)
		if err != nil {
			return nil, fmt.Errorf("bad number %q in formula", p.s[start:p.i])
		}
		return num(v), nil
	case unicode.IsLetter(rune(c)):
		l := unicode.ToUpper(rune(c))
		p.i++
		if p.i < len(p.s) && unicode.IsLetter(rune(p.s[p.i])) {
			return nil, errors.New("series are single letters in formulas (A, B, ...)")
		}
		idx := int(l - 'A')
		if idx < 0 || idx >= p.n {
			return nil, fmt.Errorf("formula uses series %c but there are only %d", l, p.n)
		}
		return ref(idx), nil
	case c == 0:
		return nil, errors.New("formula ends too early")
	}
	return nil, fmt.Errorf("unexpected %q in formula", string(c))
}
