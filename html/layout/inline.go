package layout

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/benoitkugler/webrender/logger"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/utils"
	"github.com/benoitkugler/webrender/utils/testutils/tracer"

	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/tree"
)

//     Line breaking and layout for inline-level boxes.

// IsLine returns wheter `box` is either
// an instance of LineBox or InlineBox.
func IsLine(box Box) bool {
	return bo.LineT.IsInstance(box) || bo.InlineT.IsInstance(box)
}

type lineBoxe struct {
	// laid-out LineBox with as much content as possible that
	// fits in the available width.
	line     *bo.LineBox
	resumeAt tree.ResumeStack
}

type lineBoxeIterator struct {
	firstLetterStyle pr.ElementStyle

	currentBox lineBoxe

	box                    *bo.LineBox
	containingBlock        Box
	fixedBoxes             *[]*AbsolutePlaceholder
	skipStack              tree.ResumeStack
	context                *layoutContext
	absoluteBoxes          *[]*AbsolutePlaceholder
	positionY, bottomSpace pr.Float

	resetTextIndent bool
	done            bool
}

func (l *lineBoxeIterator) Has() bool {
	if l.resetTextIndent {
		l.box.TextIndent = pr.Float(0)
	}
	if l.done {
		return false
	}
	line, resumeAt := getNextLinebox(l.context, l.box, l.positionY, l.bottomSpace, l.skipStack, l.containingBlock,
		l.absoluteBoxes, l.fixedBoxes, l.firstLetterStyle)

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("lineBoxeIterator.Has: %s", resumeAt))
	}

	if line != nil {
		handleLeader(l.context, line, l.containingBlock.Box())
		l.positionY = line.Box().PositionY + line.Box().Height.V()
	}
	if line == nil {
		l.done = true
		return false
	}
	l.currentBox = lineBoxe{line: line, resumeAt: resumeAt}
	if resumeAt == nil {
		l.done = true
	}
	l.skipStack = resumeAt
	l.resetTextIndent = true
	l.firstLetterStyle = nil
	return true
}

func (l *lineBoxeIterator) Next() lineBoxe { return l.currentBox }

func (l *lineBoxeIterator) consume() int {
	count := 0
	for l.Has() {
		count++
	}
	return count
}

// `box` is a non-laid-out `LineBox`
// positionY is the vertical top position of the line box on the page
// skipStack is “nil“ to start at the beginning of “linebox“,
// or a “resumeAt“ value to continue just after an
// already laid-out line.
func iterLineBoxes(context *layoutContext, box *bo.LineBox, positionY, bottomSpace pr.Float, skipStack tree.ResumeStack, containingBlock Box,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder, firstLetterStyle pr.ElementStyle,
) *lineBoxeIterator {
	resolvePercentages(box, bo.MaybePoint{containingBlock.Box().Width, containingBlock.Box().Height})
	if skipStack == nil {
		// TODO: wrong, see https://github.com/Kozea/WeasyPrint/issues/679
		box.TextIndent = resolveOnePercentage(box.Style.GetTextIndent(), pr.PTextIndent, containingBlock.Box().Width.V())
	} else {
		box.TextIndent = pr.Float(0)
	}
	return &lineBoxeIterator{
		box: box, context: context, positionY: positionY, bottomSpace: bottomSpace, containingBlock: containingBlock,
		absoluteBoxes: absoluteBoxes, fixedBoxes: fixedBoxes, skipStack: skipStack, firstLetterStyle: firstLetterStyle,
	}
}

func getNextLinebox(context *layoutContext, linebox *bo.LineBox, positionY, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock_ Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	firstLetterStyle pr.ElementStyle,
) (line_ *bo.LineBox, resumeAt tree.ResumeStack) {
	if traceMode {
		traceLogger.DumpTree(linebox, "getNextLinebox")
	}

	containingBlock := containingBlock_.Box()
	skipStack, cont := skipFirstWhitespace(linebox, skipStack)
	if cont {
		return
	}

	skipStack = firstLetterToBox(context, linebox, skipStack, firstLetterStyle)

	linebox.PositionY = positionY

	if len(context.excludedShapes.list) != 0 {
		// Width and height must be calculated to avoid floats
		linebox.Width = inlineMinContentWidth(context, linebox, true, skipStack, true, false)
		linebox.Height, _ = text.StrutLayout(linebox.Style, context)
	} else {
		// No float, width and height will be set by the lines
		linebox.Width = pr.Float(0)
		linebox.Height = pr.Float(0)
	}
	positionX, positionY, availableWidth := avoidCollisions(context, linebox, containingBlock, false)

	candidateHeight := linebox.Height

	excludedShapes := shapeList{list: append([]*bo.BoxFields{}, context.excludedShapes.list...), isFrozen: context.excludedShapes.isFrozen}

	var (
		linePlaceholders, lineAbsolutes, lineFixed []*AbsolutePlaceholder
		waitingFloats                              []Box
	)
	for {
		linebox.PositionX, linebox.PositionY = positionX, positionY
		originalPositionX, originalPositionY := positionX, positionY
		originalWidth := linebox.Width.V()
		waitingFloats = waitingFloats[:0] // reset

		maxX := positionX + availableWidth
		positionX += linebox.TextIndent.V()

		var preservedLineBreak bool

		spi := splitInlineBox(context, linebox, positionX, maxX, bottomSpace, skipStack, containingBlock_,
			&lineAbsolutes, &lineFixed, &linePlaceholders, &waitingFloats, nil)
		resumeAt, preservedLineBreak = spi.resumeAt, spi.preservedLineBreak
		line_ = spi.newBox.(*bo.LineBox) // splitInlineBox preserve the concrete type

		if traceMode {
			traceLogger.DumpTree(line_, "line in getNextLinebox")
		}

		line := line_.Box()
		linebox.Width, linebox.Height = line.Width, line.Height

		if isPhantomLinebox(line) && !preservedLineBreak {
			line.Height = pr.Float(0)
			break
		}

		removeLastWhitespace(context, line_)

		newPositionX, _, newAvailableWidth := avoidCollisions(context, linebox, containingBlock, false)
		alignmentAvailableWidth := newAvailableWidth + newPositionX - linebox.PositionX
		offsetX := textAlign(context, line_, alignmentAvailableWidth, resumeAt == nil || preservedLineBreak)

		if traceMode {
			traceLogger.DumpTree(line_, "line in getNextLinebox after textAlign")
		}

		if containingBlock.Style.GetDirection() == pr.Rtl {
			offsetX = -offsetX
			offsetX -= line.Width.V()
		}

		bottom, top := lineBoxVerticality(context, line_)
		line.Baseline = -top
		line.PositionY = top
		line.Height = bottom - top
		offsetY := positionY - top
		line.MarginTop = pr.Float(0)
		line.MarginBottom = pr.Float(0)

		line_.Translate(offsetX, offsetY, false)
		// Avoid floating point errors, as positionY - top + top != positionY
		// Removing this line breaks the position == linebox.Position test below
		// See https://github.com/Kozea/WeasyPrint/issues/583
		line.PositionY = positionY

		if line.Height.V() <= candidateHeight.V() {
			break
		}
		candidateHeight = line.Height

		newExcludedShapes := context.excludedShapes
		context.excludedShapes = &excludedShapes
		positionX, positionY, availableWidth = avoidCollisions(context, line_, containingBlock, false)
		var condition bool
		if containingBlock.Style.GetDirection() == pr.Ltr {
			condition = positionX == originalPositionX && positionY == originalPositionY
		} else {
			condition = positionX+line.Width.V() == originalPositionX+originalWidth && positionY == originalPositionY
		}

		if condition {
			context.excludedShapes = newExcludedShapes
			break
		}
	}

	if traceMode {
		traceLogger.DumpTree(line_, "after loop in getNextLinebox")
	}

	*absoluteBoxes = append(*absoluteBoxes, lineAbsolutes...)
	*fixedBoxes = append(*fixedBoxes, lineFixed...)

	line := line_.Box()
	for _, placeholder := range linePlaceholders {
		if placeholder.Box().Style.Specified().Display.Has(pr.Inline) {
			// Inline-level static position :
			placeholder.Translate(0, positionY-placeholder.Box().PositionY.V(), false)
		} else {
			// Block-level static position: at the start of the next line
			placeholder.Translate(line.PositionX-placeholder.Box().PositionX.V(),
				positionY+line.Height.V()-placeholder.Box().PositionY.V(), false)
		}
	}

	var floatChildren []Box
	waitingFloatsY := line.PositionY + line.Height.V()
	for _, waitingFloat_ := range waitingFloats {
		waitingFloat := waitingFloat_.Box()
		waitingFloat.PositionY = waitingFloatsY
		newWaitingFloat, waitingFloatResumeAt := floatLayout(context, waitingFloat_, containingBlock, absoluteBoxes, fixedBoxes, bottomSpace, nil)

		if traceMode {
			traceLogger.DumpTree(newWaitingFloat, "after float layout")
		}

		floatChildren = append(floatChildren, newWaitingFloat)
		if waitingFloatResumeAt != nil {
			context.brokenOutOfFlow[newWaitingFloat] = brokenBox{box: waitingFloat_, containingBlock: containingBlock_, bfcRoot: context.currentBFCRoot(), resumeAt: waitingFloatResumeAt}
		}
	}
	line.Children = append(line.Children, floatChildren...)

	return line_, resumeAt
}

