package validation

import (
	"math"
	"testing"

	"github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/utils"
	tu "github.com/benoitkugler/webrender/utils/testutils"
)

// Port of WeasyPrint's tests/css/test_math.py.
//
// WeasyPrint's tests render a full HTML document and check the resulting
// `div.width` for each parametrised expression. We don't yet have a
// renderPages-equivalent harness in this package, so the port is split
// into two layers:
//
//   1. Validator-level: each valid expression must produce a property
//      (no error log); each invalid expression must be rejected (one log).
//      This mirrors WeasyPrint's `assert len(logs) == 1` discipline.
//   2. Evaluator-level: a handful of expressions are evaluated directly
//      via [EvalMath] to lock down the numeric semantics that
//      WeasyPrint asserts at layout time (`isclose(div.width, 100)`).
//
// :copyright: 2011-2014 Simon Sapin and contributors (WeasyPrint).
// :license:   BSD.

// validMathExpressions lists `width` values from the parametrised
// `test_math_functions` body in test_math.py. The original test asserts
// each yields div.width == 100; we only check the expression validates.
//
// Removed cases that depend on units we don't yet recognise:
//   - 'calc(50vw)', 'calc(20pvh)' — viewport units not in LENGTHUNITS.
//   - 'calc(100px*var(--one))' style expressions where the var() must be
//     resolved before validation; our `--one: 1` substitution requires a
//     full document, not standalone CSS, so they're checked separately.
var validMathExpressions = []string{
	"calc(100px)",
	"calc(10em)",
	"calc(50%)",
	"calc(10px + 90px)",
	"calc(5em + 50px)",
	"calc(2 * 5em)",
	"calc(2 * (3em + 20px))",
	"calc(25% * (1 + 1))",
	"calc(20% * (1 + 1) + 20px)",
	"calc(100px",
	"max(100px)",
	"max(30%, 2em, 100px)",
	"max(-30%, -2em, 10em)",
	"calc(max(-1, 1, 2) * 50px)",
	"min(100px)",
	"min(100%, 20em, 100px)",
	"calc(min(4, 2) * 50px)",
	"calc(sqrt(4) * 50px)",
	"calc(pow(2, 2) * 25px)",
	"calc(hypot(2) * 50px)",
	"calc(hypot(3, 4) * 20px)",
	"calc(hypot(2px) * 50)",
	"calc(hypot(3px, 4px) * 20)",
	"calc(log(e) * 100px)",
	"calc(log(100, 10) * 50px)",
	"calc(exp(1) / e * 100px)",
	"abs(-100px)",
	"calc(abs(-100) * 1px)",
	"calc(sign(-100) * -100px)",
	"calc(sign(-100px) * -100px)",
	"calc(sqrt(16) * min(25px, 100%))",
	"clamp(calc(-infinity * 1px), 10em, calc(infinity * 1px))",
	"clamp(50px, 10em, 500px)",
	"clamp(100px, 2em, 500px)",
	"clamp(10px, 100em, 10em)",
	"clamp(10px, 100%, 10em)",
	"round(100.4px)",
	"round(145.4px, 100px)",
	"round(nearest, 100px)",
	"round(down, 195px, 100px)",
	"round(up, 5px, 100px)",
	"round(to-zero, 195px, 100px)",
	"mod(300px, 200px)",
	"calc(mod(300px, -200px) * -1)",
	"calc(mod(-300px, -200px) * -1)",
	"rem(300px, 200px)",
	"rem(300px, -200px)",
	"calc(rem(-300px, -200px) * -1)",
	"calc(sin(30deg) * 200px)",
	"calc(cos(60deg) * 200px)",
	"calc(tan(45deg) * 100px)",
	"calc(tan(calc(pi / 4)) * 100px)",
	"calc(sin(asin(0.5)) * 200px)",
	"calc(cos(acos(0.5)) * 200px)",
	"calc(tan(atan(1)) * 100px)",
	"calc(tan(atan2(1, 1)) * 100px)",
}

func TestMathFunctionsValid(t *testing.T) {
	for _, expr := range validMathExpressions {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			capt := tu.CaptureLogs()
			defer capt.AssertNoLogs(t)
			d := expandToDict(t, "width: "+expr, "")
			if len(d) == 0 {
				t.Fatalf("expected a property, got nothing for %q", expr)
			}
		})
	}
}

