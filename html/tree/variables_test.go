package tree

// Test CSS custom properties, also known as CSS variables.

import (
	"fmt"
	"strings"
	"testing"

	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/utils"
	tu "github.com/benoitkugler/webrender/utils/testutils"
)

// parse a simple html with style and an element and return
// the computed style for this element
func setupVar(t *testing.T, html string) (htmlS, elementS pr.ElementStyle) {
	page, err := newHtml(utils.InputString(html))
	if err != nil {
		t.Fatal(err)
	}

	styleFor := GetAllComputedStyles(page, nil, false, nil, nil, nil, nil, false, nil)
	htmlNode := page.Root
	elementNode := htmlNode.FirstChild.NextSibling.FirstChild

	htmlS = styleFor.Get((*utils.HTMLNode)(htmlNode), "")
	elementS = styleFor.Get((*utils.HTMLNode)(elementNode), "")
	return
}

func TestVariableSimple(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, style := setupVar(t, `
      <style>
        p { --var: 10px; width: var(--var); }
      </style>
      <p></p>
    `)

	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableNotComputed(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, style := setupVar(t, `
	<style>
	p { --var: 1rem; width: var(--var) }
      </style>
      <p></p>
	  `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(16))
}

func TestVariableInherit(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --var: 10px }
        p { width: var(--var) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableInheritOverride(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --var: 20px }
        p { width: var(--var); --var: 10px }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableDefaultUnknown(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, style := setupVar(t, `
      <style>
        p { width: var(--x, 10px) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableDefaultVar(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, style := setupVar(t, `
      <style>
        p { --var: 10px; width: var(--x, var(--var)) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableCaseSensitive1(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --VAR: 20px }
        p { width: var(--VAR) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, style.GetWidth(), pr.FToPx(20))
}

func TestVariableCaseSensitive2(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --var: 20px }
        body { --VAR: 10px }
        p { width: var(--VAR) }
      </style>
      <p></p>
    `)
	exp := pr.FToPx(10)
	tu.AssertEqual(t, style.GetWidth(), exp)
}

func TestVariableChain(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --foo: 10px }
        body { --var: var(--foo) }
        p { width: var(--var) }
      </style>
      <p></p>
    `)
	exp := pr.FToPx(10)
	tu.AssertEqual(t, style.GetWidth(), exp)
}

func TestVariableChainRoot(t *testing.T) {
	// Regression test for #1656.
	style, _ := setupVar(t, `
      <style>
        html { --var2: 10px; --var1: var(--var2); width: var(--var1) }
      </style>
    `)
	exp := pr.FToPx(10)
	tu.AssertEqual(t, style.GetWidth(), exp)
}

func TestVariableSelf(t *testing.T) {
	_, _ = setupVar(t, `
      <style>
        html { --var1: var(--var1) }
      </style>
    `)
}

func TestVariableLoop(t *testing.T) {
	_, _ = setupVar(t, `
      <style>
        html { --var1: var(--var2); --var2: var(--var1); padding: var(--var1) }
      </style>
    `)
}

func TestVariableChainRootMissing(t *testing.T) {
	// Regression test for #1656.
	_, _ = setupVar(t, `
      <style>
        html { --var1: var(--var-missing); width: var(--var1) }
      </style>
    `)
}

func TestVariablePartial1(t *testing.T) {
	_, style := setupVar(t, `
      <style>
        html { --var: 10px }
        div { margin: 0 0 0 var(--var) }
      </style>
      <div></div>
    `)
	exp0, exp10 := pr.FToPx(0), pr.FToPx(10)
	if got := style.GetMarginTop(); got != exp0 {
		t.Fatalf("expected %v, got %v", exp0, got)
	}
	if got := style.GetMarginRight(); got != exp0 {
		t.Fatalf("expected %v, got %v", exp0, got)
	}
	if got := style.GetMarginBottom(); got != exp0 {
		t.Fatalf("expected %v, got %v", exp0, got)
	}
	if got := style.GetMarginLeft(); got != exp10 {
		t.Fatalf("expected %v, got %v", exp10, got)
	}
}

func TestVariableShorthandMargin(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	_, div := setupVar(t, `
      <style>
        html { --var: 10px }
        div { margin: 0 0 0 var(--var) }
      </style>
      <div></div>
    `)
	tu.AssertEqual(t, div.GetMarginTop(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginRight(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginBottom(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginLeft(), pr.FToPx(10))
}

func TestVariableShorthandMarginMultiple(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --var1: 10px; --var2: 20px }
        div { margin: var(--var2) 0 0 var(--var1) }
      </style>
      <div></div>
    `)
	tu.AssertEqual(t, div.GetMarginTop(), pr.FToPx(20))
	tu.AssertEqual(t, div.GetMarginRight(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginBottom(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginLeft(), pr.FToPx(10))
}

func TestVariableShorthandMarginInvalid(t *testing.T) {
	logs := tu.CaptureLogs()
	_, div := setupVar(t, `
          <style>
            html { --var: blue }
            div { margin: 0 0 0 var(--var) }
          </style>
          <div></div>
        `)
	_ = div.GetMarginBottom()
	tu.AssertEqual(t, len(logs.Logs()), 1)
	tu.AssertEqual(t, strings.Contains(logs.Logs()[0], "invalid value"), true)

	tu.AssertEqual(t, div.GetMarginTop(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginRight(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginBottom(), pr.FToPx(0))
	tu.AssertEqual(t, div.GetMarginLeft(), pr.FToPx(0))
}

func TestVariableShorthandBorder(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --var: 1px solid blue }
        div { border: var(--var) }
      </style>
      <div></div>
    `)
	tu.AssertEqual(t, div.GetBorderTopWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderRightWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderBottomWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderLeftWidth(), pr.FToV(1))
}

func TestVariableShorthandBorderSide(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --var: 1px solid blue }
        div { border-top: var(--var) }
      </style>
      <div></div>
    `)
	tu.AssertEqual(t, div.GetBorderTopWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderRightWidth(), pr.FToV(0))
	tu.AssertEqual(t, div.GetBorderBottomWidth(), pr.FToV(0))
	tu.AssertEqual(t, div.GetBorderLeftWidth(), pr.FToV(0))
}

func TestVariableShorthandBorderMixed(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --var: 1px solid }
        div { border: blue var(--var) }
      </style>
      <div></div>
    `)
	tu.AssertEqual(t, div.GetBorderTopWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderRightWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderBottomWidth(), pr.FToV(1))
	tu.AssertEqual(t, div.GetBorderLeftWidth(), pr.FToV(1))
}

func TestVariableShorthandBorderMixedInvalid(t *testing.T) {
	logs := tu.CaptureLogs()
	_, div := setupVar(t, `
          <style>
            html { --var: 1px solid blue }
            div { border: blue var(--var) }
          </style>
          <div></div>
        `)
	// TODO: we should only get one warning here
	// trigger eval
	_ = div.GetBorderTopWidth()
	tu.AssertEqual(t, len(logs.Logs()), 2)
	tu.AssertEqual(t, strings.Contains(logs.Logs()[0], "multiple border-top-color values"), true)
	tu.AssertEqual(t, div.GetBorderTopWidth(), pr.FToV(0))
	tu.AssertEqual(t, div.GetBorderRightWidth(), pr.FToV(0))
	tu.AssertEqual(t, div.GetBorderBottomWidth(), pr.FToV(0))
	tu.AssertEqual(t, div.GetBorderLeftWidth(), pr.FToV(0))
}

func TestVariableShorthandBackground(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	for _, test := range []struct {
		var_       string
		background string
	}{
		{"blue", "var(--v)"},
		{"padding-box url(pattern.png)", "var(--v)"},
		{"padding-box url(pattern.png)", "white var(--v) center"},
		{"100%", "url(pattern.png) var(--v) var(--v) / var(--v) var(--v)"},
		{"left / 100%", "url(pattern.png) top var(--v) 100%"},
	} {
		_, _ = setupVar(t, fmt.Sprintf(`
		  <style>
			html { --v: %s }
			div { background: %s }
		  </style>
		  <div></div>
		`, test.var_, test.background))
	}
}

func TestVariableShorthandBackgroundInvalid(t *testing.T) {
	for _, test := range []struct {
		var_       string
		background string
	}{
		{"invalid", "var(--v)"},
		{"blue", "var(--v) var(--v)"},
		{"100%", "url(pattern.png) var(--v) var(--v) var(--v)"},
	} {
		logs := tu.CaptureLogs()
		_, div := setupVar(t, fmt.Sprintf(`
			  <style>
				html { --v: %s }
				div { background: %s }
			  </style>
			  <div></div>
			`, test.var_, test.background))
		_ = div.GetBackgroundColor()
		tu.AssertEqual(t, len(logs.Logs()), 1)
		// tu.AssertEqual(t, strings.Contains(logs.Logs()[0], "invalid"), true)
	}
}

func TestVariableInitial(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	// Regression test for #2075.
	html, p := setupVar(t, `
      <style>
        html { --var: initial }
        p { width: var(--var) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, html.GetWidth(), p.GetWidth())
}

func TestVariableInitialDefault(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	// Regression test for #2075.
	html, p := setupVar(t, `
	<style>
	p { --var: initial; width: var(--var, 10px) }
	</style>
	<p></p>
	`)
	tu.AssertEqual(t, html.GetWidth(), p.GetWidth())
	// tu.AssertEqual(t, style.GetWidth(), pr.FToPx(10))
}

func TestVariableInitialDefaultVar(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	// Regression test for #2075.
	html, style := setupVar(t, `
      <style>
        p { --var: initial; width: var(--var, var(--var)) }
      </style>
      <p></p>
    `)
	tu.AssertEqual(t, html.GetWidth(), style.GetWidth())
}

func TestVariableFallback(t *testing.T) {
	for prop := range pr.KnownProperties {
		_, style := setupVar(t, fmt.Sprintf(`
		  <style>
			div {
			  --var: improperValue;
			  %s: var(--var);
			}
		  </style>
		  <div></div>
		`, prop))
		_ = style.Get(prop.Key()) // just check for crashes
	}
}

// --- Regression: resolveVar must preserve nested non-var functions ---

func TestVariableNestedCalcPreserved(t *testing.T) {
	// oklch(from var(--c) calc(l + 0.15) c h) — the calc() must not be
	// dropped by resolveVar. If it is dropped, only 2 channels remain
	// and the color is invalid.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --c: red }
        div { color: oklch(from var(--c) calc(l + 0.15) c h) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(from var(--c) calc(l + 0.15) c h): expected valid color, got invalid (calc dropped by resolveVar?)")
	}
	// L increased by 0.15 from red's L≈0.6279 → L≈0.78.
	// Result should be a lighter red (R close to 1, low G/B).
	if c.RGBA.R < 0.9 {
		t.Errorf("oklch(from var(--c) calc(l + 0.15) c h): R=%.4f, expected >= 0.9 (lighter red)", c.RGBA.R)
	}
}

func TestVariableNestedCalcDarker(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --c: red }
        div { color: oklch(from var(--c) calc(l - 0.15) c h) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(from var(--c) calc(l - 0.15) c h): expected valid color, got invalid")
	}
	// L decreased by 0.15 from red's L≈0.6279 → L≈0.48.
	// Result should be darker than red.
	if c.RGBA.R >= 1.0 {
		t.Errorf("oklch(from var(--c) calc(l - 0.15) c h): R=%.4f, expected < 1.0 (darker red)", c.RGBA.R)
	}
}

func TestVariableGradientRelativeColor(t *testing.T) {
	// The exact failing case from the user's CSS:
	// linear-gradient with oklch(from var(--c) calc(...) c h) color stops.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html { --c: oklch(0.6 0.2 30) }
        div {
          background: linear-gradient(125deg,
            oklch(from var(--c) calc(l + 0.15) c h) 0%,
            oklch(from var(--c) calc(l - 0.15) c h) 100%);
        }
      </style>
      <div></div>
    `)
	images := div.GetBackgroundImage()
	if len(images) == 0 {
		t.Fatal("expected at least one background image (gradient)")
	}
	grad, ok := images[0].(pr.LinearGradient)
	if !ok {
		t.Fatalf("expected LinearGradient, got %T", images[0])
	}
	if len(grad.ColorStops) != 2 {
		t.Fatalf("expected 2 color stops, got %d", len(grad.ColorStops))
	}
	// Both stops should be valid colors (non-zero Type).
	for i, stop := range grad.ColorStops {
		if stop.Color.Type == 0 {
			t.Errorf("color stop %d: expected valid color, got invalid (calc dropped?)", i)
		}
	}
	// First stop (L+0.15) should be lighter than second stop (L-0.15).
	if grad.ColorStops[0].Color.RGBA.R <= grad.ColorStops[1].Color.RGBA.R {
		t.Errorf("expected first stop (L+0.15) to be lighter: R[0]=%.4f, R[1]=%.4f",
			grad.ColorStops[0].Color.RGBA.R, grad.ColorStops[1].Color.RGBA.R)
	}
}

// --- calc() inside non-relative oklch() channels ---

func TestCalcInOKLCHChannels(t *testing.T) {
	// oklch(0.710 calc(0.03 + 0.1 * 0.968) 265) — the calc() in the
	// chroma channel must be resolved to a plain number before parsing.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(0.710 calc(0.03 + 0.1 * 0.968) 265) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(0.710 calc(0.03 + 0.1 * 0.968) 265): expected valid color, got invalid (calc not resolved?)")
	}
	// calc(0.03 + 0.1 * 0.968) = 0.03 + 0.0968 = 0.1268
	// oklch(0.710 0.1268 265) should be a muted blue-ish color.
	// Just verify it's a valid, non-black, non-white color.
	if c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0 {
		t.Error("expected non-black color")
	}
	if c.RGBA.R == 1 && c.RGBA.G == 1 && c.RGBA.B == 1 {
		t.Error("expected non-white color")
	}
}

