package layout

import (
	"fmt"
	"math"
	"strings"

	"github.com/benoitkugler/webrender/logger"
	"github.com/benoitkugler/webrender/text"

	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/tree"
)

// Preferred and minimum preferred width, aka. max-content and min-content
// width, aka. the shrink-to-fit algorithm.

// Terms used (max-content width, min-content width) are defined in David
// Baron's unofficial draft (https://dbaron.org/css/intrinsic/).

// Return the shrink-to-fit width of “box“.
// *Warning:* both availableOuterWidth and the return value are
// for width of the *content area*, not margin area.
// https://www.w3.org/TR/CSS21/visudet.html#float-width
func shrinkToFit(context *layoutContext, box Box, availableContentWidth pr.Float) pr.Float {
	out := min(
		max(minContentWidth(context, box, false), availableContentWidth),
		maxContentWidth(context, box, false),
	)
	return out
}

// Return the min-content width for “box“.
// This is the width by breaking at every line-break opportunity.
// [outer] defaults to true
func minContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	if box.Box().IsTableWrapper {
		return tableAndColumnsPreferredWidths(context, box, outer).tableMinContentWidth
	} else if bo.TableCellT.IsInstance(box) {
		return tableCellMinContentWidth(context, box, outer)
	} else if bo.BlockContainerT.IsInstance(box) || bo.TableColumnT.IsInstance(box) {
		return blockMinContentWidth(context, box, outer)
	} else if bo.TableColumnGroupT.IsInstance(box) {
		return columnGroupContentWidth(box)
	} else if IsLine(box) {
		return inlineMinContentWidth(context, box, outer, nil, false, true)
	} else if rep, isReplaced := box.(bo.ReplacedBoxITF); isReplaced {
		return replacedMinContentWidth(rep.Replaced(), outer)
	} else if bo.FlexContainerT.IsInstance(box) {
		return flexMinContentWidth(context, box, outer)
	} else if bo.GridContainerT.IsInstance(box) {
		return blockMinContentWidth(context, box, outer)
	} else {
		panic(fmt.Sprintf("min-content width for %T not handled yet", box))
	}
}

// Return the max-content width for “box“.
// This is the width by only breaking at forced line breaks.
// outer=true
func maxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	rep, isReplaced := box.(bo.ReplacedBoxITF)
	if box.Box().IsTableWrapper {
		return tableAndColumnsPreferredWidths(context, box, outer).tableMaxContentWidth
	} else if bo.TableCellT.IsInstance(box) {
		return tableCellMaxContentWidth(context, box, outer)
	} else if bo.BlockContainerT.IsInstance(box) || bo.TableColumnT.IsInstance(box) {
		return blockMaxContentWidth(context, box, outer)
	} else if bo.TableColumnGroupT.IsInstance(box) {
		return columnGroupContentWidth(box)
	} else if IsLine(box) {
		return inlineMaxContentWidth(context, box, outer, true)
	} else if isReplaced {
		return replacedMaxContentWidth(rep.Replaced(), outer)
	} else if bo.FlexContainerT.IsInstance(box) {
		return flexMaxContentWidth(context, box, outer)
	} else if bo.GridContainerT.IsInstance(box) {
		return blockMaxContentWidth(context, box, outer)
	} else {
		logger.WarningLogger.Printf("max-content width for %T not handled yet", box)
		return 0
	}
}

type fnBlock = func(*layoutContext, Box, bool) pr.Float

// Helper to create “block_*_ContentWidth.“
func blockContentWidth(context *layoutContext, box Box, function fnBlock, outer bool) pr.Float {
	width := box.Box().Style.GetWidth()
	var widthValue pr.Float
	if width.Tag == pr.Auto || width.Unit == pr.Perc {
		// "percentages on the following properties are treated instead as
		// though they were the following: width: auto"
		// https://dbaron.org/css/intrinsic/#outer-intrinsic
		var max pr.Float = 0
		for _, child := range box.Box().Children {
			if !child.Box().IsAbsolutelyPositioned() {
				v := function(context, child, true)
				if v > max {
					max = v
				}
			}
		}
		widthValue = max
	} else {
		if width.Unit != pr.Px {
			panic(fmt.Sprintf("expected Px got %d", width.Unit))
		}
		widthValue = width.Value
	}

	return adjust(box, outer, widthValue, true, true)
}

