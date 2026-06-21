package validation

import (
	"errors"
	"strings"

	pa "github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
)

// CSS Values and Units 4 — Math Functions.
//
// This file provides:
//
//   - isMathFunction: cheap predicate matching the function name.
//   - parseMath:      shape-check + wrap into a [pr.PendingMath]. The check
//                     mirrors WeasyPrint's `check_math` in
//                     weasyprint/css/functions.py — it ensures the function
//                     name is known, the operand count fits the arity rules
//                     and every leaf is a recognisable token type for the
//                     requested resolved-to category. It does NOT evaluate
//                     anything; full evaluation happens at compute time.
//
// :copyright: 2011-2014 Simon Sapin and contributors (WeasyPrint).
// :license:   BSD.

// Math function names recognised by CSS Values 4.
//
// Arities documented inline; "*" means variadic.
var mathFunctionArity = map[string][2]int{
	// CSS Values 4 — basic
	"calc":  {1, 1},
	"min":   {1, -1}, // 1..*
	"max":   {1, -1},
	"clamp": {3, 3}, // (MIN, VAL, MAX)

	// CSS Values 4 — stepped
	"round": {1, 3}, // [<rounding-strategy>,]? A[, B]
	"mod":   {2, 2},
	"rem":   {2, 2},

	// CSS Values 4 — trig
	"sin":   {1, 1},
	"cos":   {1, 1},
	"tan":   {1, 1},
	"asin":  {1, 1},
	"acos":  {1, 1},
	"atan":  {1, 1},
	"atan2": {2, 2},

	// CSS Values 4 — exponential
	"pow":   {2, 2},
	"sqrt":  {1, 1},
	"hypot": {1, -1},
	"log":   {1, 2},
	"exp":   {1, 1},

	// CSS Values 4 — sign-related
	"abs":  {1, 1},
	"sign": {1, 1},
}

// roundingStrategies are the leading idents allowed inside round().
var roundingStrategies = map[string]bool{
	"nearest":   true,
	"up":        true,
	"down":      true,
	"to-zero":   true,
}

// errors surfaced from shape-checking. Validators usually convert these
// into a generic ErrInvalidValue, but the type lets the evaluator detect
// "obviously broken" expressions before resolution.
var (
	errMathBadFunction = errors.New("unknown math function")
	errMathBadArity    = errors.New("wrong number of arguments to math function")
	errMathBadOperand  = errors.New("invalid operand in math function")
)

// isMathFunction reports whether token is a FunctionBlock whose name is one
// of the math functions defined by CSS Values 4.
func isMathFunction(token pa.Token) bool {
	fb, ok := token.(pa.FunctionBlock)
	if !ok {
		return false
	}
	_, in := mathFunctionArity[strings.ToLower(fb.Name)]
	return in
}

// parseMath shape-checks the math function in token and returns a
// [pr.PendingMath] wrapping it. resolveTo says what unit shape the
// consuming property requires — the check accepts any operand that *might*
// resolve to that shape, deferring numeric reduction to evaluation time.
//
// percentageReferTo is propagated to the PendingMath so the evaluator
// knows which layout dimension to substitute for percentages (empty when
// percentages must remain symbolic until layout).
func parseMath(token pa.Token, resolveTo pr.MathType, percentageReferTo string) (pr.PendingMath, error) {
	fb, ok := token.(pa.FunctionBlock)
	if !ok {
		return pr.PendingMath{}, errMathBadFunction
	}
	name := strings.ToLower(fb.Name)
	if _, in := mathFunctionArity[name]; !in {
		return pr.PendingMath{}, errMathBadFunction
	}
	if err := checkMath(fb, resolveTo); err != nil {
		return pr.PendingMath{}, err
	}
	return pr.PendingMath{
		Expr:              fb,
		ResolveTo:         resolveTo,
		PercentageReferTo: percentageReferTo,
	}, nil
}