func TestCalcInOKLCHLightnessChannel(t *testing.T) {
	// calc() in the L channel.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(calc(0.5 + 0.2) 0.15 30) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(calc(0.5 + 0.2) 0.15 30): expected valid color, got invalid")
	}
	// L = 0.7, should be a fairly bright orange.
	if c.RGBA.R < 0.5 {
		t.Errorf("expected bright color with L=0.7, R=%.4f", c.RGBA.R)
	}
}

func TestCalcInOKLABChannels(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklab(0.7 calc(0.05 + 0.03) 0.02) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklab(0.7 calc(0.05 + 0.03) 0.02): expected valid color, got invalid")
	}
}

func TestCalcInLABChannels(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: lab(calc(50 + 10) 20 -30) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("lab(calc(50 + 10) 20 -30): expected valid color, got invalid")
	}
}

func TestCalcInLCHChannels(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: lch(60 calc(20 + 10) 30) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("lch(60 calc(20 + 10) 30): expected valid color, got invalid")
	}
}

// --- var() inside parenthesized calc() sub-expressions ---

func TestVarInsideParenthesizedCalc(t *testing.T) {
	// The exact pattern from the user's CSS:
	// oklch(0.870 calc(0.03 + (var(--base-chroma) - var(--chroma-floor)) * 0.750) 265)
	// The var() calls are inside a ParenthesesBlock inside calc().
	// HasVar must detect them and resolveVar must substitute them.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --base-chroma: 0.13;
          --chroma-floor: 0.03;
        }
        div {
          color: oklch(0.870 calc(0.03 + (var(--base-chroma) - var(--chroma-floor)) * 0.750) 265);
        }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch with var() inside parenthesized calc: expected valid color, got invalid (var not resolved in ParenthesesBlock?)")
	}
	// calc(0.03 + (0.13 - 0.03) * 0.750) = 0.03 + 0.10 * 0.750 = 0.03 + 0.075 = 0.105
	// oklch(0.870 0.105 265) should be a light blue-ish color.
	if c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0 {
		t.Error("expected non-black color")
	}
}