// Get box width from given width and box min- and max-widths.
func minMax(box Box, width pr.Float) pr.Float {
	minWidth := box.Box().Style.GetMinWidth()
	maxWidth := box.Box().Style.GetMaxWidth()
	var resMin, resMax pr.Float
	if minWidth.Tag == pr.Auto || minWidth.Unit == pr.Perc {
		resMin = 0
	} else {
		resMin = minWidth.Value
	}
	if maxWidth.Tag == pr.Auto || maxWidth.Unit == pr.Perc {
		resMax = pr.Inf
	} else {
		resMax = maxWidth.Value
	}

	if replaced, ok := box.(bo.ReplacedBoxITF); ok {
		_, _, ratio := replaced.Replaced().Replacement.GetIntrinsicSize(1, box.Box().Style.GetFontSize().Value)
		if ratio != nil {
			minHeight := box.Box().Style.GetMinHeight()
			if minHeight.Tag != pr.Auto && minHeight.Unit != pr.Perc {
				resMin = max(resMin, minHeight.Value*ratio.V())
			}
			maxHeight := box.Box().Style.GetMaxHeight()
			if maxHeight.Tag != pr.Auto && maxHeight.Unit != pr.Perc {
				resMax = min(resMax, maxHeight.Value*ratio.V())
			}
		}
	}

	return max(resMin, min(width, resMax))
}

// Add box paddings, borders and margins to “width“.
// left=true, right=true
func marginWidth(box *bo.BoxFields, width pr.Float, left, right bool) pr.Float {
	// See https://drafts.csswg.org/css-tables-3/#cell-intrinsic-offsets
	// It is a set of computed values for border-left-width, padding-left,
	// padding-right, and border-right-width (along with zero values for
	// margin-left and margin-right)

	var percentages pr.Float
	var cases []pr.KnownProp
	if left {
		cases = append(cases, pr.PMarginLeft, pr.PPaddingLeft)
	}
	if right {
		cases = append(cases, pr.PMarginRight, pr.PPaddingRight)
	}
	for _, value := range cases {
		styleValue := box.Style.Get(value.Key()).(pr.TaggedDim)
		if styleValue.Tag != pr.Auto {
			switch styleValue.Unit {
			case pr.Px:
				width += styleValue.Value
			case pr.Perc:
				percentages += styleValue.Value
			default:
				panic(fmt.Sprintf("expected Px or Percentage, got %d", styleValue.Unit))
			}
		}
	}

	collapse := box.Style.GetBorderCollapse() == "collapse"
	if left {
		if collapse && box.BorderLeftWidth != 0 {
			// In collapsed-borders mode: the computed horizontal padding of the
			// cell and, for border values, the used border-width values of the
			// cell (half the winning border-width)
			width += box.BorderLeftWidth
		} else {
			// In separated-borders mode: the computed horizontal padding and
			// border of the table-cell
			width += box.Style.GetBorderLeftWidth().Value
		}
	}
	if right {
		if collapse && box.BorderRightWidth != 0 {
			// [...] the used border-width values of the cell
			width += box.BorderRightWidth
		} else {
			// [...] the computed border of the table-cell
			width += box.Style.GetBorderRightWidth().Value
		}
	}

	if percentages < 100 {
		return width / (1 - percentages/100.)
	} else {
		// Pathological case, ignore
		return 0
	}
}

// Respect min/max and adjust [width] depending on “outer“.
// If “outer“ is set to “true“, return margin width, else return content
// width.
// left=true, right=true
func adjust(box Box, outer bool, width pr.Float, left, right bool) pr.Float {
	fixed := minMax(box, width)
	if outer {
		return marginWidth(box.Box(), fixed, left, right)
	} else {
		return fixed
	}
}

// Return the min-content width for a “BlockBox“.
// outer=true
func blockMinContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	v := blockContentWidth(context, box, minContentWidth, outer)
	return v
}

// Return the max-content width for a “BlockBox“.
// outer=true
func blockMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	return blockContentWidth(context, box, maxContentWidth, outer)
}

// Return the min-content width for an “InlineBox“.
//
// The width is calculated from the lines from “skipStack“. If
// “firstLine“ is “true“, only the first line minimum width is
// calculated.
// outer=true, skipStack=None, firstLine=false, isLineStart=false
func inlineMinContentWidth(context *layoutContext, box_ Box, outer bool, skipStack tree.ResumeStack,
	firstLine, isLineStart bool,
) pr.Float {
	widths := inlineLineWidths(context, box_, outer, isLineStart, true, skipStack, firstLine)

	if firstLine {
		widths = widths[0:1]
	}

	return adjust(box_, outer, pr.Maxs(widths...), true, true)
}