// checkMath walks fb to verify its shape. It does NOT evaluate operands
// nor resolve units; it only catches syntactic errors that the evaluator
// would otherwise have to re-discover later.
func checkMath(fb pa.FunctionBlock, resolveTo pr.MathType) error {
	name := strings.ToLower(fb.Name)
	arity := mathFunctionArity[name]

	args, err := splitMathArguments(fb.Arguments)
	if err != nil {
		return err
	}

	// round() may take a leading rounding-strategy ident; strip it so the
	// remaining arity check matches the documented numeric-arg counts.
	// Three numeric arguments (without strategy) is invalid.
	if name == "round" {
		if len(args) > 0 {
			if leadKw := singleIdent(args[0]); roundingStrategies[leadKw] {
				args = args[1:]
			}
		}
		// After stripping a possible leading strategy, round() accepts
		// 1 or 2 numeric arguments — never 3.
		if len(args) < 1 || len(args) > 2 {
			return errMathBadArity
		}
	} else {
		min, max := arity[0], arity[1]
		if len(args) < min || (max >= 0 && len(args) > max) {
			return errMathBadArity
		}
	}

	// Per-function operand-shape constraints. The trig/exp functions take
	// numbers (or numeric calc()) and produce numbers/angles; the sign of
	// abs() preserves its argument's shape; etc. We only enforce the
	// strictest rules — leaving unit reduction to the evaluator.
	for _, arg := range args {
		if err := checkMathOperand(arg, resolveTo, name); err != nil {
			return err
		}
	}
	return nil
}

// splitMathArguments breaks a comma-separated argument list, stripping
// surrounding whitespace from each segment. An empty list (e.g. "calc()")
// is reported as a single empty segment so arity checks reject it.
func splitMathArguments(tokens []pa.Token) ([][]pa.Token, error) {
	var out [][]pa.Token
	var current []pa.Token
	flush := func() {
		out = append(out, trimWhitespace(current))
		current = nil
	}
	for _, t := range tokens {
		if lit, ok := t.(pa.Literal); ok && lit.Value == "," {
			flush()
			continue
		}
		current = append(current, t)
	}
	flush()
	for _, seg := range out {
		if len(seg) == 0 {
			return nil, errMathBadArity
		}
	}
	return out, nil
}

// trimWhitespace strips leading/trailing whitespace tokens.
func trimWhitespace(in []pa.Token) []pa.Token {
	start, end := 0, len(in)
	for start < end {
		if _, ws := in[start].(pa.Whitespace); !ws {
			break
		}
		start++
	}
	for end > start {
		if _, ws := in[end-1].(pa.Whitespace); !ws {
			break
		}
		end--
	}
	return in[start:end]
}

// singleIdent returns the lowercase value of a single Ident operand, or "".
func singleIdent(arg []pa.Token) string {
	if len(arg) != 1 {
		return ""
	}
	id, ok := arg[0].(pa.Ident)
	if !ok {
		return ""
	}
	return strings.ToLower(id.Value)
}

// checkMathOperand walks arg (one comma-separated argument of a math
// function) and validates that every leaf is a token type compatible with
// the resolved-to category. It recurses into nested calc()/min()/etc.
//
// fnName drives a few function-specific extra rules — e.g. trig functions
// only accept numbers and angles.
func checkMathOperand(arg []pa.Token, resolveTo pr.MathType, fnName string) error {
	for _, t := range arg {
		if err := checkMathToken(t, resolveTo, fnName); err != nil {
			return err
		}
	}
	return nil
}

