package images

import (
	"fmt"
	"math"
	"strings"

	"github.com/benoitkugler/webrender/backend"
	"github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/svg"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/utils"
)

// Gradient line size: distance between the starting point and ending point.
// Positions: list of Dimension in px or % (possibliy zero)
// 0 is the starting point, 1 the ending point.
// http://drafts.csswg.org/csswg/css-images-3/#color-stop-syntax
// Return processed color stops, as a list of floats in px.
func processColorStops(gradientLineSize pr.Float, positions_ []pr.Dimension) []pr.Fl {
	L := len(positions_)
	positions := make([]pr.MaybeFloat, L)
	for i, position := range positions_ {
		positions[i] = pr.ResolvePercentage(position.Tagged(), gradientLineSize)
	}
	// First and last default to 100%
	if positions[0] == nil {
		positions[0] = pr.Float(0)
	}
	if positions[L-1] == nil {
		positions[L-1] = gradientLineSize
	}

	// Make sure positions are increasing.
	previousPos := positions[0].V()
	for i, position := range positions {
		if position != nil {
			if position.V() < previousPos {
				positions[i] = previousPos
			} else {
				previousPos = position.V()
			}
		}
	}

	// Assign missing values
	previousI := L - 1
	for i, position := range positions {
		if position != nil {
			base := positions[previousI]
			increment := (position.V() - base.V()) / pr.Float(i-previousI)
			for j := previousI + 1; j < i; j += 1 {
				positions[j] = base.V() + pr.Float(j)*increment
			}
			previousI = i
		}
	}
	out := make([]pr.Fl, L)
	for i, v := range positions {
		out[i] = pr.Fl(v.V())
	}
	return out
}

// hintSamples is the number of intermediate color stops generated when a color
// transition hint must be *sampled* into ordinary stops (repeating gradients,
// where per-gap exponents can't survive the spread/reflect array surgery). 16
// is enough for a smooth result at typical sizes while keeping the list small.
//
// For the common non-repeating case, hints are NOT sampled: resolveColorHints
// collapses each hint into a single per-gap interpolation exponent carried
// through GradientLayout.Exponents and interpolated natively by the backend
// (PDF Type 2 function / raster pow). See resolveColorHints.
const hintSamples = 16

// interpolatePremul returns the color at fraction t in [0,1] between c0 and c1,
// interpolating in premultiplied-alpha space as required by CSS Images 3.
func interpolatePremul(c0, c1 Color, t utils.Fl) Color {
	r0, g0, b0 := c0.R*c0.A, c0.G*c0.A, c0.B*c0.A
	r1, g1, b1 := c1.R*c1.A, c1.G*c1.A, c1.B*c1.A
	a := c0.A + (c1.A-c0.A)*t
	r := r0 + (r1-r0)*t
	g := g0 + (g1-g0)*t
	b := b0 + (b1-b0)*t
	if a != 0 {
		return Color{R: r / a, G: g / a, B: b / a, A: a}
	}
	return Color{}
}

// hintExponent maps a hint at normalized position h ∈ (0,1) within a gap to the
// CSS Images 3 §3.4.2 interpolation exponent N such that color(t) =
// lerp(a, b, t^N). h = 0.5 → N = 1 (linear). Clamped like WeasyPrint to avoid
// Inf/0 blowups at the extremes.
func hintExponent(h float64) pr.Fl {
	if h <= 0 {
		return 1 << 20 // ~ +inf: transition jumps to the far color immediately
	}
	if h >= 1 {
		return 1.0 / (1 << 20) // ~ 0: stays at the near color until the end
	}
	return pr.Fl(math.Log(0.5) / math.Log(h))
}

