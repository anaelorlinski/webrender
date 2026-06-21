package document

import (
	"github.com/benoitkugler/webrender/backend"
	"github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
)

// toBackendRoundedBox converts a box-model rounded box into the backend-level,
// layout-independent form used by the shadow drawing primitives.
func toBackendRoundedBox(rb bo.RoundedBox) backend.RoundedBox {
	return backend.RoundedBox{
		X: pr.Fl(rb.X), Y: pr.Fl(rb.Y),
		Width: pr.Fl(rb.Width), Height: pr.Fl(rb.Height),
		TopLeft:     [2]pr.Fl{pr.Fl(rb.TopLeft[0]), pr.Fl(rb.TopLeft[1])},
		TopRight:    [2]pr.Fl{pr.Fl(rb.TopRight[0]), pr.Fl(rb.TopRight[1])},
		BottomRight: [2]pr.Fl{pr.Fl(rb.BottomRight[0]), pr.Fl(rb.BottomRight[1])},
		BottomLeft:  [2]pr.Fl{pr.Fl(rb.BottomLeft[0]), pr.Fl(rb.BottomLeft[1])},
	}
}

// drawBoxShadow resolves the CSS box-shadow list and delegates the actual
// geometry/rasterization of each shadow to the backend (which owns offset,
// spread, blur and inset/outset clipping). Outset shadows apply to the border
// box, inset shadows to the padding box.
func (ctx drawContext) drawBoxShadow(box_ Box, insetOnly bool) {
	box := box_.Box()
	shadows := box.Style.GetBoxShadow()
	if len(shadows) == 0 {
		return
	}
	if box.Style.GetVisibility() != "visible" {
		return
	}

	borderBox := toBackendRoundedBox(box.RoundedBorderBox())
	paddingBox := toBackendRoundedBox(box.RoundedPaddingBox())

	// Shadows are painted in reverse order (first declared = topmost).
	for i := len(shadows) - 1; i >= 0; i-- {
		s := shadows[i]
		if s.Inset != insetOnly {
			continue
		}
		color := parser.RGBA(s.Color.RGBA)
		if color.A == 0 {
			continue
		}

		ox, oy := pr.Fl(s.OffsetX.Value), pr.Fl(s.OffsetY.Value)
		blur := pr.Fl(s.Blur.Value)
		spread := pr.Fl(s.Spread.Value)

		shape := borderBox
		if s.Inset {
			shape = paddingBox
		}
		ctx.dst.DrawBoxShadow(shape, ox, oy, blur, spread, color, s.Inset)
	}
}
