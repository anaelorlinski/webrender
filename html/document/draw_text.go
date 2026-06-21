package document

import (
	"strings"

	"github.com/benoitkugler/webrender/backend"
	"github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/text/draw"

	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/tree"
)

// (offsetX=0,textOverflow="clip")
//
// Fork-enhanced version preserves CSS Text Decoration Level 4 thickness/offset
// override support and per-glyph text-shadow rendering with sharp + Gaussian
// fallback paths. Upstream's simple version doesn't handle either.
func (ctx drawContext) drawText(textbox *bo.TextBox, offsetX fl, textOverflow string, blockEllipsis pr.TaggedString) {
	if textbox.Style.GetVisibility() != "visible" {
		return
	}

	// Draw text decoration

	decoration := textbox.Style.GetTextDecorationLine()
	color := tree.ResolveColor(textbox.Style, pr.PTextDecorationColor)

	var offsetY pr.Float

	metrics := textbox.TextLayout.Metrics()

	fontSize := pr.Fl(textbox.Style.GetFontSize().Value)
	thicknessOverride, hasThickness := resolveDecorationLength(
		textbox.Style.GetTextDecorationThickness(), fontSize)
	offsetOverride, hasOffset := resolveDecorationLength(
		textbox.Style.GetTextUnderlineOffset(), fontSize)

	if decoration&pr.Overline != 0 {
		thickness := metrics.UnderlineThickness
		if hasThickness {
			thickness = thicknessOverride
		}
		offsetY = textbox.Baseline.V() - pr.Float(metrics.Ascent) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
	if decoration&pr.Underline != 0 {
		thickness := metrics.UnderlineThickness
		if hasThickness {
			thickness = thicknessOverride
		}
		if hasOffset {
			// `text-underline-offset` measures from baseline to the top edge
			// of the underline; the existing draw API expects the line center.
			offsetY = textbox.Baseline.V() + pr.Float(offsetOverride) + pr.Float(thickness)/2
		} else {
			offsetY = textbox.Baseline.V() - pr.Float(metrics.UnderlinePosition) + pr.Float(thickness)/2
		}
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}

	x, y := pr.Fl(textbox.PositionX), pr.Fl(textbox.PositionY+textbox.Baseline.V())

	// Draw text-shadows (painted behind the actual text). The backend owns
	// the offset + blur geometry; we resolve the CSS shadow list, build the
	// text line once, and delegate one call per shadow (back-to-front).
	textbox.TextLayout.ApplyJustification()
	shadows := textbox.Style.GetTextShadow()
	if len(shadows) != 0 {
		if td, ok := ctx.firstLineDrawing(textbox, textOverflow, blockEllipsis, x, y); ok {
			for i := len(shadows) - 1; i >= 0; i-- {
				s := shadows[i]
				sc := parser.RGBA(s.Color.RGBA)
				if sc.A == 0 {
					continue
				}
				ctx.dst.DrawTextShadow(td, pr.Fl(s.OffsetX.Value), pr.Fl(s.OffsetY.Value), pr.Fl(s.Blur.Value), sc)
			}
		}
	}

	ctx.dst.State().SetColorRgba(textbox.Style.GetColor().RGBA, false)
	ctx.drawFirstLine(textbox, textOverflow, blockEllipsis, x, y)

	if decoration&pr.LineThrough != 0 {
		thickness := metrics.StrikethroughThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.StrikethroughPosition)
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
}

// resolveDecorationLength turns a CSS Text Decoration L4 length-or-keyword
// into a concrete pixel length. Returns ok=false for `auto`/`from-font`,
// signalling that the caller should fall back to font metrics.
func resolveDecorationLength(v pr.TaggedDim, fontSize pr.Fl) (pr.Fl, bool) {
	if v.Tag == pr.Auto || v.Tag == pr.FromFont {
		return 0, false
	}
	switch v.Unit {
	case pr.Px, pr.Scalar:
		return pr.Fl(v.Value), true
	case pr.Perc:
		return pr.Fl(v.Value) / 100 * fontSize, true
	}
	return 0, false
}

// firstLineDrawing builds the TextDrawing for the box's first line at origin
// (x, y), applying the same skip guards as drawFirstLine. ok is false when the
// line should not be drawn (blank text or degenerate font size).
func (ctx drawContext) firstLineDrawing(textbox *bo.TextBox, textOverflow string, blockEllipsis pr.TaggedString, x, y pr.Fl) (backend.TextDrawing, bool) {
	// Don’t draw lines with only invisible characters
	if strings.TrimSpace(textbox.TextS()) == "" {
		return backend.TextDrawing{}, false
	}

	fontSize := textbox.Style.GetFontSize().Value
	if fontSize < 1e-6 { // Default float precision used by pydyf
		return backend.TextDrawing{}, false
	}

	textContext := draw.Context{Output: ctx.dst, Fonts: ctx.fonts}
	return textContext.CreateFirstLine(textbox.TextLayout, textOverflow, blockEllipsis, 1, x, y, 0), true
}

func (ctx drawContext) drawFirstLine(textbox *bo.TextBox, textOverflow string, blockEllipsis pr.TaggedString, x, y pr.Fl) {
	text, ok := ctx.firstLineDrawing(textbox, textOverflow, blockEllipsis, x, y)
	if !ok {
		return
	}
	ctx.dst.DrawText([]backend.TextDrawing{text})
}

// Draw text-decoration of “textbox“ to a “context“.
func (ctx drawContext) drawTextDecoration(textbox *bo.TextBox, offsetX, offsetY, thickness pr.Fl, color Color) {
	ctx.drawLine(fl(textbox.PositionX), fl(textbox.PositionY)+offsetY, fl(textbox.PositionX)+fl(textbox.Width.V()), fl(textbox.PositionY)+offsetY,
		thickness, textbox.Style.GetTextDecorationStyle(), [2]parser.RGBA{color}, offsetX)
}
