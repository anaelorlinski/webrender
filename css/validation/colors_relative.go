package validation

import (
	"math"

	scolor "github.com/SCKelemen/color"
	pa "github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/utils"
)

// CSS Color Level 4 relative color syntax.
//
//	oklch(from <color> <L> <C> <H> [/ <alpha>])
//	oklab(from <color> <L> <A> <B> [/ <alpha>])
//	lab(  from <color> <L> <A> <B> [/ <alpha>])
//	lch(  from <color> <L> <C> <H> [/ <alpha>])
//	hwb(  from <color> <H> <W> <B> [/ <alpha>])
//
// Channel values may reference the source color's channels by keyword
// (in the output color space) and may use math functions:
//
//	oklch(from var(--c) 10% calc(0.05 + sin(1.0 * pi) * c) h)
//
// Resolution happens here because the validation package already imports
// the parser (for ParseColor) and hosts EvalMath. The result is a fully
// resolved sRGB color identical to what a plain oklch()/… would produce.
//
// rgb(from …) and hsl(from …) are not yet supported — they don't route
// through parseColorL4 and have awkward 0–255 number scaling.

// relSpace describes a color space for relative syntax.
//
// extract pulls the source color's channels in the target space; it operates
// on an in-gamut color, so scolor's conversions are safe here. build turns the
// resolved channels into a gamut-mapped sRGB color: for the L4 spaces it routes
// through the parser's CSS Color 4 gamut mapping (pa.GamutMap*) so that a color
// written with relative syntax resolves identically to the same color written
// directly. scolor's own MapToGamut is not used — its InGamut is unreliable and
// it effectively clips out-of-gamut colors.
type relSpace struct {
	keywords [3]string  // channel keyword names
	scales   [3]float64 // 100% -> scales[i]; 0 for hue channels
	hueIndex int        // index of the hue channel, or -1
	extract  func(scolor.Color) (c0, c1, c2, alpha float64)
	build    func(c0, c1, c2, alpha float64) pa.RGBA
}

var relSpaces = map[string]relSpace{
	"oklch": {
		keywords: [3]string{"l", "c", "h"},
		scales:   [3]float64{1.0, 0.4, 0},
		hueIndex: 2,
		extract: func(c scolor.Color) (float64, float64, float64, float64) {
			o := scolor.ToOKLCH(c)
			return o.L, o.C, o.H, o.A_
		},
		build: func(l, c, h, a float64) pa.RGBA {
			return pa.GamutMapOKLCH(l, c, h, a)
		},
	},
	"oklab": {
		keywords: [3]string{"l", "a", "b"},
		scales:   [3]float64{1.0, 0.4, 0.4},
		hueIndex: -1,
		extract: func(c scolor.Color) (float64, float64, float64, float64) {
			o := scolor.ToOKLAB(c)
			return o.L, o.A, o.B, o.A_
		},
		build: func(l, a, b, alpha float64) pa.RGBA {
			return pa.GamutMapOKLAB(l, a, b, alpha)
		},
	},
	"lab": {
		keywords: [3]string{"l", "a", "b"},
		scales:   [3]float64{100.0, 125.0, 125.0},
		hueIndex: -1,
		// Extract with the same D50 chain used to rebuild (pa.SRGBToLAB), not
		// scolor's D65 ToLAB, so lab(from c l a b) round-trips exactly.
		extract: func(c scolor.Color) (float64, float64, float64, float64) {
			r, g, b, a := c.RGBA()
			l, av, bv := pa.SRGBToLAB(r, g, b)
			return l, av, bv, a
		},
		build: func(l, a, b, alpha float64) pa.RGBA {
			return pa.GamutMapLAB(l, a, b, alpha)
		},
	},
	"lch": {
		keywords: [3]string{"l", "c", "h"},
		scales:   [3]float64{100.0, 150.0, 0},
		hueIndex: 2,
		extract: func(c scolor.Color) (float64, float64, float64, float64) {
			r, g, b, a := c.RGBA()
			l, cv, h := pa.SRGBToLCH(r, g, b)
			return l, cv, h, a
		},
		build: func(l, c, h, alpha float64) pa.RGBA {
			return pa.GamutMapLCH(l, c, h, alpha)
		},
	},
	"hwb": {
		keywords: [3]string{"h", "w", "b"},
		scales:   [3]float64{0, 1.0, 1.0},
		hueIndex: 0,
		extract: func(c scolor.Color) (float64, float64, float64, float64) {
			o := scolor.ToHWB(c)
			return o.H, o.W, o.B, o.A
		},
		build: func(h, w, b, alpha float64) pa.RGBA {
			// hwb() is always within the sRGB gamut, so no mapping is
			// needed — convert directly and clamp.
			r, g, bl, _ := scolor.NewHWB(h, w, b, alpha).RGBA()
			return pa.RGBA{R: clampFl(r), G: clampFl(g), B: clampFl(bl), A: utils.Fl(alpha)}
		},
	},
}

