package pricing

import (
	"fmt"
	"math/big"
	"strings"
)

type expressionEnvironment struct {
	vars    map[string]*big.Rat
	request RequestInput
	depth   int
}

func (e *expressionEnvironment) evaluate(n *expression) (any, error) {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > 256 {
		return nil, fmt.Errorf("pricing: expression evaluation exceeds depth limit")
	}
	switch n.op {
	case "literal":
		if value, ok := n.value.(*big.Rat); ok {
			return new(big.Rat).Set(value), nil
		}
		return n.value, nil
	case "var":
		return new(big.Rat).Set(e.vars[n.text]), nil
	case "conditional":
		return e.conditional(n)
	case "unary":
		return e.unary(n)
	case "binary":
		return e.binary(n)
	case "call":
		args, err := e.evalArgs(n.children)
		if err != nil {
			return nil, err
		}
		return e.call(n.text, args)
	}
	return nil, fmt.Errorf("pricing: invalid expression node")
}

// conditional evaluates a ternary node: condition, then-branch, else-branch.
func (e *expressionEnvironment) conditional(n *expression) (any, error) {
	v, err := e.evaluate(n.children[0])
	if err != nil {
		return nil, err
	}
	condition, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf("pricing: conditional requires boolean")
	}
	if condition {
		return e.evaluate(n.children[1])
	}
	return e.evaluate(n.children[2])
}

// unary evaluates `!` (boolean negation) and `-` (numeric negation).
func (e *expressionEnvironment) unary(n *expression) (any, error) {
	v, err := e.evaluate(n.children[0])
	if err != nil {
		return nil, err
	}
	if n.text == "!" {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("pricing: ! requires boolean")
		}
		return !b, nil
	}
	value, ok := v.(*big.Rat)
	if !ok {
		return nil, fmt.Errorf("pricing: unary operator requires number")
	}
	if n.text == "-" {
		value.Neg(value)
	}
	return value, nil
}

