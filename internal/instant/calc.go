// Package instant answers some searches on the server itself, above the
// results: arithmetic, unit and currency conversions, the time in a city and
// the weather. Calculations never leave the server; rates and weather come
// from keyless public APIs (open.er-api.com, open-meteo.com), asked by the
// server and kept a while.
package instant

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Answer is one instant answer.
type Answer struct {
	Kind   string `json:"kind"`   // calc | convert | currency | time | weather | tasks | trip | agenda
	Title  string `json:"title"`  // the question, cleaned up: "15% of 240"
	Value  string `json:"value"`  // the answer, big: "36"
	Detail string `json:"detail"` // one smaller line under it
	Rows   []Row  `json:"rows,omitempty"`
	Source string `json:"source,omitempty"`
	URL    string `json:"url,omitempty"` // where the answer can be seen in full
}

// Row is one line of a list answer (a forecast day, a task).
type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
	URL   string `json:"url,omitempty"`
}

var (
	reCalcChars  = regexp.MustCompile(`^[0-9\s.,+\-*/×÷^%()a-z]+$`)
	rePctOf      = regexp.MustCompile(`^\s*([0-9.,]+)\s*%\s*of\s*([0-9.,]+)\s*$`)
	reAddPct     = regexp.MustCompile(`^\s*([0-9.,]+)\s*([+-])\s*([0-9.,]+)\s*%\s*$`)
	reHasOp      = regexp.MustCompile(`[+\-*/×÷^%]|\b(sqrt|sin|cos|tan|log|ln|abs|round)\b`)
	reCalcPrefix = regexp.MustCompile(`^(calc(ulate)?|what\s+is|what's|=)\s+`)
)

// Calc answers arithmetic: "15% of 240", "(3+4)*2^3", "sqrt(2)", "120 / 7".
func Calc(q string) *Answer {
	s := strings.ToLower(strings.TrimSpace(q))
	s = reCalcPrefix.ReplaceAllString(s, "")
	s = strings.TrimSuffix(strings.TrimSpace(s), "=")
	s = strings.TrimSuffix(strings.TrimSpace(s), "?")
	if m := rePctOf.FindStringSubmatch(s); m != nil {
		a, err1 := num(m[1])
		b, err2 := num(m[2])
		if err1 == nil && err2 == nil {
			return &Answer{Kind: "calc", Title: m[1] + "% of " + m[2], Value: fmtNum(a / 100 * b)}
		}
	}
	if m := reAddPct.FindStringSubmatch(s); m != nil {
		a, err1 := num(m[1])
		b, err2 := num(m[3])
		if err1 == nil && err2 == nil {
			v := a * (1 + b/100)
			if m[2] == "-" {
				v = a * (1 - b/100)
			}
			return &Answer{Kind: "calc", Title: strings.TrimSpace(q), Value: fmtNum(v)}
		}
	}
	if !reCalcChars.MatchString(s) || !reHasOp.MatchString(s) || !strings.ContainsAny(s, "0123456789") {
		return nil
	}
	// a lone "-5" or a date "2026-11-01" is not a sum
	if regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(s) || regexp.MustCompile(`^-?\d+$`).MatchString(strings.ReplaceAll(s, " ", "")) {
		return nil
	}
	p := &parser{src: strings.NewReplacer("×", "*", "÷", "/", ",", ".").Replace(s)}
	v, err := p.expr()
	if err != nil || p.pos < len(p.src) || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &Answer{Kind: "calc", Title: strings.TrimSpace(q), Value: fmtNum(v)}
}

func num(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
}

// fmtNum shows a number the way a calculator does: no float noise, thousands spaced.
func fmtNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return group(strconv.FormatInt(int64(v), 10))
	}
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if r := strconv.FormatFloat(v, 'g', 10, 64); len(r) < len(s) {
		s = r
	}
	if i := strings.Index(s, "."); i >= 0 && !strings.ContainsAny(s, "e") {
		return group(s[:i]) + s[i:]
	}
	return s
}

func group(s string) string {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	if len(s) <= 4 {
		if neg {
			return "-" + s
		}
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// parser is a small recursive-descent calculator.
type parser struct {
	src string
	pos int
}

var errSyntax = errors.New("syntax")

func (p *parser) skip() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.skip()
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

func (p *parser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '+', '-':
			op := p.src[p.pos]
			p.pos++
			r, err := p.term()
			if err != nil {
				return 0, err
			}
			if op == '+' {
				v += r
			} else {
				v -= r
			}
		default:
			return v, nil
		}
	}
}

func (p *parser) term() (float64, error) {
	v, err := p.power()
	if err != nil {
		return 0, err
	}
	for {
		switch p.peek() {
		case '*', '/':
			op := p.src[p.pos]
			p.pos++
			r, err := p.power()
			if err != nil {
				return 0, err
			}
			if op == '*' {
				v *= r
			} else {
				if r == 0 {
					return 0, errSyntax
				}
				v /= r
			}
		default:
			return v, nil
		}
	}
}

func (p *parser) power() (float64, error) {
	v, err := p.unary()
	if err != nil {
		return 0, err
	}
	if p.peek() == '^' {
		p.pos++
		r, err := p.power()
		if err != nil {
			return 0, err
		}
		return math.Pow(v, r), nil
	}
	return v, nil
}

func (p *parser) unary() (float64, error) {
	if p.peek() == '-' {
		p.pos++
		v, err := p.unary()
		return -v, err
	}
	if p.peek() == '+' {
		p.pos++
	}
	return p.atom()
}

var funcs = map[string]func(float64) float64{
	"sqrt": math.Sqrt, "sin": math.Sin, "cos": math.Cos, "tan": math.Tan, "log": math.Log10, "ln": math.Log,
	"abs": math.Abs, "round": math.Round,
}

func (p *parser) atom() (float64, error) {
	c := p.peek()
	switch {
	case c == '(':
		p.pos++
		v, err := p.expr()
		if err != nil || p.peek() != ')' {
			return 0, errSyntax
		}
		p.pos++
		return p.percent(v), nil
	case c >= '0' && c <= '9' || c == '.':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		v, err := strconv.ParseFloat(p.src[start:p.pos], 64)
		if err != nil {
			return 0, errSyntax
		}
		return p.percent(v), nil
	case c >= 'a' && c <= 'z':
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] >= 'a' && p.src[p.pos] <= 'z' {
			p.pos++
		}
		name := p.src[start:p.pos]
		switch name {
		case "pi":
			return math.Pi, nil
		case "e":
			return math.E, nil
		}
		f, ok := funcs[name]
		if !ok {
			return 0, errSyntax
		}
		v, err := p.atom()
		if err != nil {
			return 0, err
		}
		return f(v), nil
	}
	return 0, errSyntax
}

// percent turns "50%" into 0.5 ("200 + 10%" is handled before parsing).
func (p *parser) percent(v float64) float64 {
	if p.peek() == '%' {
		p.pos++
		return v / 100
	}
	return v
}