// Return the max-content width for an “InlineBox“.
// outer=true, isLineStart=false
func inlineMaxContentWidth(context *layoutContext, box_ Box, outer, isLineStart bool) pr.Float {
	widths := inlineLineWidths(context, box_, outer, isLineStart, false, nil, false)
	// Remove trailing space, as split_first_line keeps trailing spaces when
	// max_width is not set.
	widths[len(widths)-1] -= trailingWhitespaceSize(context, box_)
	return adjust(box_, outer, pr.Maxs(widths...), true, true)
}

// Return the *-content width for a “TableColumnGroupBox“.
func columnGroupContentWidth(box Box) pr.Float {
	width := box.Box().Style.GetWidth()
	var width_ pr.Float
	if width.Tag == pr.Auto || width.Unit == pr.Perc {
		width_ = 0
	} else if width.Unit == pr.Px {
		width_ = width.Value
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}

	return adjust(box, false, width_, true, true)
}

// Return the min-content width for a “TableCellBox“.
func tableCellMinContentWidth(context *layoutContext, box_ Box, outer bool) pr.Float {
	// See https://www.w3.org/TR/css-tables-3/#outer-min-content
	// The outer min-content width of a table-cell is
	// max(min-width, min-content width) adjusted by
	// the cell intrinsic offsets.
	box := box_.Box()
	var maxChildrenWidths pr.Float
	for _, child := range box.Children {
		if !child.Box().IsAbsolutelyPositioned() {
			v := minContentWidth(context, child, true)
			if v > maxChildrenWidths {
				maxChildrenWidths = v
			}
		}
	}
	childrenMinWidth := adjust(box_, outer, maxChildrenWidths, true, true)
	return childrenMinWidth
}

// Return the max-content width for a “TableCellBox“.
func tableCellMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	return max(tableCellMinContentWidth(context, box, outer), blockMaxContentWidth(context, box, outer))
}

func orNil(v pr.Float) pr.MaybeFloat {
	if v == 0 {
		return nil
	}
	return v
}

func orZero(v pr.MaybeFloat) pr.Float {
	if v == nil {
		return 0
	}
	return v.V()
}