// isFromKeyword reports whether tok is the Ident "from".
func isFromKeyword(tok pa.Token) bool {
	ident, ok := tok.(pa.Ident)
	return ok && utils.AsciiLower(ident.Value) == "from"
}

// parseColorResolved is a drop-in replacement for pa.ParseColor that also
// understands the relative color syntax (oklch(from …), …) and resolves
// calc()/min()/max() expressions inside any color function.
//
// The resolution is generic: resolveCalcToken walks the entire token tree
// and replaces math functions with their evaluated Number/Percentage/
// Dimension equivalents before pa.ParseColor sees them. This works for
// any color function (oklch, oklab, lab, lch, hwb, rgb, hsl, …) without
// per-function special cases.
//
// contrast-color() is handled here because its <color> argument may use
// relative syntax or contain calc(), requiring recursive resolution.
func parseColorResolved(token pa.Token) pa.Color {
	if fb, ok := token.(pa.FunctionBlock); ok {
		name := utils.AsciiLower(fb.Name)

		// Relative color syntax: oklch(from <color> …), etc.
		if _, isRel := relSpaces[name]; isRel {
			args := pa.RemoveWhitespace(fb.Arguments)
			if len(args) > 0 && isFromKeyword(args[0]) {
				return resolveRelativeColor(name, args)
			}
		}

		// contrast-color() takes a single <color> argument that may
		// itself be an L4 function with calc() or relative syntax.
		// Resolve it recursively, then compute contrast-color directly.
		if name == "contrast-color" {
			args := pa.RemoveWhitespace(fb.Arguments)
			if len(args) == 1 {
				bg := parseColorResolved(args[0])
				if !bg.IsNone() && bg.Type != pa.ColorCurrentColor {
					return pa.Color{Type: pa.ColorRGBA, RGBA: pa.ContrastColor(bg.RGBA)}
				}
			}
			return pa.Color{}
		}

		// Generic calc resolution: walk the entire token tree and
		// resolve all math functions to primitive tokens before
		// delegating to the base parser.
		resolved, changed := resolveCalcTokenRec(token)
		if changed {
			return pa.ParseColor(resolved)
		}
	}
	return pa.ParseColor(token)
}

// resolveCalcTokenRec recursively walks a token tree and evaluates any
// calc()/min()/max()/clamp()/round()/mod()/rem()/sin()/cos()/etc. math
// functions to their primitive equivalents (Number, Percentage, or
// Dimension). It recurses through every compound token type (via
// pa.TransformTokens) so that nested math inside any color function —
// including contrast-color(oklch(calc(…))) — is resolved in a single pass.
//
// Returns the resolved token and a bool indicating whether any change
// was made. If no math functions are found, the original token is
// returned unchanged (allowing the caller to skip re-parsing).
func resolveCalcTokenRec(token pa.Token) (pa.Token, bool) {
	return pa.TransformTokens(token, func(t pa.Token) (pa.Token, bool, bool) {
		if !isMathFunction(t) {
			return t, false, false // descend into children
		}
		// This token is itself a math function — evaluate it. Its own
		// operands are handled by the evaluator, so stop descending.
		// isMathFunction guarantees t is a FunctionBlock.
		pm := pr.PendingMath{
			Expr:      t.(pa.FunctionBlock),
			ResolveTo: pr.MathLengthPercentage,
		}
		dim, err := EvalMathDim(pm, MathContext{})
		if err != nil {
			return t, false, true
		}
		switch dim.Unit {
		case pr.Scalar:
			return pa.NewNumber(utils.Fl(dim.Value), t.Pos()), true, true
		case pr.Perc:
			return pa.NewPercentage(utils.Fl(dim.Value), t.Pos()), true, true
		default:
			return t, false, true
		}
	})
}

