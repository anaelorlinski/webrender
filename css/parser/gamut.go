package parser

import (
	"math"

	scolor "github.com/SCKelemen/color"
	"github.com/benoitkugler/webrender/utils"
)

// oklchToLinearRGB converts OKLCH to linear sRGB without clamping.
// The SCKelemen/color library clamps in RGBA(), making InGamut always true.
func oklchToLinearRGB(l, c, h float64) (r, g, b float64) {
	hRad := h * math.Pi / 180
	a := c * math.Cos(hRad)
	bv := c * math.Sin(hRad)

	l_ := l + 0.3963377774*a + 0.2158037573*bv
	m_ := l - 0.1055613458*a - 0.0638541728*bv
	s_ := l - 0.0894841775*a - 1.2914855480*bv

	l3 := l_ * l_ * l_
	m3 := m_ * m_ * m_
	s3 := s_ * s_ * s_

	r = 4.0767416621*l3 - 3.3077115913*m3 + 0.2309699292*s3
	g = -1.2684380046*l3 + 2.6097574051*m3 - 0.3413193965*s3
	b = -0.0041960863*l3 - 0.7034186147*m3 + 1.7076147010*s3
	return r, g, b
}

// CIE LAB uses the D50 white point in CSS, per CSS Color 4.
var labWhiteD50 = [3]float64{0.3457 / 0.3585, 1.0, (1.0 - 0.3457 - 0.3585) / 0.3585}

// labToLinearRGB converts CIE LAB to linear-light sRGB without clamping,
// following the CSS Color 4 conversion chain: Lab → XYZ (D50) → XYZ (D65,
// via Bradford chromatic adaptation) → linear sRGB. The matrix and constant
// values are taken verbatim from the specification's reference sample code.
func labToLinearRGB(l, a, b float64) (r, g, bv float64) {
	// LAB → XYZ (relative to the D50 white point).
	// κ = 24389/27, ε = 216/24389 (κ·ε = 8).
	const kappa = 24389.0 / 27.0
	const epsilon = 216.0 / 24389.0
	fy := (l + 16) / 116
	fx := a/500 + fy
	fz := fy - b/200

	var xr, yr, zr float64
	if fx3 := fx * fx * fx; fx3 > epsilon {
		xr = fx3
	} else {
		xr = (116*fx - 16) / kappa
	}
	if l > kappa*epsilon {
		yr = math.Pow((l+16)/116, 3)
	} else {
		yr = l / kappa
	}
	if fz3 := fz * fz * fz; fz3 > epsilon {
		zr = fz3
	} else {
		zr = (116*fz - 16) / kappa
	}
	x := xr * labWhiteD50[0]
	y := yr * labWhiteD50[1]
	z := zr * labWhiteD50[2]

	// XYZ D50 → XYZ D65 (Bradford chromatic adaptation).
	x65 := 0.955473421488075*x - 0.02309845494876471*y + 0.06325924320057072*z
	y65 := -0.0283697093338637*x + 1.0099953980813041*y + 0.021041441191917323*z
	z65 := 0.012314014864481998*x - 0.020507649298898964*y + 1.330365926242124*z

	// XYZ D65 → linear-light sRGB.
	r = 3.2409699419045226*x65 - 1.537383177570094*y65 - 0.4986107602930034*z65
	g = -0.9692436362808796*x65 + 1.8759675015077202*y65 + 0.04155505740717559*z65
	bv = 0.05563007969699366*x65 - 0.20397695888897652*y65 + 1.0569715142428786*z65
	return r, g, bv
}

// gammaEnc applies sRGB gamma encoding to a linear value.
func gammaEnc(lin float64) float64 {
	if lin <= 0.0031308 {
		return 12.92 * lin
	}
	return 1.055*math.Pow(lin, 1.0/2.4) - 0.055
}

// inSRGBGamut checks whether linear RGB values map to sRGB in [0,1].
func inSRGBGamut(r, g, b float64) bool {
	for _, v := range []float64{gammaEnc(r), gammaEnc(g), gammaEnc(b)} {
		if v < -1e-6 || v > 1+1e-6 {
			return false
		}
	}
	return true
}

