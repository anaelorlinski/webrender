package validation

import (
	"math"
	"testing"

	pa "github.com/benoitkugler/webrender/css/parser"
	"github.com/benoitkugler/webrender/utils"
)

// tokenFromCSS tokenizes a CSS string and returns the single component value.
func tokenFromCSS(t *testing.T, input string) pa.Token {
	t.Helper()
	tokens := pa.Tokenize([]byte(input), true)
	tok := pa.ParseOneComponentValue(tokens)
	if tok == nil {
		t.Fatalf("%s: tokenization returned nil", input)
	}
	return tok
}

// resolveCSS parses input as a CSS color string and resolves it through
// parseColorResolved (which handles relative color syntax).
func resolveCSS(t *testing.T, input string) pa.Color {
	t.Helper()
	tok := tokenFromCSS(t, input)
	return parseColorResolved(tok)
}

// assertRelRGBA checks that the resolved color matches the expected RGBA
// values within tolerance.
func assertRelRGBA(t *testing.T, input string, expR, expG, expB, expA utils.Fl) {
	t.Helper()
	c := resolveCSS(t, input)
	if c.IsNone() {
		t.Fatalf("%s: expected valid color, got invalid", input)
	}
	if c.Type != pa.ColorRGBA {
		t.Fatalf("%s: expected ColorRGBA, got type %d", input, c.Type)
	}
	tol := utils.Fl(0.02)
	if !approxEqualFl(c.RGBA.R, expR, tol) {
		t.Errorf("%s: R=%.4f, want %.4f", input, c.RGBA.R, expR)
	}
	if !approxEqualFl(c.RGBA.G, expG, tol) {
		t.Errorf("%s: G=%.4f, want %.4f", input, c.RGBA.G, expG)
	}
	if !approxEqualFl(c.RGBA.B, expB, tol) {
		t.Errorf("%s: B=%.4f, want %.4f", input, c.RGBA.B, expB)
	}
	if !approxEqualFl(c.RGBA.A, expA, 0.001) {
		t.Errorf("%s: A=%.4f, want %.4f", input, c.RGBA.A, expA)
	}
}

// assertRelInvalid checks that the input does not resolve to a valid color.
func assertRelInvalid(t *testing.T, input string) {
	t.Helper()
	c := resolveCSS(t, input)
	if !c.IsNone() {
		t.Errorf("%s: expected invalid color, got R=%.4f G=%.4f B=%.4f A=%.4f",
			input, c.RGBA.R, c.RGBA.G, c.RGBA.B, c.RGBA.A)
	}
}

func approxEqualFl(a, b, tol utils.Fl) bool {
	return utils.Fl(math.Abs(float64(a-b))) <= tol
}

// --- General: identity (from <color> l c h / alpha) ---

func TestRelativeIdentity(t *testing.T) {
	// oklch(from red l c h) should produce ≈ red.
	// red in OKLCH is approximately L=0.6279, C=0.2577, H=29.23.
	// Identity should round-trip back to sRGB red.
	assertRelRGBA(t, "oklch(from red l c h)", 1.0, 0.0, 0.0, 1.0)

	// oklab identity
	assertRelRGBA(t, "oklab(from red l a b)", 1.0, 0.0, 0.0, 1.0)

	// lab identity
	assertRelRGBA(t, "lab(from red l a b)", 1.0, 0.0, 0.0, 1.0)

	// lch identity
	assertRelRGBA(t, "lch(from red l c h)", 1.0, 0.0, 0.0, 1.0)

	// hwb identity
	assertRelRGBA(t, "hwb(from red h w b)", 1.0, 0.0, 0.0, 1.0)
}

// --- General: channel overrides ---

