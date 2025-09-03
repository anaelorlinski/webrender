package document

import (
	"fmt"
	"slices"
	"sort"

	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/layout"
)

type aliasForStackingContext = bo.Box

var _ bo.Box = StackingContext{}

type BoxTree map[bo.Box]BoxTree

// Stacking contexts define the paint order of all pieces of a document.
// http://www.w3.org/TR/CSS21/visuren.html#x43
// http://www.w3.org/TR/CSS21/zindex.html
type StackingContext struct {
	// StackingContext needs to implement Box (see below)
	aliasForStackingContext
	box               Box
	page              *bo.PageBox
	blockLevelBoxes   []bo.Box
	floatContexts     []StackingContext
	negativeZContexts []StackingContext
	zeroZContexts     []StackingContext
	positiveZContexts []StackingContext
	blocksAndCells    BoxTree
	zIndex            int
}

func NewStackingContext(box Box, childContexts []StackingContext, blocks []bo.Box, floats []StackingContext, blocksAndCells BoxTree,
	page *bo.PageBox,
) StackingContext {
	self := StackingContext{}
	self.box = box
	self.page = page
	self.blockLevelBoxes = blocks        // 4: In flow, non positioned
	self.floatContexts = floats          // 5: Non positioned
	self.negativeZContexts = nil         // 3: Child contexts, z-index < 0
	self.zeroZContexts = nil             // 8: Child contexts, z-index = 0
	self.positiveZContexts = nil         // 9: Child contexts, z-index > 0
	self.blocksAndCells = blocksAndCells // 7: Non positioned

	for _, context := range childContexts {
		if context.zIndex < 0 {
			self.negativeZContexts = append(self.negativeZContexts, context)
		} else if context.zIndex == 0 {
			self.zeroZContexts = append(self.zeroZContexts, context)
		} else { // context.zIndex > 0
			self.positiveZContexts = append(self.positiveZContexts, context)
		}
	}
	sort.SliceStable(self.negativeZContexts, func(i, j int) bool {
		return self.negativeZContexts[i].zIndex < self.negativeZContexts[j].zIndex
	})
	sort.SliceStable(self.positiveZContexts, func(i, j int) bool {
		return self.positiveZContexts[i].zIndex < self.positiveZContexts[j].zIndex
	})
	// sort() is stable, so the lists are now storted
	// by z-index, then tree order.

	zIndex := box.Box().Style.GetZIndex()
	if zIndex.String == "auto" {
		self.zIndex = 0
	} else {
		self.zIndex = zIndex.Int
	}
	return self
}

func NewStackingContextFromPage(page *bo.PageBox) StackingContext {
	// Page children (the box for the root element and margin boxes)
	// as well as the page box itself are unconditionally stacking contexts.
	childContexts := make([]StackingContext, len(page.Children))
	for i, child := range page.Children {
		childContexts[i] = NewStackingContextFromBox(child, page, nil)
	}
	// Children are sub-contexts, remove them from the "normal" tree.
	page = bo.CopyWithChildren(page, nil).(*bo.PageBox)
	return NewStackingContext(page, childContexts, nil, nil, nil, page)
}

func insertStackingContext(a *[]StackingContext, i int, item StackingContext) {
	*a = slices.Insert(*a, i, item)
}

func NewStackingContextFromBox(box Box, page *bo.PageBox, childContexts *[]StackingContext) StackingContext {
	var children []StackingContext // What will be passed to this box
	if childContexts == nil {
		childContexts = &children
	}
	// childContexts: where to put sub-contexts that we find here.
	// May not be the same as children for :
	//   "treat the element as if it created a new stacking context,
	//    but any positioned descendants && descendants which actually
	//    create a new stacking context should be considered part of the
	//    parent stacking context, not this new one."
	var (
		blocks         []Box
		blocksAndCells = BoxTree{}
		floats         []StackingContext
	)

	box = dispatchChildren(box, page, childContexts, &blocks, floats, blocksAndCells)
	return NewStackingContext(box, children, blocks, floats, blocksAndCells, page)
}

func dispatch(box Box, page *bo.PageBox, childContexts *[]StackingContext,
	blocks *[]Box, floats []StackingContext, blocksAndCells BoxTree,
) Box {
	if absPlac, ok := box.(*layout.AbsolutePlaceholder); ok {
		box = absPlac.AliasBox
	} else if stack, ok := box.(StackingContext); ok {
		box = stack.box
	}
	style := box.Box().Style
	absoluteAndZIndex := style.GetPosition().String != "static" && style.GetZIndex().String != "auto"
	if absoluteAndZIndex || style.GetOpacity() < 1 ||
		// "transform: none" gives a "falsy" empty list here
		len(style.GetTransform()) != 0 || style.GetOverflow() != "visible" {

		// This box defines a new stacking context, remove it
		// from the "normal" children list.
		*childContexts = append(*childContexts, NewStackingContextFromBox(box, page, nil))
	} else {
		if style.GetPosition().String != "static" {
			if style.GetZIndex().String != "auto" {
				panic(fmt.Sprintf("expected auto z-index, got %v", style.GetZIndex()))
			}
			// "Fake" context: sub-contexts will go := range this
			// `childContexts` list.
			// Insert at the position before creating the sub-context.
			index := len(*childContexts)
			insertStackingContext(childContexts, index, NewStackingContextFromBox(box, page, childContexts))
		} else if box.Box().IsFloated() {
			floats = append(floats, NewStackingContextFromBox(box, page, childContexts))
		} else if bo.InlineBlockT.IsInstance(box) || bo.InlineFlexT.IsInstance(box) {
			// Have this fake stacking context be part of the "normal"
			// box tree, because we need its position in the middle
			// of a tree of inline boxes.
			return NewStackingContextFromBox(box, page, childContexts)
		} else {
			if bo.BlockLevelT.IsInstance(box) {
				blocksIndex := len(*blocks)
				boxBlocksAndCells := BoxTree{}
				box = dispatchChildren(box, page, childContexts, blocks, floats, boxBlocksAndCells)
				*blocks = slices.Insert(*blocks, blocksIndex, box)
				blocksAndCells[box] = boxBlocksAndCells
			} else if bo.TableCellT.IsInstance(box) {
				boxBlocksAndCells := BoxTree{}
				box = dispatchChildren(box, page, childContexts, blocks, floats, boxBlocksAndCells)
				blocksAndCells[box] = boxBlocksAndCells
			} else {
				box = dispatchChildren(box, page, childContexts, blocks, floats, blocksAndCells)
			}

			return box
		}
	}
	return nil
}

func dispatchChildren(box Box, page *bo.PageBox, childContexts *[]StackingContext,
	blocks *[]Box, floats []StackingContext, blocksAndCells BoxTree,
) Box {
	if !bo.ParentT.IsInstance(box) {
		return box
	}

	var newChildren []Box
	for _, child := range box.Box().Children {
		result := dispatch(child, page, childContexts, blocks, floats, blocksAndCells)
		if result != nil {
			newChildren = append(newChildren, result)
		}
	}
	return bo.CopyWithChildren(box, newChildren)
}

func (StackingContext) Type() bo.BoxType { return bo.StackingContextT }
