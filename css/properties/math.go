package properties

import (
	"github.com/benoitkugler/webrender/css/parser"
)

// PendingMath represents an unresolved CSS math function — calc(), min(),
// max(), clamp(), round(), mod(), rem(), sin/cos/tan/asin/acos/atan/atan2,
// pow, sqrt, hypot, log, exp, abs, sign — captured at validation time and
// evaluated at compute time once font-size, viewport units and (optionally)
// percentage refer-to context are available.
//
// The token tree is preserved verbatim (already var()-substituted) so that
// the evaluator can recurse without re-parsing. ResolveTo records the unit
// shape the consuming property requires; the evaluator uses it to detect
// invalid combinations (e.g. percentages in a number-only context).
//
// This mirrors WeasyPrint's two-stage architecture: the validator only
// shape-checks (`check_math` in WeasyPrint), the evaluator resolves
// (`resolve_math`).
type PendingMath struct {
	// Expr is the original calc() / min() / max() / etc. function block.
	Expr parser.FunctionBlock

	// ResolveTo describes what unit shape the property requires.
	ResolveTo MathType

	// PercentageReferTo, when non-empty, names the layout dimension to
	// resolve percentages against (e.g. "width", "height", "font-size").
	// Empty means percentages cannot be resolved at compute time and the
	// evaluation is deferred to layout.
	PercentageReferTo string

	// FontSize and RootFontSize are baked at compute time when the
	// percentage reference is a layout dimension (PercentageReferTo ==
	// "layout"). Layout-time evaluation reads them so em/rem/ex/ch units
	// are still computed against the element's own font, not the box
	// being laid out. Both are zero before compute time.
	FontSize, RootFontSize Fl
}

// MathType is the expected resolved unit category for a PendingMath value.
type MathType uint8

const (
	// MathNumber is a unit-less <number> (e.g. opacity, line-height multiplier).
	MathNumber MathType = iota + 1
	// MathInteger is an integer <integer>.
	MathInteger
	// MathLength is a <length> only (no percentages).
	MathLength
	// MathPercentage is a <percentage> only.
	MathPercentage
	// MathLengthPercentage is <length-percentage>.
	MathLengthPercentage
	// MathAngle is an <angle>.
	MathAngle
	// MathTime is a <time>.
	MathTime
	// MathResolution is a <resolution>.
	MathResolution
)

func (m MathType) String() string {
	switch m {
	case MathNumber:
		return "number"
	case MathInteger:
		return "integer"
	case MathLength:
		return "length"
	case MathPercentage:
		return "percentage"
	case MathLengthPercentage:
		return "length-percentage"
	case MathAngle:
		return "angle"
	case MathTime:
		return "time"
	case MathResolution:
		return "resolution"
	default:
		return "<invalid math type>"
	}
}

// AcceptsPercentage reports whether ResolveTo allows percentage operands
// somewhere in the expression.
func (m MathType) AcceptsPercentage() bool {
	return m == MathPercentage || m == MathLengthPercentage
}

// AcceptsLength reports whether ResolveTo allows length operands.
func (m MathType) AcceptsLength() bool {
	return m == MathLength || m == MathLengthPercentage
}

func (PendingMath) isDeclaredValue() {}
func (PendingMath) isCssProperty()   {}

// String renders the wrapped function block for debug/log output.
func (pm PendingMath) String() string {
	return parser.Serialize([]parser.Token{pm.Expr})
}