// Yield line width for each line.
// firstLine=false
func inlineLineWidths(context *layoutContext, box_ Box, outer, isLineStart,
	minimum bool, skipStack tree.ResumeStack, firstLine bool,
) []pr.Float {
	var (
		textIndent, currentLine pr.Float
		skip                    int
		out                     []pr.Float
		box                     = box_.Box()
	)
	// Set text indent.
	if bo.LineT.IsInstance(box_) && box.Style.GetTextIndent().Unit != pr.Perc {
		textIndent = box.Style.GetTextIndent().Value
	}

	// Yield widths for each line.
	currentLine = 0
	if skipStack != nil {
		skip, skipStack = skipStack.Unpack()
	}
	for _, child := range box.Children[skip:] {
		// Skip absolutely positioned elements.
		if child.Box().IsAbsolutelyPositioned() {
			continue
		}
		textBox, isTextBox := child.(*bo.TextBox)
		// nil is used in "lines" to track line breaks, transformed to 0 when yielded.
		var lines []pr.MaybeFloat
		if bo.InlineT.IsInstance(child) {
			// Inline box, call function recursively.
			lines_ := inlineLineWidths(context, child, outer, isLineStart, minimum, skipStack, firstLine)
			if firstLine {
				lines_ = lines_[0:1]
			}
			lines = make([]pr.MaybeFloat, len(lines_))
			for i, v := range lines_ {
				lines[i] = orNil(v)
			}
			if len(lines) == 1 {
				lines[0] = adjust(child, outer, orZero(lines[0]), true, true)
			} else {
				lines[0] = orNil(adjust(child, outer, orZero(lines[0]), true, false))
				lines[len(lines)-1] = orNil(adjust(child, outer, orZero(lines[len(lines)-1]), false, true))
			}
		} else if isTextBox {
			// Text box, split into lines.
			whiteSpace := textBox.Style.GetWhiteSpace()
			spaceCollapse := whiteSpace == pr.Normal || whiteSpace == pr.Nowrap || whiteSpace == pr.PreLine
			textWrap := whiteSpace == pr.Normal || whiteSpace == pr.PreWrap || whiteSpace == pr.PreLine
			if skipStack == nil {
				skip = 0
			} else {
				skip, skipStack = skipStack.Unpack()
				if skipStack != nil {
					panic(fmt.Sprintf("expected empty SkipStack, got %v", skipStack))
				}
			}
			childText := textBox.Text[skip:]
			if isLineStart && spaceCollapse {
				childText = text.TrimPrefix(childText, ' ')
			}
			var maxWidth pr.MaybeFloat
			if minimum {
				maxWidth = pr.Float(0)
			}
			resumeIndex := 0
			newResumeIndex := 0
			textRunes := childText
			for newResumeIndex != -1 {
				resumeIndex += newResumeIndex
				tmp := text.SplitFirstLine(textRunes[resumeIndex:], textBox.Style,
					context, maxWidth, true, isLineStart)
				newResumeIndex = tmp.ResumeAt
				lines = append(lines, orNil(tmp.Width))
				if firstLine {
					break
				}
			}
			if firstLine && newResumeIndex != -1 && newResumeIndex != 0 {
				// We only need the first line, break early.
				currentLine += orZero(lines[0])
				break
			}
			// TODO: use the real next character instead of 'a' to detect line breaks.
			sample := []rune{'a'}
			if len(childText) != 0 {
				sample = []rune{childText[len(childText)-1], 'a'}
			}
			canBreak := context.Fonts().CanBreakText(sample)
			if minimum && textWrap && canBreak == pr.True {
				// Add all possible line breaks for minimal width.
				lines = append(lines, nil)
			}
		} else {
			// Replaced elements, inline blocks…
			// https://www.w3.org/TR/css3-text/#line-break-details
			// "The line breaking behavior of a replaced element
			//  or other atomic inline is equivalent to that
			//  of the Object Replacement Character (U+FFFC)."
			// https://www.unicode.org/reports/tr14/#DescriptionOfProperties
			// "By default, there is a break opportunity
			//  both before and after any inline object."
			if minimum {
				lines = []pr.MaybeFloat{nil, minContentWidth(context, child, true), nil}
			} else {
				lines = []pr.MaybeFloat{maxContentWidth(context, child, true)}
			}
		}
		// The first text line goes on the current line
		currentLine += orZero(lines[0])
		if len(lines) > 1 {
			// Forced line break(s).
			out = append(out, currentLine+textIndent)
			textIndent = 0
			if len(lines) > 2 {
				for _, v := range lines[1 : len(lines)-1] {
					out = append(out, orZero(v))
				}
			}
			currentLine = orZero(lines[len(lines)-1])
		}
		isLineStart = lines[len(lines)-1] == nil
		skipStack = nil
	}
	out = append(out, currentLine+textIndent)
	return out
}

// Return the percentage contribution of a cell, column or column group.
// https://dbaron.org/css/intrinsic/#pct-contrib
func percentageContribution(box bo.BoxFields) pr.Float {
	var minWidth, width pr.Float
	maxWidth := pr.Inf
	miw, maw, w := box.Style.GetMinWidth(), box.Style.GetMaxWidth(), box.Style.GetWidth()
	if miw.Tag != pr.Auto && miw.Unit == pr.Perc {
		minWidth = miw.Value
	}
	if maw.Tag != pr.Auto && maw.Unit == pr.Perc {
		maxWidth = maw.Value
	}
	if w.Tag != pr.Auto && w.Unit == pr.Perc {
		width = w.Value
	}
	return max(minWidth, min(width, maxWidth))
}

type tableContentWidths struct {
	grid                         [][]Box
	columnMinContentWidths       []pr.Float
	columnMaxContentWidths       []pr.Float
	columnIntrinsicPercentages   []pr.Float
	constrainedness              []bool
	tableMinContentWidth         pr.Float
	totalHorizontalBorderSpacing pr.Float
	tableMaxContentWidth         pr.Float
}

