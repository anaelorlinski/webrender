package layout

import (
	"fmt"

	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/utils/testutils/tracer"
)

// Resolve percentages into fixed values.

// Compute a used length value from a computed length value.
//
// the return value should be set on the box
func resolveOnePercentage(value pr.DimOrS, propertyName pr.KnownProp, referTo pr.Float) pr.MaybeFloat {
	// box attributes are used values
	percent := pr.ResolvePercentage(value, referTo)

	if (propertyName == pr.PMinWidth || propertyName == pr.PMinHeight) && percent == pr.AutoF {
		percent = pr.Float(0)
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("resolveOnePercentage %s: %v %s -> %s", propertyName,
			tracer.FormatMaybeFloat(value.ToMaybeFloat()), tracer.FormatMaybeFloat(referTo),
			tracer.FormatMaybeFloat(percent)))
	}

	return percent
}

func resolvePositionPercentages(box *bo.BoxFields, containingBlock bo.Point) {
	cbWidth, cbHeight := containingBlock[0], containingBlock[1]
	box.Left = resolveOnePercentage(box.Style.GetLeft(), pr.PLeft, cbWidth)
	box.Right = resolveOnePercentage(box.Style.GetRight(), pr.PRight, cbWidth)
	box.Top = resolveOnePercentage(box.Style.GetTop(), pr.PTop, cbHeight)
	box.Bottom = resolveOnePercentage(box.Style.GetBottom(), pr.PBottom, cbHeight)
}

func resolvePercentagesBox(box Box, containingBlock containingBlock) {
	w, h := containingBlock.ContainingBlock()
	resolvePercentages(box, bo.MaybePoint{w, h})
}

// Set used values as attributes of the box object.
func resolvePercentages(box_ Box, containingBlock bo.MaybePoint) {
	cbWidth, cbHeight := containingBlock[0], containingBlock[1]

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("resolvePercentages for <%s> (%s): %s x %s", box_.Box().ElementTag(), box_.Type(),
			tracer.FormatMaybeFloat(cbWidth), tracer.FormatMaybeFloat(cbHeight)))
	}

	maybeHeight := cbWidth
	if bo.PageT.IsInstance(box_) {
		maybeHeight = cbHeight
	}
	box := box_.Box()
	box.MarginLeft = resolveOnePercentage(box.Style.GetMarginLeft(), pr.PMarginLeft, cbWidth.V())
	box.MarginRight = resolveOnePercentage(box.Style.GetMarginRight(), pr.PMarginRight, cbWidth.V())
	box.MarginTop = resolveOnePercentage(box.Style.GetMarginTop(), pr.PMarginTop, maybeHeight.V())
	box.MarginBottom = resolveOnePercentage(box.Style.GetMarginBottom(), pr.PMarginBottom, maybeHeight.V())
	box.PaddingLeft = resolveOnePercentage(box.Style.GetPaddingLeft(), pr.PPaddingLeft, cbWidth.V())
	box.PaddingRight = resolveOnePercentage(box.Style.GetPaddingRight(), pr.PPaddingRight, cbWidth.V())
	box.PaddingTop = resolveOnePercentage(box.Style.GetPaddingTop(), pr.PPaddingTop, maybeHeight.V())
	box.PaddingBottom = resolveOnePercentage(box.Style.GetPaddingBottom(), pr.PPaddingBottom, maybeHeight.V())
	box.Width = resolveOnePercentage(box.Style.GetWidth(), pr.PWidth, cbWidth.V())
	box.MinWidth = resolveOnePercentage(box.Style.GetMinWidth(), pr.PMinWidth, cbWidth.V())
	box.MaxWidth = resolveOnePercentage(box.Style.GetMaxWidth(), pr.PMaxWidth, cbWidth.V())

	// XXX later: top, bottom, left && right on positioned elements

	if cbHeight == pr.AutoF {
		// Special handling when the height of the containing block
		// depends on its content.
		height := box.Style.GetHeight()
		if height.S == "auto" || height.Unit == pr.Perc {
			box.Height = pr.AutoF
		} else {
			if height.Unit != pr.Px {
				panic(fmt.Sprintf("expected percentage, got %d", height.Unit))
			}
			box.Height = height.Value
		}
		box.MinHeight = resolveOnePercentage(box.Style.GetMinHeight(), pr.PMinHeight, pr.Float(0))
		box.MaxHeight = resolveOnePercentage(box.Style.GetMaxHeight(), pr.PMaxHeight, pr.Inf)
	} else {
		box.Height = resolveOnePercentage(box.Style.GetHeight(), pr.PHeight, cbHeight.V())
		box.MinHeight = resolveOnePercentage(box.Style.GetMinHeight(), pr.PMinHeight, cbHeight.V())
		box.MaxHeight = resolveOnePercentage(box.Style.GetMaxHeight(), pr.PMaxHeight, cbHeight.V())
	}

	collapse := box.Style.GetBorderCollapse() == "collapse"
	// Used value == computed value
	if !collapse || box.BorderTopWidth == 0 {
		box.BorderTopWidth = box.Style.GetBorderTopWidth().Value
	}
	if !collapse || box.BorderRightWidth == 0 {
		box.BorderRightWidth = box.Style.GetBorderRightWidth().Value
	}
	if !collapse || box.BorderBottomWidth == 0 {
		box.BorderBottomWidth = box.Style.GetBorderBottomWidth().Value
	}
	if !collapse || box.BorderLeftWidth == 0 {
		box.BorderLeftWidth = box.Style.GetBorderLeftWidth().Value
	}

	// Shrink *content* widths and heights according to box-sizing
	adjustBoxSizing(box)

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("after resolvePercentages: %s %s", tracer.FormatMaybeFloat(box.Width), tracer.FormatMaybeFloat(box.Height)))
	}
}

