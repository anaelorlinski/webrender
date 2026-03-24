package document

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/template"

	"github.com/benoitkugler/webrender/backend"
	"github.com/benoitkugler/webrender/logger"
	"github.com/benoitkugler/webrender/matrix"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/text/hyphen"

	"github.com/benoitkugler/webrender/html/layout"

	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/images"
	"github.com/benoitkugler/webrender/utils"

	bo "github.com/benoitkugler/webrender/html/boxes"
)

// Take an "after layout" box tree and draw it onto a cairo context.

const (
	bottom pr.KnownProp = iota
	left
	right
	top
)

var sides = [4]pr.KnownProp{top, right, bottom, left}

const (
	pi = math.Pi

	headerSVG = `
	<svg height="{{ .Height }}" width="{{ .Width }}"
		 fill="transparent" stroke="black" stroke-width="1"
		 xmlns="http://www.w3.org/2000/svg"
		 xmlns:xlink="http://www.w3.org/1999/xlink">
  	`

	crop = `
  <!-- horizontal top left -->
  <path d="M0,{{ .Bleed.Top }} h{{ .HalfBleed.Left }}" />
  <!-- horizontal top right -->
  <path d="M0,{{ .Bleed.Top }} h{{ .HalfBleed.Right }}"
        transform="translate({{ .Width }},0) scale(-1,1)" />
  <!-- horizontal bottom right -->
  <path d="M0,{{ .Bleed.Bottom }} h{{ .HalfBleed.Right }}"
        transform="translate({{ .Width }},{{ .Height }}) scale(-1,-1)" />
  <!-- horizontal bottom left -->
  <path d="M0,{{ .Bleed.Bottom }} h{{ .HalfBleed.Left }}"
        transform="translate(0,{{ .Height }}) scale(1,-1)" />
  <!-- vertical top left -->
  <path d="M{{ .Bleed.Left }},0 v{{ .HalfBleed.Top }}" />
  <!-- vertical bottom right -->
  <path d="M{{ .Bleed.Right }},0 v{{ .HalfBleed.Bottom }}"
        transform="translate({{ .Width }},{{ .Height }}) scale(-1,-1)" />
  <!-- vertical bottom left -->
  <path d="M{{ .Bleed.Left }},0 v{{ .HalfBleed.Bottom }}"
        transform="translate(0,{{ .Height }}) scale(1,-1)" />
  <!-- vertical top right -->
  <path d="M{{ .Bleed.Right }},0 v{{ .HalfBleed.Top }}"
        transform="translate({{ .Width }},0) scale(-1,1)" />
`
	cross = `
  <!-- top -->
  <circle r="{{ .HalfBleed.Top }}"
          transform="scale(0.5)
                     translate({{ .Width }},{{ .HalfBleed.Top }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Top }},{{ .HalfBleed.Top }} h{{ .Bleed.Top }}
           M0,0 v{{ .Bleed.Top }}"
        transform="scale(0.5) translate({{ .Width }},0)" />
  <!-- bottom -->
  <circle r="{{ .HalfBleed.Bottom }}"
          transform="translate(0,{{ .Height }}) scale(0.5)
                     translate({{ .Width }},-{{ .HalfBleed.Bottom }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Bottom }},-{{ .HalfBleed.Bottom }} h{{ .Bleed.Bottom }}
           M0,0 v-{{ .Bleed.Bottom }}"
        transform="translate(0,{{ .Height }}) scale(0.5) translate({{ .Width }},0)" />
  <!-- left -->
  <circle r="{{ .HalfBleed.Left }}"
          transform="scale(0.5)
                     translate({{ .HalfBleed.Left }},{{ .Height }}) scale(0.5)" />
  <path d="M{{ .HalfBleed.Left }},-{{ .HalfBleed.Left }} v{{ .Bleed.Left }}
           M0,0 h{{ .Bleed.Left }}"
        transform="scale(0.5) translate(0,{{ .Height }})" />
  <!-- right -->
  <circle r="{{ .HalfBleed.Right }}"
          transform="translate({{ .Width }},0) scale(0.5)
                     translate(-{{ .HalfBleed.Right }},{{ .Height }}) scale(0.5)" />
  <path d="M-{{ .HalfBleed.Right }},-{{ .HalfBleed.Right }} v{{ .Bleed.Right }}
           M0,0 h-{{ .Bleed.Right }}"
        transform="translate({{ .Width }},0)
                   scale(0.5) translate(0,{{ .Height }})" />
`
)

