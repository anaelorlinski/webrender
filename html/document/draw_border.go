package document

import (
	"math"

	"github.com/benoitkugler/webrender/backend"
	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/layout"
	"github.com/benoitkugler/webrender/html/tree"
	"github.com/benoitkugler/webrender/images"
	"github.com/benoitkugler/webrender/matrix"
	"github.com/benoitkugler/webrender/utils"
)

// Set “box“ mask border as alpha state on “stream“.
func (ctx drawContext) setMaskBorder(box_ Box) {
	box := box_.Box()
	style := box.Style
	if style.GetMaskBorderSource() == (pr.NoneImage{}) || box.MaskBorderImage == nil {
		return
	}
	rbb := box.RoundedBorderBox()
	mat := matrix.Translation(fl(rbb.X), fl(rbb.Y))
	mat.RightMultBy(ctx.dst.State().GetTransform())
	current := ctx.dst // save the current output ....
	ctx.dst = ctx.dst.NewGroup(fl(rbb.X), fl(rbb.Y), fl(rbb.Width), fl(rbb.Height))
	ctx.drawBorderImage(box, true)
	current.State().SetAlphaMask(ctx.dst)
	ctx.dst = current // ... and restore it
}

// Draw column rules.
func (ctx drawContext) drawColumnRules(box *bo.BoxFields) {
	ruleWidth := box.Style.GetColumnRuleWidth().Value
	ruleStyle := box.Style.GetColumnRuleStyle()
	columnGap := box.Style.GetColumnGap()
	var gap pr.Float
	if columnGap.S == "normal" {
		gap = box.Style.GetFontSize().Value // normal equals 1em
	} else {
		gap = pr.ResolvePercentage(columnGap, box.Width.V()).V()
	}

	borderWidths := pr.Rectangle{0, 0, 0, ruleWidth}
	skipNext := true
	for _, child := range box.Children {
		if child.Box().Style.GetColumnSpan() == "all" {
			skipNext = true
			continue
		} else if skipNext {
			skipNext = false
			continue
		}
		ctx.dst.OnNewStack(func() {
			positionX := child.Box().PositionX - (ruleWidth+gap)/2
			borderBox := pr.Rectangle{positionX, child.Box().PositionY, ruleWidth, child.Box().Height.V()}
			clipBorderSegment(ctx.dst, ruleStyle, fl(ruleWidth), left, borderBox, &borderWidths, nil)
			color := styledColor(ruleStyle, tree.ResolveColor(box.Style, pr.PColumnRuleColor).RGBA, left)
			ctx.drawRectBorder(borderBox, borderWidths, ruleStyle, color)
		})
	}
}

// Draw the box border and column rules
func (ctx drawContext) drawBorder(box_ Box) {
	// We need a plan to draw beautiful borders, and that's difficult, no need
	// to lie. Let's try to find the cases that we can handle in a smart way.
	box := box_.Box()

	// The box is hidden, easy.
	if box.Style.GetVisibility() != "visible" {
		return
	}

	// Draw column rules.
	columns := bo.BlockContainerT.IsInstance(box_) && (box.Style.GetColumnWidth().S != "auto" || box.Style.GetColumnCount().String != "auto")
	if columns && !box.Style.GetColumnRuleWidth().IsNone() {
		// with stream.artifact():
		ctx.drawColumnRules(box)
	}

	// If there's a border image, that takes precedence.
	if box.Style.GetBorderImageSource() != (pr.NoneImage{}) && box.BorderImage != nil {
		ctx.drawBorderImage(box, false)
		return
	}

	widths := pr.Rectangle{box.BorderTopWidth.V(), box.BorderRightWidth.V(), box.BorderBottomWidth.V(), box.BorderLeftWidth.V()}
	if widths.IsNone() {
		// No border, return early.
		return
	}
	var (
		colors    [4]Color
		colorsSet = map[Color]bool{}
		styles    [4]pr.String
		stylesSet = utils.NewSet()
	)
	for i, side := range sides {
		colors[i] = tree.ResolveColor(box.Style, pr.PBorderBottomColor+side*5).RGBA
		colorsSet[colors[i]] = true
		if colors[i].A != 0 {
			styles[i] = box.Style.Get((pr.PBorderBottomStyle + side*5).Key()).(pr.String)
		}
		stylesSet.Add(string(styles[i]))
	}

	simpleStyle := len(stylesSet) == 1 && (stylesSet.Has("solid") || stylesSet.Has("double")) // one style, simple lines
	singleColor := len(colorsSet) == 1                                                        // one color
	fourSides := widths[0] != 0 && widths[1] != 0 && widths[2] != 0 && widths[3] != 0         // no 0-width border, to avoid PDF artifacts
	if simpleStyle && singleColor && fourSides {
		// Simple case, we only draw rounded rectangles.
		ctx.drawRoundedBorder(box, styles[0], [2]Color{colors[0]})
		return
	}

	// We're not smart enough to find a good way to draw the borders, we must
	// draw them side by side. Order is not specified, but this one seems to be
	// close to what other browsers do.
	for _, i := range [...]uint8{2, 3, 1, 0} {
		side, width, color, style := sides[i], widths[i], colors[i], styles[i]
		if width == 0 || color.IsNone() {
			continue
		}
		ctx.dst.OnNewStack(func() {
			rb := box.RoundedBorderBox()
			roundedBox := pr.Rectangle{rb.X, rb.Y, rb.Width, rb.Height}
			radii := [4]bo.Point{rb.TopLeft, rb.TopRight, rb.BottomRight, rb.BottomLeft}
			clipBorderSegment(ctx.dst, style, fl(width), side,
				roundedBox, &widths, &radii)
			ctx.drawRoundedBorder(box, style, styledColor(style, color, side))
		})
	}
}