// resolveRelativeColor resolves a relative color function whose args begin
// with the "from" keyword. Returns the zero Color on any error.
func resolveRelativeColor(name string, args []pa.Token) pa.Color {
	sp := relSpaces[name]

	// args[0] is "from"; args[1] is the source color; args[2:] are channels.
	if len(args) < 2 {
		return pa.Color{}
	}

	// Parse the source color (recursively — may itself be relative).
	src := parseColorResolved(args[1])
	if src.IsNone() {
		return pa.Color{}
	}

	// Convert source to scolor.Color for channel extraction.
	var srcScolor scolor.Color
	if src.Type == pa.ColorCurrentColor {
		// currentColor needs element context; fall back to opaque black.
		srcScolor = scolor.NewRGBA(0, 0, 0, 1)
	} else {
		srcScolor = scolor.NewRGBA(
			float64(src.RGBA.R),
			float64(src.RGBA.G),
			float64(src.RGBA.B),
			float64(src.RGBA.A),
		)
	}

	// Extract source channel values in the output color space.
	v0, v1, v2, srcAlpha := sp.extract(srcScolor)

	// Build keyword→value map.
	kw := map[string]float64{
		sp.keywords[0]: v0,
		sp.keywords[1]: v1,
		sp.keywords[2]: v2,
		"alpha":        srcAlpha,
	}

	// Split channels from alpha on "/".
	channelArgs := args[2:]
	var alphaArgs []pa.Token
	for i, tok := range channelArgs {
		if pa.IsLiteral(tok, "/") {
			channelArgs = channelArgs[:i]
			if i+1 < len(args[2:]) {
				alphaArgs = args[2:][i+1:]
			}
			break
		}
	}

	// Must have exactly 3 channel tokens.
	if len(channelArgs) != 3 {
		return pa.Color{}
	}

	// Evaluate the three channels.
	vals := make([]float64, 3)
	for i := 0; i < 3; i++ {
		isHue := sp.hueIndex == i
		v, ok := evalChannel(channelArgs[i], kw, sp.scales[i], isHue)
		if !ok {
			return pa.Color{}
		}
		vals[i] = v
	}

	// Evaluate alpha: defaults to source alpha if omitted.
	alpha := srcAlpha
	if len(alphaArgs) > 0 {
		a, ok := evalChannel(alphaArgs[0], kw, 1.0, false)
		if !ok {
			return pa.Color{}
		}
		alpha = a
	}
	// Clamp alpha to [0, 1].
	alpha = math.Max(0, math.Min(1, alpha))

	// Build the final color in the target space, gamut-mapping to sRGB.
	rgba := sp.build(vals[0], vals[1], vals[2], alpha)
	// The mappers preserve the resolved alpha, but re-assert it here since
	// alpha was clamped above independently of the color channels.
	rgba.A = utils.Fl(alpha)
	return pa.Color{Type: pa.ColorRGBA, RGBA: rgba}
}

// evalChannel evaluates a single channel token to a float64.
// For hue channels (isHue=true), Dimension tokens with angle units are
// accepted and converted to degrees. For non-hue channels, FunctionBlock
// tokens are evaluated via the existing EvalMath with MathNumber shape.
// For hue channels, FunctionBlock tokens are evaluated with MathAngle
// shape (returns radians) and then converted to degrees.
func evalChannel(tok pa.Token, kw map[string]float64, scale float64, isHue bool) (float64, bool) {
	switch v := tok.(type) {
	case pa.Number:
		return float64(v.ValueF), true

	case pa.Percentage:
		return float64(v.ValueF) / 100 * scale, true

	case pa.Dimension:
		if !isHue {
			return 0, false
		}
		val := float64(v.ValueF)
		switch utils.AsciiLower(v.Unit) {
		case "deg":
			return val, true
		case "grad":
			return val * 360 / 400, true
		case "rad":
			return val * 180 / math.Pi, true
		case "turn":
			return val * 360, true
		}
		return 0, false

	case pa.Ident:
		lower := utils.AsciiLower(v.Value)
		if lower == "none" {
			return 0, true
		}
		if val, ok := kw[lower]; ok {
			return val, true
		}
		return 0, false

	case pa.FunctionBlock:
		// Substitute channel keywords in the expression, then evaluate.
		substituted := substKeywords(v, kw).(pa.FunctionBlock)
		shape := pr.MathNumber
		if isHue {
			shape = pr.MathAngle
		}
		pm := pr.PendingMath{
			Expr:      substituted,
			ResolveTo: shape,
		}
		result, err := EvalMath(pm, MathContext{})
		if err != nil {
			return 0, false
		}
		if isHue {
			// EvalMath with MathAngle returns radians; convert to degrees.
			return float64(result) * 180 / math.Pi, true
		}
		return float64(result), true
	}
	return 0, false
}

// substKeywords recursively replaces channel-keyword Ident tokens with
// Number tokens carrying the source color's channel values. It recurses
// through every compound token type (via pa.TransformTokens) so that
// keywords inside calc()/min()/etc. — at any nesting depth — are
// substituted before evaluation.
func substKeywords(tok pa.Token, kw map[string]float64) pa.Token {
	out, _ := pa.TransformTokens(tok, func(t pa.Token) (pa.Token, bool, bool) {
		if ident, ok := t.(pa.Ident); ok {
			if val, ok := kw[utils.AsciiLower(ident.Value)]; ok {
				return pa.NewNumber(utils.Fl(val), ident.Pos()), true, true
			}
		}
		return t, false, false // leaf keeps as-is; compound descends
	})
	return out
}

// clampFl clamps a float64 to [0, 1] and returns it as utils.Fl.
func clampFl(v float64) utils.Fl {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return utils.Fl(v)
}