func TestVarInsideParenthesizedCalcBackground(t *testing.T) {
	// Same pattern but for background-color, which was one of the failing properties.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --base-chroma: 0.13;
          --chroma-floor: 0.03;
        }
        div {
          background-color: oklch(0.870 calc(0.03 + (var(--base-chroma) - var(--chroma-floor)) * 0.750) 265);
        }
      </style>
      <div></div>
    `)
	bg := div.GetBackgroundColor()
	if bg.Type == 0 {
		t.Fatal("background-color with var() inside parenthesized calc: expected valid color, got invalid")
	}
}

func TestVarInsideNestedParenthesizedCalc(t *testing.T) {
	// Even deeper nesting: calc((var(--a) - (var(--b) + var(--c))) * var(--d))
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --a: 0.20;
          --b: 0.05;
          --c: 0.03;
          --d: 0.5;
        }
        div {
          color: oklch(0.6 calc((var(--a) - (var(--b) + var(--c))) * var(--d)) 150);
        }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch with deeply nested var() inside parenthesized calc: expected valid color, got invalid")
	}
	// calc((0.20 - (0.05 + 0.03)) * 0.5) = (0.20 - 0.08) * 0.5 = 0.12 * 0.5 = 0.06
	// oklch(0.6 0.06 150) should be a muted green.
	if c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0 {
		t.Error("expected non-black color")
	}
}

func TestCalcInOKLCHWithVarAndAlpha(t *testing.T) {
	// oklch with calc() in chroma and var() in alpha, using "/" separator.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --base-chroma: 0.13;
          --chroma-floor: 0.03;
          --alpha: 0.8;
        }
        div {
          color: oklch(0.710 calc(0.03 + (var(--base-chroma) - var(--chroma-floor)) * 0.968) 265 / var(--alpha));
        }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch with calc+var and alpha: expected valid color, got invalid")
	}
	// Alpha should be 0.8.
	if c.RGBA.A < 0.79 || c.RGBA.A > 0.81 {
		t.Errorf("expected alpha ≈ 0.8, got %.4f", c.RGBA.A)
	}
}

// --- contrast-color() with nested oklch+calc (Havana D Primera bug) ---

func TestContrastColorWithCalcOKLCH(t *testing.T) {
	// contrast-color(oklch(0.290 calc(0.03 + 0.1 * 0.564) 25))
	// L=0.290 is dark → contrast-color should return white.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: contrast-color(oklch(0.290 calc(0.03 + 0.1 * 0.564) 25)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("contrast-color(oklch(0.290 calc(...))): expected valid color, got invalid (calc not resolved inside contrast-color?)")
	}
	// Dark background → white text
	if c.RGBA.R != 1 || c.RGBA.G != 1 || c.RGBA.B != 1 {
		t.Errorf("contrast-color of dark oklch should be white, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

func TestContrastColorWithCalcOKLCHLight(t *testing.T) {
	// contrast-color(oklch(0.9 calc(0.03 + 0.1) 25))
	// L=0.9 is light → contrast-color should return black.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: contrast-color(oklch(0.9 calc(0.03 + 0.1) 25)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("contrast-color(oklch(0.9 calc(...))): expected valid color, got invalid")
	}
	// Light background → black text
	if c.RGBA.R != 0 || c.RGBA.G != 0 || c.RGBA.B != 0 {
		t.Errorf("contrast-color of light oklch should be black, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

func TestContrastColorWithVarAndCalc(t *testing.T) {
	// The exact Havana D Primera pattern:
	// --accent1-900: oklch(0.290 calc(var(--chroma-floor) + (var(--base-chroma) - var(--chroma-floor)) * 0.564) var(--complement-hue-1))
	// --accent1-900-text: contrast-color(var(--accent1-900))
	// color: var(--accent1-900-text)
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --base-chroma: 0.13;
          --chroma-floor: 0.03;
          --complement-hue-1: 25;
          --accent1-900: oklch(0.290 calc(var(--chroma-floor) + (var(--base-chroma) - var(--chroma-floor)) * 0.564) var(--complement-hue-1));
          --accent1-900-text: contrast-color(var(--accent1-900));
        }
        div { color: var(--accent1-900-text) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("contrast-color via var with nested oklch+calc: expected valid color, got invalid")
	}
	// L=0.290 is dark → should be white
	if c.RGBA.R != 1 || c.RGBA.G != 1 || c.RGBA.B != 1 {
		t.Errorf("contrast-color of dark oklch (L=0.290) should be white, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

func TestContrastColorWithVarAndCalcLight(t *testing.T) {
	// Same pattern but with a light color → should produce black text.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --base-chroma: 0.13;
          --chroma-floor: 0.03;
          --complement-hue-1: 25;
          --accent1-50: oklch(0.970 calc(var(--chroma-floor) + (var(--base-chroma) - var(--chroma-floor)) * 0.510) var(--complement-hue-1));
          --accent1-50-text: contrast-color(var(--accent1-50));
        }
        div { color: var(--accent1-50-text) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("contrast-color via var with light oklch+calc: expected valid color, got invalid")
	}
	// L=0.970 is very light → should be black
	if c.RGBA.R != 0 || c.RGBA.G != 0 || c.RGBA.B != 0 {
		t.Errorf("contrast-color of light oklch (L=0.970) should be black, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}

// --- HasVar / resolveVar ParenthesesBlock regression tests ---

func TestHasVarParenthesesBlock(t *testing.T) {
	// Direct test: var() inside a parenthesized sub-expression of calc()
	// must be detected by HasVar and resolved by resolveVar.
	// This is a regression test for the bug where HasVar/resolveVar
	// did not recurse into ParenthesesBlock.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --a: 0.20;
          --b: 0.05;
        }
        div {
          color: oklch(0.6 calc((var(--a) - var(--b)) * 0.5) 150);
        }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch with var() inside parenthesized calc: expected valid color, got invalid (HasVar/resolveVar ParenthesesBlock regression?)")
	}
}

func TestResolveVarNestedParenthesesBlocks(t *testing.T) {
	// Deeply nested parentheses: calc((var(--a) - (var(--b) + var(--c))) * var(--d))
	// Each ParenthesesBlock containing var() must be resolved.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        html {
          --a: 0.20;
          --b: 0.05;
          --c: 0.03;
          --d: 0.5;
        }
        div {
          background-color: oklch(0.7 calc((var(--a) - (var(--b) + var(--c))) * var(--d)) 265);
        }
      </style>
      <div></div>
    `)
	bg := div.GetBackgroundColor()
	if bg.Type == 0 {
		t.Fatal("oklch with deeply nested var() in parenthesized calc: expected valid color, got invalid")
	}
}