// Draw [box] border image on stream, shared by border-image-* and mask-border-*.
func (ctx drawContext) drawBorderImage(box *bo.BoxFields, forMask bool) {
	var (
		slice, outsets, widths pr.Values
		repeats                pr.Strings
		image                  images.Image
	)
	if forMask {
		slice, repeats, outsets, widths = box.Style.GetMaskBorderSlice(), box.Style.GetMaskBorderRepeat(), box.Style.GetMaskBorderOutset(), box.Style.GetMaskBorderWidth()
		image = box.MaskBorderImage
	} else {
		slice, repeats, outsets, widths = box.Style.GetBorderImageSlice(), box.Style.GetBorderImageRepeat(), box.Style.GetBorderImageOutset(), box.Style.GetBorderImageWidth()
		image = box.BorderImage
	}

	// See https://drafts.csswg.org/css-backgrounds-3/#border-images
	width, height, ratio := image.GetIntrinsicSize(
		box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
	intrinsicWidth_, intrinsicHeight_ := layout.DefaultImageSizing(width, height, ratio, nil, nil,
		box.BorderWidth(), box.BorderHeight())
	intrinsicWidth, intrinsicHeight := fl(intrinsicWidth_), fl(intrinsicHeight_)

	imageSlice := slice[:4]
	shouldFill := slice[4]

	computeSliceDimension := func(dimension pr.DimOrS, intrinsic pr.Float) fl {
		if dimension.Unit == pr.Scalar {
			return fl(min(dimension.Value, intrinsic))
		} else {
			// assert dimension.unit == "%"
			return fl(min(100, dimension.Value) / 100 * intrinsic)
		}
	}

	sliceTop := computeSliceDimension(imageSlice[0], intrinsicHeight_)
	sliceRight := computeSliceDimension(imageSlice[1], intrinsicWidth_)
	sliceBottom := computeSliceDimension(imageSlice[2], intrinsicHeight_)
	sliceLeft := computeSliceDimension(imageSlice[3], intrinsicWidth_)

	bBox := box.RoundedBorderBox()
	x, y, w, h := fl(bBox.X), fl(bBox.Y), fl(bBox.Width), fl(bBox.Height)
	paddingBox := box.RoundedPaddingBox()
	borderLeft := fl(paddingBox.X) - x
	borderTop := fl(paddingBox.Y) - y
	borderRight := w - fl(paddingBox.Width) - borderLeft
	borderBottom := h - fl(paddingBox.Height) - borderTop

	computeOutsetDimension := func(dimension pr.DimOrS, fromBorder fl) fl {
		if dimension.Unit == pr.Scalar {
			return fl(dimension.Value) * fromBorder
		} else {
			// assert dimension.unit == "px"
			return fl(dimension.Value)
		}
	}

	outsetTop := computeOutsetDimension(outsets[0], borderTop)
	outsetRight := computeOutsetDimension(outsets[1], borderRight)
	outsetBottom := computeOutsetDimension(outsets[2], borderBottom)
	outsetLeft := computeOutsetDimension(outsets[3], borderLeft)

	x -= outsetLeft
	y -= outsetTop
	w += outsetLeft + outsetRight
	h += outsetTop + outsetBottom

	computeWidthAdjustment := func(dimension pr.DimOrS, original, intrinsic, areaDimension fl) fl {
		if dimension.S == "auto" {
			return fl(intrinsic)
		} else if dimension.Unit == pr.Scalar {
			return fl(dimension.Value) * original
		} else if dimension.Unit == pr.Perc {
			return fl(dimension.Value) / 100 * areaDimension
		} else {
			// assert dimension.unit == "px"
			return fl(dimension.Value)
		}
	}

	// We make adjustments to the border* variables after handling outsets
	// because numerical outsets are relative to border-width, not
	// border-image-width. Also, the border image area that is used
	// for percentage-based border-image-width values includes any expanded
	// area due to border-image-outset.
	borderTop = computeWidthAdjustment(widths[0], borderTop, sliceTop, h)
	borderRight = computeWidthAdjustment(widths[1], borderRight, sliceRight, w)
	borderBottom = computeWidthAdjustment(widths[2], borderBottom, sliceBottom, h)
	borderLeft = computeWidthAdjustment(widths[3], borderLeft, sliceLeft, w)

	// repeatX="stretch", repeatY="stretch",
	// scaleX=None, scaleY=None
	drawBorderImage := func(x, y, width, height, sliceX, sliceY,
		sliceWidth, sliceHeight fl,
		repeatX, repeatY string,
		scaleX, scaleY fl,
	) (_, _ fl) {
		var (
			nRepeatsX, nRepeatsY int
			extraDx, extraDy     fl
		)
		if intrinsicWidth == 0 || width == 0 || sliceWidth == 0 {
			scaleX = 0
		} else {
			extraDx = 0
			if scaleX == 0 {
				scaleX = 1
				if height != 0 && sliceHeight != 0 {
					scaleX = (height / sliceHeight)
				}
			}
			switch repeatX {
			case "repeat":
				nRepeatsX = int(utils.Ceil(width / sliceWidth / scaleX))
			case "space":
				nRepeatsX = int(utils.Floor(width / sliceWidth / scaleX))
				// Space is before the first repeat && after the last,
				// so there"s one more space than repeat.
				extraDx = ((width/scaleX - fl(nRepeatsX)*sliceWidth) / (fl(nRepeatsX) + 1))
			case "round":
				nRepeatsX = utils.MaxInt(1, int(utils.Round(width/sliceWidth/scaleX)))
				scaleX = width / (fl(nRepeatsX) * sliceWidth)
			default:
				nRepeatsX = 1
				scaleX = width / sliceWidth
			}
		}

		if intrinsicHeight == 0 || height == 0 || sliceHeight == 0 {
			scaleY = 0
		} else {
			extraDy = 0
			if scaleY == 0 {
				scaleY = 1
				if width != 0 && sliceWidth != 0 {
					scaleY = (width / sliceWidth)
				}
			}

			switch repeatY {
			case "repeat":
				nRepeatsY = int(utils.Ceil(height / sliceHeight / scaleY))
			case "space":
				nRepeatsY = int(utils.Floor(height / sliceHeight / scaleY))
				// Space is before the first repeat and after the last,
				// so there"s one more space than repeat.
				extraDy = ((height/scaleY - fl(nRepeatsY)*sliceHeight) / (fl(nRepeatsY) + 1))
			case "round":
				nRepeatsY = utils.MaxInt(1, int(utils.Round(height/sliceHeight/scaleY)))
				scaleY = height / (fl(nRepeatsY) * sliceHeight)
			default:
				nRepeatsY = 1
				scaleY = height / sliceHeight
			}
		}

		if scaleX == 0 || scaleY == 0 {
			return scaleX, scaleY
		}

		renderedWidth := intrinsicWidth * scaleX
		renderedHeight := intrinsicHeight * scaleY
		offsetX := renderedWidth * sliceX / intrinsicWidth
		offsetY := renderedHeight * sliceY / intrinsicHeight

		ctx.dst.OnNewStack(func() {
			ctx.dst.Rectangle(x, y, width, height)
			ctx.dst.State().Clip(false)
			ctx.dst.State().Transform(matrix.Translation(x-offsetX+extraDx, y-offsetY+extraDy))
			ctx.dst.State().Transform(matrix.Scaling(scaleX, scaleY))
			for i := 0; i < nRepeatsX; i++ {
				for j := 0; j < nRepeatsY; j++ {
					ctx.dst.OnNewStack(func() {
						translateX := fl(i) * (sliceWidth + extraDx)
						translateY := fl(j) * (sliceHeight + extraDy)
						ctx.dst.State().Transform(matrix.Translation(translateX, translateY))
						ctx.dst.Rectangle(offsetX/scaleX, offsetY/scaleY, sliceWidth, sliceHeight)
						ctx.dst.State().Clip(false)
						image.Draw(ctx.dst, ctx, intrinsicWidth, intrinsicHeight,
							string(box.Style.GetImageRendering()))
					})
				}
			}
		})

		return scaleX, scaleY
	}

	// Top left.
	scaleLeft, scaleTop := drawBorderImage(x, y, borderLeft, borderTop, 0, 0, sliceLeft, sliceTop, "", "", 0, 0)
	// Top right.
	drawBorderImage(x+w-borderRight, y, borderRight, borderTop, intrinsicWidth-sliceRight, 0, sliceRight, sliceTop, "", "", 0, 0)
	// Bottom right.
	scaleRight, scaleBottom := drawBorderImage(x+w-borderRight, y+h-borderBottom, borderRight, borderBottom,
		intrinsicWidth-sliceRight, intrinsicHeight-sliceBottom, sliceRight, sliceBottom, "", "", 0, 0)
	// Bottom left.
	drawBorderImage(x, y+h-borderBottom, borderLeft, borderBottom,
		0, intrinsicHeight-sliceBottom, sliceLeft, sliceBottom, "", "", 0, 0)
	if sliceLeft+sliceRight < intrinsicWidth {
		// Top middle.
		drawBorderImage(
			x+borderLeft, y, w-borderLeft-borderRight, borderTop,
			sliceLeft, 0, intrinsicWidth-sliceLeft-sliceRight,
			sliceTop, repeats[0], "", 0, 0)
		// Bottom middle.
		drawBorderImage(
			x+borderLeft, y+h-borderBottom,
			w-borderLeft-borderRight, borderBottom,
			sliceLeft, intrinsicHeight-sliceBottom,
			intrinsicWidth-sliceLeft-sliceRight, sliceBottom,
			repeats[0], "", 0, 0)
	}
	if sliceTop+sliceBottom < intrinsicHeight {
		// Right middle.
		drawBorderImage(x+w-borderRight, y+borderTop, borderRight, h-borderTop-borderBottom,
			intrinsicWidth-sliceRight, sliceTop, sliceRight, intrinsicHeight-sliceTop-sliceBottom,
			"", repeats[1], 0, 0)
		// Left middle.
		drawBorderImage(x, y+borderTop, borderLeft, h-borderTop-borderBottom, 0, sliceTop, sliceLeft,
			intrinsicHeight-sliceTop-sliceBottom,
			"", repeats[1], 0, 0)
	}
	if !shouldFill.IsNone() && sliceLeft+sliceRight < intrinsicWidth && sliceTop+sliceBottom < intrinsicHeight {
		// Fill middle.
		if scaleLeft == 0 {
			scaleLeft = scaleRight
		}
		if sliceTop == 0 {
			sliceTop = scaleBottom
		}
		drawBorderImage(x+borderLeft, y+borderTop, w-borderLeft-borderRight, h-borderTop-borderBottom, sliceLeft, sliceTop,
			intrinsicWidth-sliceLeft-sliceRight, intrinsicHeight-sliceTop-sliceBottom,
			repeats[0], repeats[1], scaleLeft, scaleTop)
	}
}

// Clip one segment of box border (border_widths=nil, radii=nil).
// The strategy is to remove the zones not needed because of the style or the
// side before painting.
func clipBorderSegment(context backend.Canvas, style pr.String, width fl, side pr.KnownProp,
	borderBox pr.Rectangle, borderWidths *pr.Rectangle, radii *[4]bo.Point,
) {
	bbx, bby, bbw, bbh := borderBox.Unpack()
	var tlh, tlv, trh, trv, brh, brv, blh, blv fl
	if radii != nil {
		tlh, tlv, trh, trv, brh, brv, blh, blv = fl((*radii)[0][0]), fl((*radii)[0][1]), fl((*radii)[1][0]), fl((*radii)[1][1]), fl((*radii)[2][0]), fl((*radii)[2][1]), fl((*radii)[3][0]), fl((*radii)[3][1])
	}
	bt, br, bb, bl := width, width, width, width
	if borderWidths != nil {
		bt, br, bb, bl = borderWidths.Unpack()
	}

	// Get the point use for border transition.
	// The extra boolean returned is ``true`` if the point is in the padding
	// box (ie. the padding box is rounded).
	// This point is not specified. We must be sure to be inside the rounded
	// padding box, and in the zone defined in the "transition zone" allowed
	// by the specification. We chose the corner of the transition zone. It"s
	// easy to get and gives quite good results, but it seems to be different
	// from what other browsers do.
	transitionPoint := func(x1, y1, x2, y2 fl) (fl, fl, bool) {
		if math.Abs(float64(x1)) > math.Abs(float64(x2)) && math.Abs(float64(y1)) > math.Abs(float64(y2)) {
			return x1, y1, true
		}
		return x2, y2, false
	}

	// Return the length of the half of one ellipsis corner.

	// Inspired by [Ramanujan, S., "Modular Equations and Approximations to
	// pi" Quart. J. Pure. Appl. Math., vol. 45 (1913-1914), pp. 350-372],
	// wonderfully explained by Dr Rob.

	// http://mathforum.org/dr.math/faq/formulas/
	cornerHalfLength := func(a, b fl) fl {
		x := (a - b) / (a + b)
		return pi / 8 * (a + b) * (1 + 3*x*x/(10+fl(math.Sqrt(float64(4-3*x*x)))))
	}
	var (
		px1, px2, py1, py2, way, angle, mainOffset fl
		rounded1, rounded2                         bool
	)

	switch side {
	case top:
		px1, py1, rounded1 = transitionPoint(tlh, tlv, bl, bt)
		px2, py2, rounded2 = transitionPoint(-trh, trv, -br, bt)
		width = bt
		way = 1
		angle = 1
		mainOffset = bby
	case right:
		px1, py1, rounded1 = transitionPoint(-trh, trv, -br, bt)
		px2, py2, rounded2 = transitionPoint(-brh, -brv, -br, -bb)
		width = br
		way = 1
		angle = 2
		mainOffset = bbx + bbw
	case bottom:
		px1, py1, rounded1 = transitionPoint(blh, -blv, bl, -bb)
		px2, py2, rounded2 = transitionPoint(-brh, -brv, -br, -bb)
		width = bb
		way = -1
		angle = 3
		mainOffset = bby + bbh
	case left:
		px1, py1, rounded1 = transitionPoint(tlh, tlv, bl, bt)
		px2, py2, rounded2 = transitionPoint(blh, -blv, bl, -bb)
		width = bl
		way = -1
		angle = 4
		mainOffset = bbx
	}

	var a1, b1, a2, b2, lineLength, length fl
	if side == top || side == bottom {
		a1, b1 = px1-bl/2, way*py1-width/2
		a2, b2 = -px2-br/2, way*py2-width/2
		lineLength = bbw - px1 + px2
		length = bbw
		context.MoveTo(bbx+bbw, mainOffset)
		context.LineTo(bbx, mainOffset)
		context.LineTo(bbx+px1, mainOffset+py1)
		context.LineTo(bbx+bbw+px2, mainOffset+py2)
	} else if side == left || side == right {
		a1, b1 = -way*px1-width/2, py1-bt/2
		a2, b2 = -way*px2-width/2, -py2-bb/2
		lineLength = bbh - py1 + py2
		length = bbh
		context.MoveTo(mainOffset, bby+bbh)
		context.LineTo(mainOffset, bby)
		context.LineTo(mainOffset+px1, bby+py1)
		context.LineTo(mainOffset+px2, bby+bbh+py2)
	}

	if style == "dotted" || style == "dashed" {
		dash := 3 * width
		if style == "dotted" {
			dash = width
		}
		if rounded1 || rounded2 {
			// At least one of the two corners is rounded
			chl1 := cornerHalfLength(a1, b1)
			chl2 := cornerHalfLength(a2, b2)
			length = lineLength + chl1 + chl2
			dashLength := fl(math.Round(float64(length / dash)))
			if rounded1 && rounded2 {
				// 2x dashes
				dash = length / (dashLength + utils.FloatModulo(dashLength, 2))
			} else {
				// 2x - 1/2 dashes
				dash = length / (dashLength + utils.FloatModulo(dashLength, 2) - 0.5)
			}
			dashes1 := int(utils.Ceil((chl1 - dash/2) / dash))
			dashes2 := int(utils.Ceil((chl2 - dash/2) / dash))
			line := int(utils.Floor(lineLength / dash))

			drawDots := func(dashes, line int, way, x, y, px, py, chl fl) (int, fl) {
				if dashes == 0 {
					return line + 1, 0
				}
				var (
					hasBroken              bool
					offset, angle1, angle2 fl
				)
				for i_ := 0; i_ < dashes; i_ += 2 {
					i := fl(i_) + 0.5 // half dash
					angle1 = ((2*angle - way) + i*way*dash/chl) / 4 * pi

					fn := utils.MaxF
					if way > 0 {
						fn = utils.MinF
					}
					angle2 = fn(
						((2*angle-way)+(i+1)*way*dash/chl)/4*pi,
						angle*pi/2,
					)
					if side == top || side == bottom {
						context.MoveTo(x+px, mainOffset+py)
						context.LineTo(x+px-way*px*1/fl(math.Tan(float64(angle2))), mainOffset)
						context.LineTo(x+px-way*px*1/fl(math.Tan(float64(angle1))), mainOffset)
					} else if side == left || side == right {
						context.MoveTo(mainOffset+px, y+py)
						context.LineTo(mainOffset, y+py+way*py*fl(math.Tan(float64(angle2))))
						context.LineTo(mainOffset, y+py+way*py*fl(math.Tan(float64(angle1))))
					}
					if angle2 == angle*pi/2 {
						offset = (angle1 - angle2) / ((((2*angle - way) + (i+1)*way*dash/chl) /
							4 * pi) - angle1)
						line += 1
						hasBroken = true
						break
					}
				}
				if !hasBroken {
					offset = 1 - (angle*pi/2-angle2)/(angle2-angle1)
				}
				return line, offset
			}
			var offset fl
			line, offset = drawDots(dashes1, line, way, bbx, bby, px1, py1, chl1)
			line, _ = drawDots(dashes2, line, -way, bbx+bbw, bby+bbh, px2, py2, chl2)

			if lineLength > 1e-6 {
				for i_ := 0; i_ < line; i_ += 2 {
					i := fl(i_) + offset
					var x1, x2, y1, y2 fl
					if side == top || side == bottom {
						x1 = utils.MaxF(bbx+px1+i*dash, bbx+px1)
						x2 = utils.MinF(bbx+px1+(i+1)*dash, bbx+bbw+px2)
						y1 = mainOffset
						if way < 0 {
							y1 -= width
						}
						y2 = y1 + width
					} else if side == left || side == right {
						y1 = utils.MaxF(bby+py1+i*dash, bby+py1)
						y2 = utils.MinF(bby+py1+(i+1)*dash, bby+bbh+py2)
						x1 = mainOffset
						if way > 0 {
							x1 -= width
						}
						x2 = x1 + width
					}
					context.Rectangle(x1, y1, x2-x1, y2-y1)
				}
			}
		} else {
			// 2x + 1 dashes
			context.State().Clip(true)
			ld := fl(math.Round(float64(length / dash)))
			denom := ld - utils.FloatModulo(ld+1, 2)
			dash = length
			if denom != 0 {
				dash /= denom
			}
			maxI := int(math.Round(float64(length / dash)))
			for i_ := 0; i_ < maxI; i_ += 2 {
				i := fl(i_)
				switch side {
				case top:
					context.Rectangle(bbx+i*dash, bby, dash, width)
				case right:
					context.Rectangle(bbx+bbw-width, bby+i*dash, width, dash)
				case bottom:
					context.Rectangle(bbx+i*dash, bby+bbh-width, dash, width)
				case left:
					context.Rectangle(bbx, bby+i*dash, width, dash)
				}
			}
		}
	}
	context.State().Clip(true)
}

func (ctx drawContext) drawRoundedBorder(box *bo.BoxFields, style pr.String, colors [2]Color) {
	if style == "ridge" || style == "groove" {
		ctx.dst.State().SetColorRgba(colors[0], false)
		roundedBox(ctx.dst, box.RoundedPaddingBox())
		roundedBox(ctx.dst, box.RoundedBoxRatio(1./2))
		ctx.dst.Paint(backend.FillEvenOdd)
		ctx.dst.State().SetColorRgba(colors[1], false)
		roundedBox(ctx.dst, box.RoundedBoxRatio(1./2))
		roundedBox(ctx.dst, box.RoundedBorderBox())
		ctx.dst.Paint(backend.FillEvenOdd)
		return
	}

	ctx.dst.State().SetColorRgba(colors[0], false)
	roundedBox(ctx.dst, box.RoundedPaddingBox())
	if style == "double" {
		roundedBox(ctx.dst, box.RoundedBoxRatio(1./3))
		roundedBox(ctx.dst, box.RoundedBoxRatio(2./3))
	}
	roundedBox(ctx.dst, box.RoundedBorderBox())
	ctx.dst.Paint(backend.FillEvenOdd)
}

func (ctx drawContext) drawRectBorder(box, widths pr.Rectangle, style pr.String, color [2]Color) {
	bbx, bby, bbw, bbh := box.Unpack()
	bt, br, bb, bl := widths.Unpack()
	if style == "ridge" || style == "groove" {
		ctx.dst.State().SetColorRgba(color[0], false)
		ctx.dst.Rectangle(box.Unpack())
		ctx.dst.Rectangle(bbx+bl/2, bby+bt/2, bbw-(bl+br)/2, bbh-(bt+bb)/2)
		ctx.dst.Paint(backend.FillEvenOdd)
		ctx.dst.Rectangle(bbx+bl/2, bby+bt/2, bbw-(bl+br)/2, bbh-(bt+bb)/2)
		ctx.dst.Rectangle(bbx+bl, bby+bt, bbw-bl-br, bbh-bt-bb)
		ctx.dst.State().SetColorRgba(color[1], false)
		ctx.dst.Paint(backend.FillEvenOdd)
		return
	}
	ctx.dst.State().SetColorRgba(color[0], false)
	ctx.dst.Rectangle(box.Unpack())
	if style == "double" {
		ctx.dst.Rectangle(bbx+bl/3, bby+bt/3, bbw-(bl+br)/3, bbh-(bt+bb)/3)
		ctx.dst.Rectangle(bbx+bl*2/3, bby+bt*2/3, bbw-(bl+br)*2/3, bbh-(bt+bb)*2/3)
	}
	ctx.dst.Rectangle(bbx+bl, bby+bt, bbw-bl-br, bbh-bt-bb)
	ctx.dst.Paint(backend.FillEvenOdd)
}

// Only works for vertical or horizontal lines : x1 == x2 or y1 == y2
func (ctx drawContext) drawLine(x1, y1, x2, y2, thickness pr.Fl, style pr.String, colors [2]Color, offset fl) {
	ctx.dst.OnNewStack(func() {
		if !(style == "ridge" || style == "groove") {
			ctx.dst.State().SetColorRgba(colors[0], true)
		}

		if style == "dashed" {
			ctx.dst.State().SetDash([]fl{5 * thickness}, offset)
		} else if style == "dotted" {
			ctx.dst.State().SetDash([]fl{thickness}, offset)
		}

		if style == "double" {
			ctx.dst.State().SetLineWidth(thickness / 3)
			if x1 == x2 {
				ctx.dst.MoveTo(x1-thickness/3, y1)
				ctx.dst.LineTo(x2-thickness/3, y2)
				ctx.dst.MoveTo(x1+thickness/3, y1)
				ctx.dst.LineTo(x2+thickness/3, y2)
			} else if y1 == y2 {
				ctx.dst.MoveTo(x1, y1-thickness/3)
				ctx.dst.LineTo(x2, y2-thickness/3)
				ctx.dst.MoveTo(x1, y1+thickness/3)
				ctx.dst.LineTo(x2, y2+thickness/3)
			}
		} else if style == "ridge" || style == "groove" {
			ctx.dst.State().SetLineWidth(thickness / 2)
			ctx.dst.State().SetColorRgba(colors[0], true)
			if x1 == x2 {
				ctx.dst.MoveTo(x1+thickness/4, y1)
				ctx.dst.LineTo(x2+thickness/4, y2)
			} else if y1 == y2 {
				ctx.dst.MoveTo(x1, y1+thickness/4)
				ctx.dst.LineTo(x2, y2+thickness/4)
			}
			ctx.dst.Paint(backend.Stroke)
			ctx.dst.State().SetColorRgba(colors[1], true)
			if x1 == x2 {
				ctx.dst.MoveTo(x1-thickness/4, y1)
				ctx.dst.LineTo(x2-thickness/4, y2)
			} else if y1 == y2 {
				ctx.dst.MoveTo(x1, y1-thickness/4)
				ctx.dst.LineTo(x2, y2-thickness/4)
			}
		} else if style == "wavy" {
			// assert y1 == y2  # Only allowed for text decoration
			var up pr.Fl = 1
			radius := 0.75 * thickness

			ctx.dst.Rectangle(x1, y1-2*radius, x2-x1, 4*radius)
			ctx.dst.State().Clip(false)

			x := x1 - offset
			ctx.dst.MoveTo(x, y1)

			for x < x2 {
				ctx.dst.CubicTo(x+radius/2, y1+up*radius,
					x+3*radius/2, y1+up*radius,
					x+2*radius, y1)
				x += 2 * radius
				up *= -1
			}
		} else {
			ctx.dst.State().SetLineWidth(thickness)
			ctx.dst.MoveTo(x1, y1)
			ctx.dst.LineTo(x2, y2)
		}

		ctx.dst.Paint(backend.Stroke)
	})
}

func (ctx drawContext) drawOutline(box_ Box) {
	box := box_.Box()
	width_ := box.Style.GetOutlineWidth()
	color := tree.ResolveColor(box.Style, pr.POutlineColor).RGBA
	style := box.Style.GetOutlineStyle()
	if box.Style.GetVisibility() == "visible" && width_.Value != 0 && color.A != 0 {
		width := width_.Value
		outlineBox := pr.Rectangle{
			box.BorderBoxX() - width, box.BorderBoxY() - width,
			box.BorderWidth() + 2*width, box.BorderHeight() + 2*width,
		}
		for _, side := range sides {
			ctx.dst.OnNewStack(func() {
				clipBorderSegment(ctx.dst, style, fl(width), side, outlineBox, nil, nil)
				ctx.drawRectBorder(outlineBox, pr.Rectangle{width, width, width, width},
					style, styledColor(style, color, side))
			})
		}
	}

	for _, child := range box.Children {
		if child.Type().IsClassical() {
			ctx.drawOutline(child)
		}
	}
}

// Draw the path of the border radius box.
// “widths“ is a tuple of the inner widths (top, right, bottom, left) from
// the border box. Radii are adjusted from these values. Default is (0, 0, 0,
// 0).
func roundedBox(context backend.Canvas, radii bo.RoundedBox) {
	x, y, w, h, tl, tr, br, bl := pr.Fl(radii.X), pr.Fl(radii.Y), pr.Fl(radii.Width), pr.Fl(radii.Height), radii.TopLeft, radii.TopRight, radii.BottomRight, radii.BottomLeft
	if (tl[0] == 0 || tl[1] == 0) && (tr[0] == 0 || tr[1] == 0) &&
		(br[0] == 0 || br[1] == 0) && (bl[0] == 0 || bl[1] == 0) {
		// No radius, draw a rectangle
		context.Rectangle(x, y, w, h)
		return
	}

	var r pr.Fl = 0.45

	context.MoveTo(x+pr.Fl(tl[0]), y)
	context.LineTo(x+w-pr.Fl(tr[0]), y)
	context.CubicTo(
		x+w-pr.Fl(tr[0])*r, y, x+w, y+pr.Fl(tr[1])*r, x+w, y+pr.Fl(tr[1]))
	context.LineTo(x+w, y+h-pr.Fl(br[1]))
	context.CubicTo(
		x+w, y+h-pr.Fl(br[1])*r, x+w-pr.Fl(br[0])*r, y+h, x+w-pr.Fl(br[0]),
		y+h)
	context.LineTo(x+pr.Fl(bl[0]), y+h)
	context.CubicTo(
		x+pr.Fl(bl[0])*r, y+h, x, y+h-pr.Fl(bl[1])*r, x, y+h-pr.Fl(bl[1]))
	context.LineTo(x, y+pr.Fl(tl[1]))
	context.CubicTo(
		x, y+pr.Fl(tl[1])*r, x+pr.Fl(tl[0])*r, y, x+pr.Fl(tl[0]), y)
}