type svgArgs struct {
	Width, Height    fl
	Bleed, HalfBleed bo.Bleed
}

// text layout is needed by SVG images
var _ text.TextLayoutContext = drawContext{}

type drawContext struct {
	dst   backend.Canvas
	fonts text.FontConfiguration

	hyphenCache       map[text.HyphenDictKey]hyphen.Hyphener
	strutLayoutsCache map[text.StrutLayoutKey][2]pr.Float
}

func (ctx drawContext) Fonts() text.FontConfiguration { return ctx.fonts }

func (ctx drawContext) HyphenCache() map[text.HyphenDictKey]hyphen.Hyphener {
	return ctx.hyphenCache
}

func (ctx drawContext) StrutLayoutsCache() map[text.StrutLayoutKey][2]pr.Float {
	return ctx.strutLayoutsCache
}

// Draw the given PageBox.
func (ctx drawContext) drawPage(page *bo.PageBox) {
	marks := page.Style.GetMarks()
	stackingContext := NewStackingContextFromPage(page)
	ctx.drawBackground(stackingContext.box.Box().Background, false, page.Bleed(), marks)
	ctx.setMaskBorder(page)
	ctx.drawBackground(page.CanvasBackground, false, bo.Bleed{}, pr.Marks{})
	ctx.drawBorder(page)
	ctx.drawStackingContext(stackingContext)
}

// Draw a “stackingContext“ on “context“.
func (ctx drawContext) drawStackingContext(stackingContext StackingContext) {
	// See http://www.w3.org/TR/CSS2/zindex.html
	ctx.dst.OnNewStack(func() {
		box_ := stackingContext.box
		box := box_.Box()

		// apply the viewport_overflow to the html box, see #35
		if box.IsForRootElement && (stackingContext.page.Style.GetOverflow() != "visible") {
			roundedBox(
				ctx.dst, stackingContext.page.RoundedPaddingBox())
			ctx.dst.State().Clip(false)
		}

		if clips := box.Style.GetClip(); box.IsAbsolutelyPositioned() && len(clips) != 0 {
			top, right, bottom, left := clips[0], clips[1], clips[2], clips[3]
			if top.Tag == pr.Auto {
				top.Value = 0
			}
			if right.Tag == pr.Auto {
				right.Value = 0
			}
			if bottom.Tag == pr.Auto {
				bottom.Value = box.BorderHeight()
			}
			if left.Tag == pr.Auto {
				left.Value = box.BorderWidth()
			}
			ctx.dst.Rectangle(
				fl(box.BorderBoxX()+right.Value),
				fl(box.BorderBoxY()+top.Value),
				fl(left.Value-right.Value),
				fl(bottom.Value-top.Value),
			)
			ctx.dst.State().Clip(false)
		}

		originalDst := ctx.dst
		opacity := fl(box.Style.GetOpacity())
		if opacity < 1 { // we draw all the following to a separate group
			ctx.dst = ctx.dst.NewGroup(pr.Fl(box.BorderBoxX()), pr.Fl(box.BorderBoxY()),
				pr.Fl(box.BorderWidth()), pr.Fl(box.BorderHeight()))
		}

		if mat, ok := getMatrix(box_); ok {
			if mat.Determinant() != 0 {
				ctx.dst.State().Transform(mat)
			} else {
				logger.WarningLogger.Printf("non invertible transformation matrix %v\n", mat)
				return
			}
		}

		// Point 1 is done in drawPage

		// Point 2
		if bo.BlockT.IsInstance(box_) || bo.MarginT.IsInstance(box_) ||
			bo.InlineBlockT.IsInstance(box_) || bo.TableCellT.IsInstance(box_) ||
			bo.FlexContainerT.IsInstance(box_) || bo.GridContainerT.IsInstance(box_) || bo.ReplacedT.IsInstance(box_) {
			ctx.setMaskBorder(box_)
			// The canvas background was removed by layout_backgrounds.
			ctx.drawBackgroundDefaut(box_.Box().Background)
			ctx.drawBorder(box_)
		}

		ctx.dst.OnNewStack(func() {
			// dont clip the PageBox, see #35
			if box.Style.GetOverflow() != "visible" && !bo.PageT.IsInstance(box_) {
				// Only clip the content and the children:
				// - the background is already clipped
				// - the border must *not* be clipped
				roundedBox(ctx.dst, box.RoundedPaddingBox())
				ctx.dst.State().Clip(false)
			}

			// Point 3
			for _, childContext := range stackingContext.negativeZContexts {
				ctx.drawStackingContext(childContext)
			}

			// Point 4
			for _, block := range stackingContext.blockLevelBoxes {
				ctx.setMaskBorder(block)
				if box_, ok := block.(bo.TableBoxITF); ok {
					ctx.drawTable(box_.Table())
				} else {
					ctx.drawBackgroundDefaut(block.Box().Background)
					ctx.drawBorder(block)
				}
			}

			// Point 5
			for _, childContext := range stackingContext.floatContexts {
				ctx.drawStackingContext(childContext)
			}

			// Point 6
			if bo.InlineT.IsInstance(box_) {
				ctx.drawInlineLevel(stackingContext.page, box_, 0, "clip", pr.TaggedString{Tag: pr.None})
			}

			// Point 7
			ctx.drawBlockLevel(stackingContext.page, BoxTree{box_: stackingContext.blocksAndCells})

			// Point 8
			for _, childContext := range stackingContext.zeroZContexts {
				ctx.drawStackingContext(childContext)
			}

			// Point 9
			for _, childContext := range stackingContext.positiveZContexts {
				ctx.drawStackingContext(childContext)
			}
		})

		// Point 10
		ctx.drawOutline(box_)

		if opacity < 1 {
			group := ctx.dst
			ctx.dst = originalDst
			ctx.dst.OnNewStack(func() {
				ctx.dst.DrawWithOpacity(opacity, group)
			})
		}
	})
}