// Return the “skipStack“ to start just after the removed spaces
// at the beginning of the line.
// See https://www.w3.org/TR/CSS21/text.html#white-space-model
func skipFirstWhitespace(box Box, skipStack tree.ResumeStack) (tree.ResumeStack, bool) {
	var (
		index         int
		nextSkipStack tree.ResumeStack
	)
	if skipStack != nil {
		index, nextSkipStack = skipStack.Unpack()
	}
	if textBox, ok := box.(*bo.TextBox); ok {
		if nextSkipStack != nil {
			panic(fmt.Sprintf("expected nil nextSkipStack, got %v", nextSkipStack))
		}
		whiteSpace := textBox.Style.GetWhiteSpace()
		text := textBox.Text
		length := len(text)
		if index == length {
			// Starting a the end of the TextBox, no text to see: Continue
			return nil, true
		}
		if whiteSpace == pr.Normal || whiteSpace == pr.Nowrap || whiteSpace == pr.PreLine {
			for index < length && text[index] == ' ' {
				index += 1
			}
		}
		if index != 0 {
			return tree.ResumeStack{index: nil}, false
		}
		return nil, false
	}

	if IsLine(box) {
		children := box.Box().Children
		if index == 0 && len(children) == 0 {
			return nil, false
		}
		if index >= len(children) {
			// Skip stack points past the last child (this can happen on
			// repagination of an absolutely-positioned box whose children
			// were all consumed on the previous page). Treat as fully
			// consumed and tell the caller to continue.
			return nil, true
		}
		result, cont := skipFirstWhitespace(children[index], nextSkipStack)
		if cont {
			index += 1
			if index >= len(children) {
				return nil, true
			}
			result, _ = skipFirstWhitespace(children[index], nil)
		}
		if index != 0 || result != nil {
			return tree.ResumeStack{index: result}, false
		}
		return nil, false
	}

	if skipStack != nil {
		panic(fmt.Sprintf("unexpected skip inside %s", box.Type()))
	}

	return nil, false
}

// Remove in place space characters at the end of a line.
// This also reduces the width of the inline parents of the modified text.
func removeLastWhitespace(context *layoutContext, line *bo.LineBox) {
	var ancestors []Box
	box := Box(line)
	for IsLine(box) {
		ancestors = append(ancestors, box)
		ch := box.Box().Children
		if len(ch) == 0 {
			return
		}
		box = ch[len(ch)-1]
	}
	textBox, ok := box.(*bo.TextBox)
	if ws := box.Box().Style.GetWhiteSpace(); !(ok && (ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine)) {
		return
	}
	newText := text.TrimSuffix(textBox.Text, ' ')
	firstLineIsRTL := textBox.FirstLineIsRTL
	var spaceWidth pr.Float
	if L := len(newText); L != 0 {
		if L == len(textBox.Text) {
			return
		}
		textBox.Text = newText
		newBox, resume, _ := splitTextBox(context, textBox, nil, 0, true)
		if newBox == nil || resume != -1 {
			panic(fmt.Sprintf("expected newBox and no resume, got %v, %v", newBox, resume))
		}
		spaceWidth = textBox.Width.V() - newBox.Box().Width.V()
		textBox.Width = newBox.Box().Width
	} else {
		spaceWidth = textBox.Width.V()
		textBox.Width = pr.Float(0)
		textBox.Text = nil
		// We don't have layout for an empty box, so fall back to the
		// line's CSS direction.
		firstLineIsRTL = line.Style.GetDirection() == pr.Rtl
	}

	// RTL line, the trailing space is at the left of the box. Translate
	// every direct child of the line to align the stripped text with the
	// right edge of the box.
	if firstLineIsRTL {
		for _, child := range line.Children {
			child.Translate(-spaceWidth, 0, true)
		}
	}

	for _, ancestor := range ancestors {
		ancestor.Box().Width = ancestor.Box().Width.V() - spaceWidth
	}

	// TODO: All tabs (U+0009) are rendered as a horizontal shift that
	// lines up the start edge of the next glyph with the next tab stop.
	// Tab stops occur at points that are multiples of 8 times the width
	// of a space (U+0020) rendered in the block"s font from the block"s
	// starting content edge.

	// TODO: If spaces (U+0020) or tabs (U+0009) at the end of a line have
	// "white-space" set to "pre-wrap", UAs may visually collapse them.
}