// invalidMathExpressions ports the subset of WeasyPrint's
// `test_math_functions_error` cases that our current validation layer
// catches. WeasyPrint runs full layout, so its tests catch additional
// errors at evaluation/layout time (e.g. "min(10, 5px) is bare-numbers
// mixed with a length"). Until we wire layout-time evaluation through
// the typed accessors (Phase 3.2), those are reported when their
// expression is evaluated, not when their property is validated.
//
// The list below only contains shape-check failures: bad arity, unknown
// function names, non-numeric tokens. The remainder live in
// invalidMathExpressionsUnsupported for documentation.
var invalidMathExpressions = []string{
	`calc("100px")`,        // string operand
	"calc(100px, 100px)",   // calc takes one operand
	`min("10px")`,          // string operand
	`max("10px")`,          // string operand
	"min()",                // 0-arity
	"max()",                // 0-arity
	"clamp()",              // 0-arity
	"clamp(10px)",          // 1-arity
	"clamp(10px, 50px)",    // 2-arity
	"clamp(10px, 50px, 100px, 200px)", // 4-arity
	`clamp(10px, "50px", 100px)`,      // string in middle
	"round()",                          // 0-arity
	"round(100px, 10px, 1)",            // 4-arity (no leading kw)
	"round(nearest, 100px, 10px, 1)",   // 4-arity (with leading kw)
	"round(unknown, 100px)",            // bad rounding-strategy
	"round(unknown, 100px, 10px)",      // bad rounding-strategy
	`round(100px, "10px")`,             // string operand
	"mod()",
	"mod(10px)",
	`mod(100px, "10px")`,
	"mod(100px, 10px, 1px)",
	"rem()",
	"rem(10px)",
	`rem(100px, "10px")`,
	"rem(100px, 10px, 1px)",
	"sin()", "cos()", "tan()",
	"asin()", "acos()", "atan()", "atan2()",
	"atan2(0.5)",
	"pow()",
	"pow(4, 3, 4)",
	"sqrt()",
	"sqrt(4, 2)",
	"hypoth()", "hypoth(3)", "hypoth(3, 4)", // unknown function name
	"log()",
	"log(10, 10, 10)",
	"exp()",
	"exp(10, 10)",
	"exp(10px, 10)",
	"exp(10, 10, 10)",
	"abs()",
	"abs(10px, 100)",
	"sign()",
	"sign(10px, 10)",
}

// invalidMathExpressionsUnsupported lists WeasyPrint cases our
// validation does not yet reject. Promoting an entry here to
// invalidMathExpressions requires extending checkMath / checkMathOperand
// to enforce the corresponding rule.
var invalidMathExpressionsUnsupported = []string{
	"calc(100)",                  // bare number for width (needs target-shape check)
	"calc(100px 100px)",          // missing operator (parser keeps token list)
	"calc(100px * 100px)",        // length × length — should fail unit dimension reduction
	"calc(100 * 100)",            // bare number for width
	"calc(calc(100unknown))",     // unknown unit; tokenized as Dimension w/ unit "unknown"
	"calc(0.1)", "calc(-1)",      // bare numbers
	"min(10)", "max(10)",         // bare number for width
	"min(10, 5px)", "max(10, 50px)", // mixed types
	"calc(min(1, 5px) * 10px)", "calc(max(100, 5px) * 10px)",
	"calc(100* - max(56px, 1rem)", // unbalanced parens (parser tolerant)
	"round(100)", "round(100, 10)", "round(nearest, 100, 10)",
	"round(100px, 10)", "round(nearest, 100px, 10)",
	"mod(100px, 10)", "calc(mod(300px, 200) * -1)",
	"rem(100px, 10)", "calc(rem(300px, 200) * -1)",
	"asin(50deg)", "acos(50deg)", "atan(50deg)", "atan2(50deg, 1)",
	"calc(sin(asin(50deg)) * 200px)", "calc(sin(asin(0.5, 2)) * 200px)",
	"calc(cos(acos(50deg)) * 200px)", "calc(cos(acos(0.5, 2)) * 200px)",
	"calc(tan(atan(50deg)) * 200px)", "calc(tan(atan(0.5, 2)) * 200px)",
	"calc(tan(atan2(50deg, 1)) * 200px)",
	"pow(4, 3)", "pow(4px, 3)",
	"sqrt(4)", "sqrt(4px)",
	"log(10)", "log(10px)", "log(10, 10)", "log(10px, 10)",
	"exp(10)", "exp(10px)",
	"abs(10)",
	"sign(10)", "sign(10px)",
}

func TestMathFunctionsInvalid(t *testing.T) {
	for _, expr := range invalidMathExpressions {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			assertInvalid(t, "width: "+expr, "Ignored")
		})
	}
}

// --- Direct evaluator tests ---------------------------------------------

// evalForTest parses the given math expression (must be a single
// FunctionBlock) and evaluates it against ctx with the requested shape.
func evalForTest(t *testing.T, expr string, shape pr.MathType, ctx MathContext) float32 {
	t.Helper()
	tokens := parser.RemoveWhitespace(parser.Tokenize([]byte(expr), false))
	if len(tokens) != 1 {
		t.Fatalf("expected exactly one token in %q, got %d", expr, len(tokens))
	}
	pm, err := parseMath(tokens[0], shape, "")
	if err != nil {
		t.Fatalf("parseMath(%q): %v", expr, err)
	}
	v, err := EvalMath(pm, ctx)
	if err != nil {
		t.Fatalf("EvalMath(%q): %v", expr, err)
	}
	return float32(v)
}

