package properties

import (
	"fmt"
	"math"
)

// During layout, float numbers sometimes need special values like "auto" or nil (None in Python).
// This file define a float64-like type handling these cases.

const (
	// AutoF indicates a value specified as "auto", which will
	// be resolved during layout.
	AutoF special = true
)

type MaybeFloat interface {
	V() Float
}

func (f Float) V() Float { return f }

type special bool

func (f special) V() Float { return 0 }

func (f special) String() string {
	if f {
		return "auto"
	}
	return "-"
}

// Return true except for 0 or nil
func Is(m MaybeFloat) bool {
	if m == nil {
		return false
	}
	if f, ok := m.(Float); ok {
		return f != 0
	}
	return false
}

// MaybeFloatToFloat is the same as MaybeFloat.V(),
// but handles nil values
func MaybeFloatToFloat(mf MaybeFloat) Float {
	if mf == nil {
		return 0
	}
	return mf.V()
}

func MaybeFloatToValue(mf MaybeFloat) TaggedDim {
	if mf == nil {
		return TaggedDim{}
	}
	if mf == AutoF {
		return TagToV(Auto)
	}
	return mf.V().ToValue()
}

func Floor(x Float) Float {
	return Float(math.Floor(float64(x)))
}

func Maxs(values ...Float) Float {
	max := -Inf
	for _, w := range values {
		if w > max {
			max = w
		}
	}
	return max
}

func Mins(values ...Float) Float {
	min := Inf
	for _, w := range values {
		if w < min {
			min = w
		}
	}
	return min
}

func Hypot(a, b Float) Float {
	return Float(math.Hypot(float64(a), float64(b)))
}

func Abs(x Float) Float {
	if x < 0 {
		return -x
	}
	return x
}

// ResolvePercentage returns the percentage of the reference value, or the value unchanged.
// “referTo“ is the length for 100%. If “referTo“ is not a number, it
// just replaces percentages.
func ResolvePercentage(value TaggedDim, referTo Float) MaybeFloat {
	if value.IsNone() {
		return nil
	} else if value.Tag == Auto {
		return AutoF
	} else if value.Dimension.Math != nil {
		v, ok := evalDeferredMath(value.Dimension.Math, referTo)
		if !ok {
			return Float(0)
		}
		return Float(v)
	} else if value.Unit == Px {
		return value.Value
	} else {
		if value.Unit != Perc {
			panic(fmt.Sprintf("expected percentage, got %d", value.Unit))
		}
		return referTo * value.Value / 100.
	}
}

// deferredMathEvaluator is plugged in by the css/validation package via
// SetDeferredMathEvaluator. It avoids an import cycle (validation imports
// properties, not the other way around) while letting layout-time code
// finish a percentage-deferred PendingMath without re-walking the cascade.
type deferredMathEvaluator func(pm *PendingMath, referTo Fl) (Fl, error)

var deferredEval deferredMathEvaluator

// SetDeferredMathEvaluator registers the layout-time math resolver. It is
// called once during package initialisation by css/validation.
func SetDeferredMathEvaluator(f func(pm *PendingMath, referTo Fl) (Fl, error)) {
	deferredEval = f
}

func evalDeferredMath(pm *PendingMath, referTo Float) (Fl, bool) {
	if deferredEval == nil {
		return 0, false
	}
	v, err := deferredEval(pm, Fl(referTo))
	if err != nil {
		return 0, false
	}
	return v, true
}