// Create a box for the ::first-letter selector.
func firstLetterToBox(context *layoutContext, box Box, skipStack tree.ResumeStack, firstLetterStyle pr.ElementStyle) tree.ResumeStack {
	if firstLetterStyle == nil || len(box.Box().Children) == 0 {
		return skipStack
	}

	// Some properties must be ignored :in first-letter boxes.
	// https://drafts.csswg.org/selectors-3/#application-in-css
	// At least, position is ignored to avoid layout troubles.
	firstLetterStyle.SetPosition(pr.BoolString{String: "static"})

	firstLetter := ""
	child := box.Box().Children[0]
	var childSkipStack tree.ResumeStack
	if textBox, ok := child.(*bo.TextBox); ok {
		letterStyle := tree.ComputedFromCascaded(nil, nil, firstLetterStyle, context)
		if strings.HasSuffix(textBox.ElementTag(), "::first-letter") {
			letterBox := bo.NewInlineBox(letterStyle, textBox.Element, "first-letter", []Box{child})
			box.Box().Children[0] = letterBox
		} else if len(textBox.Text) != 0 {
			text := textBox.Text
			characterFound := false
			if skipStack != nil {
				_, childSkipStack = skipStack.Unpack()
				if childSkipStack != nil {
					index, _ := childSkipStack.Unpack()
					text = text[index:]
					skipStack = nil
				}
			}
			for len(text) != 0 {
				nextLetter := text[0]
				isPunc := unicode.In(nextLetter, bo.TableFirstLetter...)
				if !isPunc {
					if characterFound {
						break
					}
					characterFound = true
				}
				firstLetter += string(nextLetter)
				text = text[1:]
			}
			textBox.Text = text
			if strings.TrimLeft(firstLetter, "\n") != "" {
				// "This type of initial letter is similar to an
				// inline-level element if its "float" property is "none",
				// otherwise it is similar to a floated element."
				//
				// Per WP commit 9b6b5d02: when the first-letter floats,
				// its inner TextBox/LineBox children must use an
				// anonymous style derived from the letter style, not
				// the letter style itself. Sharing the letter style
				// would let "float" propagate into the children, which
				// then crashes during inline layout.
				childrenStyle := tree.NewAnonymousStyle(letterStyle)
				if firstLetterStyle.GetFloat() == "none" {
					letterBox := bo.NewInlineBox(firstLetterStyle, textBox.Element, "first-letter", nil)
					textBox = bo.NewTextBox(childrenStyle, textBox.Element, "first-letter", []rune(firstLetter))
					letterBox.Children = []Box{textBox}
					textBox.Children = append([]Box{letterBox}, textBox.Children...)
				} else {
					letterBox := bo.NewBlockBox(firstLetterStyle, textBox.Element, "first-letter", nil)
					letterBox.FirstLetterStyle = nil
					lineBox := bo.NewLineBox(childrenStyle, textBox.Element, "first-letter", nil)
					letterBox.Children = []Box{&lineBox}
					textBox = bo.NewTextBox(childrenStyle, textBox.Element, "first-letter", []rune(firstLetter))
					lineBox.Children = []Box{textBox}
					textBox.Children = append([]Box{letterBox}, textBox.Children...)
				}
				bo.ProcessTextTransform(textBox)
				if skipStack != nil && childSkipStack != nil {
					index, _ := skipStack.Unpack()
					childIndex, grandChildSkipStack := childSkipStack.Unpack()
					skipStack = tree.ResumeStack{index: tree.ResumeStack{childIndex + 1: grandChildSkipStack}}
				}
			}
		}
	} else if bo.ParentT.IsInstance(child) {
		if skipStack != nil {
			_, childSkipStack = skipStack.Unpack()
		} else {
			childSkipStack = nil
		}
		childSkipStack = firstLetterToBox(context, child, childSkipStack, firstLetterStyle)
		if skipStack != nil {
			resumeIndex, _ := skipStack.Unpack()
			skipStack = tree.ResumeStack{resumeIndex: childSkipStack}
		}
	}
	return skipStack
}

func resolveMarginAuto(box *bo.BoxFields) {
	if box.MarginTop == pr.AutoF {
		box.MarginTop = pr.Float(0)
	}
	if box.MarginRight == pr.AutoF {
		box.MarginRight = pr.Float(0)
	}
	if box.MarginBottom == pr.AutoF {
		box.MarginBottom = pr.Float(0)
	}
	if box.MarginLeft == pr.AutoF {
		box.MarginLeft = pr.Float(0)
	}
}

// Compute the width and the height of the atomic “box“.
func atomicBox(context *layoutContext, box_ Box, positionX pr.Float, skipStack tree.ResumeStack, containingBlock *bo.BoxFields,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) Box {
	if _, ok := box_.(bo.ReplacedBoxITF); ok {
		box_ = box_.Copy()
		inlineReplacedBoxLayout(box_, containingBlock)
		box_.Box().Baseline = box_.Box().MarginHeight()
	} else if bo.InlineBlockT.IsInstance(box_) {
		var width, minWidth, maxWidth pr.MaybeFloat
		if box := box_.Box(); box.IsTableWrapper {
			tableWrapperWidth(context, box_, bo.MaybePoint{containingBlock.Width, containingBlock.Height})
			width, minWidth, maxWidth = box.Width, box.MinWidth, box.MaxWidth
		}
		box_ = inlineBlockBoxLayout(context, box_, positionX, skipStack, containingBlock,
			absoluteBoxes, fixedBoxes)
		if box := box_.Box(); box.IsTableWrapper {
			box.Width, box.MinWidth, box.MaxWidth = width, minWidth, maxWidth
		}
	} else {
		panic(fmt.Sprintf("Layout for %s not handled yet", box_))
	}
	return box_
}

func inlineBlockBoxLayout(context *layoutContext, box_ Box, positionX pr.Float, skipStack tree.ResumeStack,
	containingBlock *bo.BoxFields, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
) Box {
	resolvePercentagesBox(box_, containingBlock)
	box := box_.Box()
	// https://www.w3.org/TR/CSS21/visudet.html#inlineblock-width
	if box.MarginLeft == pr.AutoF {
		box.MarginLeft = pr.Float(0)
	}
	if box.MarginRight == pr.AutoF {
		box.MarginRight = pr.Float(0)
	}
	// https://www.w3.org/TR/CSS21/visudet.html#block-root-margin
	if box.MarginTop == pr.AutoF {
		box.MarginTop = pr.Float(0)
	}
	if box.MarginBottom == pr.AutoF {
		box.MarginBottom = pr.Float(0)
	}

	inlineBlockWidth(box_, context, containingBlock)


	box.PositionX = positionX
	box.PositionY = 0
	box_, _, _ = blockContainerLayout(context, box_, -pr.Inf, skipStack,
		true, absoluteBoxes, fixedBoxes, new([]pr.Float), false, -1)
	box_.Box().Baseline = inlineBlockBaseline(box_)
	return box_
}

// Return the y position of the baseline for an inline block
// from the top of its margin box.
// https://www.w3.org/TR/CSS21/visudet.html#propdef-vertical-align
func inlineBlockBaseline(box_ Box) pr.Float {
	box := box_.Box()
	if box.IsTableWrapper {
		// Inline table's baseline is its first row's baseline
		for _, child := range box.Children {
			if bo.TableT.IsInstance(child) {
				if cc := child.Box().Children; len(cc) != 0 && len(cc[0].Box().Children) != 0 {
					firstRow := cc[0].Box().Children[0]
					return firstRow.Box().Baseline.V()
				}
			}
		}
	} else if box.Style.GetOverflow() == "visible" {
		result := findInFlowBaseline(box_, true)
		if pr.Is(result) {
			return result.V()
		}
	}
	return box.PositionY + box.MarginHeight()
}

var inlineBlockWidth = handleMinMaxWidth(inlineBlockWidth_)

// @handleMinMaxWidth
func inlineBlockWidth_(box_ Box, context *layoutContext, containingBlock containingBlock) (bool, pr.Float) {
	if box := box_.Box(); box.Width == pr.AutoF {
		cbWidth, _ := containingBlock.ContainingBlock()
		availableContentWidth := cbWidth.V() - (box.MarginLeft.V() + box.MarginRight.V() +
			box.BorderLeftWidth.V() + box.BorderRightWidth.V() +
			box.PaddingLeft.V() + box.PaddingRight.V())
		box.Width = shrinkToFit(context, box_, availableContentWidth)
	}
	return false, 0
}

type widths struct {
	left, right pr.Float
}

func (w *widths) add(key pr.String, value pr.Float) {
	switch key {
	case "left":
		w.left += value
	case "right":
		w.right += value
	default:
		panic("unexpected key " + key)
	}
}

type splitedInline struct {
	newBox                  Box
	resumeAt                tree.ResumeStack
	preservedLineBreak      bool
	firstLetter, lastLetter rune // 0 for none
	floatWidths             widths
}