// Return content widths for the auto layout table and its columns.
// https://dbaron.org/css/intrinsic/
// outer = true
func tableAndColumnsPreferredWidths(context *layoutContext, box_ Box, outer bool) tableContentWidths {
	box := box_.Box()
	table_ := box.GetWrappedTable()
	table := table_.Table()

	if result := context.tables[table]; result != nil {
		return result[outer]
	}

	// Create the grid
	var gridWidth, gridHeight int
	rowNumber := 0
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			for _, cell := range row.Box().Children {
				gridWidth = max(cell.Box().GridX+cell.Box().Colspan, gridWidth)
				gridHeight = max(rowNumber+cell.Box().Rowspan, gridHeight)
			}
			rowNumber += 1
		}
	}
	// zippedGrid = list(zip(*grid)), which is the transpose of grid
	grid, zippedGrid := make([][]Box, gridHeight), make([][]Box, gridWidth)
	for i := range grid {
		grid[i] = make([]Box, gridWidth)
	}
	for j := range zippedGrid {
		zippedGrid[j] = make([]Box, gridHeight)
	}
	rowNumber = 0
	for _, rowGroup := range table.Children {
		for _, row := range rowGroup.Box().Children {
			for _, cell := range row.Box().Children {
				grid[rowNumber][cell.Box().GridX] = cell
				zippedGrid[cell.Box().GridX][rowNumber] = cell
			}
			rowNumber += 1
		}
	}

	// Define the total horizontal border spacing
	var totalHorizontalBorderSpacing pr.Float
	if table.Style.GetBorderCollapse() == "separate" && gridWidth > 0 {
		var tot pr.Float = 1
		for _, column := range zippedGrid {
			any := false
			for _, b := range column {
				if b != nil {
					any = true
					break
				}
			}
			if any {
				tot += 1
			}
		}
		totalHorizontalBorderSpacing = table.Style.GetBorderSpacing()[0].Value * tot
	}

	if gridWidth == 0 || gridHeight == 0 {
		table.Children = nil
		minWidth := blockMinContentWidth(context, table_, false)
		maxWidth := blockMaxContentWidth(context, table_, false)
		outerMinWidth := adjust(box_, true, blockMinContentWidth(context, table_, true), true, true)
		outerMaxWidth := adjust(box_, true, blockMaxContentWidth(context, table_, true), true, true)
		context.tables[table] = map[bool]tableContentWidths{
			false: {
				tableMinContentWidth:         minWidth,
				tableMaxContentWidth:         maxWidth,
				totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
			},
			true: {
				tableMinContentWidth:         outerMinWidth,
				tableMaxContentWidth:         outerMaxWidth,
				totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
			},
		}
		return context.tables[table][outer]
	}

	columnGroups := make([]Box, gridWidth)
	columns := make([]Box, gridWidth)
	columnNumber := 0