func checkMathToken(t pa.Token, resolveTo pr.MathType, fnName string) error {
	switch v := t.(type) {
	case pa.Whitespace, pa.Comment:
		return nil
	case pa.Literal:
		// Operators + - * / and parens-implicit comma are accepted —
		// detailed precedence checking happens during evaluation.
		switch v.Value {
		case "+", "-", "*", "/":
			return nil
		}
		return errMathBadOperand
	case pa.Number:
		return nil
	case pa.Percentage:
		if !resolveTo.AcceptsPercentage() {
			return errMathBadOperand
		}
		return nil
	case pa.Dimension:
		unit := strings.ToLower(string(v.Unit))
		if _, isLength := LENGTHUNITS[unit]; isLength {
			if !resolveTo.AcceptsLength() {
				return errMathBadOperand
			}
			return nil
		}
		if _, isAngle := AngleUnits[unit]; isAngle {
			if resolveTo == pr.MathAngle || isTrigFunction(fnName) {
				return nil
			}
			return errMathBadOperand
		}
		if _, isResolution := RESOLUTIONTODPPX[unit]; isResolution {
			if resolveTo == pr.MathResolution {
				return nil
			}
			return errMathBadOperand
		}
		return errMathBadOperand
	case pa.Ident:
		// Math constants — pi, e, infinity, -infinity, NaN.
		switch strings.ToLower(v.Value) {
		case "pi", "e", "infinity", "-infinity", "nan":
			return nil
		}
		return errMathBadOperand
	case pa.ParenthesesBlock:
		return checkMathOperand(v.Arguments, resolveTo, fnName)
	case pa.FunctionBlock:
		if !isMathFunction(v) {
			return errMathBadFunction
		}
		return checkMath(v, resolveTo)
	}
	return errMathBadOperand
}

func isTrigFunction(name string) bool {
	switch name {
	case "sin", "cos", "tan", "asin", "acos", "atan", "atan2":
		return true
	}
	return false
}

// --- Validator helpers ------------------------------------------------------
//
// These wrap getLength/getAngle/etc. so a property can transparently accept
// either a concrete value or a math function. A single nil return means the
// token is neither a valid concrete value nor a valid math function.

// getLengthOrCalc returns either a [pr.DimOrS] (concrete length) or a
// [pr.PendingMath] (deferred). negative and percentage flags propagate to the
// concrete-path [getLength]. percentageReferTo, when non-empty, marks the
// PendingMath so the evaluator knows which layout dimension to use.
func getLengthOrCalc(token pa.Token, negative, percentage bool, percentageReferTo string) pr.CssProperty {
	if l := getLength(token, negative, percentage); !l.IsNone() {
		return l.Tagged()
	}
	if !isMathFunction(token) {
		return nil
	}
	shape := pr.MathLength
	if percentage {
		shape = pr.MathLengthPercentage
	}
	pm, err := parseMath(token, shape, percentageReferTo)
	if err != nil {
		return nil
	}
	return pm
}

// getLengthPercentageOrCalc is a thin wrapper requiring percentage support.
func getLengthPercentageOrCalc(token pa.Token, negative bool, percentageReferTo string) pr.CssProperty {
	return getLengthOrCalc(token, negative, true, percentageReferTo)
}

// getNumberOrCalc accepts a <number> or a math function that resolves to a
// number. Returns pr.Float on the concrete path, pr.PendingMath on the math
// path, nil otherwise.
func getNumberOrCalc(token pa.Token) pr.CssProperty {
	if n, ok := token.(pa.Number); ok {
		return pr.Float(n.ValueF)
	}
	if !isMathFunction(token) {
		return nil
	}
	pm, err := parseMath(token, pr.MathNumber, "")
	if err != nil {
		return nil
	}
	return pm
}

// getAngleOrCalc accepts an <angle> or a math function that resolves to an
// angle (in radians). Returns pr.Float (radians) on the concrete path.
func getAngleOrCalc(token pa.Token) pr.CssProperty {
	if rad, ok := getAngle(token); ok {
		return pr.Float(rad)
	}
	if !isMathFunction(token) {
		return nil
	}
	pm, err := parseMath(token, pr.MathAngle, "")
	if err != nil {
		return nil
	}
	return pm
}