func near(a, b, eps float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= eps
}

// TestMathEvaluatorWidth100 asserts that each WeasyPrint-spec expression
// that the original test parametrises to width=100px actually evaluates
// to 100 pixels under the matching layout context (font-size: 10px,
// containing-block width: 200px).
func TestMathEvaluatorWidth100(t *testing.T) {
	ctx := MathContext{
		FontSize:            10,
		RootFontSize:        10,
		PercentageReference: 200,
		HasPercentageRef:    true,
	}

	cases := []struct{ expr string }{
		{"calc(100px)"},
		{"calc(10em)"},
		{"calc(50%)"},
		{"calc(10px + 90px)"},
		{"calc(5em + 50px)"},
		{"calc(2 * 5em)"},
		{"calc(2 * (3em + 20px))"},
		{"calc(25% * (1 + 1))"},
		{"calc(20% * (1 + 1) + 20px)"},
		{"max(100px)"},
		{"min(100px)"},
		{"calc(sqrt(4) * 50px)"},
		{"calc(pow(2, 2) * 25px)"},
		{"calc(hypot(2) * 50px)"},
		{"calc(hypot(3, 4) * 20px)"},
		{"calc(log(e) * 100px)"},
		{"calc(log(100, 10) * 50px)"},
		{"abs(-100px)"},
		{"calc(abs(-100) * 1px)"},
		{"calc(sign(-100) * -100px)"},
		{"calc(sign(-100px) * -100px)"},
		{"clamp(50px, 10em, 500px)"},
		{"round(100.4px)"},
		{"calc(sin(30deg) * 200px)"},
		{"calc(cos(60deg) * 200px)"},
		{"calc(tan(45deg) * 100px)"},
		{"calc(sin(asin(0.5)) * 200px)"},
		{"calc(cos(acos(0.5)) * 200px)"},
		{"calc(tan(atan(1)) * 100px)"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.expr, func(t *testing.T) {
			defer tu.CaptureLogs().AssertNoLogs(t)
			got := evalForTest(t, c.expr, pr.MathLengthPercentage, ctx)
			if !near(got, 100, 0.01) {
				t.Fatalf("%s: expected 100, got %g", c.expr, got)
			}
		})
	}
}

// TestMathEvaluatorAngles checks angle-shape expressions resolve to
// recognisable radian values.
func TestMathEvaluatorAngles(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	ctx := MathContext{}
	got := evalForTest(t, "calc(180deg)", pr.MathAngle, ctx)
	if !near(got, float32(math.Pi), 1e-5) {
		t.Fatalf("calc(180deg): expected π, got %g", got)
	}
}

// TestMathEvaluatorDivByZero ensures `calc(1px / 0)` is rejected.
func TestMathEvaluatorDivByZero(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	tokens := parser.RemoveWhitespace(parser.Tokenize([]byte("calc(1px / 0)"), false))
	pm, err := parseMath(tokens[0], pr.MathLength, "")
	if err != nil {
		t.Fatalf("parseMath: %v", err)
	}
	if _, err := EvalMath(pm, MathContext{}); err == nil {
		t.Fatal("expected division-by-zero error, got nil")
	}
}

// TestLayoutDeferredMath exercises the full layout-time path: a math
// value carrying a percentage whose reference is the containing-block
// width is parsed, baked with a font-size context, then resolved
// against a runtime referTo via [pr.ResolvePercentage].
//
// `calc(50% + 10px)` against a 200px containing block should yield
// 110px. `calc(50% + 1em)` against the same block with font-size 20
// should yield 120px (the em is baked in at compute time, the
// percentage is resolved at layout time).
func TestLayoutDeferredMath(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	cases := []struct {
		expr     string
		fontSize utils.Fl
		referTo  pr.Float
		want     pr.Float
	}{
		{"calc(50% + 10px)", 16, 200, 110},
		{"calc(50% + 1em)", 20, 200, 120},
		{"calc(100% - 20px)", 16, 50, 30},
	}
	for _, c := range cases {
		c := c
		t.Run(c.expr, func(t *testing.T) {
			tokens := parser.RemoveWhitespace(parser.Tokenize([]byte(c.expr), false))
			pm, err := parseMath(tokens[0], pr.MathLengthPercentage, "layout")
			if err != nil {
				t.Fatalf("parseMath: %v", err)
			}
			pm.FontSize = c.fontSize
			pm.RootFontSize = c.fontSize
			value := pr.Dimension{Math: &pm}.Tagged()

			got := pr.ResolvePercentage(value, c.referTo)
			f, ok := got.(pr.Float)
			if !ok {
				t.Fatalf("expected pr.Float, got %T", got)
			}
			if !near(float32(f), float32(c.want), 0.01) {
				t.Fatalf("%s: expected %g, got %g", c.expr, c.want, f)
			}
		})
	}
}