func formatSVG(svg string, data svgArgs) (string, error) {
	tmp, err := template.New("svg").Parse(svg)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err := tmp.Execute(&b, data); err != nil {
		return "", fmt.Errorf("unexpected template error : %s", err)
	}
	return b.String(), nil
}

func reversed(in []bo.BackgroundLayer) []bo.BackgroundLayer {
	N := len(in)
	out := make([]bo.BackgroundLayer, N)
	for i, v := range in {
		out[N-1-i] = v
	}
	return out
}

func (ctx drawContext) drawBackgroundDefaut(bg *bo.Background) {
	ctx.drawBackground(bg, true, bo.Bleed{}, pr.Marks{})
}

// Draw the background color and image
// If “clipBox“ is set to “false“, the background is not clipped to the
// border box of the background, but only to the painting area
// clipBox=true bleed=nil marks=()
func (ctx drawContext) drawBackground(bg *bo.Background, clipBox bool, bleed bo.Bleed, marks pr.Marks) {
	if bg == nil {
		return
	}

	ctx.dst.OnNewStack(func() {
		if clipBox {
			for _, box := range bg.Layers[len(bg.Layers)-1].ClippedBoxes {
				roundedBox(ctx.dst, box)
			}
			ctx.dst.State().Clip(false)
		}

		// Background color
		if bg.Color.A > 0 {
			ctx.dst.OnNewStack(func() {
				ctx.dst.State().SetColorRgba(bg.Color, false)
				paintingArea := bg.Layers[len(bg.Layers)-1].PaintingArea
				ctx.dst.Rectangle(paintingArea.Unpack())
				ctx.dst.State().Clip(false)
				ctx.dst.Rectangle(paintingArea.Unpack())
				ctx.dst.Paint(backend.FillNonZero)
			})
		}

		if (bleed != bo.Bleed{}) && !marks.IsNone() {
			x, y, width, height := bg.Layers[len(bg.Layers)-1].PaintingArea.Unpack()
			svg := headerSVG
			if marks.Crop {
				svg += crop
			}
			if marks.Cross {
				svg += cross
			}
			svg += "</svg>"
			halfBleed := bo.Bleed{
				Top:    bleed.Top * 0.5,
				Bottom: bleed.Bottom * 0.5,
				Left:   bleed.Left * 0.5,
				Right:  bleed.Right * 0.5,
			}
			svg, err := formatSVG(svg, svgArgs{Width: width, Height: height, Bleed: bleed, HalfBleed: halfBleed})
			if err != nil {
				logger.WarningLogger.Println(err)
				return
			}
			image, err := images.NewSVGImage(strings.NewReader(svg), "", nil)
			if err != nil {
				logger.WarningLogger.Println(err)
				return
			}

			// Painting area is the PDF media box
			size := [2]pr.Float{pr.Float(width), pr.Float(height)}
			position := bo.Position{Point: bo.MaybePoint{pr.Float(x), pr.Float(y)}}
			repeat := bo.Repeat{Reps: [2]string{"no-repeat", "no-repeat"}}
			unbounded := true
			paintingArea := pr.Rectangle{pr.Float(x), pr.Float(y), pr.Float(width), pr.Float(height)}
			positioningArea := pr.Rectangle{0, 0, pr.Float(width), pr.Float(height)}
			layer := bo.BackgroundLayer{
				Image: image, Size: size, Position: position, Repeat: repeat, Unbounded: unbounded,
				PaintingArea: paintingArea, PositioningArea: positioningArea,
			}
			bg.Layers = append([]bo.BackgroundLayer{layer}, bg.Layers...)
		}
		// Paint in reversed order: first layer is "closest" to the viewer.
		for _, layer := range reversed(bg.Layers) {
			ctx.drawBackgroundImage(layer, bg.ImageRendering)
		}
	})
}