// clampF clamps a float64 to [0, 1].
func clampF(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Constants for the CSS Color 4 gamut mapping algorithm.
const (
	// gamutJND is the "just noticeable difference" threshold in OKLab: a
	// clipped color within this ΔEOK of the requested color is accepted as
	// perceptually indistinguishable. Per CSS Color 4 §14.2.
	gamutJND = 0.02
	// gamutEpsilon is the chroma binary-search termination width.
	gamutEpsilon = 0.0001
)

// clipToSRGB clamps an OKLCH color to sRGB by clipping each channel, and
// returns the resulting in-gamut sRGB channels (in [0,1]).
func clipToSRGB(l, c, h float64) (r, g, b float64) {
	lr, lg, lb := oklchToLinearRGB(l, c, h)
	return clampF(gammaEnc(lr)), clampF(gammaEnc(lg)), clampF(gammaEnc(lb))
}

// deltaEOKClip computes ΔEOK between an OKLCH color (l, c, h) and its clipped
// sRGB rendering (clipR, clipG, clipB). The clipped color is in gamut, so we
// reuse scolor's ToOKLAB for it (its inverse-gamma curve matches ours). The
// unclipped color's OKLab coordinates are its own polar form — computing them
// directly avoids scolor's clamping round-trip, which would corrupt the
// distance for out-of-gamut colors.
func deltaEOKClip(l, c, h, clipR, clipG, clipB float64) float64 {
	clipped := scolor.ToOKLAB(scolor.NewRGBA(clipR, clipG, clipB, 1))
	hRad := h * math.Pi / 180
	dl := l - clipped.L
	da := c*math.Cos(hRad) - clipped.A
	db := c*math.Sin(hRad) - clipped.B
	return math.Sqrt(dl*dl + da*da + db*db)
}

// gamutMapOKLCH maps an OKLCH color into the sRGB gamut using the CSS Color 4
// "Binary Search Gamut Mapping with Local MINDE" algorithm (§14.2): hold L and
// H fixed, binary-search chroma downward, and at each step compare the clipped
// candidate against the (possibly out-of-gamut) candidate via ΔEOK. When the
// clip is within the JND, the clipped color is returned — this preserves more
// chroma than reducing to the exact gamut boundary would.
func gamutMapOKLCH(l, c, h, alpha float64) RGBA {
	// Trivial lightness clamps (black / white), per spec.
	if l >= 1 {
		return RGBA{R: 1, G: 1, B: 1, A: utils.Fl(alpha)}
	}
	if l <= 0 {
		return RGBA{R: 0, G: 0, B: 0, A: utils.Fl(alpha)}
	}

	// Already in gamut: no mapping needed.
	if lr, lg, lb := oklchToLinearRGB(l, c, h); inSRGBGamut(lr, lg, lb) {
		return oklchToRGBA(l, c, h, alpha)
	}

	// If clipping the requested color is already imperceptible, use it.
	cr, cg, cb := clipToSRGB(l, c, h)
	if deltaEOKClip(l, c, h, cr, cg, cb) < gamutJND {
		return RGBA{R: utils.Fl(cr), G: utils.Fl(cg), B: utils.Fl(cb), A: utils.Fl(alpha)}
	}

	// Binary-search chroma, tracking whether the low bound is still in gamut.
	min, max := 0.0, c
	minInGamut := true
	current := c
	for max-min > gamutEpsilon {
		current = (min + max) / 2
		lr, lg, lb := oklchToLinearRGB(l, current, h)
		if minInGamut && inSRGBGamut(lr, lg, lb) {
			min = current
			continue
		}
		cr, cg, cb = clipToSRGB(l, current, h)
		e := deltaEOKClip(l, current, h, cr, cg, cb)
		if e < gamutJND {
			if gamutJND-e < gamutEpsilon {
				break
			}
			minInGamut = false
			min = current
		} else {
			max = current
		}
	}
	cr, cg, cb = clipToSRGB(l, current, h)
	return RGBA{R: utils.Fl(cr), G: utils.Fl(cg), B: utils.Fl(cb), A: utils.Fl(alpha)}
}

// gamutMapOKLAB converts OKLAB to OKLCH then gamut-maps.
func gamutMapOKLAB(l, a, b, alpha float64) RGBA {
	ol, oc, oh := oklabToOKLCH(l, a, b)
	return gamutMapOKLCH(ol, oc, oh, alpha)
}

// gamutMapLAB maps a CIE LAB color into the sRGB gamut. Per CSS Color 4, gamut
// mapping is always performed in OKLCH regardless of the source space, so the
// LAB color is first rendered to sRGB, converted to OKLCH, and mapped there.
func gamutMapLAB(l, a, b, alpha float64) RGBA {
	r, g, bv := labToLinearRGB(l, a, b)
	if inSRGBGamut(r, g, bv) {
		return linearToRGBA(r, g, bv, alpha)
	}
	ol, oc, oh := srgbToOKLCH(gammaEnc(r), gammaEnc(g), gammaEnc(bv))
	return gamutMapOKLCH(ol, oc, oh, alpha)
}

// gamutMapLCH maps a CIE LCH color into the sRGB gamut, mapping in OKLCH per
// CSS Color 4 (see gamutMapLAB).
func gamutMapLCH(l, c, h, alpha float64) RGBA {
	hRad := h * math.Pi / 180
	a := c * math.Cos(hRad)
	bv := c * math.Sin(hRad)
	return gamutMapLAB(l, a, bv, alpha)
}

// GamutMapOKLCH, GamutMapOKLAB, GamutMapLAB and GamutMapLCH expose the CSS
// Color 4 gamut mapping to other packages (notably relative-color resolution
// in css/validation) so that a color specified via relative syntax and the
// same color specified directly resolve identically.
func GamutMapOKLCH(l, c, h, alpha float64) RGBA { return gamutMapOKLCH(l, c, h, alpha) }
func GamutMapOKLAB(l, a, b, alpha float64) RGBA { return gamutMapOKLAB(l, a, b, alpha) }
func GamutMapLAB(l, a, b, alpha float64) RGBA   { return gamutMapLAB(l, a, b, alpha) }
func GamutMapLCH(l, c, h, alpha float64) RGBA   { return gamutMapLCH(l, c, h, alpha) }

// oklabToOKLCH converts OKLAB to OKLCH polar form.
func oklabToOKLCH(l, a, b float64) (ol, oc, oh float64) {
	ol = l
	oc = math.Sqrt(a*a + b*b)
	oh = math.Atan2(b, a) * 180 / math.Pi
	if oh < 0 {
		oh += 360
	}
	return
}

// srgbToOKLCH converts sRGB [0,1] to OKLCH.
func srgbToOKLCH(r, g, b float64) (ol, oc, oh float64) {
	// sRGB → linear
	lr := invGamma(r)
	lg := invGamma(g)
	lb := invGamma(b)
	// linear → OKLab
	l_ := 0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb
	m_ := 0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb
	s_ := 0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb
	l3 := math.Cbrt(l_)
	m3 := math.Cbrt(m_)
	s3 := math.Cbrt(s_)
	okl := 0.2104542553*l3 + 0.7936177850*m3 - 0.0040720468*s3
	oka := 1.9779984951*l3 - 2.4285922050*m3 + 0.4505937099*s3
	okb := 0.0259040371*l3 + 0.7827717662*m3 - 0.8086757660*s3
	return oklabToOKLCH(okl, oka, okb)
}

func invGamma(s float64) float64 {
	if s <= 0.04045 {
		return s / 12.92
	}
	return math.Pow((s+0.055)/1.055, 2.4)
}

// srgbToLab converts sRGB [0,1] to CIE LAB, inverting labToLinearRGB: sRGB →
// linear → XYZ (D65) → XYZ (D50, Bradford) → Lab (D50). Matrix and constant
// values are taken verbatim from the CSS Color 4 reference sample code, so it
// is the exact inverse of the LAB→sRGB path used for gamut mapping.
func srgbToLab(r, g, b float64) (l, a, bv float64) {
	lr, lg, lb := invGamma(r), invGamma(g), invGamma(b)

	// linear sRGB → XYZ (D65).
	x65 := 0.4123907992659595*lr + 0.357584339383878*lg + 0.1804807884018343*lb
	y65 := 0.2126390058715104*lr + 0.7151686787677559*lg + 0.07219231536073371*lb
	z65 := 0.01933081871559185*lr + 0.119194779794626*lg + 0.9505321522496606*lb

	// XYZ D65 → XYZ D50 (Bradford chromatic adaptation).
	x := 1.0479297925449969*x65 + 0.022946870601609652*y65 - 0.05019226628920524*z65
	y := 0.02962780877005599*x65 + 0.9904344267538799*y65 - 0.017073799063418826*z65
	z := -0.009243040646204504*x65 + 0.015055191490298152*y65 + 0.7518742814281371*z65

	// XYZ (D50) → Lab.
	const kappa = 24389.0 / 27.0
	const epsilon = 216.0 / 24389.0
	f := func(t float64) float64 {
		if t > epsilon {
			return math.Cbrt(t)
		}
		return (kappa*t + 16) / 116
	}
	fx := f(x / labWhiteD50[0])
	fy := f(y / labWhiteD50[1])
	fz := f(z / labWhiteD50[2])
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

// labToLCH converts CIE LAB to LCH polar form (hue in degrees, [0, 360)).
func labToLCH(l, a, b float64) (ol, oc, oh float64) {
	oc = math.Sqrt(a*a + b*b)
	oh = math.Atan2(b, a) * 180 / math.Pi
	if oh < 0 {
		oh += 360
	}
	return l, oc, oh
}

// SRGBToLAB and SRGBToLCH expose the CSS Color 4 sRGB→LAB/LCH conversions
// (D50 white point) so relative-color resolution extracts channels with the
// same white point used to rebuild them, keeping identity round-trips exact.
func SRGBToLAB(r, g, b float64) (l, a, bv float64) { return srgbToLab(r, g, b) }
func SRGBToLCH(r, g, b float64) (l, c, h float64) {
	return labToLCH(srgbToLab(r, g, b))
}

// oklchToRGBA converts OKLCH to clamped sRGB RGBA (parser.RGBA).
func oklchToRGBA(l, c, h, alpha float64) RGBA {
	r, g, b := oklchToLinearRGB(l, c, h)
	return linearToRGBA(r, g, b, alpha)
}

// linearToRGBA converts linear sRGB to clamped parser.RGBA.
func linearToRGBA(r, g, b, alpha float64) RGBA {
	return RGBA{
		R: clamp(utils.Fl(gammaEnc(r))),
		G: clamp(utils.Fl(gammaEnc(g))),
		B: clamp(utils.Fl(gammaEnc(b))),
		A: utils.Fl(alpha),
	}
}

// relativeLuminance computes the WCAG 2.1 relative luminance of an sRGB color.
// Input channels are in [0,1] sRGB (gamma-encoded). Returns a value in [0,1].
func relativeLuminance(r, g, b utils.Fl) float64 {
	lin := func(c utils.Fl) float64 {
		f := float64(c)
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrastRatio computes the WCAG 2.1 contrast ratio between two sRGB colors.
// Returns a ratio in [1, 21] (1 = no contrast, 21 = max black/white).
func contrastRatio(l1, l2 float64) float64 {
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// contrastColor implements the CSS Color 5 contrast-color() function.
// It returns white or black, whichever has higher contrast against the
// given background color. White wins ties (per spec).
func ContrastColor(bg RGBA) RGBA {
	lBg := relativeLuminance(bg.R, bg.G, bg.B)
	lWhite := 1.0
	lBlack := 0.0

	rWhite := contrastRatio(lWhite, lBg)
	rBlack := contrastRatio(lBlack, lBg)

	if rWhite >= rBlack {
		return RGBA{R: 1, G: 1, B: 1, A: 1}
	}
	return RGBA{R: 0, G: 0, B: 0, A: 1}
}