outerLoop:
	for _, columnGroup := range table.ColumnGroups {
		for _, column := range columnGroup.Children {
			columnGroups[columnNumber] = columnGroup
			columns[columnNumber] = column
			columnNumber += 1
			if columnNumber == gridWidth {
				break outerLoop
			}
		}
	}

	var colspanCells []Box

	// Define the intermediate content widths
	minContentWidths := make([]pr.Float, gridWidth)
	maxContentWidths := make([]pr.Float, gridWidth)
	intrinsicPercentages := make([]pr.Float, gridWidth)

	groupss := [2]*[]Box{&columnGroups, &columns}

	// Intermediate content widths for span 1
	for i := range minContentWidths {
		for _, groups := range groupss {
			if b := (*groups)[i]; b != nil {
				minContentWidths[i] = max(minContentWidths[i], minContentWidth(context, b, true))
				maxContentWidths[i] = max(maxContentWidths[i], maxContentWidth(context, b, true))
				intrinsicPercentages[i] = max(intrinsicPercentages[i], percentageContribution(*b.Box()))
			}
		}
		for _, cell := range zippedGrid[i] {
			if cell != nil {
				if cell.Box().Colspan == 1 {
					minContentWidths[i] = max(minContentWidths[i], minContentWidth(context, cell, true))
					maxContentWidths[i] = max(maxContentWidths[i], maxContentWidth(context, cell, true))
					intrinsicPercentages[i] = max(intrinsicPercentages[i], percentageContribution(*cell.Box()))
				} else {
					colspanCells = append(colspanCells, cell)
				}
			}
		}
	}

	// Intermediate content widths for span > 1 is wrong in the 4.1 section, as
	// explained in its third issue. Min- and max-content widths are handled by
	// the excess width distribution method, and percentages do not distribute
	// widths to columns that have originating cells.

	// Intermediate intrinsic percentage widths for span > 1
	for span := 1; span < gridWidth; span += 1 {
		var percentageContributions []pr.Float
		for i, percentageContribution_ := range intrinsicPercentages {
			for j := range zippedGrid[i] {
				var indexes []int
				for k := 0; k < i+1; k += 1 {
					if grid[j][k] != nil {
						indexes = append(indexes, k)
					}
				}
				if len(indexes) == 0 {
					continue
				}
				origin := indexes[len(indexes)-1] // = max
				originCell := grid[j][origin]
				ocColspan := originCell.Box().Colspan
				if ocColspan-1 != span {
					continue
				}
				var baselinePercentage pr.Float
				for u := origin; u < origin+ocColspan; u += 1 {
					baselinePercentage += intrinsicPercentages[u]
				}

				// Cell contribution to intrinsic percentage width
				if intrinsicPercentages[i] == 0 {
					diff := max(0, percentageContribution(*originCell.Box())-baselinePercentage)
					var (
						otherColumnsContributions    []pr.Float
						otherColumnsContributionsSum pr.Float
					)
					for s := origin; s < origin+ocColspan; s += 1 {
						if intrinsicPercentages[s] == 0 {
							otherColumnsContributions = append(otherColumnsContributions, maxContentWidths[s])
							otherColumnsContributionsSum += maxContentWidths[s]
						}
					}
					var ratio pr.Float
					if otherColumnsContributionsSum == 0 {
						if len(otherColumnsContributions) > 0 {
							ratio = 1. / pr.Float(len(otherColumnsContributions))
						} else {
							ratio = 1
						}
					} else {
						ratio = maxContentWidths[i] / otherColumnsContributionsSum
					}
					percentageContribution_ = max(percentageContribution_, diff*ratio)
				}
			}
			percentageContributions = append(percentageContributions, percentageContribution_)
		}
		intrinsicPercentages = percentageContributions
	}

	// Define constrainedness
	constrainedness := make([]bool, gridWidth)
	for i := range constrainedness {
		if columnGroups[i] != nil {
			if wid := columnGroups[i].Box().Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit != pr.Perc {
				constrainedness[i] = true
				continue
			}
		}
		if columns[i] != nil {
			if wid := columns[i].Box().Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit != pr.Perc {
				constrainedness[i] = true
				continue
			}
		}
		for _, cell := range zippedGrid[i] {
			if cell != nil {
				if wid := cell.Box().Style.GetWidth(); cell.Box().Colspan == 1 && wid.Tag != pr.Auto && wid.Unit != pr.Perc {
					constrainedness[i] = true
					break
				}
			}
		}
	}
	var cumsum pr.Float
	for i, percentage := range intrinsicPercentages {
		u := min(percentage, 100-cumsum)
		cumsum += percentage
		intrinsicPercentages[i] = u
	}

	// Max- and min-content widths for span > 1
	for _, cell_ := range colspanCells {
		cell := cell_.Box()
		minContent := minContentWidth(context, cell_, true)
		maxContent := maxContentWidth(context, cell_, true)
		columnSlice := [2]int{cell.GridX, cell.GridX + cell.Colspan}
		var columnsMinContent, columnsMaxContent pr.Float
		for s := columnSlice[0]; s < columnSlice[1]; s += 1 {
			columnsMinContent += minContentWidths[s]
			columnsMaxContent += maxContentWidths[s]
		}
		var spacing pr.Float
		if table.Style.GetBorderCollapse() == "separate" {
			spacing = pr.Float(cell.Colspan-1) * table.Style.GetBorderSpacing()[0].Value
		}

		if minContent > columnsMinContent+spacing {
			excessWidth := minContent - (columnsMinContent + spacing)
			distributeExcessWidth(zippedGrid, excessWidth, minContentWidths,
				constrainedness, intrinsicPercentages, maxContentWidths, columnSlice)
		}

		if maxContent > columnsMaxContent+spacing {
			excessWidth := maxContent - (columnsMaxContent + spacing)
			distributeExcessWidth(zippedGrid, excessWidth, maxContentWidths,
				constrainedness, intrinsicPercentages, maxContentWidths, columnSlice)
		}
	}

	// Calculate the max- and min-content widths of table and columns
	var (
		smallpercentageContributions               []pr.Float
		largepercentageContributionNumerator, sum_ pr.Float
	)
	for i, v := range intrinsicPercentages {
		sum_ += v
		if v != 0 {
			smallpercentageContributions = append(smallpercentageContributions, maxContentWidths[i]/(v/100.))
		} else {
			largepercentageContributionNumerator += maxContentWidths[i]
		}
	}
	largepercentageContributionDenominator := (100. - sum_) / 100.
	var largepercentageContribution pr.Float
	if largepercentageContributionDenominator == 0 {
		if largepercentageContributionNumerator == 0 {
			largepercentageContribution = 0
		} else {
			// "the large percentage contribution of the table [is] an
			// infinitely large number if the numerator is nonzero [and] the
			// denominator of that ratio is 0."
			//
			// https://dbaron.org/css/intrinsic/#autotableintrinsic
			//
			// Please note that "an infinitely large number" is not "infinite",
			// and that"s probably not a coincindence: putting "inf" here breaks
			// some cases (see #305).
			largepercentageContribution = math.MaxInt32
		}
	} else {
		largepercentageContribution = largepercentageContributionNumerator / largepercentageContributionDenominator
	}

	var sumMin, sumMax pr.Float
	for i := range minContentWidths {
		sumMin += minContentWidths[i]
		sumMax += maxContentWidths[i]
	}
	tableMinContentWidth := totalHorizontalBorderSpacing + sumMin
	tableMaxContentWidth := totalHorizontalBorderSpacing + max(
		max(sumMax, largepercentageContribution),
		pr.Maxs(smallpercentageContributions...))

	tableMinWidth := tableMinContentWidth
	tableMaxWidth := tableMaxContentWidth
	if wid := table.Style.GetWidth(); wid.Tag != pr.Auto && wid.Unit == pr.Px {
		// "percentages on the following properties are treated instead as
		// though they were the following: width: auto"
		// https://dbaron.org/css/intrinsic/#outer-intrinsic
		tableMinWidth = wid.Value
		tableMaxWidth = wid.Value
	}

	tableMinContentWidth = max(tableMinContentWidth, adjust(table, false, tableMinWidth, true, true))
	tableMaxContentWidth = max(tableMaxContentWidth, adjust(table, false, tableMaxWidth, true, true))
	tableOuterMinContentWidth := marginWidth(&table.BoxFields, marginWidth(box, tableMinContentWidth, true, true), true, true)
	tableOuterMaxContentWidth := marginWidth(&table.BoxFields, marginWidth(box, tableMaxContentWidth, true, true), true, true)

	result := tableContentWidths{
		columnMinContentWidths:       minContentWidths,
		columnMaxContentWidths:       maxContentWidths,
		columnIntrinsicPercentages:   intrinsicPercentages,
		constrainedness:              constrainedness,
		totalHorizontalBorderSpacing: totalHorizontalBorderSpacing,
		grid:                         zippedGrid,
	}
	resultFalse := result
	resultFalse.tableMinContentWidth = tableMinContentWidth
	resultFalse.tableMaxContentWidth = tableMaxContentWidth
	resultTrue := result
	resultTrue.tableMinContentWidth = tableOuterMinContentWidth
	resultTrue.tableMaxContentWidth = tableOuterMaxContentWidth

	context.tables[table] = map[bool]tableContentWidths{
		false: resultFalse,
		true:  resultTrue,
	}
	return context.tables[table][outer]
}

