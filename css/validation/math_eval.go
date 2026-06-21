package validation

import (
	"errors"
	"fmt"
	"math"
	"strings"

	pa "github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/utils"
)

// CSS math evaluator. Ports WeasyPrint's `_resolve_calc_*` and
// `resolve_math` from weasyprint/css/__init__.py.
//
// Strategy:
//   1. Parse the validated FunctionBlock token list with a recursive-
//      descent parser respecting CSS arithmetic precedence:
//          sum     := product (('+' | '-') product)*
//          product := value   (('*' | '/') value)*
//          value   := <number> | <dimension> | <percentage> | <ident-const>
//                  | '(' sum ')' | <math-func>
//   2. Each sub-expression resolves to a [resolvedValue] carrying a Float
//      and a Unit category. Adding mismatched categories returns an error.
//   3. The dispatcher applies the function semantics (min, max, clamp,
//      round, etc.) on top of resolved sub-expressions.

// MathContext supplies the dynamic information required to resolve a
// PendingMath at compute or layout time.
type MathContext struct {
	// FontSize is the computed `font-size` of the element in pixels.
	FontSize utils.Fl
	// RootFontSize is the document root font-size in pixels.
	RootFontSize utils.Fl
	// ExRatio / ChRatio are the 1ex / font-size and 1ch / font-size
	// ratios for the element's font; 0 falls back to the canonical 0.5/0.5.
	ExRatio utils.Fl
	ChRatio utils.Fl

	// PercentageReference, when non-zero, is the dimension (in pixels)
	// that 100% resolves to. When zero, percentages remain unresolved
	// and any percentage operand causes an error — callers that don't
	// have a reference yet must defer evaluation to layout time.
	PercentageReference utils.Fl
	HasPercentageRef    bool
}

// resolvedValue is the intermediate currency of the evaluator. The Unit
// category is one of: number, length (pixels), percentage, angle (radians),
// resolution (dppx). Lengths are normalised to pixels using the context.
type resolvedValue struct {
	V    utils.Fl
	Unit pr.Unit
}

func (r resolvedValue) isNumber() bool     { return r.Unit == pr.Scalar }
func (r resolvedValue) isLength() bool     { return r.Unit == pr.Px }
func (r resolvedValue) isPercentage() bool { return r.Unit == pr.Perc }
func (r resolvedValue) isAngle() bool      { return r.Unit == pr.Rad }

var (
	errMathTypeMismatch = errors.New("incompatible operand types in math expression")
	errMathDivByZero    = errors.New("division by zero in math expression")
	// ErrMathNeedPercRef is returned by EvalMath when the expression
	// contains a percentage but the context provides no reference value.
	// Callers wishing to defer evaluation to layout time should detect
	// this error specifically (with [errors.Is]) rather than falling back.
	ErrMathNeedPercRef = errors.New("percentage in math expression has no resolution context")
)

// EvalMath evaluates pm using ctx and returns a resolved value matching
// pm.ResolveTo. Length results are in pixels, angle in radians.
func EvalMath(pm pr.PendingMath, ctx MathContext) (utils.Fl, error) {
	rv, err := evalMathFunction(pm.Expr, pm.ResolveTo, ctx)
	if err != nil {
		return 0, err
	}
	return castToShape(rv, pm.ResolveTo, ctx)
}

func init() {
	// Wire layout-time deferred math resolution back into css/properties
	// (which cannot import validation due to the dependency direction).
	pr.SetDeferredMathEvaluator(evalDeferred)
}

// evalDeferred completes a layout-deferred PendingMath. The font-size
// context was baked at compute time; only the percentage reference is
// supplied now. Returns pixels for length-percentage shapes.
func evalDeferred(pm *pr.PendingMath, referTo utils.Fl) (utils.Fl, error) {
	ctx := MathContext{
		FontSize:            pm.FontSize,
		RootFontSize:        pm.RootFontSize,
		PercentageReference: referTo,
		HasPercentageRef:    true,
	}
	return EvalMath(*pm, ctx)
}