func (ctx drawContext) drawBackgroundImage(layer bo.BackgroundLayer, imageRendering pr.String) {
	if layer.Image == nil || layer.Size[0] == 0 || layer.Size[1] == 0 {
		return
	}

	paintingX, paintingY, paintingWidth, paintingHeight := layer.PaintingArea.Unpack()
	positioningX, positioningY, positioningWidth, positioningHeight := layer.PositioningArea.Unpack()
	positionX, positionY := layer.Position.Point[0], layer.Position.Point[1]
	repeatX, repeatY := layer.Repeat.Reps[0], layer.Repeat.Reps[1]
	imageWidth, imageHeight := pr.Fl(layer.Size[0]), pr.Fl(layer.Size[1])
	var repeatWidth, repeatHeight pr.Fl
	switch repeatX {
	case "no-repeat":
		// We want at least the whole image_width drawn on sub_surface, but we
		// want to be sure it will not be repeated on the painting_width. We
		// double the painting width to ensure viewers don't incorrectly bleed
		// the edge of the pattern into the painting area. (See #1539.)
		repeatWidth = utils.Maxs(imageWidth, 2*paintingWidth)
	case "repeat", "round":
		// We repeat the image each imageWidth.
		repeatWidth = imageWidth
	case "space":
		nRepeats := pr.Fl(math.Floor(float64(positioningWidth / imageWidth)))
		if nRepeats >= 2 {
			// The repeat width is the whole positioning width with one image
			// removed, divided by (the number of repeated images - 1). This
			// way, we get the width of one image + one space. We ignore
			// background-position for this dimension.
			repeatWidth = (positioningWidth - imageWidth) / (nRepeats - 1)
			positionX = pr.Float(0)
		} else {
			// We don't repeat the image.
			repeatWidth = positioningWidth
		}
	default:
		panic(fmt.Sprintf("unexpected repeatX %s", repeatX))
	}

	// Comments above apply here too.
	switch repeatY {
	case "no-repeat":
		repeatHeight = utils.Maxs(imageHeight, 2*paintingHeight)
	case "repeat", "round":
		repeatHeight = imageHeight
	case "space":
		nRepeats := fl(math.Floor(float64(positioningHeight / imageHeight)))
		if nRepeats >= 2 {
			repeatHeight = (positioningHeight - imageHeight) / (nRepeats - 1)
			positionY = pr.Float(0)
		} else {
			repeatHeight = positioningHeight
		}
	default:
		panic(fmt.Sprintf("unexpected repeatY %s", repeatY))
	}

	X := pr.Fl(positionX.V()) + positioningX
	Y := pr.Fl(positionY.V()) + positioningY

	// draw the image on a pattern
	patttern := ctx.dst.NewGroup(0, 0, repeatWidth, repeatHeight)
	layer.Image.Draw(patttern, ctx, imageWidth, imageHeight, string(imageRendering))

	ctx.dst.OnNewStack(func() {
		mat := matrix.New(1, 0, 0, 1, X, Y) // translate
		ctx.dst.State().SetColorPattern(patttern, imageWidth, imageHeight, mat, false)
		if layer.Unbounded {
			x1, y1, x2, y2 := ctx.dst.GetBoundingBox()
			ctx.dst.Rectangle(x1, y1, x2-x1, y2-y1)
		} else {
			ctx.dst.Rectangle(paintingX, paintingY, paintingWidth, paintingHeight)
		}
		ctx.dst.Paint(backend.FillNonZero)
	})
}