// Return the min-content width for an “InlineReplacedBox“.
// outer=true
func replacedMinContentWidth(box *bo.ReplacedBox, outer bool) pr.Float {
	width := box.Style.GetWidth()
	var (
		h pr.MaybeFloat
		w pr.Float
	)
	if width.Tag == pr.Auto {
		height := box.Style.GetHeight()
		if height.Tag == pr.Auto || height.Unit == pr.Perc {
			h = pr.AutoF
		} else if height.Unit == pr.Px {
			h = height.Value
		} else {
			panic(fmt.Sprintf("expected Px got %d", height.Unit))
		}
		if mw := box.Style.GetMaxWidth(); mw.Tag != pr.Auto && mw.Unit == pr.Perc {
			// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
			w = pr.Float(0)
		} else {
			image := box.Replacement
			iwidth, iheight, iratio := image.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
			if pr.Is(iratio) && !pr.Is(iwidth) && !pr.Is(iheight) {
				w = 0
			} else {
				w, _ = DefaultImageSizing(iwidth, iheight, iratio, pr.AutoF, h, 300, 150)
			}
		}
	} else if width.Unit == pr.Perc {
		// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
		w = 0
	} else if width.Unit == pr.Px {
		w = width.Value
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}
	return adjust(box, outer, w, true, true)
}

