package layout

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/benoitkugler/webrender/html/tree"

	pr "github.com/benoitkugler/webrender/css/properties"
	kw "github.com/benoitkugler/webrender/css/properties/keywords"
	bo "github.com/benoitkugler/webrender/html/boxes"
)

// Layout for flex containers && flex-items.

type indexedBox struct {
	box   Box
	index int
}

type flexLine struct {
	line                     []indexedBox
	crossSize, lowerBaseline pr.Float
}

func (f flexLine) reverse() { slices.Reverse(f.line) }

func (f flexLine) sum() pr.Float {
	var sum pr.Float
	for _, child := range f.line {
		box := child.box.Box()
		sum += box.HypotheticalMainSize + box.MainOuterExtra
	}
	return sum
}

func (f flexLine) allFrozen() bool {
	for _, child := range f.line {
		if !child.box.Box().Frozen {
			return false
		}
	}
	return true
}

func (f flexLine) adjustements() pr.Float {
	var sum pr.Float
	for _, child := range f.line {
		sum += child.box.Box().Adjustment
	}
	return sum
}

func (f flexLine) flexItems() []*bo.BoxFields {
	var out []*bo.BoxFields
	for _, child := range f.line {
		if box := child.box.Box(); box.IsFlexItem {
			out = append(out, box)
		}
	}
	return out
}

func sumCross(f []flexLine) pr.Float {
	var sumCross pr.Float
	for _, line := range f {
		sumCross += line.crossSize
	}
	return sumCross
}

func getAttr(box *bo.BoxFields, axis pr.KnownProp, min string) pr.MaybeFloat {
	var boxAxis pr.MaybeFloat
	if axis == pr.PWidth {
		boxAxis = box.Width
		if min == "min" {
			boxAxis = box.MinWidth
		} else if min == "max" {
			boxAxis = box.MaxWidth
		}
	} else {
		boxAxis = box.Height
		if min == "min" {
			boxAxis = box.MinHeight
		} else if min == "max" {
			boxAxis = box.MaxHeight
		}
	}
	return boxAxis
}

// use child.Style
func getCrossMargins(child *bo.BoxFields, cross pr.KnownProp) bo.MaybePoint {
	if cross == pr.PWidth {
		return bo.MaybePoint{child.Style.GetMarginLeft().Value, child.Style.GetMarginRight().Value}
	}
	return bo.MaybePoint{child.Style.GetMarginTop().Value, child.Style.GetMarginBottom().Value}
}

func getDimOrS(box *bo.BoxFields, cross pr.KnownProp) pr.DimOrS {
	out, _ := box.Style.Get(cross.Key()).(pr.DimOrS)
	return out
}

const (
	directionX = true
	directionY = false
)

func setDirection(box *bo.BoxFields, position bool, value pr.Float) {
	if position == directionX {
		box.PositionX = value
	} else {
		box.PositionY = value
	}
}