func (ctx drawContext) drawTable(table *bo.TableBox) {
	// Draw the background color and image of the table children.
	ctx.drawBackgroundDefaut(table.Background)
	for _, columnGroup := range table.ColumnGroups {
		ctx.drawBackgroundDefaut(columnGroup.Background)
		for _, column := range columnGroup.Children {
			ctx.drawBackgroundDefaut(column.Box().Background)
		}
	}
	for _, rowGroup := range table.Children {
		ctx.drawBackgroundDefaut(rowGroup.Box().Background)
		for _, row := range rowGroup.Box().Children {
			ctx.drawBackgroundDefaut(row.Box().Background)
			for _, cell := range row.Box().Children {
				cell := cell.Box()
				if table.Style.GetBorderCollapse() == "collapse" ||
					cell.Style.GetEmptyCells() == "show" || !cell.Empty {
					ctx.drawBackgroundDefaut(cell.Background)
				}
			}
		}
	}

	// Draw borders
	if table.Style.GetBorderCollapse() == "collapse" {
		ctx.drawCollapsedBorders(table)
		return
	}

	ctx.drawBorder(table)
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			for _, cell := range row.Box().Children {
				if cell.Box().Style.GetEmptyCells() == "show" || !cell.Box().Empty {
					ctx.drawBorder(cell)
				}
			}
		}
	}
}