// resolveColorHints removes color transition hints from a gradient without
// sampling: each hint entry is dropped and replaced by an interpolation
// exponent on the preceding real stop's gap. Returns the hint-free colors and
// positions plus a parallel exponents slice (0 = linear) suitable for
// GradientLayout.Exponents. isHint[i] marks entry i as a hint (only
// positions[i] is meaningful).
//
// This is the exact, resolution-independent path (backend interpolates t^N).
// See http://drafts.csswg.org/csswg/css-images-3/#color-transition-hint
func resolveColorHints(colors []Color, positions []pr.Fl, isHint []bool) ([]Color, []pr.Fl, []pr.Fl) {
	outColors := make([]Color, 0, len(colors))
	outPos := make([]pr.Fl, 0, len(positions))
	var exps []pr.Fl // lazily allocated; stays nil when there are no hints

	for i := range colors {
		if isHint[i] {
			// A hint sits between two real stops; parsing guarantees this.
			// Attach its exponent to the gap starting at the previous real
			// stop (the last one appended to outColors).
			if i == 0 || i == len(colors)-1 || len(outColors) == 0 {
				continue
			}
			pA, pB, pH := positions[i-1], positions[i+1], positions[i]
			span := pB - pA
			if span <= 0 {
				continue
			}
			if exps == nil {
				exps = make([]pr.Fl, len(outColors)) // fill gaps so far as linear
			}
			// Grow to match outColors, then set the exponent on the last gap.
			for len(exps) < len(outColors) {
				exps = append(exps, 0)
			}
			exps[len(outColors)-1] = hintExponent(float64((pH - pA) / span))
			continue
		}
		outColors = append(outColors, colors[i])
		outPos = append(outPos, positions[i])
		if exps != nil {
			for len(exps) < len(outColors) {
				exps = append(exps, 0)
			}
		}
	}
	return outColors, outPos, exps
}

// prependExponent keeps the exponents slice aligned when a duplicate boundary
// stop is prepended to colors (the new leading gap is linear → exponent 0).
// nil in stays nil out (all-linear gradient, no per-gap exponents needed).
func prependExponent(exps []pr.Fl, colors []Color) []pr.Fl {
	if exps == nil {
		return nil
	}
	return append([]pr.Fl{0}, exps...)
}

