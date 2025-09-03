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
func (ctx drawContext) drawText(textbox *bo.TextBox, offsetX fl, textOverflow string, blockEllipsis pr.TaggedString) {
	if textbox.Style.GetVisibility() != "visible" {
		return
	}

	// Draw text decoration

	decoration := textbox.Style.GetTextDecorationLine()
	color := tree.ResolveColor(textbox.Style, pr.PTextDecorationColor)

	var offsetY pr.Float

	metrics := textbox.TextLayout.Metrics()

	if decoration&pr.Overline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.Ascent) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
	if decoration&pr.Underline != 0 {
		thickness := metrics.UnderlineThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.UnderlinePosition) + pr.Float(thickness)/2
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}

	x, y := pr.Fl(textbox.PositionX), pr.Fl(textbox.PositionY+textbox.Baseline.V())
	ctx.dst.State().SetColorRgba(textbox.Style.GetColor().RGBA, false)

	textbox.TextLayout.ApplyJustification()
	ctx.drawFirstLine(textbox, textOverflow, blockEllipsis, x, y)

	if decoration&pr.LineThrough != 0 {
		thickness := metrics.StrikethroughThickness
		offsetY = textbox.Baseline.V() - pr.Float(metrics.StrikethroughPosition)
		ctx.drawTextDecoration(textbox, offsetX, pr.Fl(offsetY), thickness, color.RGBA)
	}
}

func (ctx drawContext) drawFirstLine(textbox *bo.TextBox, textOverflow string, blockEllipsis pr.TaggedString, x, y pr.Fl) {
	// Don’t draw lines with only invisible characters
	if strings.TrimSpace(textbox.TextS()) == "" {
		return
	}

	fontSize := textbox.Style.GetFontSize().Value
	if fontSize < 1e-6 { // Default float precision used by pydyf
		return
	}

	textContext := draw.Context{Output: ctx.dst, Fonts: ctx.fonts}
	text := textContext.CreateFirstLine(textbox.TextLayout, textOverflow, blockEllipsis, 1, x, y, 0)
	ctx.dst.DrawText([]backend.TextDrawing{text})
}

// Draw text-decoration of “textbox“ to a “context“.
func (ctx drawContext) drawTextDecoration(textbox *bo.TextBox, offsetX, offsetY, thickness pr.Fl, color Color) {
	ctx.drawLine(fl(textbox.PositionX), fl(textbox.PositionY)+offsetY, fl(textbox.PositionX)+fl(textbox.Width.V()), fl(textbox.PositionY)+offsetY,
		thickness, textbox.Style.GetTextDecorationStyle(), [2]parser.RGBA{color}, offsetX)
}