type segment struct {
	side pr.KnownProp
	bo.Border
	borderBox pr.Rectangle
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Draw borders of table cells when they collapse.
func (ctx drawContext) drawCollapsedBorders(table *bo.TableBox) {
	var rowHeights, rowPositions []pr.Float
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			rowHeights = append(rowHeights, row.Box().Height.V())
			rowPositions = append(rowPositions, row.Box().PositionY)
		}
	}
	columnWidths := table.ColumnWidths
	if len(rowHeights) == 0 || len(columnWidths) == 0 {
		// One of the list is empty: don’t bother with empty tables
		return
	}
	columnPositions := table.ColumnPositions // shallow copy
	gridHeight := len(rowHeights)
	gridWidth := len(columnWidths)

	if gridWidth != len(columnPositions) {
		panic(fmt.Sprintf("expected same gridWidth and columnPositions length, got %d, %d", gridWidth, len(columnPositions)))
	}

	// Add the end of the last column, but make a copy from the table attr.
	if table.Style.GetDirection() == pr.Ltr {
		columnPositions = append(columnPositions, columnPositions[len(columnPositions)-1]+columnWidths[len(columnWidths)-1])
	} else {
		columnPositions = append([]pr.Float{columnPositions[0] + columnWidths[0]}, columnPositions...)
	}

	// Add the end of the last row.
	rowPositions = append(rowPositions, rowPositions[len(rowPositions)-1]+rowHeights[len(rowHeights)-1])
	verticalBorders, horizontalBorders := table.CollapsedBorderGrid.Vertical, table.CollapsedBorderGrid.Horizontal

	headerRows := 0
	if table.Children[0].Box().IsHeader {
		headerRows = len(table.Children[0].Box().Children)
	}

	footerRows := 0
	if L := len(table.Children); table.Children[L-1].Box().IsFooter {
		footerRows = len(table.Children[L-1].Box().Children)
	}

	skippedRows := table.SkippedRows
	bodyRowsOffset := 0
	if skippedRows != 0 {
		bodyRowsOffset = skippedRows - headerRows
	}

	originalGridHeight := len(verticalBorders)
	footerRowsOffset := originalGridHeight - gridHeight

	rowNumber := func(y int, horizontal bool) int {
		// Examples in comments for 2 headers rows, 5 body rows, 3 footer rows
		if headerRows != 0 && y < (headerRows+boolToInt(horizontal)) {
			// Row in header: y < 2 for vertical, y < 3 for horizontal
			return y
		} else if footerRows != 0 && y >= (gridHeight-footerRows-boolToInt(horizontal)) {
			// Row in footer: y >= 7 for vertical, y >= 6 for horizontal
			return y + footerRowsOffset
		} else {
			// Row in body: 2 >= y > 7 for vertical, 3 >= y > 6 for horizontal
			return y + bodyRowsOffset
		}
	}

	var segments []segment

	// vertical=true
	halfMaxWidth := func(borderList [][]bo.Border, yxPairs [2][2]int, vertical bool) pr.Float {
		var result pr.Float
		for _, tmp := range yxPairs {
			y, x := tmp[0], tmp[1]
			cond := 0 <= y && y <= gridHeight && 0 <= x && x < gridWidth
			if vertical {
				cond = 0 <= y && y < gridHeight && 0 <= x && x <= gridWidth
			}
			if cond {
				yy := rowNumber(y, !vertical)
				width := pr.Float(borderList[yy][x].Width)
				result = max(result, width)
			}
		}
		return result / 2
	}

	addVertical := func(x, y int) {
		yy := rowNumber(y, false)
		border := verticalBorders[yy][x]
		if border.Width == 0 || border.Color.RGBA.A == 0 {
			return
		}
		posX := columnPositions[x]
		posY1 := rowPositions[y]
		if y != 0 || !table.SkipCellBorderTop {
			posY1 -= halfMaxWidth(horizontalBorders, [2][2]int{{y, x - 1}, {y, x}}, false)
		}
		posY2 := rowPositions[y+1]
		if y != gridHeight-1 || !table.SkipCellBorderBottom {
			posY2 += halfMaxWidth(horizontalBorders, [2][2]int{{y + 1, x - 1}, {y + 1, x}}, false)
		}
		segments = append(segments, segment{
			Border: border, side: left,
			borderBox: pr.Rectangle{posX, posY1, 0, posY2 - posY1},
		})
	}

	addHorizontal := func(x, y int) {
		if y == 0 && table.SkipCellBorderTop {
			return
		}
		if y == gridHeight && table.SkipCellBorderBottom {
			return
		}

		yy := rowNumber(y, true)
		border := horizontalBorders[yy][x]
		if border.Width == 0 || border.Color.RGBA.A == 0 {
			return
		}
		posY := rowPositions[y]
		shiftBefore := halfMaxWidth(verticalBorders, [2][2]int{{y - 1, x}, {y, x}}, true)
		shiftAfter := halfMaxWidth(verticalBorders, [2][2]int{{y - 1, x + 1}, {y, x + 1}}, true)
		var posX1, posX2 pr.Float
		if table.Style.GetDirection() == pr.Ltr {
			posX1 = columnPositions[x] - shiftBefore
			posX2 = columnPositions[x+1] + shiftAfter
		} else {
			posX1 = columnPositions[x+1] - shiftAfter
			posX2 = columnPositions[x] + shiftBefore
		}
		segments = append(segments, segment{
			Border: border, side: top,
			borderBox: pr.Rectangle{posX1, posY, posX2 - posX1, 0},
		})
	}

	for x := 0; x < gridWidth; x++ {
		addHorizontal(x, 0)
	}
	for y := 0; y < gridHeight; y++ {
		addVertical(0, y)
		for x := 0; x < gridWidth; x++ {
			addVertical(x+1, y)
			addHorizontal(x, y+1)
		}
	}

	// Sort bigger scores last (painted later, on top)
	// Since the number of different scores is expected to be small compared
	// to the number of segments, there should be little changes and Timsort
	// should be closer to O(n) than O(n * log(n))
	sort.SliceStable(segments, func(i, j int) bool {
		return segments[i].Border.Score.Lower(segments[j].Border.Score)
	})

	for _, segment := range segments {
		ctx.dst.OnNewStack(func() {
			bx, by, bw, bh := segment.borderBox.Unpack()
			ctx.drawLine(bx, by, bx+bw, by+bh, segment.Width, segment.Style,
				styledColor(segment.Style, segment.Color.RGBA, segment.side), 0)
		})
	}
}