func resoudRadius(box *bo.BoxFields, v pr.Point, side1, side2 bo.Side) bo.MaybePoint {
	// rx, ry = v
	if v[0] == pr.ZeroPixels || v[1] == pr.ZeroPixels { // Short track for common case
		return bo.MaybePoint{pr.Float(0), pr.Float(0)}
	}

	if box.RemoveDecorationSides[side1] || box.RemoveDecorationSides[side2] {
		return bo.MaybePoint{pr.Float(0), pr.Float(0)}
	}
	rx := pr.ResolvePercentage(v[0].ToValue(), box.BorderWidth())
	ry := pr.ResolvePercentage(v[1].ToValue(), box.BorderHeight())
	return bo.MaybePoint{rx, ry}
}

func resolveRadiiPercentages(box *bo.BoxFields) {
	box.BorderTopLeftRadius = resoudRadius(box, box.Style.GetBorderTopLeftRadius(), bo.STop, bo.SLeft).V()
	box.BorderTopRightRadius = resoudRadius(box, box.Style.GetBorderTopRightRadius(), bo.STop, bo.SRight).V()
	box.BorderBottomRightRadius = resoudRadius(box, box.Style.GetBorderBottomRightRadius(), bo.SBottom, bo.SRight).V()
	box.BorderBottomLeftRadius = resoudRadius(box, box.Style.GetBorderBottomLeftRadius(), bo.SBottom, bo.SLeft).V()
}

func adjustBoxSizing(box *bo.BoxFields) {
	// Thanks heavens and the spec: Our validator rejects negative values
	// for padding and border-width
	var horizontalDelta, verticalDelta pr.Float
	switch box.Style.GetBoxSizing() {
	case "border-box":
		horizontalDelta = box.PaddingLeft.V() + box.PaddingRight.V() + box.BorderLeftWidth.V() + box.BorderRightWidth.V()
		verticalDelta = box.PaddingTop.V() + box.PaddingBottom.V() + box.BorderTopWidth.V() + box.BorderBottomWidth.V()
	case "padding-box":
		horizontalDelta = box.PaddingLeft.V() + box.PaddingRight.V()
		verticalDelta = box.PaddingTop.V() + box.PaddingBottom.V()
	case "content-box":
		horizontalDelta = 0
		verticalDelta = 0
	default:
		panic(fmt.Sprintf("invalid box sizing %s", box.Style.GetBoxSizing()))
	}

	// Keep at least min* >= 0 to prevent funny output in case box.Width or
	// box.Height become negative.
	// Restricting max* seems reasonable, too.
	if horizontalDelta > 0 {
		if box.Width != pr.AutoF {
			box.Width = max(0, box.Width.V()-horizontalDelta)
		}
		box.MaxWidth = max(0, box.MaxWidth.V()-horizontalDelta)
		if box.MinWidth != pr.AutoF {
			box.MinWidth = max(0, box.MinWidth.V()-horizontalDelta)
		}
	}
	if verticalDelta > 0 {
		if box.Height != pr.AutoF {
			box.Height = max(0, box.Height.V()-verticalDelta)
		}
		box.MaxHeight = max(0, box.MaxHeight.V()-verticalDelta)
		if box.MinHeight != pr.AutoF {
			box.MinHeight = max(0, box.MinHeight.V()-verticalDelta)
		}
	}
}
