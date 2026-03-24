package properties

// Keyword is a flag indicating special values,
// such as "none" or "auto".
type Keyword uint8

const (
	_       Keyword = iota
	Auto            // "auto"
	None            // "none"
	Span            // "span"
	Subgrid         // "subgrid"
	Attr            // "attr()"

	// url related
	Internal
	External
	Local
	Attachment

	// length related
	Content
	FromFont
	Fill
	MinContent
	MaxContent
	Normal

	// background size
	Cover
	Contain

	// font size : the order matters
	XxSmall
	XSmall
	Small
	Medium
	Large
	XLarge
	XxLarge

	Smaller
	Larger

	Thin
	Thick

	Baseline
	Middle
	TextTop
	TextBottom
	Top
	Bottom
	Super
	Sub

	Block
	Center
	End
	First
	Flex
	FlexEnd
	FlexStart
	Flow
	FlowRoot
	Grid
	Inline
	InlineBlock
	InlineFlex
	InlineGrid
	InlineTable
	Last
	Left
	Legacy
	ListItem
	Ltr
	Right
	Rtl
	Safe
	SelfEnd
	SelfStart
	SpaceAround
	SpaceBetween
	SpaceEvenly
	Start
	Stretch
	Table
	TableCaption
	TableCell
	TableColumn
	TableColumnGroup
	TableFooterGroup
	TableHeaderGroup
	TableRow
	TableRowGroup
	Unsafe
)
