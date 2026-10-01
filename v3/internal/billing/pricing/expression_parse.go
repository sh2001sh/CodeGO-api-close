package pricing

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
)

type expression struct {
	op       string
	text     string
	value    any
	children []*expression
}

type expressionParser struct {
	tokens []string
	pos    int
	depth  int
	used   map[string]bool
}

func parseExpression(source string) (*expression, map[string]bool, error) {
	if len(source) == 0 || len(source) > 16384 {
		return nil, nil, fmt.Errorf("pricing: expression length must be 1..16384")
	}
	var scan scanner.Scanner
	scan.Init(strings.NewReader(source))
	scan.Mode = scanner.ScanIdents | scanner.ScanInts | scanner.ScanFloats | scanner.ScanStrings
	var scanErr error
	scan.Error = func(s *scanner.Scanner, msg string) {
		scanErr = fmt.Errorf("pricing: expression %s: %s", s.Position, msg)
	}
	var tokens []string
	for scan.Scan() != scanner.EOF {
		tokens = append(tokens, scan.TokenText())
		if len(tokens) > 4096 {
			return nil, nil, fmt.Errorf("pricing: expression has too many tokens")
		}
	}
	if scanErr != nil {
		return nil, nil, scanErr
	}
	// The standard scanner emits punctuation separately; join compound operators.
	joined := tokens[:0]
	for i := 0; i < len(tokens); i++ {
		if i+1 < len(tokens) {
			pair := tokens[i] + tokens[i+1]
			switch pair {
			case "&&", "||", "<=", ">=", "==", "!=":
				joined = append(joined, pair)
				i++
				continue
			}
		}
		joined = append(joined, tokens[i])
	}
	p := expressionParser{tokens: joined, used: make(map[string]bool)}
	n, err := p.conditional()
	if err == nil && p.pos != len(p.tokens) {
		err = fmt.Errorf("pricing: unexpected expression token %q", p.peek())
	}
	return n, p.used, err
}

func (p *expressionParser) peek() string {
	if p.pos == len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *expressionParser) expect(token string) error {
	if p.peek() != token {
		return fmt.Errorf("pricing: expected %q, got %q", token, p.peek())
	}
	p.pos++
	return nil
}

func (p *expressionParser) conditional() (*expression, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		return nil, fmt.Errorf("pricing: expression nesting exceeds 64")
	}
	n, err := p.binary(1)
	if err != nil || p.peek() != "?" {
		return n, err
	}
	p.pos++
	a, err := p.conditional()
	if err != nil {
		return nil, err
	}
	if err := p.expect(":"); err != nil {
		return nil, err
	}
	b, err := p.conditional()
	if err != nil {
		return nil, err
	}
	return &expression{op: "conditional", children: []*expression{n, a, b}}, nil
}

func precedence(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=", "<", "<=", ">", ">=", "has":
		return 3
	case "+", "-":
		return 4
	case "*", "/", "%":
		return 5
	}
	return 0
}

func (p *expressionParser) binary(minimum int) (*expression, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 64 {
		return nil, fmt.Errorf("pricing: expression nesting exceeds 64")
	}
	n, err := p.primary()
	if err != nil {
		return nil, err
	}
	for precedence(p.peek()) >= minimum {
		op := p.peek()
		p.pos++
		other, err := p.binary(precedence(op) + 1)
		if err != nil {
			return nil, err
		}
		n = &expression{op: "binary", text: op, children: []*expression{n, other}}
	}
	return n, nil
}

func (p *expressionParser) primary() (*expression, error) {
	token := p.peek()
	if token == "" {
		return nil, fmt.Errorf("pricing: incomplete expression")
	}
	p.pos++
	if token == "+" || token == "-" || token == "!" {
		n, err := p.binary(6)
		return &expression{op: "unary", text: token, children: []*expression{n}}, err
	}
	if token == "(" {
		n, err := p.conditional()
		if err != nil {
			return nil, err
		}
		return n, p.expect(")")
	}
	if strings.HasPrefix(token, `"`) {
		value, err := strconv.Unquote(token)
		return &expression{op: "literal", value: value}, err
	}
	if n, err := exactNumber(token); err == nil {
		return &expression{op: "literal", value: n}, nil
	}
	if token == "true" || token == "false" {
		return &expression{op: "literal", value: token == "true"}, nil
	}
	if token == "nil" || token == "null" {
		return &expression{op: "literal"}, nil
	}
	if p.peek() == "(" {
		if !allowedFunction(token) {
			return nil, fmt.Errorf("pricing: unknown expression function %q", token)
		}
		p.pos++
		n := &expression{op: "call", text: token}
		for p.peek() != ")" {
			arg, err := p.conditional()
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, arg)
			if p.peek() != "," {
				break
			}
			p.pos++
		}
		return n, p.expect(")")
	}
	switch token {
	case "p", "c", "len", "cr", "cc", "cc1h", "img", "img_o", "ai", "ao":
		p.used[token] = true
		return &expression{op: "var", text: token}, nil
	}
	return nil, fmt.Errorf("pricing: unknown expression variable %q", token)
}

func allowedFunction(name string) bool {
	switch name {
	case "when", "tier", "param", "header", "has", "hour", "minute", "weekday", "month", "day", "min", "max", "abs", "ceil", "floor":
		return true
	}
	return false
}