// EvalMathDim is like EvalMath but returns a [pr.Dimension] preserving
// percentage units when the context has no percentage reference. This is
// useful for compute-time resolution: percentages may need to remain
// percentages until layout, while lengths and numbers can be reduced now.
func EvalMathDim(pm pr.PendingMath, ctx MathContext) (pr.Dimension, error) {
	rv, err := evalMathFunction(pm.Expr, pm.ResolveTo, ctx)
	if err != nil {
		return pr.Dimension{}, err
	}
	switch rv.Unit {
	case pr.Px:
		return pr.NewDim(pr.Float(rv.V), pr.Px), nil
	case pr.Perc:
		return pr.NewDim(pr.Float(rv.V), pr.Perc), nil
	case pr.Scalar:
		return pr.NewDim(pr.Float(rv.V), pr.Scalar), nil
	case pr.Rad:
		return pr.NewDim(pr.Float(rv.V), pr.Rad), nil
	}
	return pr.Dimension{}, fmt.Errorf("unexpected resolved unit %s", rv.Unit)
}

func castToShape(rv resolvedValue, shape pr.MathType, ctx MathContext) (utils.Fl, error) {
	switch shape {
	case pr.MathNumber, pr.MathInteger:
		if !rv.isNumber() {
			return 0, errMathTypeMismatch
		}
		return rv.V, nil
	case pr.MathLength:
		if !rv.isLength() {
			return 0, errMathTypeMismatch
		}
		return rv.V, nil
	case pr.MathPercentage:
		if rv.isPercentage() {
			if !ctx.HasPercentageRef {
				return 0, ErrMathNeedPercRef
			}
			return rv.V * ctx.PercentageReference / 100, nil
		}
		return 0, errMathTypeMismatch
	case pr.MathLengthPercentage:
		if rv.isLength() {
			return rv.V, nil
		}
		if rv.isPercentage() {
			if !ctx.HasPercentageRef {
				return 0, ErrMathNeedPercRef
			}
			return rv.V * ctx.PercentageReference / 100, nil
		}
		return 0, errMathTypeMismatch
	case pr.MathAngle:
		if !rv.isAngle() {
			return 0, errMathTypeMismatch
		}
		return rv.V, nil
	}
	return 0, fmt.Errorf("unsupported math shape %s", shape)
}

