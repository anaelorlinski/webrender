package properties

//go:generate go run gen/gen.go

// This file is used to generate typed accessors
// The order of props matters : it is choosen so that the more frequent
// props come first

// KnownProp efficiently encode a known CSS property
type KnownProp uint8

const (
	_ KnownProp = iota

	// DO NOT CHANGE the order, because
	// the following properties are grouped by side,
	// in the [bottom, left, right, top] order,
	// so that, if side in an index (0, 1, 2 or 3),
	// the property is a PBorderBottomColor + side * 5
	PBorderBottomColor
	PBorderBottomStyle
	PBorderBottomWidth
	PMarginBottom
	PPaddingBottom

	PBorderLeftColor
	PBorderLeftStyle
	PBorderLeftWidth
	PMarginLeft
	PPaddingLeft

	PBorderRightColor
	PBorderRightStyle
	PBorderRightWidth
	PMarginRight
	PPaddingRight

	PBorderTopColor
	PBorderTopStyle
	PBorderTopWidth
	PMarginTop
	PPaddingTop

	// min-XXX is at +2, max-XXX is at + 4
	PWidth
	PHeight
	PMinWidth
	PMinHeight
	PMaxWidth
	PMaxHeight

	PBorderCollapse
	PTabSize

	PBorderImageSource
	PMaskBorderSource

	PColor
	PDirection
	PDisplay
	PFloat
	PLineHeight

	PPosition
	PTableLayout
	PTop
	PVerticalAlign
	PVisibility

	PBorderBottomLeftRadius
	PBorderBottomRightRadius
	PBorderTopLeftRadius
	PBorderTopRightRadius

	PColumnCount
	PColumnWidth

	PFontFamily
	PFontFeatureSettings
	PFontKerning
	PFontLanguageOverride
	PFontSize
	PFontStretch
	PFontStyle

	// The order for this group matters (see expandFontVariant)
	PFontVariantAlternates
	PFontVariantCaps
	PFontVariantEastAsian
	PFontVariantLigatures
	PFontVariantNumeric
	PFontVariantPosition

	PFontWeight
	PFontVariationSettings

	PHyphenateLimitZone
	PHyphenateCharacter
	PHyphenateLimitChars
	PHyphens
	PLetterSpacing
	PTextAlignAll
	PTextAlignLast
	PTextIndent
	PTextTransform
	PWhiteSpace
	PWordBreak
	PWordSpacing
	PTransform

	PContinue
	PMaxLines
	POverflow
	POverflowWrap
	PCounterIncrement
	PCounterReset
	PCounterSet

	PAnchor
	PLink
	PLang

	PBoxDecorationBreak

	PBookmarkLabel
	PBookmarkLevel
	PBookmarkState
	PContent

	PStringSet
	PImageOrientation

	PPage
	PAppearance
	POutlineColor
	POutlineStyle
	POutlineWidth
	PBoxSizing

	// The following properties are all background related,
	// in the order expected by expandBackground
	PBackgroundColor
	PBackgroundImage
	PBackgroundRepeat
	PBackgroundAttachment
	PBackgroundPosition
	PBackgroundSize
	PBackgroundClip
	PBackgroundOrigin

	PBreakAfter
	PBreakBefore
	PBreakInside

	// text-decoration-XXX
	PTextDecorationLine
	PTextDecorationColor
	PTextDecorationStyle
	PTextDecorationThickness
	PTextUnderlineOffset

	PGridAutoColumns
	PGridAutoFlow
	PGridAutoRows
	// the order matter
	PGridTemplateColumns
	PGridTemplateRows
	PGridTemplateAreas
	PGridRowStart
	PGridColumnStart
	PGridRowEnd
	PGridColumnEnd

	PAlignContent
	PAlignItems
	PAlignSelf
	PFlexBasis
	PFlexDirection
	PFlexGrow
	PFlexShrink
	PFlexWrap
	PJustifyContent
	PJustifyItems
	PJustifySelf
	POrder
	PColumnGap
	PRowGap

	PBottom
	PCaptionSide
	PClear
	PClip
	PEmptyCells
	PLeft
	PRight

	PListStyleImage
	PListStylePosition
	PListStyleType

	PTextOverflow
	PBlockEllipsis
	PBorderSpacing

	PTransformOrigin

	PFontVariant

	PMarginBreak
	POrphans
	PWidows

	PFootnoteDisplay
	PFootnotePolicy
	PQuotes

	PImageResolution
	PImageRendering

	PColumnFill
	PColumnSpan
	PColumnRuleColor

	PSize
	PBleedLeft
	PBleedRight
	PBleedTop
	PBleedBottom
	PMarks

	PUnicodeBidi
	PZIndex
	POpacity
	PColumnRuleStyle
	PColumnRuleWidth

	PBorderImageSlice
	PBorderImageWidth
	PBorderImageOutset
	PBorderImageRepeat
	PMaskBorderSlice
	PMaskBorderWidth
	PMaskBorderOutset
	PMaskBorderRepeat
	PMaskBorderMode

	PObjectFit
	PObjectPosition

	PBoxShadow
	PTextShadow

	nbProperties
)