// the returned box as same concrete type than box_
func flexLayout(context *layoutContext, box_ Box, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock containingBlock,
	pageIsEmpty bool, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, discard bool,
) (bo.Box, blockLayout) {
	if traceMode {
		traceLogger.DumpTree(box_, "starting flexLayout")
	}
	box := box_.Box()

	context.createFlexFormattingContext()

	var resumeAt tree.ResumeStack

	is_start := skipStack == nil
	box_.RemoveDecoration(box, !is_start, false)

	discard = discard || box.Style.GetContinue() == "discard"
	draw_bottom_decoration := discard || box.Style.GetBoxDecorationBreak() == "clone"

	rowGap, columnGap := box.Style.GetRowGap(), box.Style.GetColumnGap()

	if draw_bottom_decoration {
		bottomSpace += box.PaddingBottom.V() + box.BorderBottomWidth + box.MarginBottom.V()
	}

	if box.Style.GetPosition().String == "relative" {
		// New containing block, use a new absolute list
		absoluteBoxes = new([]*AbsolutePlaceholder)
	}

	// References are to: https://www.w3.org/TR/css-flexbox-1/#layout-algorithm.

	// 1 Initial setup, done in formatting_structure.build.

	// 2 Determine the available main and cross space for the flex items.
	main, cross := pr.PHeight, pr.PWidth
	if strings.HasPrefix(string(box.Style.GetFlexDirection()), "row") {
		main, cross = pr.PWidth, pr.PHeight
	}

	var marginLeft pr.Float
	if box.MarginLeft != pr.AutoF {
		marginLeft = box.MarginLeft.V()
	}
	var marginRight pr.Float
	if box.MarginRight != pr.AutoF {
		marginRight = box.MarginRight.V()
	}

	// Define available main space.
	cbWidth, _ := containingBlock.ContainingBlock()
	var availableMainSpace pr.Float

	boxAxis := getAttr(box, main, "")
	if boxAxis != pr.AutoF {
		availableMainSpace = boxAxis.V()
	} else {
		// Otherwise, subtract the flex container’s margin, border, and padding…
		if main == pr.PWidth {
			availableMainSpace = cbWidth.V() - marginLeft - marginRight -
				box.PaddingLeft.V() - box.PaddingRight.V() - box.BorderLeftWidth.V() - box.BorderRightWidth.V()
		} else {
			availableMainSpace = pr.Inf
		}
	}

	// Same as above for available cross space.
	var availableCrossSpace pr.Float
	boxCross := getAttr(box, cross, "")
	if boxCross != pr.AutoF {
		availableCrossSpace = boxCross.V()
	} else {
		if cross == pr.PWidth {
			availableCrossSpace = cbWidth.V() - marginLeft - marginRight -
				box.PaddingLeft.V() - box.PaddingRight.V() - box.BorderLeftWidth.V() - box.BorderRightWidth.V()
		} else {
			availableCrossSpace = pr.Inf
		}
	}

	// 3 Determine the flex base size and hypothetical main size of each item.
	parentBox_ := box_.Copy()
	parentBox := parentBox_.Box()
	resolvePercentagesBox(parentBox_, containingBlock)
	blockLevelWidth(parentBox_, nil, containingBlock)
	children := append([]Box{}, box.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].Box().Style.GetOrder() < children[j].Box().Style.GetOrder() })

	originalSkipStack := skipStack
	var skip int
	if skipStack != nil {
		skip, skipStack = skipStack.Unpack()
		if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
			children = children[:skip+1]
		} else {
			children = children[skip:]
		}
	} else {
		skipStack = nil
	}

	childSkipStack := skipStack

	// resolve gaps and stores it into rowGap.Value
	if rowGap.S == "normal" {
		rowGap.Value = 0
	} else if rowGap.Unit == pr.Perc {
		if box.Height == pr.AutoF {
			rowGap.Value = 0
		} else {
			rowGap.Value = rowGap.Value / 100 * box.Height.V()
		}
	}
	if columnGap.S == "normal" {
		columnGap.Value = 0
	} else if columnGap.Unit == pr.Perc {
		if box.Width == pr.AutoF {
			columnGap.Value = 0
		} else {
			columnGap.Value = columnGap.Value / 100 * box.Width.V()
		}
	}

	var mainGap, crossGap pr.Float
	if main == pr.PWidth {
		mainGap, crossGap = columnGap.Value, rowGap.Value
	} else {
		mainGap, crossGap = rowGap.Value, columnGap.Value
	}

	positionX := (parentBox.PositionX + parentBox.BorderLeftWidth + parentBox.PaddingLeft.V())
	if parentBox.MarginLeft != pr.AutoF {
		positionX += parentBox.MarginLeft.V()
	}
	positionY := (parentBox.PositionY + parentBox.BorderTopWidth + parentBox.PaddingTop.V())
	if parentBox.MarginTop != pr.AutoF {
		positionY += parentBox.MarginTop.V()
	}

	for index, child_ := range children {
		child := child_.Box()
		if !child.IsFlexItem {
			// Absolute child layout: create placeholder.
			if child.IsAbsolutelyPositioned() {
				child.PositionX = positionX
				child.PositionY = positionY
				placeholder := NewAbsolutePlaceholder(child_)
				placeholder.Box().Index = index
				children[index] = placeholder
				if child.Style.GetPosition().String == "absolute" {
					*absoluteBoxes = append(*absoluteBoxes, placeholder)
				} else {
					*fixedBoxes = append(*fixedBoxes, placeholder)
				}
			} else if child.IsRunning() {
				runningName := child.Style.GetPosition().String
				page := context.currentPage
				context.runningElements[runningName][page] = append(context.runningElements[runningName][page], child_)
			}
			continue
		}

		// See https://www.W3.org/TR/css-flexbox-1/#min-size-auto

		var childContainingBlock bo.MaybePoint
		if main == pr.PWidth {
			childContainingBlock = bo.MaybePoint{availableMainSpace, parentBox.Height}
		} else {
			childContainingBlock = bo.MaybePoint{parentBox.Width, availableMainSpace}
		}
		resolvePercentages(child_, childContainingBlock)
		if child.IsTableWrapper {
			tableWrapperWidth(context, child_, childContainingBlock)
		}
		child.PositionX = positionX
		child.PositionY = positionY
		if child.Style.GetMinWidth().S == "auto" {
			specifiedSize := child.Width
			newChild := child_.Copy()
			if bo.ParentT.IsInstance(child_) {
				newChild = bo.CopyWithChildren(child_, child.Children)
			}
			newChild.Box().Style = child.Style.Copy()
			newChild.Box().Style.SetWidth(pr.SToV("auto"))
			newChild.Box().Style.SetMinWidth(pr.ZeroPixels.ToValue())
			newChild.Box().Style.SetMaxWidth(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.ToValue())
			contentSize := minContentWidth(context, newChild, false)
			var transferredSize pr.MaybeFloat
			if replaced, isReplaced := child_.(bo.ReplacedBoxITF); isReplaced {
				image := replaced.Replaced().Replacement
				_, intrinsicHeight, intrinsicRatio := image.GetIntrinsicSize(
					child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
				if pr.Is(intrinsicRatio) && pr.Is(intrinsicHeight) {
					transferredSize = intrinsicHeight.V() * intrinsicRatio.V()
					contentSize = max(child.MinWidth.V(), min(child.MaxWidth.V(), contentSize))
				}
			}
			if specifiedSize != pr.AutoF {
				child.MinWidth = min(specifiedSize.V(), contentSize)
			} else if transferredSize != nil {
				child.MinWidth = min(transferredSize.V(), contentSize)
			} else {
				child.MinWidth = contentSize
			}
		}
		if child.Style.GetMinHeight().S == "auto" {
			specifiedSize := child.Height
			newChild := child_.Copy()
			newChild.Box().Style = child.Style.Copy()
			newChild.Box().Style.SetHeight(pr.SToV("auto"))
			newChild.Box().Style.SetMinHeight(pr.ZeroPixels.ToValue())
			newChild.Box().Style.SetMaxHeight(pr.Dimension{Value: pr.Inf, Unit: pr.Px}.ToValue())
			if cs := newChild.Box().Style; cs.GetWidth().S == "auto" {
				newChildWidth := maxContentWidth(context, newChild, true)
				cs.SetWidth(pr.FToPx(newChildWidth))
			}
			newChild, _, _ = blockLevelLayout(context, newChild.(bo.BlockLevelBoxITF),
				bottomSpace, childSkipStack, parentBox, pageIsEmpty, nil, nil, nil, false, -1)
			var contentSize pr.Float
			if newChild != nil {
				contentSize = newChild.Box().Height.V()
			}
			var transferredSize pr.MaybeFloat
			if replaced, isReplaced := child_.(bo.ReplacedBoxITF); isReplaced {
				image := replaced.Replaced().Replacement
				intrinsicWidth, _, intrinsicRatio := image.GetIntrinsicSize(
					child.Style.GetImageResolution().Value, child.Style.GetFontSize().Value)
				if pr.Is(intrinsicRatio) && pr.Is(intrinsicWidth) {
					transferredSize = intrinsicWidth.V() / intrinsicRatio.V()
					contentSize = max(child.MinHeight.V(), min(child.MaxHeight.V(), contentSize))
				} else if !pr.Is(intrinsicWidth) {
					// TODO: wrongly set by block_level_layout, would be OK with
					// min_content_height.
					contentSize = 0
				}
			}
			if specifiedSize != pr.AutoF {
				child.MinHeight = min(specifiedSize.V(), contentSize)
			} else if transferredSize != nil {
				child.MinHeight = min(transferredSize.V(), contentSize)
			} else {
				child.MinHeight = contentSize
			}
		}

		child.Style = child.Style.Copy()
		var flexBasis pr.DimOrS
		if child.Style.GetFlexBasis().S == "content" {
			flexBasis = pr.SToV("content")
		} else {
			resolved := pr.ResolvePercentage(child.Style.GetFlexBasis(), availableMainSpace)
			if resolved == pr.AutoF {
				flexBasis = getDimOrS(child, main)
				if flexBasis.S == "auto" {
					flexBasis = pr.SToV("content")
				}
			} else {
				flexBasis = resolved.V().ToValue()
			}
		}

		// 3.A If the item has a definite used flex basis…
		if flexBasis.S != "content" {
			child.FlexBaseSize = flexBasis.Value
			if main == pr.PWidth {
				child.MainOuterExtra = (child.BorderLeftWidth + child.BorderRightWidth +
					child.PaddingLeft.V() + child.PaddingRight.V())
				if child.MarginLeft != pr.AutoF {
					child.MainOuterExtra += child.MarginLeft.V()
				}
				if child.MarginRight != pr.AutoF {
					child.MainOuterExtra += child.MarginRight.V()
				}
			} else {
				child.MainOuterExtra = (child.BorderTopWidth + child.BorderBottomWidth +
					child.PaddingTop.V() + child.PaddingBottom.V())
				if child.MarginTop != pr.AutoF {
					child.MainOuterExtra += child.MarginTop.V()
				}
				if child.MarginBottom != pr.AutoF {
					child.MainOuterExtra += child.MarginBottom.V()
				}
			}
		} else if false {
			// TODO: 3.B If the flex item has an intrinsic aspect ratio…
			// TODO: 3.C If the used flex basis is 'content'…
			// TODO: 3.D Otherwise, if the used flex basis is 'content'…
		} else {
			// 3.E Otherwise…
			newChild_ := child_.Copy()
			newChild := newChild_.Box()
			newChild.Style = child.Style.Copy()
			if main == pr.PWidth {
				// … the item’s min and max main sizes are ignored.
				newChild.Style.SetMinWidth(pr.ZeroPixels.ToValue())
				newChild.Style.SetMaxWidth(pr.FToPx(pr.Inf))

				child.FlexBaseSize = maxContentWidth(context, newChild_, false)
				child.MainOuterExtra = maxContentWidth(context, child_, true) - child.FlexBaseSize
			} else {
				// … the item’s min and max main sizes are ignored.
				newChild.Style.SetMinHeight(pr.ZeroPixels.ToValue())
				newChild.Style.SetMaxHeight(pr.FToPx(pr.Inf))

				newChild.Width = pr.Inf
				var tmp blockLayout
				newChild_, tmp, _ = blockLevelLayout(
					context, newChild_.(bo.BlockLevelBoxITF), bottomSpace, childSkipStack, parentBox,
					pageIsEmpty, absoluteBoxes, fixedBoxes, nil, false, -1)
				if newChild != nil {
					// As flex items margins never collapse (with other flex items or
					// with the flex container), we can add the adjoining margins to the
					// child height.
					newChild = newChild_.Box()
					newChild.Height = newChild.Height.V() + collapseMargin(tmp.adjoiningMargins)
					child.FlexBaseSize = newChild.Height.V()
					child.MainOuterExtra = newChild.MarginHeight() - newChild.Height.V()
				} else {
					child.FlexBaseSize, child.MainOuterExtra = 0, 0
				}
			}
		}

		if main == pr.PWidth {
			positionX += child.FlexBaseSize + child.MainOuterExtra
			child.HypotheticalMainSize = max(child.MinWidth.V(), min(child.FlexBaseSize, child.MaxWidth.V()))
		} else {
			positionY += child.FlexBaseSize + child.MainOuterExtra
			child.HypotheticalMainSize = max(child.MinHeight.V(), min(child.FlexBaseSize, child.MaxHeight.V()))
		}

		// Skip stack is only for the first child
		childSkipStack = nil
	}

	// 4 Determine the main size of the flex container using the rules of the formatting
	// context in which it participates.

	if main == pr.PWidth {
		blockLevelWidth(box_, nil, containingBlock)
	} else {
		if box.Height == pr.AutoF {
			box.Height = pr.Float(0)
			for i, child_ := range children {
				child := child_.Box()
				if !child.IsFlexItem {
					continue
				}
				box.Height = child.HypotheticalMainSize + child.MainOuterExtra
				if i != 0 {
					box.Height = box.Height.V() + mainGap
				}
			}
		}
		box.Height = max(box.MinHeight.V(), min(box.Height.V(), box.MaxHeight.V()))
	}

	// 5 If the flex container is single-line, collect all the flex items into a single
	// flex line.
	var flexLines []flexLine
	var line flexLine
	var lineSize pr.Float
	mainSize := getAttr(box, main, "")

	for i := skip; i < len(children); i++ {
		child_ := children[i]
		child := child_.Box()
		if !child.IsFlexItem {
			continue
		}
		lineSize += child.HypotheticalMainSize + child.MainOuterExtra
		if i > skip {
			lineSize += mainGap
		}
		if box.Style.GetFlexWrap() != "nowrap" && lineSize > mainSize.V() {
			if len(line.line) != 0 {
				flexLines = append(flexLines, line)
				line = flexLine{line: []indexedBox{{index: i, box: child_}}}
				lineSize = child.HypotheticalMainSize + child.MainOuterExtra
			} else {
				line.line = append(line.line, indexedBox{index: i, box: child_})
				flexLines = append(flexLines, line)
				line.line = nil
				lineSize = 0
			}
		} else {
			line.line = append(line.line, indexedBox{index: i, box: child_})
		}
	}
	if len(line.line) != 0 {
		flexLines = append(flexLines, line)
	}

	// TODO: handle *-reverse using the terminology from the specification
	if box.Style.GetFlexWrap() == "wrap-reverse" {
		slices.Reverse(flexLines)
	}
	if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
		for _, line := range flexLines {
			line.reverse()
		}
	}

	// 6 Resolve the flexible lengths of all the flex items to find their used main size.
	availableMainSpace = getAttr(box, main, "").V()
	for _, line := range flexLines {
		// 9.7.1 Determine the used flex factor.
		hypotheticalMainSize := line.sum()
		flexFactorType := "shrink"
		if hypotheticalMainSize < availableMainSpace {
			flexFactorType = "grow"
		}

		// 9.7.3 Size inflexible items.
		for _, v := range line.line {
			child := v.box.Box()
			var flexCondition bool
			if flexFactorType == "grow" {
				child.FlexFactor = child.Style.GetFlexGrow()
				flexCondition = child.FlexBaseSize > child.HypotheticalMainSize
			} else {
				child.FlexFactor = child.Style.GetFlexShrink()
				flexCondition = child.FlexBaseSize < child.HypotheticalMainSize
			}
			if child.FlexFactor == 0 || flexCondition {
				child.TargetMainSize = child.HypotheticalMainSize
				child.Frozen = true
			} else {
				child.Frozen = false
			}
		}

		// 9.7.4 Calculate initial free space.
		initialFreeSpace := availableMainSpace
		for i, v := range line.line {
			child := v.box.Box()
			if child.Frozen {
				initialFreeSpace -= child.TargetMainSize + child.MainOuterExtra
			} else {
				initialFreeSpace -= child.FlexBaseSize + child.MainOuterExtra
			}
			if i != 0 {
				initialFreeSpace -= mainGap
			}
		}

		// 9.7.5.a Check for flexible items.
		for !line.allFrozen() {
			var unfrozenFactorSum pr.Float
			remainingFreeSpace := availableMainSpace

			// 9.7.5.b Calculate the remaining free space.
			for i, v := range line.line {
				child := v.box.Box()
				if child.Frozen {
					remainingFreeSpace -= child.TargetMainSize + child.MainOuterExtra
				} else {
					remainingFreeSpace -= child.FlexBaseSize + child.MainOuterExtra
					unfrozenFactorSum += child.FlexFactor
				}
				if i != 0 {
					remainingFreeSpace -= mainGap
				}
			}

			if unfrozenFactorSum < 1 {
				initialFreeSpace *= unfrozenFactorSum
			}

			if initialFreeSpace == pr.Inf {
				initialFreeSpace = math.MaxInt32
			}
			if remainingFreeSpace == pr.Inf {
				remainingFreeSpace = math.MaxInt32
			}

			initialMagnitude := -pr.Inf
			if initialFreeSpace > 0 {
				initialMagnitude = pr.Float(math.Round(math.Log10(float64(initialFreeSpace))))
			}
			remainingMagnitude := -pr.Inf
			if remainingFreeSpace > 0 {
				remainingMagnitude = pr.Float(math.Round(math.Log10(float64(remainingFreeSpace))))
			}
			if initialMagnitude < remainingMagnitude {
				remainingFreeSpace = initialFreeSpace
			}

			// 9.7.5.c Distribute free space proportional to the flex factors.
			if remainingFreeSpace == 0 {
				// If the remaining free space is zero: "Do nothing", but we at least set
				// the flex_base_size as target_main_size for next step.
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						child.TargetMainSize = child.FlexBaseSize
					}
				}
			} else {
				var scaledFlexShrinkFactorsSum, flexGrowFactorsSum pr.Float
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						child.ScaledFlexShrinkFactor = child.FlexBaseSize * child.Style.GetFlexShrink()
						scaledFlexShrinkFactorsSum += child.ScaledFlexShrinkFactor
						flexGrowFactorsSum += child.Style.GetFlexGrow()
					}
				}
				for _, v := range line.line {
					child := v.box.Box()
					if !child.Frozen {
						if flexFactorType == "grow" {
							// If using the flex grow factor…
							ratio := child.Style.GetFlexGrow() / flexGrowFactorsSum
							child.TargetMainSize = child.FlexBaseSize + remainingFreeSpace*ratio
						} else if flexFactorType == "shrink" {
							// If using the flex shrink factor…
							if scaledFlexShrinkFactorsSum == 0 {
								child.TargetMainSize = child.FlexBaseSize
							} else {
								ratio := child.ScaledFlexShrinkFactor / scaledFlexShrinkFactorsSum
								child.TargetMainSize = child.FlexBaseSize + remainingFreeSpace*ratio
							}
						}
						child.TargetMainSize = minMax(v.box, child.TargetMainSize)
					}
				}
			}

			// 9.7.5.d Fix min/max violations.
			for _, v := range line.line {
				child := v.box.Box()
				child.Adjustment = 0
				if !child.Frozen {
					minSize := getAttr(child, main, "min").V()
					maxSize := getAttr(child, main, "max").V()
					minSize = max(minSize, min(child.TargetMainSize, maxSize))
					if child.TargetMainSize < minSize {
						child.Adjustment = minSize - child.TargetMainSize
						child.TargetMainSize = minSize
					}
				}
			}

			// 9.7.5.e Freeze over-flexed items.
			adjustments := line.adjustements()
			for _, v := range line.line {
				child := v.box.Box()
				if adjustments == 0 {
					child.Frozen = true
				} else if adjustments > 0 && child.Adjustment > 0 {
					child.Frozen = true
				} else if adjustments < 0 && child.Adjustment < 0 {
					child.Frozen = true
				}
			}
		}
		// 9.7.6 Set each item’s used main size to its target main size.
		for _, v := range line.line {
			child := v.box.Box()
			if main == pr.PWidth {
				child.Width = child.TargetMainSize
			} else {
				child.Height = child.TargetMainSize
			}
		}
	}

	// 7 Determine the hypothetical cross size of each item.

	var newFlexLines []flexLine
	childSkipStack = skipStack
	for _, line := range flexLines {
		var newFlexLine flexLine
		for _, v := range line.line {
			child_ := v.box
			child := child_.Box()
			// TODO: Fix this value, see test_flex_item_auto_margin_cross.
			if child.MarginTop == pr.AutoF {
				child.MarginTop = pr.Float(0)
			}
			if child.MarginBottom == pr.AutoF {
				child.MarginBottom = pr.Float(0)
			}
			// TODO: Find another way than calling block_level_layout_switch.
			newChild_ := child_.Copy()
			newChild_, tmp, _ := blockLevelLayoutSwitch(context, newChild_.(bo.BlockLevelBoxITF), -pr.Inf, childSkipStack,
				parentBox, pageIsEmpty, absoluteBoxes, fixedBoxes, new([]pr.Float), discard, -1)
			adjoiningMargins := tmp.adjoiningMargins
			child.Baseline = pr.Float(0)
			if bl := findInFlowBaseline(newChild_, false); bl != nil {
				child.Baseline = bl.V()
			}
			if cross == pr.PHeight {
				child.Height = newChild_.Box().Height
				// As flex items margins never collapse (with other flex items or
				// with the flex container), we can add the adjoining margins to the
				// child height.
				child.MarginBottom = child.MarginBottom.V() + collapseMargin(adjoiningMargins)
			} else {
				if child.Width == pr.AutoF {
					minWidth := minContentWidth(context, child_, false)
					maxWidth := maxContentWidth(context, child_, false)
					child.Width = min(max(minWidth, newChild_.Box().Width.V()), maxWidth)
				} else {
					child.Width = newChild_.Box().Width
				}
			}

			newFlexLine.line = append(newFlexLine.line, indexedBox{index: v.index, box: child_})

			// Skip stack is only for the first child
			childSkipStack = nil
		}
		if len(newFlexLine.line) != 0 {
			newFlexLines = append(newFlexLines, newFlexLine)
		}
	}
	flexLines = newFlexLines

	// 8 Calculate the cross size of each flex line.
	crossSize := getAttr(box, cross, "")
	if len(flexLines) == 1 && crossSize != pr.AutoF {
		// If the flex container is single-line…
		flexLines[0].crossSize = crossSize.V()
	} else {
		// Otherwise, for each flex line…
		// 8.1 Collect all the flex items whose inline-axis is parallel to the main-axis…
		for index, line := range flexLines {
			var collectedItems, notCollectedItems []*bo.BoxFields
			for _, v := range line.line {
				child := v.box.Box()
				alignSelf := child.Style.GetAlignSelf()
				collect := strings.HasPrefix(string(box.Style.GetFlexDirection()), "row") && alignSelf.Has(kw.Baseline) &&
					child.MarginTop != pr.AutoF && child.MarginBottom != pr.AutoF
				if collect {
					collectedItems = append(collectedItems, child)
				} else {
					notCollectedItems = append(notCollectedItems, child)
				}
			}
			var crossStartDistance, crossEndDistance pr.Float
			for _, child := range collectedItems {
				baseline := child.Baseline.V() - child.PositionY
				crossStartDistance = max(crossStartDistance, baseline)
				crossEndDistance = max(crossEndDistance, child.MarginHeight()-baseline)
			}
			collectedCrossSize := crossStartDistance + crossEndDistance
			var nonCollectedCrossSize pr.Float
			// 8.2 Find the largest outer hypothetical cross size.
			if len(notCollectedItems) != 0 {
				nonCollectedCrossSize = -pr.Inf
				for _, child := range notCollectedItems {
					var childCrossSize pr.Float
					if cross == pr.PHeight {
						childCrossSize = child.BorderHeight()
						if child.MarginTop != pr.AutoF {
							childCrossSize += child.MarginTop.V()
						}
						if child.MarginBottom != pr.AutoF {
							childCrossSize += child.MarginBottom.V()
						}
					} else {
						childCrossSize = child.BorderWidth()
						if child.MarginLeft != pr.AutoF {
							childCrossSize += child.MarginLeft.V()
						}
						if child.MarginRight != pr.AutoF {
							childCrossSize += child.MarginRight.V()
						}
					}
					nonCollectedCrossSize = max(childCrossSize, nonCollectedCrossSize)
				}
			}
			// 8.3 Set the used cross-size of the flex line.
			flexLines[index].crossSize = max(collectedCrossSize, nonCollectedCrossSize)
		}
	}

	// 8.3 If the flex container is single-line…
	if len(flexLines) == 1 {
		line := flexLines[0]
		minCrossSize := getAttr(box, cross, "min")
		if minCrossSize == pr.AutoF {
			minCrossSize = -pr.Inf
		}
		maxCrossSize := getAttr(box, cross, "max")
		if maxCrossSize == pr.AutoF {
			maxCrossSize = pr.Inf
		}
		line.crossSize = max(minCrossSize.V(), min(line.crossSize, maxCrossSize.V()))
	}

	// 9 Handle 'align-content: stretch'.
	alignContent := box.Style.GetAlignContent()
	if alignContent.Has(kw.Normal) {
		alignContent = pr.JustifyOrAlign{kw.Stretch}
	}
	if alignContent.Has(kw.Stretch) {
		var definiteCrossSize pr.MaybeFloat
		if he := box.Style.GetHeight(); cross == pr.PHeight && he.S != "auto" {
			definiteCrossSize = he.Value
		} else if cross == pr.PWidth {
			if bo.FlexT.IsInstance(box_) {
				if box.Style.GetWidth().S == "auto" {
					definiteCrossSize = availableCrossSpace
				} else {
					definiteCrossSize = box.Style.GetWidth().Value
				}
			}
		}
		if definiteCrossSize != nil {
			extraCrossSize := definiteCrossSize.V()
			for _, line := range flexLines {
				extraCrossSize -= line.crossSize
			}
			extraCrossSize -= pr.Float(len(flexLines)-1) * crossGap

			if extraCrossSize != 0 {
				for i, line := range flexLines {
					line.crossSize += extraCrossSize / pr.Float(len(flexLines))
					flexLines[i] = line
				}
			}
		}
	}

	// TODO: 10 Collapse 'visibility: collapse' items.

	// 11 Determine the used cross size of each flex item.

	alignItems := box.Style.GetAlignItems()
	if alignItems.Has(kw.Normal) {
		alignItems = pr.JustifyOrAlign{kw.Stretch}
	}
	for _, line := range flexLines {
		for _, v := range line.line {
			child := v.box.Box()
			alignSelf := child.Style.GetAlignSelf()
			if alignSelf.Has(kw.Normal) {
				alignSelf = pr.JustifyOrAlign{kw.Stretch}
			} else if alignSelf.Has(kw.Auto) {
				alignSelf = alignItems
			}
			if alignSelf.Has(kw.Stretch) && getDimOrS(child, cross).S == "auto" {
				crossMargins := getCrossMargins(child, cross)
				if !(crossMargins[0] == pr.AutoF || crossMargins[1] == pr.AutoF) {
					crossSize := line.crossSize
					if cross == pr.PHeight {
						crossSize -= child.MarginTop.V() + child.MarginBottom.V() +
							child.PaddingTop.V() + child.PaddingBottom.V() + child.BorderTopWidth.V() + child.BorderBottomWidth.V()
					} else {
						crossSize -= child.MarginLeft.V() + child.MarginRight.V() +
							child.PaddingLeft.V() + child.PaddingRight.V() + child.BorderLeftWidth.V() + child.BorderRightWidth.V()
					}
					if cross == pr.PWidth {
						child.Width = crossSize
					} else {
						child.Height = crossSize
					}
				}
			} // else: Cross size has been set by step 7
		}
	}

	// 12 Distribute any remaining free space.
	originalPositionMain := box.ContentBoxY()
	if main == pr.PWidth {
		originalPositionMain = box.ContentBoxX()
	}
	justifyContent := box.Style.GetJustifyContent()
	if justifyContent.Has(kw.Normal) {
		justifyContent = pr.JustifyOrAlign{kw.FlexStart}
	}
	if strings.HasSuffix(string(box.Style.GetFlexDirection()), "-reverse") {
		if justifyContent.Has(kw.FlexStart) {
			justifyContent = pr.JustifyOrAlign{kw.FlexEnd}
		} else if justifyContent.Has(kw.FlexEnd) {
			justifyContent = pr.JustifyOrAlign{kw.FlexStart}
		} else if justifyContent.Has(kw.Start) {
			justifyContent = pr.JustifyOrAlign{kw.End}
		} else if justifyContent.Has(kw.End) {
			justifyContent = pr.JustifyOrAlign{kw.Start}
		}
	}

	for _, line := range flexLines {
		positionMain := originalPositionMain
		var freeSpace pr.Float
		if main == pr.PWidth {
			freeSpace = box.Width.V()
			for _, v := range line.line {
				child := v.box.Box()
				freeSpace -= child.BorderWidth()
				if child.MarginLeft != pr.AutoF {
					freeSpace -= child.MarginLeft.V()
				}
				if child.MarginRight != pr.AutoF {
					freeSpace -= child.MarginRight.V()
				}
			}
		} else {
			freeSpace = box.Height.V()
			for _, v := range line.line {
				child := v.box.Box()
				freeSpace -= child.BorderHeight()
				if child.MarginTop != pr.AutoF {
					freeSpace -= child.MarginTop.V()
				}
				if child.MarginBottom != pr.AutoF {
					freeSpace -= child.MarginBottom.V()
				}
			}
		}
		freeSpace -= pr.Float(len(line.line)-1) * mainGap

		// 12.1 If the remaining free space is positive…
		var margins pr.Float
		for _, v := range line.line {
			child := v.box.Box()
			if main == pr.PWidth {
				if child.MarginLeft == pr.AutoF {
					margins += 1
				}
				if child.MarginRight == pr.AutoF {
					margins += 1
				}
			} else {
				if child.MarginTop == pr.AutoF {
					margins += 1
				}
				if child.MarginBottom == pr.AutoF {
					margins += 1
				}
			}
		}
		if margins != 0 {
			freeSpace /= margins
			for _, v := range line.line {
				child := v.box.Box()
				if main == pr.PWidth {
					if child.MarginLeft == pr.AutoF {
						child.MarginLeft = freeSpace
					}
					if child.MarginRight == pr.AutoF {
						child.MarginRight = freeSpace
					}
				} else {
					if child.MarginTop == pr.AutoF {
						child.MarginTop = freeSpace
					}
					if child.MarginBottom == pr.AutoF {
						child.MarginBottom = freeSpace
					}
				}
			}
			freeSpace = 0
		}

		if box.Style.GetDirection() == "rtl" && main == pr.PWidth {
			freeSpace = -freeSpace
		}

		// 12.2 Align the items along the main-axis per justify-content.
		if justifyContent.Intersects(kw.FlexEnd, kw.End, kw.Right) {
			positionMain += freeSpace
		} else if justifyContent.Has(kw.Center) {
			positionMain += freeSpace / 2
		} else if justifyContent.Has(kw.SpaceAround) {
			positionMain += freeSpace / pr.Float(len(line.line)) / 2
		} else if justifyContent.Has(kw.SpaceEvenly) {
			positionMain += freeSpace / (pr.Float(len(line.line)) + 1)
		}

		var growths pr.Float
		for _, child := range children {
			growths += child.Box().Style.GetFlexGrow()
		}
		for i, v := range line.line {
			child := v.box.Box()
			if i != 0 {
				positionMain += mainGap
			}
			if main == pr.PWidth {
				child.PositionX = positionMain
				if justifyContent.Has(kw.Stretch) && growths != 0 {
					child.Width = child.Width.V() + freeSpace*child.Style.GetFlexGrow()/growths
				}
			} else {
				child.PositionY = positionMain
			}

			var marginMain pr.Float
			if main == pr.PWidth {
				marginMain = child.MarginWidth()
			} else {
				marginMain = child.MarginHeight()
			}
			if box.Style.GetDirection() == "rtl" && main == pr.PWidth {
				marginMain *= -1
			}
			positionMain += marginMain

			if justifyContent.Has(kw.SpaceAround) {
				positionMain += freeSpace / pr.Float(len(line.line))
			} else if justifyContent.Has(kw.SpaceBetween) {
				if len(line.line) > 1 {
					positionMain += freeSpace / (pr.Float(len(line.line)) - 1)
				}
			} else if justifyContent.Has(kw.SpaceEvenly) {
				positionMain += freeSpace / (pr.Float(len(line.line)) + 1)
			}
		}
	}

	// 13 Resolve cross-axis auto margins.
	if cross == pr.PWidth {
		// Make sure width/margins are no longer "auto", as we did not do it above in
		// step 4.
		blockLevelWidth(box_, context, containingBlock)
	}
	positionCross := box.ContentBoxX()
	if cross == pr.PHeight {
		positionCross = box.ContentBoxY()
	}
	for index, line := range flexLines {
		line.lowerBaseline = -pr.Inf
		// TODO: Don't duplicate this loop
		for _, v := range line.line {
			child := v.box.Box()
			alignSelf := child.Style.GetAlignSelf()
			if alignSelf.Has(kw.Auto) {
				alignSelf = box.Style.GetAlignItems()
			}
			if alignSelf.Has(kw.Baseline) && main == pr.PWidth {
				// TODO: handle vertical text
				child.Baseline = child.Baseline.V() - positionCross
				line.lowerBaseline = max(line.lowerBaseline, child.Baseline.V())
			}
		}
		if line.lowerBaseline == -pr.Inf {
			if len(line.line) != 0 {
				line.lowerBaseline = line.line[0].box.Box().Baseline.V()
			} else {
				line.lowerBaseline = 0
			}
		}
		for _, v := range line.line {
			child := v.box.Box()
			crossMargins := getCrossMargins(child, cross)
			var autoMargins pr.Float
			if crossMargins[0] == pr.AutoF {
				autoMargins += 1
			}
			if crossMargins[1] == pr.AutoF {
				autoMargins += 1
			}
			// If a flex item has auto cross-axis margins…
			if autoMargins != 0 {
				extraCross := line.crossSize
				if cross == pr.PHeight {
					extraCross -= child.BorderHeight()
					if child.MarginTop != pr.AutoF {
						extraCross -= child.MarginTop.V()
					}
					if child.MarginBottom != pr.AutoF {
						extraCross -= child.MarginBottom.V()
					}
				} else {
					extraCross -= child.BorderWidth()
					if child.MarginLeft != pr.AutoF {
						extraCross -= child.MarginLeft.V()
					}
					if child.MarginRight != pr.AutoF {
						extraCross -= child.MarginRight.V()
					}
				}
				if extraCross > 0 {
					// If its outer cross size is less than the cross size…
					extraCross /= autoMargins
					if cross == pr.PHeight {
						if child.Style.GetMarginTop().S == "auto" {
							child.MarginTop = extraCross
						}
						if child.Style.GetMarginBottom().S == "auto" {
							child.MarginBottom = extraCross
						}
					} else {
						if child.Style.GetMarginLeft().S == "auto" {
							child.MarginLeft = extraCross
						}
						if child.Style.GetMarginRight().S == "auto" {
							child.MarginRight = extraCross
						}
					}
				} else {
					// Otherwise…
					if cross == pr.PHeight {
						if child.MarginTop == pr.AutoF {
							child.MarginTop = pr.Float(0)
						}
						child.MarginBottom = extraCross
					} else {
						if child.MarginLeft == pr.AutoF {
							child.MarginLeft = pr.Float(0)
						}
						child.MarginRight = extraCross
					}
				}
			} else {
				// 14 Align all flex items along the cross-axis.
				alignSelf := child.Style.GetAlignSelf()
				if alignSelf.Has(kw.Normal) {
					alignSelf = pr.JustifyOrAlign{kw.Stretch}
				} else if alignSelf.Has(kw.Auto) {
					alignSelf = alignItems
				}
				if cross == pr.PHeight {
					child.PositionY = positionCross
				} else {
					child.PositionX = positionCross
				}
				if alignSelf.Intersects(kw.End, kw.SelfEnd, kw.FlexEnd) {
					if cross == pr.PHeight {
						child.PositionY += line.crossSize - child.MarginHeight()
					} else {
						child.PositionX += line.crossSize - child.MarginWidth()
					}
				} else if alignSelf.Has(kw.Center) {
					if cross == pr.PHeight {
						child.PositionY += (line.crossSize - child.MarginHeight()) / 2
					} else {
						child.PositionX += (line.crossSize - child.MarginWidth()) / 2
					}
				} else if alignSelf.Has(kw.Baseline) {
					if cross == pr.PHeight {
						child.PositionY += line.lowerBaseline - child.Baseline.V()
					}
					// TODO: Handle vertical text.
				} else if alignSelf.Has(kw.Stretch) {
					if getDimOrS(child, cross).S == "auto" {
						var margins pr.Float
						if cross == pr.PHeight {
							margins = child.MarginTop.V() + child.MarginBottom.V()
						} else {
							margins = child.MarginLeft.V() + child.MarginRight.V()
						}
						if child.Style.GetBoxSizing() == "content-box" {
							if cross == pr.PHeight {
								margins += child.BorderTopWidth.V() + child.BorderBottomWidth.V() +
									child.PaddingTop.V() + child.PaddingBottom.V()
							} else {
								margins += child.BorderLeftWidth.V() + child.BorderRightWidth.V() +
									child.PaddingLeft.V() + child.PaddingRight.V()
							}
						}
					}
				}
			}
		}
		positionCross += line.crossSize
		flexLines[index] = line
	}

	// 15 Determine the flex container’s used cross size.
	// TODO: Use the updated algorithm.
	if getDimOrS(box, cross).S == "auto" {
		// Otherwise, use the sum of the flex lines' cross sizes…
		// TODO: Handle min-max.
		// TODO: What about align-content here?
		crossSize := sumCross(flexLines)
		crossSize += pr.Float(len(flexLines)-1) * crossGap
		if cross == pr.PHeight {
			box.Height = crossSize
		} else {
			box.Width = crossSize
		}
	}
	if len(flexLines) > 1 {
		// 15 If the cross size property is a definite size, use that…
		extraCrossSize := getAttr(box, cross, "").V()
		extraCrossSize -= sumCross(flexLines)
		extraCrossSize -= pr.Float(len(flexLines)-1) * crossGap
		// 16 Align all flex lines per align-content.
		var crossTranslate pr.Float
		direction := directionX
		if cross == pr.PHeight {
			direction = directionY
		}
		for i, line := range flexLines {
			flexItems := line.flexItems()
			if i != 0 {
				crossTranslate += crossGap
			}
			var currentValue pr.Float
			for _, child := range flexItems {
				currentValue = child.PositionX
				if direction == directionY {
					currentValue = child.PositionY
				}
				currentValue += crossTranslate
				setDirection(child, direction, currentValue)
			}
			if extraCrossSize == 0 {
				continue
			}
			for _, child := range flexItems {
				switch {
				case alignContent.Intersects(kw.End, kw.FlexEnd):
					setDirection(child, direction, currentValue+extraCrossSize)
				case alignContent.Has(kw.Center):
					setDirection(child, direction, currentValue+extraCrossSize/2)
				case alignContent.Has(kw.SpaceAround):
					setDirection(child, direction, currentValue+extraCrossSize/pr.Float(len(flexLines))/2)
				case alignContent.Has(kw.SpaceEvenly):
					setDirection(child, direction, currentValue+extraCrossSize/(pr.Float(len(flexLines))+1))
				}
			}
			switch {
			case alignContent.Has(kw.SpaceBetween):
				crossTranslate += extraCrossSize / (pr.Float(len(flexLines)) - 1)
			case alignContent.Has(kw.SpaceAround):
				crossTranslate += extraCrossSize / pr.Float(len(flexLines))
			case alignContent.Has(kw.SpaceEvenly):
				crossTranslate += extraCrossSize / (pr.Float(len(flexLines)) + 1)
			}
		}
	}

	// Now we are no longer in the flex algorithm.
	var newChildren []Box
	for _, child := range children {
		if child.Box().IsAbsolutelyPositioned() {
			newChildren = append(newChildren, child)
		}
	}
	box_ = bo.CopyWithChildren(box_, newChildren)
	box = box_.Box()
	childSkipStack = skipStack
	for _, line := range flexLines {
		for _, v := range line.line {
			i, child := v.index, v.box.Box()
			if child.IsFlexItem {
				// TODO: Don't use block_level_layout_switch.
				newChild, tmp, _ := blockLevelLayoutSwitch(context, v.box.(bo.BlockLevelBoxITF), bottomSpace, childSkipStack, box,
					pageIsEmpty, absoluteBoxes, fixedBoxes, new([]pr.Float), discard, -1)
				childResumeAt := tmp.resumeAt
				if newChild == nil {
					if resumeAt != nil {
						if resumeIndex, _ := resumeAt.Unpack(); resumeIndex != 0 {
							resumeAt = tree.ResumeStack{resumeIndex + i - 1: nil}
						}
					}
				} else {
					box.Children = append(box.Children, newChild)
					if childResumeAt != nil {
						firstLevelSkip := 0
						if originalSkipStack != nil {
							firstLevelSkip, _ = originalSkipStack.Unpack()
						}
						if resumeAt != nil {
							resumeIndex, _ := resumeAt.Unpack()
							firstLevelSkip += resumeIndex
						}
						resumeAt = tree.ResumeStack{firstLevelSkip + i: childResumeAt}
					}
				}
				if resumeAt != nil {
					break
				}
			}

			// Skip stack is only for the first child
			childSkipStack = nil
		}
		if resumeAt != nil {
			break
		}
	}

	if box.Style.GetPosition().String == "relative" {
		// New containing block, resolve the layout of the absolute descendants.
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, box_, fixedBoxes, bottomSpace, nil)
		}
	}

	// TODO: Use real algorithm, see https://www.w3.org/TR/css-flexbox-1/#flex-baselines.
	if bo.InlineFlexT.IsInstance(box_) {
		if main == pr.PWidth { // and main text direction is horizontal
			if len(flexLines) != 0 {
				box.Baseline = flexLines[0].lowerBaseline
			} else {
				box.Baseline = pr.Float(0)
			}
		} else {
			box.Baseline = pr.Float(0)
			for _, child := range box.Children {
				if child.Box().IsInNormalFlow() {
					var val pr.MaybeFloat
					if len(box.Children) != 0 {
						val = findInFlowBaseline(child, false)
					}
					if val != nil {
						box.Baseline = val.V()
					} else {
						box.Baseline = pr.Float(0)
					}
					break
				}
			}
		}
	}

	box_.RemoveDecoration(box, false, resumeAt != nil && !discard)

	context.finishFlexFormattingContext(box_)

	// TODO: check these returned values
	return box_, blockLayout{
		resumeAt:          resumeAt,
		nextPage:          tree.PageBreak{Break: "any"},
		adjoiningMargins:  nil,
		collapsingThrough: false,
	}
}