// alignExponents returns exps padded/truncated to len(colors) (one exponent
// per stop, last unused), or nil when there are no non-linear gaps. Guards
// against the exponents slice drifting out of sync with the final stop list
// after boundary fixups; a length mismatch would silently mis-map hints.
func alignExponents(exps []pr.Fl, colors []parser.RGBA) []pr.Fl {
	if exps == nil {
		return nil
	}
	// Any non-zero exponent worth keeping?
	any := false
	for _, e := range exps {
		if e != 0 {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	out := make([]pr.Fl, len(colors))
	copy(out, exps)
	return out
}

// expandColorHints removes color transition hints by SAMPLING the transition
// curve into ordinary stops. Used only for repeating gradients, where the
// spread/reflect array surgery can't carry per-gap exponents. The common
// non-repeating path uses resolveColorHints (exact) instead.
func expandColorHints(colors []Color, positions []pr.Fl, isHint []bool) ([]Color, []pr.Fl) {
	// Fast path: no hints.
	hasHint := false
	for _, h := range isHint {
		if h {
			hasHint = true
			break
		}
	}
	if !hasHint {
		return colors, positions
	}

	outColors := make([]Color, 0, len(colors))
	outPos := make([]pr.Fl, 0, len(positions))
	for i := range colors {
		if !isHint[i] {
			outColors = append(outColors, colors[i])
			outPos = append(outPos, positions[i])
			continue
		}
		// A hint must sit between two real stops; parsing guarantees this,
		// but guard defensively.
		if i == 0 || i == len(colors)-1 {
			continue
		}
		pA, pB, pH := positions[i-1], positions[i+1], positions[i]
		cA, cB := colors[i-1], colors[i+1]
		span := pB - pA
		// Degenerate span or hint at an endpoint: nothing to interpolate,
		// the adjacent stops already produce a hard transition.
		if span <= 0 || pH <= pA || pH >= pB {
			continue
		}
		h := float64((pH - pA) / span)
		exponent := math.Log(0.5) / math.Log(h)
		// Sample strictly between the surrounding stops.
		for s := 1; s < hintSamples; s++ {
			p := float64(s) / float64(hintSamples)
			c := utils.Fl(math.Pow(p, exponent))
			outColors = append(outColors, interpolatePremul(cA, cB, c))
			outPos = append(outPos, pA+pr.Fl(p)*span)
		}
	}
	return outColors, outPos
}

// http://drafts.csswg.org/csswg/css-images-3/#find-the-average-color-of-a-gradient
func gradientAverageColor(colors []Color, positions []pr.Fl) Color {
	nbStops := len(positions)
	if nbStops <= 1 || nbStops != len(colors) {
		panic(fmt.Sprintf("expected same length, at least 2, got %d, %d", nbStops, len(colors)))
	}
	totalLength := positions[nbStops-1] - positions[0]
	if totalLength == 0 {
		for i := range positions {
			positions[i] = pr.Fl(i)
		}
		totalLength = pr.Fl(nbStops - 1)
	}
	premulR := make([]utils.Fl, nbStops)
	premulG := make([]utils.Fl, nbStops)
	premulB := make([]utils.Fl, nbStops)
	alpha := make([]utils.Fl, nbStops)
	for i, col := range colors {
		premulR[i] = col.R * col.A
		premulG[i] = col.G * col.A
		premulB[i] = col.B * col.A
		alpha[i] = col.A
	}
	var resultR, resultG, resultB, resultA utils.Fl
	totalWeight := 2 * totalLength
	for i_, position := range positions[1:] {
		i := i_ + 1
		weight := utils.Fl((position - positions[i-1]) / totalWeight)
		j := i - 1
		resultR += premulR[j] * weight
		resultG += premulG[j] * weight
		resultB += premulB[j] * weight
		resultA += alpha[j] * weight
		j = i
		resultR += premulR[j] * weight
		resultG += premulG[j] * weight
		resultB += premulB[j] * weight
		resultA += alpha[j] * weight
	}
	// Un-premultiply:
	if resultA != 0 {
		return Color{
			R: resultR / resultA,
			G: resultG / resultA,
			B: resultB / resultA,
			A: resultA,
		}
	}
	return Color{}
}

type layouter interface {
	// width, height: Gradient box. Top-left is at coordinates (0, 0).
	Layout(width, height pr.Float) backend.GradientLayout
}

// LayoutableGradient is implemented by gradient images that can compute
// their GradientLayout for direct rendering via Canvas.DrawGradient,
// bypassing the group/pattern pipeline.
type LayoutableGradient interface {
	Image
	ComputeLayout(width, height pr.Fl) backend.GradientLayout
}

type gradient struct {
	layouter

	colors        []Color
	stopPositions []pr.Dimension
	// isHint[i] reports whether entry i is a color transition hint rather
	// than a real color stop. Hints carry only a position; their colors
	// slot is unused. Interleaved with the color stops, same length.
	isHint    []bool
	repeating bool
}

// ComputeLayout returns the GradientLayout for the given concrete dimensions.
// This satisfies the LayoutableGradient interface.
func (g gradient) ComputeLayout(concreteWidth, concreteHeight pr.Fl) backend.GradientLayout {
	layout := g.layouter.Layout(pr.Float(concreteWidth), pr.Float(concreteHeight))
	layout.Reapeating = g.repeating
	return layout
}

func newGradient(colorStops []pr.ColorStop, repeating bool) gradient {
	self := gradient{}
	self.colors = make([]Color, len(colorStops))
	self.stopPositions = make([]pr.Dimension, len(colorStops))
	self.isHint = make([]bool, len(colorStops))
	for i, v := range colorStops {
		self.colors[i] = v.Color.RGBA
		self.stopPositions[i] = v.Position
		self.isHint[i] = v.IsHint
	}
	self.repeating = repeating
	return self
}

func (g gradient) GetIntrinsicSize(_, _ pr.Float) (pr.MaybeFloat, pr.MaybeFloat, pr.MaybeFloat) {
	// Gradients are not affected by image resolution, parent or font size.
	return nil, nil, nil
}

func (g gradient) Draw(dst backend.Canvas, _ text.TextLayoutContext, concreteWidth, concreteHeight pr.Fl, _ string) {
	layout := g.layouter.Layout(pr.Float(concreteWidth), pr.Float(concreteHeight))
	layout.Reapeating = g.repeating

	if layout.Kind == "solid" {
		dst.Rectangle(0, 0, concreteWidth, concreteHeight)
		dst.State().SetColorRgba(layout.Colors[0], false)
		dst.Paint(backend.FillNonZero)
		return
	}

	dst.DrawGradient(layout, concreteWidth, concreteHeight)
}

type LinearGradient struct {
	direction pr.DirectionType
	gradient
}

func (LinearGradient) isImage() {}

func NewLinearGradient(from pr.LinearGradient) LinearGradient {
	self := LinearGradient{gradient: newGradient(from.ColorStops, from.Repeating)}
	self.layouter = &self
	// ("corner", keyword) or ("angle", radians)
	self.direction = from.Direction
	return self
}

func (lg LinearGradient) Layout(width, height pr.Float) backend.GradientLayout {
	// Only one color, render the gradient as a solid color
	if len(lg.colors) == 1 {
		return backend.GradientLayout{ScaleY: 1, GradientKind: backend.GradientKind{Kind: "solid"}, Colors: []parser.RGBA{lg.colors[0]}}
	}
	// (dx, dy) is the unit vector giving the direction of the gradient.
	// Positive dx: right, positive dy: down.
	var dx, dy pr.Fl
	if lg.direction.Corner != "" {
		var factorX, factorY pr.Float
		switch lg.direction.Corner {
		case "top_left":
			factorX, factorY = -1, -1
		case "top_right":
			factorX, factorY = 1, -1
		case "bottom_left":
			factorX, factorY = -1, 1
		case "bottom_right":
			factorX, factorY = 1, 1
		}
		diagonal := pr.Hypot(width, height)
		// Note the direction swap: dx based on height, dy based on width
		// The gradient line is perpendicular to a diagonal.
		dx = pr.Fl(factorX * height / diagonal)
		dy = pr.Fl(factorY * width / diagonal)
	} else {
		angle := float64(lg.direction.Angle) // 0 upwards, then clockwise
		dx = pr.Fl(math.Sin(angle))
		dy = pr.Fl(-math.Cos(angle))
	}

	// Round dx and dy to avoid floating points errors caused by
	// trigonometry and angle units conversions
	dx, dy = utils.Round6(dx), utils.Round6(dy)

	// Distance between center && ending point,
	// ie. half of between the starting point && ending point :
	colors := lg.colors
	vectorLength := pr.Fl(pr.Abs(width*pr.Float(dx)) + pr.Abs(height*pr.Float(dy)))
	positions := processColorStops(pr.Float(vectorLength), lg.stopPositions)
	// Color transition hints: for non-repeating gradients keep them exact by
	// collapsing each into a per-gap interpolation exponent (backend does the
	// t^N curve). Repeating gradients can't carry exponents through the
	// spread surgery, so sample the curve into stops there.
	var exponents []pr.Fl
	if lg.repeating {
		colors, positions = expandColorHints(colors, positions, lg.isHint)
	} else {
		colors, positions, exponents = resolveColorHints(colors, positions, lg.isHint)
	}

	if !lg.repeating {
		// Add explicit colors at boundaries if needed, because PDF doesn’t
		// extend color stops that are not displayed
		if positions[0] == positions[1] {
			positions = append([]pr.Fl{positions[0] - 1}, positions...)
			colors = append([]parser.RGBA{colors[0]}, colors...)
			exponents = prependExponent(exponents, colors)
		}
		if positions[len(positions)-2] == positions[len(positions)-1] {
			positions = append(positions, positions[len(positions)-1]+1)
			colors = append(colors, colors[len(colors)-1])
		}
	}

	spread := svg.NoRepeat
	if lg.repeating {
		spread = svg.Repeat
	}
	startX := (pr.Fl(width) - dx*vectorLength) / 2
	startY := (pr.Fl(height) - dy*vectorLength) / 2

	out := spread.LinearGradient(positions, colors, startX, startY, dx, dy, vectorLength)
	out.Exponents = alignExponents(exponents, out.Colors)
	return out
}

type RadialGradient struct {
	gradient
	shape  string
	size   pr.GradientSize
	center pr.CenterPos
}

func (RadialGradient) isImage() {}

func NewRadialGradient(from pr.RadialGradient) RadialGradient {
	self := RadialGradient{gradient: newGradient(from.ColorStops, from.Repeating)}
	self.layouter = &self
	//  Type of ending shape: "circle" || "ellipse"
	self.shape = from.Shape
	// sizeType: "keyword"
	//   size: "closest-corner", "farthest-corner",
	//         "closest-side", || "farthest-side"
	// sizeType: "explicit"
	//   size: (radiusX, radiusY)
	self.size = from.Size
	// Center of the ending shape. (originX, posX, originY, posY)
	self.center = from.Center
	return self
}

func handleDegenerateRadial(sizeX, sizeY pr.Float) (pr.Float, pr.Float) {
	// http://drafts.csswg.org/csswg/css-images-3/#degenerate-radials
	if sizeX == 0 && sizeY == 0 {
		sizeX = 1e-7
		sizeY = 1e-7
	} else if sizeX == 0 {
		sizeX = 1e-7
		sizeY = 1e7
	} else if sizeY == 0 {
		sizeX = 1e7
		sizeY = 1e-7
	}
	return sizeX, sizeY
}

func (rg RadialGradient) Layout(width, height pr.Float) backend.GradientLayout {
	if len(rg.colors) == 1 {
		return backend.GradientLayout{ScaleY: 1, GradientKind: backend.GradientKind{Kind: "solid"}, Colors: []parser.RGBA{rg.colors[0]}}
	}
	originX, centerX_, originY, centerY_ := rg.center.OriginX, rg.center.Pos[0], rg.center.OriginY, rg.center.Pos[1]
	centerX := pr.ResolvePercentage(centerX_.Tagged(), width).V()
	centerY := pr.ResolvePercentage(centerY_.Tagged(), height).V()
	if originX == pr.Right {
		centerX = width - centerX
	}
	if originY == pr.Bottom {
		centerY = height - centerY
	}

	sizeX, sizeY := handleDegenerateRadial(rg.resolveSize(width, height, centerX, centerY))
	scaleY := pr.Fl(sizeY / sizeX)

	colors := rg.colors
	positions := processColorStops(sizeX, rg.stopPositions)
	// Color transition hints — see the linear Layout for the rationale.
	// Non-repeating: exact per-gap exponents. Repeating: sample.
	var exponents []pr.Fl
	if rg.repeating {
		colors, positions = expandColorHints(colors, positions, rg.isHint)
	} else {
		colors, positions, exponents = resolveColorHints(colors, positions, rg.isHint)
	}
	if !rg.repeating {
		// Add explicit colors at boundaries if needed, because PDF doesn’t
		// extend color stops that are not displayed
		if positions[0] > 0 && positions[0] == positions[1] {
			positions = append([]pr.Fl{0}, positions...)
			colors = append([]parser.RGBA{colors[0]}, colors...)
			exponents = prependExponent(exponents, colors)
		}
		if positions[len(positions)-2] == positions[len(positions)-1] {
			positions = append(positions, positions[len(positions)-1]+1)
			colors = append(colors, colors[len(colors)-1])
		}
	}

	if positions[0] < 0 {
		// The negative-radius surgery below slices/reorders the stop list;
		// per-gap exponents can't be kept in sync, so fall back to linear
		// interpolation for this rare hint+negative-radius combination.
		exponents = nil
	}
	if positions[0] < 0 {
		// PDF does not like negative radiuses,
		// shift into the positive realm.
		if rg.repeating {
			// Add vector lengths to first position until positive
			vectorLength := positions[len(positions)-1] - positions[0]
			offset := vectorLength * pr.Fl(1+math.Floor(float64(-positions[0]/vectorLength)))
			for i, p := range positions {
				positions[i] = p + offset
			}
		} else {
			// only keep colors with position >= 0, interpolate if needed
			if positions[len(positions)-1] <= 0 {
				// All stops are negatives,
				// everything is "padded" with the last color.
				return backend.GradientLayout{ScaleY: 1, GradientKind: backend.GradientKind{Kind: "solid"}, Colors: []parser.RGBA{rg.colors[len(rg.colors)-1]}}
			}

			for i, position := range positions {
				if position == 0 {
					// Keep colors and positions from this rank
					colors, positions = colors[i:], positions[i:]
					break
				}

				if position > 0 {
					// Interpolate with the previous to get the color at 0.
					color := colors[i]
					negColor := colors[i-1]
					negPosition := positions[i-1]
					if negPosition >= 0 {
						panic(fmt.Sprintf("expected non positive negPosition, got %f", negPosition))
					}
					intermediateColor := gradientAverageColor(
						[]Color{negColor, negColor, color, color},
						[]pr.Fl{negPosition, 0, 0, position})
					colors = append([]Color{intermediateColor}, colors[i:]...)
					positions = append([]pr.Fl{0}, positions[i:]...)
					break
				}
			}

		}
	}

	spread := svg.NoRepeat
	if rg.repeating {
		spread = svg.Repeat
	}

	// spread.RadialGradient works with absolute lengths: apply scaleY
	fx := pr.Fl(centerX)
	fy := pr.Fl(centerY) / scaleY
	cx, cy := fx, fy
	var fr, r pr.Fl = 0, 1
	out := spread.RadialGradient(positions, colors, fx, fy, fr, cx, cy, r, pr.Fl(width)/scaleY, pr.Fl(height)/scaleY)

	out.Exponents = alignExponents(exponents, out.Colors)
	out.ScaleY = scaleY // restore the scale
	return out
}

func (rg RadialGradient) resolveSize(width, height, centerX, centerY pr.Float) (pr.Float, pr.Float) {
	if rg.size.IsExplicit() {
		sizeX, sizeY := rg.size.Explicit[0], rg.size.Explicit[1]
		sizeX_ := pr.ResolvePercentage(sizeX.Tagged(), width).V()
		sizeY_ := pr.ResolvePercentage(sizeY.Tagged(), height).V()
		return sizeX_, sizeY_
	}
	left := pr.Abs(centerX)
	right := pr.Abs(width - centerX)
	top := pr.Abs(centerY)
	bottom := pr.Abs(height - centerY)
	pick := pr.Maxs
	if strings.HasPrefix(rg.size.Keyword, "closest") {
		pick = pr.Mins
	}
	if strings.HasSuffix(rg.size.Keyword, "side") {
		if rg.shape == "circle" {
			sizeXy := pick(left, right, top, bottom)
			return sizeXy, sizeXy
		}
		// else: ellipse
		return pick(left, right), pick(top, bottom)
	}
	// else: corner
	if rg.shape == "circle" {
		sizeXy := pick(pr.Hypot(left, top), pr.Hypot(left, bottom),
			pr.Hypot(right, top), pr.Hypot(right, bottom))
		return sizeXy, sizeXy
	}
	// else: ellipse
	keys := [4]pr.Float{pr.Hypot(left, top), pr.Hypot(left, bottom), pr.Hypot(right, top), pr.Hypot(right, bottom)}
	m := map[pr.Float][2]pr.Float{
		keys[0]: {left, top},
		keys[1]: {left, bottom},
		keys[2]: {right, top},
		keys[3]: {right, bottom},
	}
	c := m[pick(keys[0], keys[1], keys[2], keys[3])]
	cornerX, cornerY := c[0], c[1]
	return cornerX * pr.Float(math.Sqrt(2)), cornerY * pr.Float(math.Sqrt(2))
}