// --- calc() in color channels regression tests ---

func TestCalcInOKLCHHueChannel(t *testing.T) {
	// calc() in the hue channel (angle).
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(0.7 0.15 calc(180 + 85)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(0.7 0.15 calc(180 + 85)): expected valid color, got invalid")
	}
}

func TestCalcInHWBChannels(t *testing.T) {
	// calc() in hwb channels.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: hwb(calc(120 + 60) calc(10 + 20) calc(30 + 10)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("hwb(calc(120 + 60) calc(10 + 20) calc(30 + 10)): expected valid color, got invalid")
	}
}

func TestCalcInOKLCHPercentageChannel(t *testing.T) {
	// calc() with percentage in oklch channel.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(calc(50% + 20%) 0.15 265) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(calc(50% + 20%) 0.15 265): expected valid color, got invalid")
	}
	// 50% + 20% = 70% → L = 0.7
	// Should be a fairly bright blue-ish color.
	if c.RGBA.R == 0 && c.RGBA.G == 0 && c.RGBA.B == 0 {
		t.Error("expected non-black color")
	}
}

func TestMinInOKLCHChannel(t *testing.T) {
	// min() in oklch chroma channel.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(0.7 min(0.2, 0.15) 265) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(0.7 min(0.2, 0.15) 265): expected valid color, got invalid")
	}
}