// Draw the given `bo.ReplacedBox`
func (ctx drawContext) drawReplacedbox(box_ bo.ReplacedBoxITF) {
	box := box_.Replaced()
	if box.Style.GetVisibility() != "visible" || !pr.Is(box.Width) || !pr.Is(box.Height) {
		return
	}

	drawWidth, drawHeight, drawX, drawY := layout.LayoutReplacedBox(box_)
	if drawWidth <= 0 || drawHeight <= 0 {
		return
	}

	ctx.dst.OnNewStack(func() {
		ctx.dst.State().SetAlpha(1, false)
		ctx.dst.State().Transform(matrix.Translation(fl(drawX), fl(drawY)))
		ctx.dst.OnNewStack(func() {
			box.Replacement.Draw(ctx.dst, ctx, pr.Fl(drawWidth), pr.Fl(drawHeight), string(box.Style.GetImageRendering()))
		})
	})
}

// offsetX=0, textOverflow="clip"
func (ctx drawContext) drawInlineLevel(page *bo.PageBox, box_ Box, offsetX fl, textOverflow string, blockEllipsis pr.TaggedString) {
	if stackingContext, ok := box_.(StackingContext); ok {
		if !(bo.InlineBlockT.IsInstance(stackingContext.box) || bo.InlineFlexT.IsInstance(stackingContext.box) || bo.InlineGridT.IsInstance(stackingContext.box)) {
			panic(fmt.Sprintf("expected InlineBlock or InlineFlex, got %v", stackingContext.box))
		}
		ctx.drawStackingContext(stackingContext)
	} else {
		ctx.setMaskBorder(box_)
		box := box_.Box()
		ctx.drawBackgroundDefaut(box.Background)
		ctx.drawBorder(box_)
		textBox, isTextBox := box_.(*bo.TextBox)
		replacedBox, isReplacedBox := box_.(bo.ReplacedBoxITF)
		if layout.IsLine(box_) {
			if lineBox, ok := box_.(*bo.LineBox); ok {
				textOverflow = lineBox.TextOverflow
				blockEllipsis = lineBox.BlockEllipsis
			}
			for _, child := range box.Children {
				childOffsetX := offsetX
				if _, ok := child.(StackingContext); !ok {
					childOffsetX = offsetX + fl(child.Box().PositionX) - fl(box.PositionX)
				}
				if childT, ok := child.(*bo.TextBox); ok {
					ctx.drawText(childT, childOffsetX, textOverflow, blockEllipsis)
				} else {
					ctx.drawInlineLevel(page, child, childOffsetX, textOverflow, blockEllipsis)
				}
			}
		} else if isReplacedBox {
			ctx.drawReplacedbox(replacedBox)
		} else if isTextBox {
			// Should only happen for list markers
			ctx.drawText(textBox, offsetX, textOverflow, blockEllipsis)
		} else {
			panic(fmt.Sprintf("unexpected box %s", box_.Type()))
		}
	}
}

func (ctx *drawContext) drawBlockLevel(page *bo.PageBox, blocksAndCells BoxTree) {
	for block, innerBlocksAndCells := range blocksAndCells {
		if blockRep, ok := block.(bo.ReplacedBoxITF); ok {
			ctx.drawReplacedbox(blockRep)
		} else if children := block.Box().Children; len(children) != 0 {
			if bo.LineT.IsInstance(children[len(children)-1]) {
				for _, child := range children {
					ctx.drawInlineLevel(page, child, 0, "clip", pr.TaggedString{Tag: pr.None})
				}
			}
		}
		ctx.drawBlockLevel(page, innerBlocksAndCells)
	}
}