// evalMathFunction dispatches by the name of fb.
func evalMathFunction(fb pa.FunctionBlock, shape pr.MathType, ctx MathContext) (resolvedValue, error) {
	name := strings.ToLower(fb.Name)
	args, err := splitMathArguments(fb.Arguments)
	if err != nil {
		return resolvedValue{}, err
	}

	// round() rounding-strategy prefix.
	strategy := "nearest"
	if name == "round" && len(args) >= 1 {
		if leadKw := singleIdent(args[0]); roundingStrategies[leadKw] {
			strategy = leadKw
			args = args[1:]
		}
	}

	switch name {
	case "calc":
		return evalSum(args[0], shape, ctx)

	case "min":
		out, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		for _, a := range args[1:] {
			r, err := evalSum(a, shape, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
			if r.Unit != out.Unit {
				return resolvedValue{}, errMathTypeMismatch
			}
			if r.V < out.V {
				out = r
			}
		}
		return out, nil

	case "max":
		out, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		for _, a := range args[1:] {
			r, err := evalSum(a, shape, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
			if r.Unit != out.Unit {
				return resolvedValue{}, errMathTypeMismatch
			}
			if r.V > out.V {
				out = r
			}
		}
		return out, nil

	case "clamp":
		minV, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		val, err := evalSum(args[1], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		maxV, err := evalSum(args[2], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if minV.Unit != val.Unit || maxV.Unit != val.Unit {
			return resolvedValue{}, errMathTypeMismatch
		}
		v := val.V
		if v < minV.V {
			v = minV.V
		}
		if v > maxV.V {
			v = maxV.V
		}
		return resolvedValue{V: v, Unit: val.Unit}, nil

	case "round":
		a, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		// 1-arg form: round to nearest integer in A's unit (B defaults
		// to 1 unit). 2-arg form: rounding step is the second argument.
		var b resolvedValue
		if len(args) == 2 {
			b, err = evalSum(args[1], shape, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
		} else {
			b = resolvedValue{V: 1, Unit: a.Unit}
		}
		if a.Unit != b.Unit || b.V == 0 {
			return resolvedValue{}, errMathDivByZero
		}
		q := a.V / b.V
		var r utils.Fl
		switch strategy {
		case "up":
			r = utils.Fl(math.Ceil(float64(q)))
		case "down":
			r = utils.Fl(math.Floor(float64(q)))
		case "to-zero":
			r = utils.Fl(math.Trunc(float64(q)))
		default: // nearest
			r = utils.Fl(math.RoundToEven(float64(q)))
		}
		return resolvedValue{V: r * b.V, Unit: a.Unit}, nil

	case "mod":
		a, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		b, err := evalSum(args[1], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if a.Unit != b.Unit || b.V == 0 {
			return resolvedValue{}, errMathDivByZero
		}
		// Floor-modulo (sign of divisor).
		v := utils.Fl(math.Mod(float64(a.V), float64(b.V)))
		if (v < 0 && b.V > 0) || (v > 0 && b.V < 0) {
			v += b.V
		}
		return resolvedValue{V: v, Unit: a.Unit}, nil

	case "rem":
		a, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		b, err := evalSum(args[1], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if a.Unit != b.Unit || b.V == 0 {
			return resolvedValue{}, errMathDivByZero
		}
		v := utils.Fl(math.Mod(float64(a.V), float64(b.V)))
		return resolvedValue{V: v, Unit: a.Unit}, nil

	case "sin", "cos", "tan":
		a, err := evalSum(args[0], pr.MathAngle, ctx)
		if err != nil {
			// Numbers are also accepted (treated as radians).
			a, err = evalSum(args[0], pr.MathNumber, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
		}
		var v float64
		switch name {
		case "sin":
			v = math.Sin(float64(a.V))
		case "cos":
			v = math.Cos(float64(a.V))
		case "tan":
			v = math.Tan(float64(a.V))
		}
		return resolvedValue{V: utils.Fl(v), Unit: pr.Scalar}, nil

	case "asin", "acos", "atan":
		a, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		var v float64
		switch name {
		case "asin":
			v = math.Asin(float64(a.V))
		case "acos":
			v = math.Acos(float64(a.V))
		case "atan":
			v = math.Atan(float64(a.V))
		}
		return resolvedValue{V: utils.Fl(v), Unit: pr.Rad}, nil

	case "atan2":
		y, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		x, err := evalSum(args[1], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		v := math.Atan2(float64(y.V), float64(x.V))
		return resolvedValue{V: utils.Fl(v), Unit: pr.Rad}, nil

	case "pow":
		a, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		b, err := evalSum(args[1], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		v := math.Pow(float64(a.V), float64(b.V))
		return resolvedValue{V: utils.Fl(v), Unit: pr.Scalar}, nil

	case "sqrt":
		a, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		v := math.Sqrt(float64(a.V))
		return resolvedValue{V: utils.Fl(v), Unit: pr.Scalar}, nil

	case "hypot":
		sum := 0.0
		var unit pr.Unit
		for i, arg := range args {
			r, err := evalSum(arg, shape, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
			if i == 0 {
				unit = r.Unit
			} else if r.Unit != unit {
				return resolvedValue{}, errMathTypeMismatch
			}
			sum += float64(r.V) * float64(r.V)
		}
		return resolvedValue{V: utils.Fl(math.Sqrt(sum)), Unit: unit}, nil

	case "log":
		a, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		base := math.E
		if len(args) == 2 {
			b, err := evalSum(args[1], pr.MathNumber, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
			base = float64(b.V)
		}
		v := math.Log(float64(a.V)) / math.Log(base)
		return resolvedValue{V: utils.Fl(v), Unit: pr.Scalar}, nil

	case "exp":
		a, err := evalSum(args[0], pr.MathNumber, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		v := math.Exp(float64(a.V))
		return resolvedValue{V: utils.Fl(v), Unit: pr.Scalar}, nil

	case "abs":
		a, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if a.V < 0 {
			a.V = -a.V
		}
		return a, nil

	case "sign":
		a, err := evalSum(args[0], shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		switch {
		case a.V > 0:
			return resolvedValue{V: 1, Unit: pr.Scalar}, nil
		case a.V < 0:
			return resolvedValue{V: -1, Unit: pr.Scalar}, nil
		}
		return resolvedValue{V: 0, Unit: pr.Scalar}, nil
	}

	return resolvedValue{}, fmt.Errorf("unsupported math function %s", name)
}

// --- Recursive descent over a single argument's token slice ----------------

type mathParser struct {
	toks []pa.Token
	pos  int
}

func (p *mathParser) skipWS() {
	for p.pos < len(p.toks) {
		if _, ok := p.toks[p.pos].(pa.Whitespace); !ok {
			return
		}
		p.pos++
	}
}

func (p *mathParser) peek() pa.Token {
	p.skipWS()
	if p.pos >= len(p.toks) {
		return nil
	}
	return p.toks[p.pos]
}

func (p *mathParser) next() pa.Token {
	tok := p.peek()
	if tok != nil {
		p.pos++
	}
	return tok
}

func (p *mathParser) consumeLiteral(want string) bool {
	if lit, ok := p.peek().(pa.Literal); ok && lit.Value == want {
		p.pos++
		return true
	}
	return false
}

func evalSum(arg []pa.Token, shape pr.MathType, ctx MathContext) (resolvedValue, error) {
	p := &mathParser{toks: arg}
	rv, err := p.parseSum(shape, ctx)
	if err != nil {
		return resolvedValue{}, err
	}
	p.skipWS()
	if p.pos != len(p.toks) {
		return resolvedValue{}, errMathTypeMismatch
	}
	return rv, nil
}

func (p *mathParser) parseSum(shape pr.MathType, ctx MathContext) (resolvedValue, error) {
	left, err := p.parseProduct(shape, ctx)
	if err != nil {
		return resolvedValue{}, err
	}
	for {
		// In CSS Values 4, '+' and '-' MUST be surrounded by whitespace.
		// Our parser already trimmed/skipped whitespace; parser tokens
		// retain the operator as a Literal regardless. Best-effort.
		op := p.peek()
		lit, ok := op.(pa.Literal)
		if !ok || (lit.Value != "+" && lit.Value != "-") {
			return left, nil
		}
		p.pos++
		right, err := p.parseProduct(shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if left, right, err = unifyLengthPerc(left, right, shape, ctx); err != nil {
			return resolvedValue{}, err
		}
		if lit.Value == "+" {
			left.V += right.V
		} else {
			left.V -= right.V
		}
	}
}

// unifyLengthPerc converts the operands to a common unit when the
// expression context allows. Currently this fuses Px and Perc when shape
// is LengthPercentage and the context provides a percentage reference.
// Without a reference, mixed Px/Perc remains an error to be surfaced to
// callers (which may then defer to layout).
func unifyLengthPerc(a, b resolvedValue, shape pr.MathType, ctx MathContext) (resolvedValue, resolvedValue, error) {
	if a.Unit == b.Unit {
		return a, b, nil
	}
	mixesLengthPerc := (a.Unit == pr.Px && b.Unit == pr.Perc) ||
		(a.Unit == pr.Perc && b.Unit == pr.Px)
	if mixesLengthPerc && shape == pr.MathLengthPercentage && ctx.HasPercentageRef {
		if a.Unit == pr.Perc {
			a = resolvedValue{V: a.V * ctx.PercentageReference / 100, Unit: pr.Px}
		}
		if b.Unit == pr.Perc {
			b = resolvedValue{V: b.V * ctx.PercentageReference / 100, Unit: pr.Px}
		}
		return a, b, nil
	}
	if mixesLengthPerc && shape == pr.MathLengthPercentage && !ctx.HasPercentageRef {
		// Caller may want to retry at layout time; surface the canonical
		// "needs percentage reference" error so it can recognise the
		// reason and act accordingly.
		return a, b, ErrMathNeedPercRef
	}
	return a, b, errMathTypeMismatch
}

func (p *mathParser) parseProduct(shape pr.MathType, ctx MathContext) (resolvedValue, error) {
	left, err := p.parseValue(shape, ctx)
	if err != nil {
		return resolvedValue{}, err
	}
	for {
		op := p.peek()
		lit, ok := op.(pa.Literal)
		if !ok || (lit.Value != "*" && lit.Value != "/") {
			return left, nil
		}
		p.pos++
		right, err := p.parseValue(shape, ctx)
		if err != nil {
			return resolvedValue{}, err
		}
		if lit.Value == "*" {
			// One side must be a scalar.
			switch {
			case right.isNumber():
				left.V *= right.V
			case left.isNumber():
				right.V *= left.V
				left = right
			default:
				return resolvedValue{}, errMathTypeMismatch
			}
		} else {
			// Division: the divisor must be a scalar.
			if !right.isNumber() {
				return resolvedValue{}, errMathTypeMismatch
			}
			if right.V == 0 {
				return resolvedValue{}, errMathDivByZero
			}
			left.V /= right.V
		}
	}
}

func (p *mathParser) parseValue(shape pr.MathType, ctx MathContext) (resolvedValue, error) {
	tok := p.next()
	if tok == nil {
		return resolvedValue{}, errMathTypeMismatch
	}
	switch v := tok.(type) {
	case pa.Number:
		return resolvedValue{V: v.ValueF, Unit: pr.Scalar}, nil
	case pa.Percentage:
		return resolvedValue{V: v.ValueF, Unit: pr.Perc}, nil
	case pa.Dimension:
		return resolveDimension(v, ctx)
	case pa.Ident:
		switch strings.ToLower(v.Value) {
		case "pi":
			return resolvedValue{V: utils.Fl(math.Pi), Unit: pr.Scalar}, nil
		case "e":
			return resolvedValue{V: utils.Fl(math.E), Unit: pr.Scalar}, nil
		case "infinity":
			return resolvedValue{V: utils.Fl(math.Inf(+1)), Unit: pr.Scalar}, nil
		case "-infinity":
			return resolvedValue{V: utils.Fl(math.Inf(-1)), Unit: pr.Scalar}, nil
		case "nan":
			return resolvedValue{V: utils.Fl(math.NaN()), Unit: pr.Scalar}, nil
		}
		return resolvedValue{}, errMathTypeMismatch
	case pa.ParenthesesBlock:
		// A bare parenthesised expression at the value level.
		return evalSum(v.Arguments, shape, ctx)
	case pa.FunctionBlock:
		if !isMathFunction(v) {
			return resolvedValue{}, errMathTypeMismatch
		}
		return evalMathFunction(v, shape, ctx)
	case pa.Literal:
		// Unary +/- in front of a value.
		if v.Value == "+" || v.Value == "-" {
			rv, err := p.parseValue(shape, ctx)
			if err != nil {
				return resolvedValue{}, err
			}
			if v.Value == "-" {
				rv.V = -rv.V
			}
			return rv, nil
		}
	}
	return resolvedValue{}, errMathTypeMismatch
}

// resolveDimension converts a dimension token to pixels (length), radians
// (angle), or dppx (resolution) using ctx.
func resolveDimension(d pa.Dimension, ctx MathContext) (resolvedValue, error) {
	unit := strings.ToLower(string(d.Unit))
	if u, ok := LENGTHUNITS[unit]; ok {
		return resolveLengthDim(d.ValueF, u, ctx)
	}
	if u, ok := AngleUnits[unit]; ok {
		return resolvedValue{V: d.ValueF * ANGLETORADIANS[u], Unit: pr.Rad}, nil
	}
	if factor, ok := RESOLUTIONTODPPX[unit]; ok {
		return resolvedValue{V: d.ValueF * factor, Unit: pr.Px /* dppx, reuse Px slot */}, nil
	}
	return resolvedValue{}, errMathTypeMismatch
}

func resolveLengthDim(v utils.Fl, unit pr.Unit, ctx MathContext) (resolvedValue, error) {
	switch unit {
	case pr.Px:
		return resolvedValue{V: v, Unit: pr.Px}, nil
	case pr.Em:
		return resolvedValue{V: v * ctx.FontSize, Unit: pr.Px}, nil
	case pr.Rem:
		return resolvedValue{V: v * ctx.RootFontSize, Unit: pr.Px}, nil
	case pr.Ex:
		ratio := ctx.ExRatio
		if ratio == 0 {
			ratio = 0.5
		}
		return resolvedValue{V: v * ctx.FontSize * ratio, Unit: pr.Px}, nil
	case pr.Ch:
		ratio := ctx.ChRatio
		if ratio == 0 {
			ratio = 0.5
		}
		return resolvedValue{V: v * ctx.FontSize * ratio, Unit: pr.Px}, nil
	case pr.Pt, pr.Pc, pr.In, pr.Cm, pr.Mm, pr.Q:
		factor := pr.LengthsToPixels[unit]
		return resolvedValue{V: v * utils.Fl(factor), Unit: pr.Px}, nil
	}
	return resolvedValue{}, errMathTypeMismatch
}
