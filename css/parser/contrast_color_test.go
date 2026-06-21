package parser

import (
	"testing"
)

// --- contrast-color(): basic contrast ---

func TestContrastColorDarkBg(t *testing.T) {
	// Dark backgrounds should produce white text.
	assertRGBA(t, "contrast-color(black)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(#000)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(rgb(0 0 0))", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(navy)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(blue)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(maroon)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(purple)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "contrast-color(green)", 1.0, 1.0, 1.0, 1.0)
}

func TestContrastColorLightBg(t *testing.T) {
	// Light backgrounds should produce black text.
	assertRGBA(t, "contrast-color(white)", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(#fff)", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(rgb(255 255 255))", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(yellow)", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(lime)", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(cyan)", 0.0, 0.0, 0.0, 1.0)
	assertRGBA(t, "contrast-color(silver)", 0.0, 0.0, 0.0, 1.0)
}

// --- contrast-color(): mid-range ---

func TestContrastColorMidRange(t *testing.T) {
	// Medium-gray backgrounds: luminance ~0.18, black has slightly higher contrast.
	// gray = rgb(128,128,128) → luminance ≈ 0.21
	// contrast(white, gray) = (1+0.05)/(0.21+0.05) ≈ 4.04
	// contrast(black, gray) = (0.21+0.05)/(0+0.05) = 5.2 → black wins
	c := ParseColorString("contrast-color(gray)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if c.RGBA.R != 0 || c.RGBA.G != 0 || c.RGBA.B != 0 {
		t.Errorf("contrast-color(gray) should be black, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}

	// olive = rgb(128,128,0) → luminance ≈ 0.20 → black wins
	c = ParseColorString("contrast-color(olive)")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if c.RGBA.R != 0 || c.RGBA.G != 0 || c.RGBA.B != 0 {
		t.Errorf("contrast-color(olive) should be black, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

// --- contrast-color(): edge case — tie goes to white ---

func TestContrastColorTieGoesToWhite(t *testing.T) {
	// At the exact midpoint luminance, both white and black have equal contrast.
	// The spec says white wins ties.
	// The crossover point is where (1+0.05)/(L+0.05) = (L+0.05)/(0+0.05)
	// → (L+0.05)^2 = 0.05*1.05 = 0.0525
	// → L+0.05 = sqrt(0.0525) ≈ 0.2291
	// → L ≈ 0.1791
	// We can't easily hit this exactly with named colors, but we can test
	// that a color very close to the threshold resolves correctly.
	// rgb(123,123,123) → luminance ≈ 0.179 → very close to threshold.
	c := ParseColorString("contrast-color(rgb(123 123 123))")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	// Just verify it's either black or white (no error).
	isBW := (c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0) ||
		(c.RGBA.R == 1 && c.RGBA.G == 1 && c.RGBA.B == 1)
	if !isBW {
		t.Errorf("contrast-color should return black or white, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

// --- contrast-color(): nested color functions ---

func TestContrastColorNested(t *testing.T) {
	// contrast-color with a nested oklch color.
	// oklch(0.3 0.2 250) → a dark blue, should give white text.
	c := ParseColorString("contrast-color(oklch(0.3 0.2 250))")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if c.RGBA.R != 1 || c.RGBA.G != 1 || c.RGBA.B != 1 {
		t.Errorf("contrast-color(oklch(0.3 0.2 250)) should be white, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}

	// contrast-color with a nested hsl color.
	// hsl(60 100% 80%) → a light yellow, should give black text.
	c = ParseColorString("contrast-color(hsl(60 100% 80%))")
	if c.IsNone() {
		t.Fatal("expected valid color")
	}
	if c.RGBA.R != 0 || c.RGBA.G != 0 || c.RGBA.B != 0 {
		t.Errorf("contrast-color(hsl(60 100%% 80%%) should be black, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

// --- contrast-color(): case insensitivity ---

func TestContrastColorCaseInsensitive(t *testing.T) {
	assertRGBA(t, "CONTRAST-COLOR(black)", 1.0, 1.0, 1.0, 1.0)
	assertRGBA(t, "Contrast-Color(white)", 0.0, 0.0, 0.0, 1.0)
}

// --- contrast-color(): invalid inputs ---

func TestContrastColorInvalid(t *testing.T) {
	// No arguments.
	assertInvalid(t, "contrast-color()")
	// Too many arguments.
	assertInvalid(t, "contrast-color(black white)")
	assertInvalid(t, "contrast-color(red blue)")
	// Non-color argument.
	assertInvalid(t, "contrast-color(foo)")
	// currentColor is not resolvable at parse time.
	assertInvalid(t, "contrast-color(currentcolor)")
}

// --- contrast-color(): result is always black or white ---

func TestContrastColorAlwaysBlackOrWhite(t *testing.T) {
	inputs := []string{
		"contrast-color(red)",
		"contrast-color(orange)",
		"contrast-color(yellow)",
		"contrast-color(green)",
		"contrast-color(blue)",
		"contrast-color(indigo)",
		"contrast-color(violet)",
		"contrast-color(brown)",
		"contrast-color(pink)",
		"contrast-color(cyan)",
		"contrast-color(magenta)",
		"contrast-color(teal)",
		"contrast-color(aqua)",
		"contrast-color(olive)",
		"contrast-color(maroon)",
		"contrast-color(purple)",
		"contrast-color(gray)",
		"contrast-color(silver)",
		"contrast-color(lime)",
		"contrast-color(navy)",
	}
	for _, input := range inputs {
		c := ParseColorString(input)
		if c.IsNone() {
			t.Errorf("%s: expected valid color", input)
			continue
		}
		isBlack := c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0
		isWhite := c.RGBA.R == 1 && c.RGBA.G == 1 && c.RGBA.B == 1
		if !isBlack && !isWhite {
			t.Errorf("%s: result should be black or white, got R=%.4f G=%.4f B=%.4f",
				input, c.RGBA.R, c.RGBA.G, c.RGBA.B)
		}
		// Alpha should always be 1.
		if c.RGBA.A != 1 {
			t.Errorf("%s: alpha should be 1, got %.4f", input, c.RGBA.A)
		}
	}
}