// Return the max-content width for an “InlineReplacedBox“.
func replacedMaxContentWidth(box *bo.ReplacedBox, outer bool) pr.Float {
	width := box.Style.GetWidth()
	var (
		h pr.MaybeFloat
		w pr.Float
	)
	if width.Tag == pr.Auto {
		height := box.Style.GetHeight()
		if height.Tag == pr.Auto || height.Unit == pr.Perc {
			h = pr.AutoF
		} else if height.Unit == pr.Px {
			h = height.Value
		} else {
			panic(fmt.Sprintf("expected Px got %d", height.Unit))
		}

		image := box.Replacement
		iwidth, iheight, ratio := image.GetIntrinsicSize(box.Style.GetImageResolution().Value, box.Style.GetFontSize().Value)
		w, _ = DefaultImageSizing(iwidth, iheight, ratio, pr.AutoF, h, 300, 150)

	} else if width.Unit == pr.Perc {
		// See https://drafts.csswg.org/css-sizing/#intrinsic-contribution
		w = 0
	} else if width.Unit == pr.Px {
		w = width.Value
	} else {
		panic(fmt.Sprintf("expected Px got %d", width.Unit))
	}
	return adjust(box, outer, w, true, true)
}

// Return the min-content width for an “FlexContainerBox“.
// outer=true
func flexMinContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	// TODO: use real values, see
	// https://www.w3.org/TR/css-flexbox-1/#intrinsic-sizes
	var sum, max pr.Float
	for _, child := range box.Box().Children {
		if child.Box().IsFlexItem {
			v := minContentWidth(context, child, true)
			sum += v
			if v > max {
				max = v
			}
		}
	}
	if strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") && box.Box().Style.GetFlexWrap() == "nowrap" {
		return adjust(box, outer, sum, true, true)
	} else {
		return adjust(box, outer, max, true, true)
	}
}

// Return the max-content width for an “FlexContainerBox“.
func flexMaxContentWidth(context *layoutContext, box Box, outer bool) pr.Float {
	// TODO: use real values, see
	// https://www.w3.org/TR/css-flexbox-1/#intrinsic-sizes
	var sum, max pr.Float
	for _, child := range box.Box().Children {
		if child.Box().IsFlexItem {
			v := maxContentWidth(context, child, true)
			sum += v
			if v > max {
				max = v
			}
		}
	}
	if strings.HasPrefix(string(box.Box().Style.GetFlexDirection()), "row") {
		return adjust(box, outer, sum, true, true)
	} else {
		return adjust(box, outer, max, true, true)
	}
}

// Return the size of the trailing whitespace of “box“.
func trailingWhitespaceSize(context *layoutContext, box Box) pr.Float {
	// Find last box child, keep last parent to remove nested trailing spaces.
	var lastParent Box
	for IsLine(box) {
		ch := box.Box().Children
		if len(ch) == 0 {
			return 0
		}
		lastParent, box = box, ch[len(ch)-1]
	}
	// Return early if possible.
	textBox, ok := box.(*bo.TextBox)
	if !ok || len(textBox.Text) == 0 {
		// There’s no text in last child.
		return 0
	} else if ws := box.Box().Style.GetWhiteSpace(); !(ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine) {
		// Spaces don’t collapse.
		return 0
	} else if box.Box().Style.GetFontSize() == pr.FToV(0) {
		// Trailing spaces take no space.
		return 0
	} else if !text.HasSuffix(textBox.Text, ' ') {
		// No trailing space.
		return 0
	}

	// Strip text.
	if strippedText := text.TrimSuffix(textBox.Text, ' '); len(strippedText) != 0 {
		// Stripped text is not empty, calculate width difference.
		resume, oldResume := 0, -1
		var oldBox *bo.TextBox
		for resume != -1 {
			oldResume = resume
			oldBox, resume, _ = splitTextBox(context, textBox, nil, resume, true)
		}
		if oldBox == nil {
			panic("oldBox can't be nil")
		}
		strippedBox := textBox.CopyWithText(strippedText)
		strippedBox, resume, _ = splitTextBox(context, strippedBox, nil, oldResume, true)
		if strippedBox == nil {
			// Old box is split just before the trailing spaces.
			return oldBox.Box().Width.V()
		} else {
			if resume != -1 {
				panic("invalid resume at")
			}
			return oldBox.Box().Width.V() - strippedBox.Box().Width.V()
		}
	} else {
		// Stripped text is empty, render spaces to get width.
		spli := text.SplitFirstLine(textBox.Text, textBox.Style, context, nil, false, true)
		width := spli.Width
		// Remove possible trailing spaces from previous child.
		if lastParent != nil && len(lastParent.Box().Children) >= 2 {
			children := lastParent.Box().Children
			width += trailingWhitespaceSize(context, children[len(children)-2])
		}
		return width
	}
}