func TestMaxInOKLCHChannel(t *testing.T) {
	// max() in oklch chroma channel.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: oklch(0.7 max(0.05, 0.15) 265) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("oklch(0.7 max(0.05, 0.15) 265): expected valid color, got invalid")
	}
}

// --- generic calc resolution in non-L4 color functions ---

func TestCalcInRGBChannels(t *testing.T) {
	// calc() in rgb() — this was not handled before the generic refactoring
	// because rgb() is not in relSpaces.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: rgb(calc(50 + 100) calc(20 + 40) calc(10 + 30)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("rgb(calc(50 + 100) calc(20 + 40) calc(10 + 30)): expected valid color, got invalid")
	}
	// rgb(150, 60, 40)
	if c.RGBA.R < 0.58 || c.RGBA.R > 0.59 {
		t.Errorf("expected R ≈ 150/255 ≈ 0.588, got %.4f", c.RGBA.R)
	}
}

func TestCalcInHSLChannels(t *testing.T) {
	// calc() in hsl() — this was not handled before the generic refactoring.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: hsl(calc(120 + 60) calc(50% + 50%) calc(25% + 25%)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("hsl(calc(120 + 60) calc(50% + 50%) calc(25% + 25%)): expected valid color, got invalid")
	}
}

func TestCalcInRGBPercentageChannels(t *testing.T) {
	// calc() with percentages in rgb().
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: rgb(calc(50% + 25%) calc(10% + 20%) calc(5% + 5%)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("rgb(calc(50% + 25%) calc(10% + 20%) calc(5% + 5%)): expected valid color, got invalid")
	}
	// rgb(75%, 30%, 10%)
	if c.RGBA.R < 0.74 || c.RGBA.R > 0.76 {
		t.Errorf("expected R ≈ 0.75, got %.4f", c.RGBA.R)
	}
}

func TestCalcNestedInContrastColor(t *testing.T) {
	// contrast-color(oklch(calc(0.03 + 0.26) calc(0.03 + 0.05) 25))
	// — calc in both channels, nested inside contrast-color.
	defer tu.CaptureLogs().AssertNoLogs(t)
	_, div := setupVar(t, `
      <style>
        div { color: contrast-color(oklch(calc(0.03 + 0.26) calc(0.03 + 0.05) 25)) }
      </style>
      <div></div>
    `)
	c := div.GetColor()
	if c.Type == 0 {
		t.Fatal("contrast-color(oklch(calc(...), calc(...), 25)): expected valid color, got invalid")
	}
	// L=0.29 is dark → white
	if c.RGBA.R != 1 || c.RGBA.G != 1 || c.RGBA.B != 1 {
		t.Errorf("contrast-color of dark oklch should be white, got R=%.4f G=%.4f B=%.4f",
			c.RGBA.R, c.RGBA.G, c.RGBA.B)
	}
}