// Fit as much content as possible from an inline-level box in a width.
//
// Return “(newBox, resumeAt, preservedLineBreak, firstLetter,
// lastLetter)“. “resumeAt“ is “nil“ if all of the content
// fits. Otherwise it can be passed as a “skipStack“ parameter to resume
// where we left off.
//
// “newBox“ is non-empty (unless the box is empty) and as big as possible
// while being narrower than “availableWidth“, if possible (may overflow
// is no split is possible.)
func splitInlineLevel(context *layoutContext, box_ Box, positionX, maxX, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock Box, absoluteBoxes, fixedBoxes,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
) splitedInline {
	if traceMode {
		traceLogger.Indent()
		defer traceLogger.DeIndent()
		traceLogger.DumpTree(box_, "splitInlineLevel")
	}

	box := box_.Box()
	resolvePercentagesBox(box_, containingBlock.Box())
	floatWidths := widths{}
	var (
		newBox                  Box
		preservedLineBreak      bool
		resumeAt                tree.ResumeStack
		firstLetter, lastLetter rune
	)

	if textBox, ok := box_.(*bo.TextBox); ok {
		textBox.PositionX = positionX
		skip := 0
		if skipStack != nil {
			skip, skipStack = skipStack.Unpack()
			if skipStack != nil {
				panic(fmt.Sprintf("expected empty skipStack, got %v", skipStack))
			}
		}
		var newTextBox *bo.TextBox
		isLineStart := len(lineChildren) == 0
		newTextBox, skip, preservedLineBreak = splitTextBox(context, textBox, maxX-positionX, skip, isLineStart)
		if skip != -1 {
			resumeAt = tree.ResumeStack{skip: nil}
		}
		if newTextBox != nil { // we dont want a non nil interface value with a nil pointer
			newBox = newTextBox
		}
		if text := textBox.Text; len(text) != 0 {
			firstLetter = text[0]
			if skip == -1 {
				lastLetter = text[len(text)-1]
			} else {
				lastLetter = text[skip-1]
			}
		}
	} else if bo.InlineT.IsInstance(box_) {
		if box.MarginLeft == pr.AutoF {
			box.MarginLeft = pr.Float(0)
		}
		if box.MarginRight == pr.AutoF {
			box.MarginRight = pr.Float(0)
		}
		tmp := splitInlineBox(context, box_, positionX, maxX, bottomSpace, skipStack, containingBlock,
			absoluteBoxes, fixedBoxes, linePlaceholders, waitingFloats, lineChildren)
		newBox, resumeAt, preservedLineBreak, firstLetter, lastLetter, floatWidths = tmp.newBox, tmp.resumeAt, tmp.preservedLineBreak, tmp.firstLetter, tmp.lastLetter, tmp.floatWidths
	} else if bo.AtomicInlineLevelT.IsInstance(box_) {
		newBox = atomicBox(context, box_, positionX, skipStack, containingBlock.Box(), absoluteBoxes, fixedBoxes)
		newBox.Box().PositionX = positionX
		resumeAt = nil
		preservedLineBreak = false
		// See https://www.w3.org/TR/css-text-3/#line-breaking
		// Atomic inlines behave like ideographic characters.
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else if bo.InlineFlexT.IsInstance(box_) {
		box.PositionX = positionX
		box.PositionY = 0
		resolveMarginAuto(box)
		var v blockLayout
		newBox, v = flexLayout(context, box_, -pr.Inf, skipStack,
			containingBlock.Box(), false, absoluteBoxes, fixedBoxes, false)
		resumeAt = v.resumeAt
		preservedLineBreak = false
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else if bo.InlineGridT.IsInstance(box_) {
		box.PositionX = positionX
		box.PositionY = 0
		resolveMarginAuto(box)
		var v blockLayout
		newBox, v = gridLayout(context, box_, -pr.Inf, skipStack, containingBlock.Box(), false, absoluteBoxes, fixedBoxes)
		resumeAt = v.resumeAt
		preservedLineBreak = false
		firstLetter = '\u2e80'
		lastLetter = '\u2e80'
	} else { // pragma: no cover
		logger.WarningLogger.Printf("Layout for %v not handled yet", box)
		return splitedInline{}
	}

	if traceMode {
		traceLogger.DumpTree(newBox, fmt.Sprintf("end splitInlineLevel %s", resumeAt))
	}

	return splitedInline{
		newBox:             newBox,
		resumeAt:           resumeAt,
		preservedLineBreak: preservedLineBreak,
		firstLetter:        firstLetter,
		lastLetter:         lastLetter,
		floatWidths:        floatWidths,
	}
}

const (
	letterTrue  rune = -1
	letterFalse rune = -2
)

func isInBoxes(b Box, boxes []Box) bool {
	for _, v := range boxes {
		if v == b {
			return true
		}
	}
	return false
}

func breakWaitingChildren(context *layoutContext, box Box, bottomSpace pr.Float, initialSkipStack tree.ResumeStack,
	absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
	children *[]indexedBoxC, waitingChildren []indexedBoxC,
) tree.ResumeStack {
	if len(waitingChildren) != 0 {
		// Too wide, let's try to cut inside waiting children,
		// starting from the end.
		// TODO: we should take care of children added into
		// absoluteBoxes, fixedBoxes and other lists.
		waitingChildrenCopy := append([]indexedBoxC{}, waitingChildren...)
		for len(waitingChildrenCopy) != 0 {
			var tmp indexedBoxC
			tmp, waitingChildrenCopy = waitingChildrenCopy[len(waitingChildrenCopy)-1], waitingChildrenCopy[:len(waitingChildrenCopy)-1]
			childIndex, child, originalChild := tmp.index, tmp.box, tmp.child
			if !child.Box().IsInNormalFlow() || canBreakInside(context, child) != pr.True {
				continue
			}

			childSkipStack := initialSkipStack[childIndex]

			// We break the waiting child at its last possible
			// breaking point.
			// TODO: The dirty solution chosen here is to
			// decrease the actual size by 1 and render the
			// waiting child again with this constraint. We may
			// find a better way.
			maxX := child.Box().PositionX + child.Box().MarginWidth() - 1
			var (
				hasBroken     bool
				newChild      Box
				childResumeAt tree.ResumeStack
			)
			for maxX > child.Box().PositionX {
				tmp := splitInlineLevel(context, originalChild, child.Box().PositionX, maxX, bottomSpace,
					childSkipStack, box, absoluteBoxes, fixedBoxes, linePlaceholders, waitingFloats, lineChildren)
				newChild, childResumeAt = tmp.newBox, tmp.resumeAt

				if childResumeAt != nil {
					hasBroken = true
					break
				}
				maxX -= 1
			}
			if !hasBroken { // else
				// No line break found
				continue
			}

			*children = append(*children, waitingChildrenCopy...)
			if newChild == nil {
				// May be nil where we have an empty TextBox.
				if !bo.TextT.IsInstance(child) {
					panic(fmt.Sprintf("only text box may yield empty child, got %s", child))
				}
			} else {
				*children = append(*children, indexedBoxC{index: childIndex, box: newChild, child: child})
			}
			return tree.ResumeStack{childIndex: childResumeAt}
		}
	}

	if l := len(*children); l != 0 {
		// Too wide, can't break waiting children and the inline is
		// non-empty: put child entirely on the next line.
		return tree.ResumeStack{(*children)[l-1].index + 1: nil}
	}

	return nil
}

// Same behavior as splitInlineLevel.
// the returned newBox has same concrete type has box_
func splitInlineBox(context *layoutContext, box_ Box, positionX, maxX, bottomSpace pr.Float, skipStack tree.ResumeStack,
	containingBlock Box, absoluteBoxes, fixedBoxes *[]*AbsolutePlaceholder,
	linePlaceholders *[]*AbsolutePlaceholder, waitingFloats *[]Box, lineChildren []indexedBox,
) splitedInline {
	if !IsLine(box_) {
		panic(fmt.Sprintf("expected Line or Inline Box, got %T", box_))
	}
	box := box_.Box()

	initialSkipStack := skipStack
	isStart := skipStack == nil
	var skip int
	if !isStart {
		skip, skipStack = skipStack.Unpack()
	}

	if traceMode {
		traceLogger.DumpTree(box_, fmt.Sprintf("splitInlineBox %d %v", skip, tracer.FormatMaybeFloat(maxX)))
	}

	// In some cases (shrink-to-fit result being the preferred width)
	// maxX is coming from Pango itself,
	// but floating point errors have accumulated:
	//   width2 = (width + X) - X   // in some cases, width2 < width
	// Increase the value a bit to compensate and not introduce
	// an unexpected line break. The 1e-6 comes from tests.
	maxX *= 1 + 1e-6

	initialPositionX := positionX

	leftSpacing := box.PaddingLeft.V() + box.MarginLeft.V() + box.BorderLeftWidth.V()
	rightSpacing := box.PaddingRight.V() + box.MarginRight.V() + box.BorderRightWidth.V()
	contentBoxLeft := positionX

	if box.Style.GetPosition().String == "relative" {
		absoluteBoxes = &[]*AbsolutePlaceholder{}
	}

	var (
		i, floatResumeAt          int
		L                         = len(box.Children[skip:])
		resumeAt                  tree.ResumeStack
		children, waitingChildren []indexedBoxC
		firstLetter, lastLetter   rune
		preservedLineBreak        = false
		floatWidths               widths
	)

	for ; i < L; i++ {
		index := i + skip
		child_ := box.Children[index]
		child := child_.Box()
		child.PositionY = box.PositionY
		if !child.IsInNormalFlow() {
			inlineOutOfFlowLayout(context, box_, containingBlock, index, child_, children, lineChildren,
				&waitingChildren, waitingFloats, absoluteBoxes, fixedBoxes, linePlaceholders, &floatWidths, maxX, positionX, bottomSpace)
			if child.IsFloated() {
				floatResumeAt = index + 1
				if !isInBoxes(child_, *waitingFloats) {
					maxX -= child.MarginWidth()
				}
			}
			continue
		}

		lastChild := index == len(box.Children)-1
		availableWidth := maxX
		var childWaitingFloats []Box
		v := splitInlineLevel(context, child_, positionX, availableWidth, bottomSpace, skipStack,
			containingBlock, absoluteBoxes, fixedBoxes, linePlaceholders, &childWaitingFloats, lineChildren)
		resumeAt = v.resumeAt
		newChild, preserved, first, last, newFloatWidths := v.newBox, v.preservedLineBreak, v.firstLetter, v.lastLetter, v.floatWidths

		var endSpacing pr.Float
		if box.Style.GetDirection() == pr.Rtl {
			endSpacing = leftSpacing
			maxX -= newFloatWidths.left
		} else {
			endSpacing = rightSpacing
			maxX -= newFloatWidths.right
		}

		if lastChild && rightSpacing != 0 && resumeAt == nil {
			// TODO: we should take care of children added into absoluteBoxes,
			// fixedBoxes and other lists.
			availableWidth -= endSpacing
			v := splitInlineLevel(context, child_, positionX, availableWidth, bottomSpace, skipStack,
				containingBlock, absoluteBoxes, fixedBoxes, linePlaceholders, &childWaitingFloats, lineChildren)
			newChild, resumeAt, preserved, first, last, newFloatWidths = v.newBox, v.resumeAt, v.preservedLineBreak, v.firstLetter, v.lastLetter, v.floatWidths
		}

		// Per WP commit c3c25f2c (issue #1510): floats inside nested
		// inline boxes must be reflected back into the outer line's
		// float-widths accumulator so subsequent siblings get the
		// reduced available width.
		if newFloatWidths.left > floatWidths.left {
			floatWidths.left = newFloatWidths.left
		}
		if newFloatWidths.right > floatWidths.right {
			floatWidths.right = newFloatWidths.right
		}

		skipStack = nil
		if preserved {
			preservedLineBreak = true
		}

		var canBreak pr.MaybeBool
		if lastLetter == letterTrue {
			lastLetter = ' '
		} else if lastLetter == letterFalse {
			lastLetter = ' ' // no-break space
		} else if box.Style.GetWhiteSpace() == pr.Pre || box.Style.GetWhiteSpace() == pr.Nowrap {
			canBreak = pr.False
		}
		if canBreak == nil {
			if lastLetter == 0 || first == 0 {
				canBreak = pr.False
			} else if first == letterTrue {
				canBreak = pr.True
			} else if first == letterFalse {
				canBreak = pr.False
			} else if lastLetter == '\u2e80' || first == '\u2e80' {
				// CSS Text Level 3 §5.2: Atomic inlines (inline-block, replaced elements)
				// should be treated as U+FFFC (Contingent Break class), allowing breaks
				// on both sides. Using '\u2e80' (Ideographic class) with CanBreakText
				// incorrectly prevents breaks before punctuation (LB13: ×IS).
				// Always allow breaks adjacent to atomic inlines.
				canBreak = pr.True
			} else {
				canBreak = context.Fonts().CanBreakText([]rune{lastLetter, first})
			}
		}

		if canBreak == pr.True {
			children = append(children, waitingChildren...)
			waitingChildren = nil
		}

		if firstLetter == 0 {
			firstLetter = first
		}
		if child.TrailingCollapsibleSpace {
			lastLetter = letterTrue
		} else {
			lastLetter = last
		}

		if asTextBox, ok := newChild.(*bo.TextBox); newChild == nil || (ok && asTextBox == nil) {
			// May be nil where we have an empty TextBox.
		} else {
			// Store lines to get previous break points.
			if bo.LineT.IsInstance(box_) {
				lineChildren = append(lineChildren, indexedBox{index: index, box: newChild})
			}
			// Check that text doesn’t overflow.
			newPositionX := newChild.Box().PositionX + newChild.Box().MarginWidth()
			if newPositionX-trailingWhitespaceSize(context, newChild) > maxX {
				// Text overflows, find previous break point.
				previousResumeAt := breakWaitingChildren(context, containingBlock, bottomSpace, initialSkipStack, absoluteBoxes, fixedBoxes,
					linePlaceholders, waitingFloats, lineChildren, &children, waitingChildren)
				if previousResumeAt != nil {
					// Per CSS Text §4.1, trailing whitespace at the end
					// of a line is collapsed for line-break decisions
					// but kept on the breaking line as a zero-width
					// box. If the overflow is caused by a
					// pure-whitespace TextBox AND a wrappable sibling
					// follows (so the break point logically falls
					// AFTER the whitespace), keep the whitespace on
					// the current line so removeLastWhitespace can
					// later trim it to width 0.
					if index < len(box.Children)-1 {
						if tb, ok := newChild.(*bo.TextBox); ok && tb != nil && len(tb.Text) > 0 {
							allWS := true
							for _, r := range tb.Text {
								if r != ' ' && r != '\t' {
									allWS = false
									break
								}
							}
							if allWS {
								children = append(children, indexedBoxC{index: index, box: newChild, child: child_})
								previousResumeAt = tree.ResumeStack{index + 1: nil}
							}
						}
					}
					resumeAt = previousResumeAt
					break
				}
			}

			positionX = newPositionX
			waitingChildren = append(waitingChildren, indexedBoxC{index: index, box: newChild, child: child_})
		}
		*waitingFloats = append(*waitingFloats, childWaitingFloats...)
		if resumeAt != nil {
			children = append(children, waitingChildren...)
			resumeAt = tree.ResumeStack{index: resumeAt}
			break
		}
	}
	if i == L {
		children = append(children, waitingChildren...)
		resumeAt = nil
	}

	// Reorder inline blocks when direction is rtl
	if box.Style.GetDirection() == pr.Rtl && len(children) > 1 {
		var inFlowChildren []Box
		for _, child := range children {
			if child.box.Box().IsInNormalFlow() {
				inFlowChildren = append(inFlowChildren, child.box)
			}
		}
		posX := inFlowChildren[0].Box().PositionX
		for _, child := range reversedBoxes(inFlowChildren) {
			child.Translate((posX - child.Box().PositionX), 0, true)
			posX += child.Box().MarginWidth()
		}
	}

	isEnd := resumeAt == nil
	toCopy := make([]Box, len(children))
	for i, boxChild := range children {
		toCopy[i] = boxChild.box
	}

	newBox_ := bo.CopyWithChildren(box_, toCopy)
	newBox := newBox_.Box()
	newBox_.RemoveDecoration(newBox, !isStart, !isEnd)

	if bo.LineT.IsInstance(box_) {
		// We must reset line box width according to its new children
		newBox.Width = pr.Float(0)
		children := newBox.Children
		if newBox.Style.GetDirection() == pr.Ltr {
			children = reversedBoxes(children)
		}
		for _, boxChild := range children {
			if boxChild.Box().IsInNormalFlow() {
				newBox.Width = boxChild.Box().PositionX + boxChild.Box().MarginWidth() - newBox.PositionX
				break
			}
		}
	} else {
		newBox.PositionX = initialPositionX
		var translationNeeded bool
		if box.Style.GetBoxDecorationBreak() == "clone" {
			translationNeeded = true
		} else if box.Style.GetDirection() == pr.Ltr {
			translationNeeded = isStart
		} else {
			translationNeeded = isEnd
		}
		if translationNeeded {
			for _, child := range newBox.Children {
				child.Translate(leftSpacing, 0, false)
			}
		}
		newBox.Width = positionX - contentBoxLeft
		newBox_.Translate(floatWidths.left, 0, true)
	}
	lineHeight, baseline := text.StrutLayout(box.Style, context)
	newBox.Baseline = baseline
	newBox.Height = box.Style.GetFontSize().ToMaybeFloat()
	halfLeading := (lineHeight - newBox.Height.V()) / 2.
	// Set margins to the half leading but also compensate for borders and
	// paddings. We want marginHeight() == lineHeight
	newBox.MarginTop = halfLeading - newBox.BorderTopWidth.V() - newBox.PaddingTop.V()
	newBox.MarginBottom = halfLeading - newBox.BorderBottomWidth.V() - newBox.PaddingBottom.V()

	if newBox.Style.GetPosition().String == "relative" {
		for _, absoluteBox := range *absoluteBoxes {
			absoluteLayout(context, absoluteBox, newBox_, fixedBoxes, bottomSpace, nil)
		}
	}

	if resumeAt != nil {
		resumeIndex, _ := resumeAt.Unpack()
		if resumeIndex < floatResumeAt {
			resumeAt = tree.ResumeStack{floatResumeAt: nil}
		}
	}

	if box.IsLeader {
		firstLetter = letterTrue
		lastLetter = letterFalse
	}

	return splitedInline{
		newBox:             newBox_,
		resumeAt:           resumeAt,
		preservedLineBreak: preservedLineBreak,
		firstLetter:        firstLetter,
		lastLetter:         lastLetter,
		floatWidths:        floatWidths,
	}
}

func (context *layoutContext) addRunning(child_ bo.Box) {
	runningName := child_.Box().Style.GetPosition().String
	currentRE, has := context.runningElements[runningName]
	if !has {
		currentRE = map[int][]Box{}
		context.runningElements[runningName] = currentRE
	}
	currentRE[context.currentPage] = append(currentRE[context.currentPage], child_)
}

type indexedBoxC struct {
	index int
	box   Box
	child Box
}

func inlineOutOfFlowLayout(context *layoutContext, box Box, containingBlock Box, index int, child_ Box, children []indexedBoxC,
	lineChildren []indexedBox, waitingChildren *[]indexedBoxC, waitingFloats *[]Box,
	absoluteBoxes, fixedBoxes, linePlaceholders *[]*AbsolutePlaceholder, floatWidths *widths,
	maxX, positionX, bottomSpace pr.Float,
) {
	if traceMode {
		traceLogger.DumpTree(box, "inlineOutOfFlowLayout")
		traceLogger.Dump(fmt.Sprintf("is absolute: %v", child_.Box().IsAbsolutelyPositioned()))
	}

	child := child_.Box()
	if child.IsAbsolutelyPositioned() {
		child.PositionX = positionX
		placeholder := NewAbsolutePlaceholder(child_)
		*linePlaceholders = append(*linePlaceholders, placeholder)
		*waitingChildren = append(*waitingChildren, indexedBoxC{index: index, box: placeholder, child: child_})
		if child.Style.GetPosition().String == "absolute" {
			*absoluteBoxes = append(*absoluteBoxes, placeholder)
		} else {
			*fixedBoxes = append(*fixedBoxes, placeholder)
		}
	} else if child.IsFloated() {
		child.PositionX = positionX
		floatWidth := shrinkToFit(context, child_, containingBlock.Box().Width.V())

		// To retrieve the real available space for floats, we must remove
		// the trailing whitespaces from the line
		var nonFloatingChildren []Box
		for _, v := range append(children, *waitingChildren...) {
			if !v.box.Box().IsFloated() {
				nonFloatingChildren = append(nonFloatingChildren, v.box)
			}
		}
		if Lf := len(nonFloatingChildren); Lf != 0 {
			floatWidth -= trailingWhitespaceSize(context, nonFloatingChildren[Lf-1])
		}

		if floatWidth > maxX-positionX || len(*waitingFloats) != 0 {
			// TODO: the absolute and fixed boxes in the floats must be
			// added here, and not in iterLineBoxes
			*waitingFloats = append(*waitingFloats, child_)
		} else {
			newChild, floatResumeAt := floatLayout(context, child_, containingBlock.Box(), absoluteBoxes, fixedBoxes,
				bottomSpace, nil)

			if floatResumeAt != nil {
				context.brokenOutOfFlow[child_] = brokenBox{box: child_, containingBlock: containingBlock, bfcRoot: context.currentBFCRoot(), resumeAt: floatResumeAt}
			}
			*waitingChildren = append(*waitingChildren, indexedBoxC{index: index, box: newChild, child: child_})
			child_ = newChild
			child = child_.Box()

			// Translate previous line children
			dx := max(child.MarginWidth(), 0)
			floatWidths.add(child.Style.GetFloat(), dx)
			if child.Style.GetFloat() == "left" {
				if bo.LineT.IsInstance(box) {
					// The parent is the line, update the current position
					// for the next child. When the parent is not the line
					// (it is an inline block), the current position of the
					// line is updated by the box itself (see next
					// splitInlineLevel call).
					positionX += dx
				}
			} else if child.Style.GetFloat() == "right" {
				// Update the maximum x position for the next children
				maxX -= dx
			}
			for _, oldChild := range lineChildren {
				if !oldChild.box.Box().IsInNormalFlow() {
					continue
				}
				if (child.Style.GetFloat() == "left" && box.Box().Style.GetDirection() == pr.Ltr) ||
					(child.Style.GetFloat() == "right" && box.Box().Style.GetDirection() == pr.Rtl) {
					oldChild.box.Translate(dx, 0, true)
				}
			}
		}
	} else if child.IsRunning() {
		context.addRunning(child_)
	}
}

// See https://unicode.org/reports/tr14/
// \r is already handled by processWhitespace
var lineBreaks = utils.NewSet("\n", "\t", "\f", "\u0085", "\u2028", "\u2029")

// Keep as much text as possible from a TextBox in a limited width.
//
// Try not to overflow but always have some text in the returned [TextBox]
//
// Also returns 'skip', the number of
// runes to skip form the start of the TextBox for the next line, or
// -1 if all of the text fits.
//
// Also break on preserved line breaks.
func splitTextBox(context *layoutContext, box *bo.TextBox, availableWidth pr.MaybeFloat,
	skip int, isLineStart bool,
) (_ *bo.TextBox, _ int, _ bool) {
	fontSize := box.Style.GetFontSize()
	text_ := box.Text[skip:]
	if fontSize == pr.FToV(0) || len(text_) == 0 {
		return nil, -1, false
	}
	v := text.SplitFirstLine(text_, box.Style, context, availableWidth, false, isLineStart)
	layout, length, resumeIndex, width, height := v.Layout, v.Length, v.ResumeAt, v.Width, v.Height
	if resumeIndex == 0 {
		panic("resumeAt should not be 0 here")
	}

	if traceMode {
		traceLogger.Dump(fmt.Sprintf("splitTextBox (availableWidth %v) <%q> (len: %d) resumeIndex: %d", tracer.FormatMaybeFloat(availableWidth), string(text_), len(text_), resumeIndex))
	}

	if newText := layout.Text(); length > 0 {
		box = box.CopyWithText(newText)
		box.Width = width
		box.TextLayout = layout
		box.FirstLineIsRTL = v.FirstLineRTL
		// "The height of the content area should be based on the font,
		//  but this specification does not specify how."
		// https://www.w3.org/TR/CSS21/visudet.html#inline-non-replaced
		// We trust Pango and use the height of the LayoutLine.
		box.Height = height
		// "only the "line-height" is used when calculating the height
		//  of the line box."
		// Set margins so that marginHeight() == lineHeight
		lineHeight, _ := text.StrutLayout(box.Style, context)
		halfLeading := (lineHeight - height) / 2.
		box.MarginTop = halfLeading
		box.MarginBottom = halfLeading
		// form the top of the content box
		box.Baseline = v.Baseline + box.MarginTop.V() // form the top of the margin box
	} else {
		box = nil
	}

	preservedLineBreak := false
	if resumeIndex != -1 {
		if resumeIndex > len(text_) {
			resumeIndex = len(text_)
		}
		if length > resumeIndex {
			length = resumeIndex
		}
		between := string(text_[length:resumeIndex])
		preservedLineBreak = len(strings.Trim(between, " ")) != 0
		if preservedLineBreak {
			if !lineBreaks.Has(between) {
				panic(fmt.Sprintf("Got %s between two lines. Expected nothing or a preserved line break", between))
			}
		}
		resumeIndex += skip
	}

	return box, resumeIndex, preservedLineBreak
}

type boxMinMax struct {
	box      Box
	max, min pr.MaybeFloat
}

// Handle “vertical-align“ within an `LineBox` (or of a
// non-align sub-tree).
// Place all boxes vertically assuming that the baseline of “box“
// is at `y = 0`.
// Return “(maxY, minY)“, the maximum and minimum vertical position
// of margin boxes.
func lineBoxVerticality(context *layoutContext, box Box) (pr.Float, pr.Float) {
	var topBottomSubtrees []Box
	maxY, minY := alignedSubtreeVerticality(context, box, &topBottomSubtrees, 0)
	subtreesWithMinMax := make([]boxMinMax, 0, len(topBottomSubtrees))
	for i := 0; i < len(topBottomSubtrees); i++ { // note that topBottomSubtrees may grow over this loop
		subtree := topBottomSubtrees[i]
		var subMaxY, subMinY pr.MaybeFloat
		if !subtree.Box().IsFloated() {
			subMaxY, subMinY = alignedSubtreeVerticality(context, subtree, &topBottomSubtrees, 0)
		}
		subtreesWithMinMax = append(subtreesWithMinMax, boxMinMax{box: subtree, max: subMaxY, min: subMinY})
	}

	if len(subtreesWithMinMax) != 0 {
		var highestSub pr.Float
		for _, subtree := range subtreesWithMinMax {
			if !subtree.box.Box().IsFloated() {
				m := subtree.max.V() - subtree.min.V()
				if m > highestSub {
					highestSub = m
				}
			}
		}
		maxY = max(maxY, minY+highestSub)
	}

	for _, v := range subtreesWithMinMax {
		va := v.box.Box().Style.GetVerticalAlign()
		var dy pr.Float
		if v.box.Box().IsFloated() {
			dy = minY - v.box.Box().PositionY
		} else if va.Tag == pr.Top {
			dy = minY - v.min.V()
		} else if va.Tag == pr.Bottom {
			dy = maxY - v.max.V()
		} else {
			panic(fmt.Sprintf("expected top or bottom, got %v", va))
		}
		translateSubtree(v.box, dy)
	}
	return maxY, minY
}

func translateSubtree(box Box, dy pr.Float) {
	if bo.InlineT.IsInstance(box) {
		box.Box().PositionY += dy
		if va := box.Box().Style.GetVerticalAlign().Tag; va == pr.Top || va == pr.Bottom {
			for _, child := range box.Box().Children {
				translateSubtree(child, dy)
			}
		}
	} else {
		// Text or atomic boxes
		box.Translate(0, dy, true)
	}
}

func alignedSubtreeVerticality(context *layoutContext, box Box, topBottomSubtrees *[]Box, baselineY pr.Float) (pr.Float, pr.Float) {
	maxY, minY := inlineBoxVerticality(context, box, topBottomSubtrees, baselineY)
	// Account for the line box itself :
	top := baselineY - box.Box().Baseline.V()
	bottom := top + box.Box().MarginHeight()
	if minY == nil || top < minY.V() {
		minY = top
	}
	if maxY == nil || bottom > maxY.V() {
		maxY = bottom
	}

	return maxY.V(), minY.V()
}

// Handle “vertical-align“ within an `InlineBox`.
//
//	Place all boxes vertically assuming that the baseline of ``box``
//	is at `y = baselineY`.
//	Return ``(maxY, minY)``, the maximum and minimum vertical position
//	of margin boxes.
func inlineBoxVerticality(context *layoutContext, box_ Box, topBottomSubtrees *[]Box, baselineY pr.Float) (maxY, minY pr.MaybeFloat) {
	if !IsLine(box_) {
		return maxY, minY
	}
	box := box_.Box()
	for _, child_ := range box_.Box().Children {
		child := child_.Box()
		if !child.IsInNormalFlow() {
			if child.IsFloated() {
				*topBottomSubtrees = append(*topBottomSubtrees, child_)
			}
			continue
		}
		var childBaselineY pr.Float
		verticalAlign := child.Style.GetVerticalAlign()
		switch verticalAlign.Tag {
		case pr.Baseline:
			childBaselineY = baselineY
		case pr.Middle:
			oneEx := box.Style.GetFontSize().Value * text.CharacterRatio(box.Style, box.Style.Cache(), false, context.fontConfig)
			top := baselineY - (oneEx+child.MarginHeight())/2.
			childBaselineY = top + child.Baseline.V()
		case pr.TextTop:
			// align top with the top of the parent’s content area
			top := baselineY - box.Baseline.V() + box.MarginTop.V() +
				box.BorderTopWidth.V() + box.PaddingTop.V()
			childBaselineY = top + child.Baseline.V()
		case pr.TextBottom:
			// align bottom with the bottom of the parent’s content area
			bottom := baselineY - box.Baseline.V() + box.MarginTop.V() +
				box.BorderTopWidth.V() + box.PaddingTop.V() + box.Height.V()
			childBaselineY = bottom - child.MarginHeight() + child.Baseline.V()
		case pr.Top, pr.Bottom:
			// TODO: actually implement vertical-align: top and bottom
			// Later, we will assume for this subtree that its baseline
			// is at y=0.
			childBaselineY = 0
		default:
			// Numeric value: The child’s baseline is `verticalAlign` above
			// (lower y) the parent’s baseline.
			childBaselineY = baselineY - verticalAlign.Value
		}

		// the child’s `top` is `child.Baseline` above (lower y) its baseline.
		top := childBaselineY - child.Baseline.V()
		if bo.InlineBlockT.IsInstance(child_) || bo.InlineFlexT.IsInstance(child_) || bo.InlineGridT.IsInstance(child_) {
			// This also includes table wrappers for inline tables.
			child_.Translate(0, top-child.PositionY, false)
		} else {
			child.PositionY = top
			// grand-children for inline boxes are handled below
		}

		if verticalAlign.Tag == pr.Top || verticalAlign.Tag == pr.Bottom {
			// top or bottom are special, they need to be handled in
			// a later pass.
			*topBottomSubtrees = append(*topBottomSubtrees, child_)
			continue
		}

		bottom := top + child.MarginHeight()
		if minY == nil || top < minY.V() {
			minY = top
		}
		if maxY == nil || bottom > maxY.V() {
			maxY = bottom
		}
		if bo.InlineT.IsInstance(child_) {
			childrenMaxY, childrenMinY := inlineBoxVerticality(context, child_, topBottomSubtrees, childBaselineY)
			if childrenMinY != nil && childrenMinY.V() < minY.V() {
				minY = childrenMinY
			}
			if childrenMaxY != nil && childrenMaxY.V() > maxY.V() {
				maxY = childrenMaxY
			}
		}
	}
	return maxY, minY
}

// Return how much the line should be moved horizontally according to
// the `text-align` property.
func textAlign(context *layoutContext, line *bo.LineBox, availableWidth pr.Float, last bool) pr.Float {
	// "When the total width of the inline-level boxes on a line is less than
	// the width of the line box containing them, their horizontal distribution
	// within the line box is determined by the "text-align" property."
	if line.Width.V() >= availableWidth {
		return 0
	}
	align := line.Style.GetTextAlignAll()
	if last {
		alignLast := line.Style.GetTextAlignLast()
		if alignLast != "auto" {
			align = alignLast
		}
	}
	ws := line.Style.GetWhiteSpace()
	spaceCollapse := ws == pr.Normal || ws == pr.Nowrap || ws == pr.PreLine

	if align == "left" || align == "right" {
		if (align == "left") != (line.Style.GetDirection() == pr.Rtl) { // xor
			align = "start"
		} else {
			align = "end"
		}
	}

	if align == "start" {
		return 0
	}

	offset := availableWidth - line.Width.V()
	switch align {
	case "justify":
		if spaceCollapse {
			// Justification of texts where white space is not collapsing is
			// - forbidden by CSS 2, and
			// - not required by CSS 3 Text.
			justifyLine(context, line, offset)
		}
		return 0
	case "center":
		return offset / 2
	case "end":
		return offset
	}
	panic(fmt.Sprintf("align should be 'justify', 'center' or 'end', got %s", align))
}

func justifyLine(context *layoutContext, line *bo.LineBox, extraWidth pr.Float) {
	// TODO: We should use a better alorithm here, see
	// https://www.w3.org/TR/css-text-3/#justify-algos
	if nbSpaces := countExpandableSpaces(line); nbSpaces != 0 {
		addWordSpacing(context, line, extraWidth/pr.Float(nbSpaces), 0)
	}
}

// Count expandable spaces (space and nbsp) for justification.
func countExpandableSpaces(box Box) int {
	if textBox, isTextBox := box.(*bo.TextBox); isTextBox {
		// TODO: remove trailing spaces correctly
		return strings.Count(textBox.TextS(), " ") + strings.Count(textBox.TextS(), "\u00a0")
	} else if IsLine(box) {
		var sum int
		for _, child := range box.Box().Children {
			sum += countExpandableSpaces(child)
		}
		return sum
	} else {
		return 0
	}
}

func addWordSpacing(context *layoutContext, box_ Box, justificationSpacing, xAdvance pr.Float) pr.Float {
	if textBox, isTextBox := box_.(*bo.TextBox); isTextBox {
		// textBox.JustificationSpacing = justificationSpacing
		textBox.PositionX += xAdvance
		nbSpaces := pr.Float(countExpandableSpaces(box_))
		if nbSpaces > 0 {
			textBox.TextLayout.SetJustification(justificationSpacing)
			extraSpace := justificationSpacing * nbSpaces
			xAdvance += extraSpace
			textBox.Width = textBox.Width.V() + extraSpace
		}
	} else if IsLine(box_) {
		box := box_.Box()
		box.PositionX += xAdvance
		previousXAdvance := xAdvance
		// Per WP commit 32a7cd58: walk children in visual order. RTL
		// inline boxes lay out their children right-to-left, so the
		// running x-advance must accumulate over them in reverse.
		children := slices.Clone(box.Children)
		if box.Style.GetDirection() == pr.Rtl {
			slices.Reverse(children)
		}
		for _, child := range children {
			if child.Box().IsInNormalFlow() {
				xAdvance = addWordSpacing(context, child, justificationSpacing, xAdvance)
			}
		}
		box.Width = box.Width.V() + xAdvance - previousXAdvance
	} else {
		// Atomic inline-level box
		box_.Translate(xAdvance, 0, false)
	}
	return xAdvance
}

// Shttps://www.w3.org/TR/CSS21/visuren.html#phantom-line-box
func isPhantomLinebox(linebox *bo.BoxFields) bool {
	for _, child_ := range linebox.Children {
		child := child_.Box()
		if bo.InlineT.IsInstance(child_) {
			if !isPhantomLinebox(child) {
				return false
			}
			for side := pr.KnownProp(0); side < 4; side++ {
				m := getDimOrS(child, pr.PMarginBottom+side*5).Value
				b := getDimOrS(child, pr.PBorderBottomWidth+side*5).Value
				p := getDimOrS(child, pr.PPaddingBottom+side*5).Value
				if m != 0 || b != 0 || p != 0 {
					return false
				}
			}
		} else if child.IsInNormalFlow() {
			return false
		}
	}
	return true
}

func canBreakInside(ctx *layoutContext, box Box) pr.MaybeBool {
	// See https://www.w3.org/TR/css-text-3/#white-space-property
	ws := box.Box().Style.GetWhiteSpace()
	textWrap := ws == pr.Normal || ws == pr.PreWrap || ws == pr.PreLine
	textBox, isTextBox := box.(*bo.TextBox)
	if bo.AtomicInlineLevelT.IsInstance(box) {
		return pr.False
	} else if textWrap && isTextBox {
		return ctx.Fonts().CanBreakText(textBox.Text)
	} else if textWrap && bo.ParentT.IsInstance(box) {
		for _, child := range box.Box().Children {
			if canBreakInside(ctx, child) == pr.True {
				return pr.True
			}
		}
		return pr.False
	}
	return pr.False
}