func TestRelativeChannelOverride(t *testing.T) {
	// Override lightness to 50% (0.5) while keeping chroma and hue from red.
	// oklch(0.5 0.2577 29.23) → a darker red.
	c := resolveCSS(t, "oklch(from red 0.5 c h)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	// Should be darker than red (R < 1.0) but still reddish.
	if c.RGBA.R <= 0.5 || c.RGBA.R >= 1.0 {
		t.Errorf("oklch(from red 0.5 c h): R=%.4f, expected 0.5 < R < 1.0", c.RGBA.R)
	}
	if c.RGBA.G > 0.1 || c.RGBA.B > 0.1 {
		t.Errorf("oklch(from red 0.5 c h): G=%.4f B=%.4f, expected low (reddish)", c.RGBA.G, c.RGBA.B)
	}

	// Override lightness via percentage: 50% L → 0.5. Matches the direct
	// form oklch(0.5 0.2577 29.23) after CSS Color 4 OKLCH gamut mapping.
	assertRelRGBA(t, "oklch(from red 50% c h)",
		0.7663, 0.0, 0.0, 1.0)

	// Override chroma to 0 → grayscale at red's lightness.
	c = resolveCSS(t, "oklch(from red l 0 h)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	// With C=0, R should equal G and B (achromatic).
	if !approxEqualFl(c.RGBA.R, c.RGBA.G, 0.02) || !approxEqualFl(c.RGBA.G, c.RGBA.B, 0.02) {
		t.Errorf("oklch(from red l 0 h): expected achromatic, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}

	// Override hue to 240 (blue) while keeping red's L and C.
	c = resolveCSS(t, "oklch(from red l c 240)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	// Should be bluish (B > R and B > G).
	if c.RGBA.B <= c.RGBA.R || c.RGBA.B <= c.RGBA.G {
		t.Errorf("oklch(from red l c 240): expected blue-dominant, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

// --- General: math expressions ---

func TestRelativeMath(t *testing.T) {
	// calc(l * 0.5) halves the lightness of red (L≈0.6279 → 0.314).
	// This is NOT the same as oklch(from red 0.5 c h) which sets L=0.5.
	c1 := resolveCSS(t, "oklch(from red calc(l * 0.5) c h)")
	c2 := resolveCSS(t, "oklch(from red 0.314 c h)")
	if c1.IsNone() || c2.IsNone() {
		t.Fatal("expected valid colors")
	}
	if !approxEqualFl(c1.RGBA.R, c2.RGBA.R, 0.02) {
		t.Errorf("calc(l*0.5): R=%.4f, want ≈ %.4f (from 0.314 c h)", c1.RGBA.R, c2.RGBA.R)
	}

	// sin(0.5 * pi) = 1.0, so calc(sin(0.5 * pi) * 0.1) → C = 0.1.
	// oklch(from red l 0.1 h) should match.
	c3 := resolveCSS(t, "oklch(from red l calc(sin(0.5 * pi) * 0.1) h)")
	c4 := resolveCSS(t, "oklch(from red l 0.1 h)")
	if c3.IsNone() || c4.IsNone() {
		t.Fatal("expected valid colors")
	}
	if !approxEqualFl(c3.RGBA.R, c4.RGBA.R, 0.02) {
		t.Errorf("sin(0.5*pi)*0.1: R=%.4f, want ≈ %.4f", c3.RGBA.R, c4.RGBA.R)
	}

	// The hero case: oklch(from oklch(0.5 0.1 240) 10% calc(0.05 + (sin(1.0 * pi) * c)) h)
	// sin(pi) ≈ 0 (tiny float epsilon), so C ≈ 0.05. L = 10% = 0.1. H = 240 (from source).
	// Source C=0.1, so c=0.1, making C = 0.05 + sin(pi)*0.1 ≈ 0.05.
	// Use non-zero source chroma so hue is well-defined through the sRGB round-trip.
	c5 := resolveCSS(t, "oklch(from oklch(0.5 0.1 240) 10% calc(0.05 + (sin(1.0 * pi) * c)) h)")
	c6 := resolveCSS(t, "oklch(0.1 0.05 240)")
	if c5.IsNone() || c6.IsNone() {
		t.Fatal("expected valid colors")
	}
	// Looser tolerance for sin(pi) float epsilon and OKLCH round-trip.
	if !approxEqualFl(c5.RGBA.R, c6.RGBA.R, 0.03) {
		t.Errorf("hero case R: %.4f, want ≈ %.4f", c5.RGBA.R, c6.RGBA.R)
	}
	if !approxEqualFl(c5.RGBA.B, c6.RGBA.B, 0.03) {
		t.Errorf("hero case B: %.4f, want ≈ %.4f", c5.RGBA.B, c6.RGBA.B)
	}
}

// --- Alpha ---

func TestRelativeAlpha(t *testing.T) {
	// Explicit alpha override.
	assertRelRGBA(t, "oklch(from red l c h / 0.5)", 1.0, 0.0, 0.0, 0.5)

	// Omitted alpha inherits source alpha.
	// rgba(255, 0, 0, 0.3) → alpha should be 0.3.
	c := resolveCSS(t, "oklch(from rgba(255, 0, 0, 0.3) l c h)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if !approxEqualFl(c.RGBA.A, 0.3, 0.01) {
		t.Errorf("inherited alpha: A=%.4f, want 0.3", c.RGBA.A)
	}

	// alpha keyword → source alpha (1.0 for red).
	assertRelRGBA(t, "oklch(from red l c h / alpha)", 1.0, 0.0, 0.0, 1.0)

	// alpha keyword from semi-transparent source.
	c = resolveCSS(t, "oklch(from rgba(255, 0, 0, 0.7) l c h / alpha)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if !approxEqualFl(c.RGBA.A, 0.7, 0.01) {
		t.Errorf("alpha keyword: A=%.4f, want 0.7", c.RGBA.A)
	}
}

// --- Keywords / units ---

func TestRelativeKeywordsAndUnits(t *testing.T) {
	// none keyword for L → L=0.
	c := resolveCSS(t, "oklch(from red none c h)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	// L=0 → very dark (gamut mapping may leave a tiny residual).
	if c.RGBA.R > 0.08 {
		t.Errorf("oklch(from red none c h): R=%.4f, expected near 0 (dark)", c.RGBA.R)
	}

	// Angle units in hue: 0.5turn = 180deg.
	c1 := resolveCSS(t, "oklch(from red l c 0.5turn)")
	c2 := resolveCSS(t, "oklch(from red l c 180)")
	if c1.IsNone() || c2.IsNone() {
		t.Fatal("expected valid colors")
	}
	if !approxEqualFl(c1.RGBA.R, c2.RGBA.R, 0.02) {
		t.Errorf("0.5turn vs 180deg: R=%.4f vs %.4f", c1.RGBA.R, c2.RGBA.R)
	}

	// Case-insensitive function name and keywords.
	c3 := resolveCSS(t, "OKLCH(FROM red L C H)")
	if c3.IsNone() {
		t.Fatal("expected valid color for uppercase OKLCH")
	}
	if !approxEqualFl(c3.RGBA.R, 1.0, 0.02) {
		t.Errorf("OKLCH(FROM red L C H): R=%.4f, want ≈ 1.0", c3.RGBA.R)
	}
}

// --- Nested relative ---

func TestRelativeNested(t *testing.T) {
	// Double identity: oklch(from oklch(from red l c h) l c h) ≈ red.
	c := resolveCSS(t, "oklch(from oklch(from red l c h) l c h)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if !approxEqualFl(c.RGBA.R, 1.0, 0.03) {
		t.Errorf("nested identity: R=%.4f, want ≈ 1.0", c.RGBA.R)
	}
	if !approxEqualFl(c.RGBA.G, 0.0, 0.03) {
		t.Errorf("nested identity: G=%.4f, want ≈ 0.0", c.RGBA.G)
	}
	if !approxEqualFl(c.RGBA.B, 0.0, 0.03) {
		t.Errorf("nested identity: B=%.4f, want ≈ 0.0", c.RGBA.B)
	}
}

// --- Corner / invalid ---

func TestRelativeInvalid(t *testing.T) {
	// "from" with no source color.
	assertRelInvalid(t, "oklch(from)")

	// Too few channels.
	assertRelInvalid(t, "oklch(from red l c)")

	// Too many channels.
	assertRelInvalid(t, "oklch(from red l c h extra)")

	// Bad source color.
	assertRelInvalid(t, "oklch(from notacolor l c h)")

	// Unknown keyword in channel position.
	assertRelInvalid(t, "oklch(from red l badkeyword h)")
}

// --- Regression: non-relative colors still work ---

func TestRelativeRegression(t *testing.T) {
	// Plain oklch (no "from") should still parse correctly.
	assertRelRGBA(t, "oklch(0.7 0.2 40)", 1.0, 0.403, 0.158, 1.0)

	// Named colors.
	assertRelRGBA(t, "red", 1.0, 0.0, 0.0, 1.0)
	assertRelRGBA(t, "blue", 0.0, 0.0, 1.0, 1.0)

	// Hex.
	assertRelRGBA(t, "#ff0000", 1.0, 0.0, 0.0, 1.0)

	// RGB.
	assertRelRGBA(t, "rgb(255, 0, 0)", 1.0, 0.0, 0.0, 1.0)

	// currentcolor should still return ColorCurrentColor type.
	c := resolveCSS(t, "currentcolor")
	if c.Type != pa.ColorCurrentColor {
		t.Errorf("currentcolor: expected type %d, got %d", pa.ColorCurrentColor, c.Type)
	}
}