// evalArgs evaluates a call node's argument expressions in order.
func (e *expressionEnvironment) evalArgs(children []*expression) ([]any, error) {
	args := make([]any, len(children))
	for i, child := range children {
		v, err := e.evaluate(child)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	return args, nil
}

func (e *expressionEnvironment) binary(n *expression) (any, error) {
	a, err := e.evaluate(n.children[0])
	if err != nil {
		return nil, err
	}
	if n.text == "&&" || n.text == "||" {
		return e.logicalBinary(n, a)
	}
	b, err := e.evaluate(n.children[1])
	if err != nil {
		return nil, err
	}
	if n.text == "has" {
		return e.call("has", []any{a, b})
	}
	left, leftNumeric := a.(*big.Rat)
	right, rightNumeric := b.(*big.Rat)
	if n.text == "==" || n.text == "!=" {
		return equality(n.text, a, b, left, right, leftNumeric, rightNumeric), nil
	}
	if !leftNumeric || !rightNumeric {
		return nil, fmt.Errorf("pricing: %s requires numbers", n.text)
	}
	return arithmeticBinary(n.text, left, right)
}

// logicalBinary evaluates `&&`/`||` with short-circuiting: a is already
// evaluated, and n.children[1] is only evaluated when a doesn't decide it.
func (e *expressionEnvironment) logicalBinary(n *expression, a any) (any, error) {
	v, ok := a.(bool)
	if !ok {
		return nil, fmt.Errorf("pricing: logical operator requires boolean")
	}
	if (n.text == "&&" && !v) || (n.text == "||" && v) {
		return v, nil
	}
	b, err := e.evaluate(n.children[1])
	if err != nil {
		return nil, err
	}
	if v, ok := b.(bool); ok {
		return v, nil
	}
	return nil, fmt.Errorf("pricing: logical operator requires boolean")
}

// equality implements `==`/`!=` across numbers, strings, bools and nil.
func equality(op string, a, b any, left, right *big.Rat, leftNumeric, rightNumeric bool) bool {
	var equal bool
	if leftNumeric && rightNumeric {
		equal = left.Cmp(right) == 0
	} else {
		switch v := a.(type) {
		case nil:
			equal = b == nil
		case string:
			value, ok := b.(string)
			equal = ok && v == value
		case bool:
			value, ok := b.(bool)
			equal = ok && v == value
		}
	}
	if op == "!=" {
		equal = !equal
	}
	return equal
}

// arithmeticBinary implements the numeric operators. left is mutated and
// reused as the result for the arithmetic cases, matching prior behavior.
func arithmeticBinary(op string, left, right *big.Rat) (any, error) {
	switch op {
	case "+":
		left.Add(left, right)
	case "-":
		left.Sub(left, right)
	case "*":
		left.Mul(left, right)
	case "/":
		if right.Sign() == 0 {
			return nil, fmt.Errorf("pricing: division by zero")
		}
		left.Quo(left, right)
	case "%":
		if right.Sign() == 0 || !left.IsInt() || !right.IsInt() {
			return nil, fmt.Errorf("pricing: remainder requires integers and nonzero divisor")
		}
		left.SetInt(new(big.Int).Rem(left.Num(), right.Num()))
	case "<":
		return left.Cmp(right) < 0, nil
	case "<=":
		return left.Cmp(right) <= 0, nil
	case ">":
		return left.Cmp(right) > 0, nil
	case ">=":
		return left.Cmp(right) >= 0, nil
	default:
		return nil, fmt.Errorf("pricing: unknown operator %s", op)
	}
	if left.Num().BitLen() > 512 || left.Denom().BitLen() > 512 {
		return nil, fmt.Errorf("pricing: expression intermediate exceeds size limit")
	}
	return left, nil
}

func (e *expressionEnvironment) call(name string, args []any) (any, error) {
	switch name {
	case "tier":
		value, err := callTier(args)
		if err != nil {
			return nil, err
		}
		if value != nil {
			return value, nil
		}
	case "has":
		return callHas(args)
	case "param", "header", "hour", "minute", "weekday", "month", "day":
		return e.requestFunction(name, args)
	case "min", "max":
		if v, ok := callMinMax(name, args); ok {
			return v, nil
		}
	case "abs", "floor", "ceil":
		if v, ok := callRound(name, args); ok {
			return v, nil
		}
	}
	return nil, fmt.Errorf("pricing: %s received invalid argument types", name)
}

// callTier validates and evaluates tier(label, value). A nil, nil return
// means the arguments were well-formed (two args, string label) but the
// value wasn't numeric; the caller falls through to the generic
// "invalid argument types" error for that case, matching prior behavior.
func callTier(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("pricing: tier requires two arguments")
	}
	if _, ok := args[0].(string); !ok {
		return nil, fmt.Errorf("pricing: tier label must be string")
	}
	if value, ok := args[1].(*big.Rat); ok {
		return value, nil
	}
	return nil, nil
}

func callHas(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("pricing: has requires two arguments")
	}
	sub, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("pricing: has substring must be string")
	}
	if args[0] == nil || sub == "" {
		return false, nil
	}
	return strings.Contains(fmt.Sprint(args[0]), sub), nil
}

func callMinMax(name string, args []any) (any, bool) {
	if len(args) != 2 {
		return nil, false
	}
	a, aOK := args[0].(*big.Rat)
	b, bOK := args[1].(*big.Rat)
	if !aOK || !bOK {
		return nil, false
	}
	if (name == "min" && a.Cmp(b) > 0) || (name == "max" && a.Cmp(b) < 0) {
		return b, true
	}
	return a, true
}

func callRound(name string, args []any) (any, bool) {
	if len(args) != 1 {
		return nil, false
	}
	value, ok := args[0].(*big.Rat)
	if !ok {
		return nil, false
	}
	if name == "abs" {
		return value.Abs(value), true
	}
	whole, rem := new(big.Int), new(big.Int)
	whole.QuoRem(value.Num(), value.Denom(), rem)
	if rem.Sign() != 0 {
		if name == "ceil" && value.Sign() > 0 {
			whole.Add(whole, big.NewInt(1))
		}
		if name == "floor" && value.Sign() < 0 {
			whole.Sub(whole, big.NewInt(1))
		}
	}
	return value.SetInt(whole), true
}
