package tree

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	kw "github.com/benoitkugler/webrender/css/properties/keywords"
	"github.com/benoitkugler/webrender/css/validation"
	"github.com/benoitkugler/webrender/logger"
	"github.com/benoitkugler/webrender/text"

	"github.com/benoitkugler/webrender/css/parser"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/utils"
)

// Convert *specified* property values (the result of the cascade and
// inheritance) into *computed* values (that are inherited).

// :copyright: Copyright 2011-2014 Simon Sapin and contributors, see AUTHORS.
// :license: BSD, see LICENSE for details.

var (
	// These are unspecified, other than 'thin' <='medium' <= 'thick'.
	// Values are in pixels.
	borderWidthKeywords = map[pr.Tag]pr.Float{
		pr.Thin:   1,
		pr.Medium: 3,
		pr.Thick:  5,
	}

	// http://www.w3.org/TR/CSS21/fonts.html#propdef-font-weight
	fontWeightRelative = struct {
		bolder, lighter map[int]int
	}{
		bolder: map[int]int{
			100: 400,
			200: 400,
			300: 400,
			400: 700,
			500: 700,
			600: 900,
			700: 900,
			800: 900,
			900: 900,
		},
		lighter: map[int]int{
			100: 100,
			200: 100,
			300: 100,
			400: 100,
			500: 100,
			600: 400,
			700: 400,
			800: 700,
			900: 700,
		},
	}

	// Maps property names to functions returning the computed values
	computerFunctions = [pr.NbProperties]computerFunc{}

	// to avoid declaration cycle
	tmp = [pr.NbProperties]computerFunc{
		pr.PBackgroundImage:    backgroundImage,
		pr.PBackgroundPosition: backgroundPosition,
		pr.PObjectPosition:     objectPosition,
		pr.PTransformOrigin:    transformOrigin,

		pr.PBorderSpacing:           borderSpacing,
		pr.PSize:                    size,
		pr.PClip:                    clip,
		pr.PBorderTopLeftRadius:     borderRadius,
		pr.PBorderTopRightRadius:    borderRadius,
		pr.PBorderBottomLeftRadius:  borderRadius,
		pr.PBorderBottomRightRadius: borderRadius,

		pr.PBreakBefore: break_,
		pr.PBreakAfter:  break_,

		pr.PTop:                     length,
		pr.PRight:                   length,
		pr.PLeft:                    length,
		pr.PBottom:                  length,
		pr.PMarginTop:               length,
		pr.PMarginRight:             length,
		pr.PMarginBottom:            length,
		pr.PMarginLeft:              length,
		pr.PHeight:                  length,
		pr.PWidth:                   length,
		pr.PMinWidth:                length,
		pr.PMinHeight:               length,
		pr.PMaxWidth:                length,
		pr.PMaxHeight:               length,
		pr.PPaddingTop:              length,
		pr.PPaddingRight:            length,
		pr.PPaddingBottom:           length,
		pr.PPaddingLeft:             length,
		pr.PTextIndent:              length,
		pr.PHyphenateLimitZone:      length,
		pr.PFlexBasis:               length,
		pr.PTextUnderlineOffset:     length,
		pr.PTextDecorationThickness: length,

		pr.PBleedLeft:         bleed,
		pr.PBleedRight:        bleed,
		pr.PBleedTop:          bleed,
		pr.PBleedBottom:       bleed,
		pr.PLetterSpacing:     pixelLength,
		pr.PBackgroundSize:    backgroundSize,
		pr.PImageOrientation:  imageOrientation,
		pr.PBorderTopWidth:    borderWidth,
		pr.PBorderRightWidth:  borderWidth,
		pr.PBorderLeftWidth:   borderWidth,
		pr.PBorderBottomWidth: borderWidth,
		pr.PColumnRuleWidth:   borderWidth,
		pr.POutlineWidth:      borderWidth,
		pr.PColumnWidth:       columnWidth,

		pr.PBorderImageSlice:  borderImageSlice,
		pr.PBorderImageWidth:  borderImageWidth,
		pr.PBorderImageOutset: borderImageOutset,
		pr.PBorderImageRepeat: borderImageRepeat,
		pr.PMaskBorderSlice:   borderImageSlice,
		pr.PMaskBorderWidth:   borderImageWidth,
		pr.PMaskBorderOutset:  borderImageOutset,
		pr.PMaskBorderRepeat:  borderImageRepeat,

		pr.PGridTemplateColumns: gridTemplate,
		pr.PGridTemplateRows:    gridTemplate,
		pr.PGridAutoColumns:     gridAuto,
		pr.PGridAutoRows:        gridAuto,

		pr.PColumnGap:     gap,
		pr.PRowGap:        gap,
		pr.PContent:       content,
		pr.PDisplay:       display,
		pr.PFloat:         floating,
		pr.PFontSize:      fontSize,
		pr.PFontWeight:    fontWeight,
		pr.PLineHeight:    lineHeight,
		pr.PAnchor:        anchor,
		pr.PLang:          lang,
		pr.PTabSize:       tabSize,
		pr.PTransform:     transforms,
		pr.PVerticalAlign: verticalAlign,
		pr.PWordSpacing:   wordSpacing,
		pr.PBookmarkLabel: bookmarkLabel,
		pr.PStringSet:     stringSet,
		pr.PLink:          link,
	}
)

func init() {
	if pr.InitialValues.GetBorderTopWidth().Value != borderWidthKeywords[pr.Medium] {
		panic("border-top-width and medium should be the same !")
	}

	// In "portrait" orientation.
	for _, size := range pr.PageSizes {
		if size[0].Value > size[1].Value {
			panic("page size should be in portrait orientation")
		}
	}

	computerFunctions = tmp
}

type computerFunc = func(*ComputedStyle, pr.KnownProp, pr.CssProperty) pr.CssProperty

// backgroundImage computes lenghts in gradient background-image.
func backgroundImage(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Images)
	for i, image := range value {
		switch gradient := image.(type) {
		case pr.LinearGradient:
			for j, cl := range gradient.ColorStops {
				if !cl.Position.IsNone() {
					cl.Position = length_(computer, pr.TaggedDim{Dimension: cl.Position}, -1, false).Dimension
					gradient.ColorStops[j] = cl
				}
			}
			image = gradient
		case pr.RadialGradient:
			for j, cl := range gradient.ColorStops {
				if !cl.Position.IsNone() {
					cl.Position = length_(computer, pr.TaggedDim{Dimension: cl.Position}, -1, false).Dimension
					gradient.ColorStops[j] = cl
				}
			}
			gradient.Center = centers(computer, []pr.Center{gradient.Center})[0]
			if gradient.Size.IsExplicit() {
				l := _lengthOrPercentageTuple2(computer, gradient.Size.Explicit.ToSlice())
				gradient.Size.Explicit = pr.Point{l[0], l[1]}
			}
			image = gradient
		}
		value[i] = image
	}
	return value
}

func centers(computer *ComputedStyle, value pr.Centers) pr.Centers {
	out := make(pr.Centers, len(value))
	for index, v := range value {
		out[index] = pr.Center{
			OriginX: v.OriginX,
			OriginY: v.OriginY,
			Pos: pr.Point{
				length_(computer, pr.TaggedDim{Dimension: v.Pos[0]}, -1, false).Dimension,
				length_(computer, pr.TaggedDim{Dimension: v.Pos[1]}, -1, false).Dimension,
			},
		}
	}
	return out
}

// backgroundPosition compute lengths in background-position.
func backgroundPosition(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Centers)
	return centers(computer, value)
}

func objectPosition(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Center)
	return centers(computer, pr.Centers{value})[0]
}

func transformOrigin(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Point)
	l := _lengthOrPercentageTuple2(computer, value.ToSlice())
	return pr.Point{l[0], l[1]}
}

func clip(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	return lengths_(computer, _value.(pr.Values))
}

// Compute the lists of lengths that can be percentages.
// returns a slice with same length as input
func lengths_(computer *ComputedStyle, value pr.Values) pr.Values {
	out := make(pr.Values, len(value))
	for index, v := range value {
		out[index] = length_(computer, v, -1, true)
	}
	return out
}

// Compute the lists of lengths that can be percentages.
func size(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Point)
	for index, v := range value {
		value[index] = length_(computer, v.Tagged(), -1, true).Dimension
	}
	return value
}

func borderSpacing(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Point)
	values := pr.Values{value[0].Tagged(), value[1].Tagged()}
	tmp := lengths_(computer, values)
	return pr.Point{tmp[0].Dimension, tmp[1].Dimension}
}

func borderRadius(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Point)
	var out pr.Point
	for index, v := range value {
		out[index] = length_(computer, v.Tagged(), -1, false).Dimension
	}
	return out
}

// Compute the lists of lengths that can be percentages.
func _lengthOrPercentageTuple2(computer *ComputedStyle, value []pr.Dimension) []pr.Dimension {
	out := make([]pr.Dimension, len(value))
	for index, v := range value {
		out[index] = length_(computer, v.Tagged(), -1, false).Dimension
	}
	return out
}

// Compute the “break-before“ and “break-after“ pr.
func break_(_ *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.String)
	if value == "always" {
		return pr.String("page")
	}
	return value
}

func length(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	return length_(computer, value, -1, false)
}

func asPixels(v pr.TaggedDim, pixelsOnly bool) pr.TaggedDim {
	if pixelsOnly {
		v.Unit = pr.Scalar
	}
	return v
}

// Computes a length “value“.
// passing a negative fontSize means null
// Always returns a Value which is interpreted as float64 if Unit is zero.
// pixelsOnly=false
func length_(computer *ComputedStyle, value pr.TaggedDim, fontSize pr.Float, pixelsOnly bool) pr.TaggedDim {
	if value.Tag == pr.Auto || value.Tag == pr.Content || value.Tag == pr.FromFont {
		return value
	}
	if value.Value == 0 {
		return asPixels(pr.ZeroPixels.Tagged(), pixelsOnly)
	}

	var result pr.Float
	switch unit := value.Unit; unit {
	case pr.Px:
		return asPixels(value, pixelsOnly)
	case pr.Pt, pr.Pc, pr.In, pr.Cm, pr.Mm, pr.Q:
		// Convert absolute lengths to pixels
		result = value.Value * pr.LengthsToPixels[unit]
	case pr.Em, pr.Ex, pr.Ch, pr.Rem, pr.Lh, pr.Rlh:
		if fontSize < 0 {
			fontSize = computer.GetFontSize().Value
		}
		var fonts text.FontConfiguration
		if computer.textContext != nil {
			fonts = computer.textContext.Fonts()
		}
		switch unit {
		case pr.Ex:
			ratio := text.CharacterRatio(computer, computer.cache, false, fonts)
			result = value.Value * fontSize * ratio
		case pr.Ch:
			ratio := text.CharacterRatio(computer, computer.cache, true, fonts)
			result = value.Value * fontSize * ratio
		case pr.Em:
			result = value.Value * fontSize
		case pr.Rem:
			result = value.Value * computer.rootStyle.GetFontSize().Value
		case pr.Lh:
			line, _ := text.StrutLayout(computer, computer.textContext)
			result = value.Value * line
		case pr.Rlh:
			line, _ := text.StrutLayout(computer.rootStyle, computer.textContext)
			result = value.Value * line
		}
	default:
		// A percentage or "auto": no conversion needed.
		return value
	}

	return asPixels(pr.Dimension{Value: result, Unit: pr.Px}.Tagged(), pixelsOnly)
}

func bleed(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if value.Tag == pr.Auto {
		if computer.GetMarks().Crop {
			return pr.Dimension{Value: 8, Unit: pr.Px}.Tagged() // 6pt
		}
		return pr.ZeroPixels.Tagged()
	}
	return length_(computer, value, -1, false)
}

func pixelLength(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if value.Tag == pr.Normal {
		return value
	}
	out := length_(computer, value, -1, true)
	return out
}

// Compute the “background-size“ pr.
func backgroundSize(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Sizes)
	out := make(pr.Sizes, len(value))
	for index, v := range value {
		if v.Tag == pr.Contain || v.Tag == pr.Cover {
			out[index] = pr.Size{Tag: v.Tag}
		} else {
			out[index] = pr.Size{
				Width:  length_(computer, v.Width, -1, false),
				Height: length_(computer, v.Height, -1, false),
			}
		}
	}
	return out
}

// Compute the “image-orientation“ properties.
func imageOrientation(_ *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.SBoolFloat)
	if value.String == "none" || value.String == "from-image" {
		return _value
	}
	angle := value.Float
	value.Float = pr.Fl(int(math.Round(float64(angle)/math.Pi*2)) % 4 * 90)
	return value
}

// Compute the “border-*-width“ pr.
// value.String may be the string representation of an int
func borderWidth(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	// style prop is just before width
	style := computer.Get(pr.PropKey{KnownProp: name - 1}).(pr.String)

	if style == "none" || style == "hidden" {
		return pr.FToV(0)
	}
	if bw, in := borderWidthKeywords[value.Tag]; in {
		return bw.ToValue()
	}
	d := length_(computer, value, -1, true)
	return d
}

// Compute the “border-image-slice“ property.
func borderImageSlice(_ *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	var (
		values         = _value.(pr.Values)
		computedValues pr.Values
		fill           pr.TaggedDim
	)
	for _, value := range values {
		if value.Tag == pr.Fill {
			fill = value
		} else {
			if value.Unit != pr.Scalar {
				value.Unit = pr.Perc
			}
			computedValues = append(computedValues, value)
		}
	}

	switch len(computedValues) {
	case 1:
		computedValues = computedValues.Repeat(4)
	case 2:
		computedValues = computedValues.Repeat(2)
	case 3:
		computedValues = append(computedValues, computedValues[1])
	}
	return append(computedValues, fill)
}

// Compute the “border-image-width“ property.
func borderImageWidth(_ *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	values := _value.(pr.Values)
	switch len(values) {
	case 1:
		return values.Repeat(4)
	case 2:
		return values.Repeat(2)
	case 3:
		values = append(values, values[1])
	}
	return values
}

// Compute the “border-image-outset“ property.
func borderImageOutset(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	values := _value.(pr.Values)
	for i, value := range values {
		if value.Unit == pr.Scalar {
			values[i] = value
		} else {
			values[i] = length_(computer, value, 0, false)
		}
	}

	switch len(values) {
	case 1:
		return values.Repeat(4)
	case 2:
		return values.Repeat(2)
	case 3:
		values = append(values, values[1])
	}
	return values
}

// Compute the “border-image-repeat“ property.
func borderImageRepeat(_ *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	values := _value.(pr.Strings)
	if len(values) == 1 {
		return pr.Strings{values[0], values[0]}
	}
	return values
}

// Compute the “column-width“ property.
func columnWidth(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	return length_(computer, value, -1, false)
}

// Compute the “column-gap“ property.
func gap(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if value.Tag == pr.Normal {
		return value
	}
	return length_(computer, value, -1, false)
}

func computeAttrFunction(computer *ComputedStyle, values pr.AttrData) (out pr.ContentProperty, err error) {
	attrName, typeOrUnit, fallback := values.Name, values.TypeOrUnit, values.Fallback
	node, ok := computer.element.(*utils.HTMLNode)
	if !ok {
		return
	}

	var prop pr.InnerContent
	attrValue := node.Get(attrName)
	if attrValue == "" && fallback != nil {
		atrValue_, ok := fallback.(pr.InnerContent)
		if !ok {
			return out, fmt.Errorf("fallback type not supported : %T", fallback)
		}
		prop = atrValue_
	} else {
		switch typeOrUnit {
		case "string":
			prop = pr.String(attrValue) // Keep the string
		case "url":
			if strings.HasPrefix(attrValue, "#") {
				prop = pr.TaggedString{Tag: pr.Internal, S: utils.Unquote(attrValue[1:])}
			} else {
				u, err := utils.SafeUrljoin(computer.baseUrl, attrValue, false)
				if err != nil {
					return out, err
				}
				prop = pr.TaggedString{Tag: pr.External, S: u}
			}
		case "color":
			prop = pr.Color(parser.ParseColorString(strings.TrimSpace(attrValue)))
		case "integer":
			i, err := strconv.Atoi(strings.TrimSpace(attrValue))
			if err != nil {
				return out, err
			}
			prop = pr.Int(i)
		case "number":
			f, err := strconv.ParseFloat(strings.TrimSpace(attrValue), 64)
			if err != nil {
				return out, err
			}
			prop = pr.Float(f)
		case "%":
			f, err := strconv.ParseFloat(strings.TrimSpace(attrValue), 64)
			if err != nil {
				return out, err
			}
			prop = pr.Dimension{Value: pr.Float(f), Unit: pr.Perc}.Tagged()
			typeOrUnit = "length"
		default:
			unit, isUnit := validation.LENGTHUNITS[typeOrUnit]
			angle, isAngle := validation.AngleUnits[typeOrUnit]
			if isUnit {
				f, err := strconv.ParseFloat(strings.TrimSpace(attrValue), 64)
				if err != nil {
					return out, err
				}
				prop = pr.Dimension{Value: pr.Float(f), Unit: unit}.Tagged()
				typeOrUnit = "length"
			} else if isAngle {
				f, err := strconv.ParseFloat(strings.TrimSpace(attrValue), 64)
				if err != nil {
					return out, err
				}
				prop = pr.Dimension{Value: pr.Float(f), Unit: angle}.Tagged()
				typeOrUnit = "angle"
			}
		}
	}
	return pr.ContentProperty{Type: typeOrUnit, Content: prop}, nil
}

func contentList(computer *ComputedStyle, values pr.ContentProperties) (pr.ContentProperties, error) {
	var computedValues pr.ContentProperties
	for _, value := range values {
		var computedValue pr.ContentProperty
		switch value.Type {
		case "string", "content", "url", "quote", "leader()":
			computedValue = value
		case "attr()":
			attr, ok := value.Content.(pr.AttrData)
			if !ok || attr.TypeOrUnit != "string" {
				panic(fmt.Sprintf("invalid attr() property : %v", value.Content))
			}
			var err error
			computedValue, err = computeAttrFunction(computer, attr)
			if err != nil {
				return nil, err
			}
		case "counter()", "counters()", "content()", "element()", "string()":
			// Other values need layout context, their computed value cannot be
			// better than their specified value yet.
			// See build.computeContentList.
			computedValue = value
		case "target-counter()", "target-counters()", "target-text()":
			prop, ok := value.Content.(pr.SContentProps)
			if !ok || len(prop) == 0 {
				return nil, fmt.Errorf("expected a non empty list of String or ContentProperty, got %v", value.Content)
			}
			anchorToken := prop[0].ContentProperty
			if anchorToken.Type == "attr()" {
				proper, err := computeAttrFunction(computer, anchorToken.Content.(pr.AttrData))
				if err != nil {
					return nil, err
				}
				if !proper.IsNone() {
					computedValue = pr.ContentProperty{Type: value.Type, Content: append(pr.SContentProps{{ContentProperty: proper}}, prop[1:]...)}
				}
			} else {
				computedValue = value
			}
		}
		if computedValue.IsNone() {
			logger.WarningLogger.Printf("Unable to compute %v's value for content: %v\n", computer.element, value)
		} else {
			computedValues = append(computedValues, computedValue)
		}
	}
	return computedValues, nil
}

// Compute the “bookmark-label“ property.
func bookmarkLabel(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	if value, ok := _value.(pr.ContentProperties); ok {
		out, err := contentList(computer, value)
		if err != nil {
			logger.WarningLogger.Printf("error computing bookmark-label : %s\n", err)
			return pr.ContentProperties{}
		}
		return out
	}
	return pr.ContentProperties{}
}

// Compute the “string-set“ property.
func stringSet(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	// Spec asks for strings after custom keywords, but we allow content-lists
	if stringset, ok := _value.(pr.StringSet); ok {
		out := make(pr.SContents, len(stringset.Contents))
		for i, sset := range stringset.Contents {
			v, err := contentList(computer, sset.Contents)
			if err != nil {
				logger.WarningLogger.Printf("error computing string-set : %s \n", err)
				return pr.StringSet{}
			}
			out[i] = pr.SContent{String: sset.String, Contents: v}
		}
		return pr.StringSet{String: stringset.String, Contents: out}
	}
	return pr.StringSet{}
}

// Compute the “content“ property.
func content(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	if value, ok := _value.(pr.SContent); ok {
		if value.String == "normal" {
			if computer.pseudoType != "" {
				return pr.SContent{String: "inhibit"}
			} else {
				return pr.SContent{String: "contents"}
			}
		} else if value.String == "none" {
			return pr.SContent{String: "inhibit"}
		}
		props, err := contentList(computer, value.Contents)
		if err != nil {
			logger.WarningLogger.Printf("error computing content : %s\n", err)
			return pr.SContent{}
		}
		return pr.SContent{Contents: props}
	}
	return pr.SContent{}
}

// Compute the “display“ property.
// See http://www.w3.org/TR/CSS21/visuren.html#dis-pos-flo
func display(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Display)
	float_ := computer.specified.Float
	position := computer.specified.Position
	if (!position.Bool && (position.String == "absolute" || position.String == "fixed")) || float_ != "none" || computer.isRootElement() {
		if value == (pr.Display{Outside: kw.InlineTable}) {
			return pr.Display{Outside: kw.Block, Inside: kw.Table}
		} else if d := value.Outside; value.Inside == 0 && value.ListItem == 0 && d.HasTablePrefix() {
			return pr.Display{Outside: kw.Block, Inside: kw.Flow}
		} else if d == kw.Inline {
			if value.Has(kw.ListItem) {
				return pr.Display{Outside: kw.Block, Inside: kw.Flow, ListItem: kw.ListItem}
			} else {
				return pr.Display{Outside: kw.Block, Inside: kw.Flow}
			}
		}
	}
	return value
}

// Compute the “float“ property.
// See http://www.w3.org/TR/CSS21/visuren.html#dis-pos-flo
func floating(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.String)
	position := computer.specified.Position
	if position.String == "absolute" || position.String == "fixed" || position.Bool /* running*/ {
		return pr.String("none")
	}
	return value
}

// Compute the “font-size“ property.
func fontSize(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if in := pr.XxSmall <= value.Tag && value.Tag <= pr.XxLarge; in {
		return pr.FontSizeKeywords[value.Tag-pr.XxSmall].ToValue()
	}

	parentFontSize := pr.InitialValues.GetFontSize().Value
	if computer.parentStyle != nil {
		parentFontSize = computer.parentStyle.GetFontSize().Value
	}

	if value.Tag == pr.Larger {
		for _, keywordValue := range pr.FontSizeKeywords {
			if keywordValue > parentFontSize {
				return keywordValue.ToValue()
			}
		}
		return (parentFontSize * 1.2).ToValue()
	} else if value.Tag == pr.Smaller {
		for i := len(pr.FontSizeKeywords) - 1; i >= 0; i -= 1 {
			if pr.FontSizeKeywords[i] < parentFontSize {
				return (pr.FontSizeKeywords[i]).ToValue()
			}
		}
		return (parentFontSize * 0.8).ToValue()
	} else if value.Unit == pr.Perc {
		return (value.Value * parentFontSize / 100.).ToValue()
	} else {
		return length_(computer, value, parentFontSize, true)
	}
}

// Compute the “font-weight“ property.
func fontWeight(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.IntString)
	var out int
	switch value.String {
	case "normal":
		out = 400
	case "bold":
		out = 700
	case "bolder":
		parentValue := computer.parentStyle.GetFontWeight().Int
		out = fontWeightRelative.bolder[parentValue]
	case "lighter":
		parentValue := computer.parentStyle.GetFontWeight().Int
		out = fontWeightRelative.lighter[parentValue]
	default:
		out = value.Int
	}
	return pr.IntString{Int: out}
}

// Compute track breadth.
func computeTrackBreadth(computer *ComputedStyle, value pr.TaggedDim) pr.TaggedDim {
	if value.Tag == pr.Auto || value.Tag == pr.MinContent || value.Tag == pr.MaxContent {
		return value
	} else {
		if value.Unit == pr.Fr {
			return value
		} else {
			return length_(computer, value, 0, false)
		}
	}
}

// Compute track size.
func trackSize(computer *ComputedStyle, values []pr.GridSpec) []pr.GridSpec {
	var returnValues []pr.GridSpec
	for i, value := range values {
		if i%2 == 0 {
			// line name
			returnValues = append(returnValues, value)
		} else {
			// track section
			switch value := value.(type) {
			case pr.GridDims:
				returnValues = append(returnValues, computeGridDims(computer, value))
			case pr.GridRepeat:
				returnValues = append(returnValues, pr.GridRepeat{Names: trackSize(computer, value.Names), Repeat: value.Repeat})
			case pr.GridNames, pr.GridNameRepeat: // not supported here
			}
		}
	}
	return returnValues
}

// Compute the “grid-template-*“ properties.
func gridTemplate(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	values := _value.(pr.GridTemplate)
	if values.Tag == pr.None || values.Tag == pr.Subgrid {
		return values
	} else {
		return pr.GridTemplate{Names: trackSize(computer, values.Names)}
	}
}

// Compute the “grid-auto-*“ properties.
func gridAuto(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	values := _value.(pr.GridAuto)
	for i, value := range values {
		values[i] = computeGridDims(computer, value)
	}
	return values
}

func computeGridDims(computer *ComputedStyle, value pr.GridDims) pr.GridDims {
	if v1, v2, ok := value.IsMinmax(); ok {
		return pr.NewGridDimsMinmax(computeTrackBreadth(computer, v1), computeTrackBreadth(computer, v2))
	} else if v, ok := value.IsFitcontent(); ok {
		return pr.NewGridDimsFitcontent(computeTrackBreadth(computer, v).Dimension)
	}
	return pr.NewGridDimsValue(computeTrackBreadth(computer, value.V))
}

// Compute the “line-height“ property.
func lineHeight(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	var pixels pr.Float
	switch {
	case value.Tag == pr.Normal:
		return value
	case value.Unit == pr.Scalar:
		return value
	case value.Unit == pr.Perc:
		factor := value.Value / 100.
		fontSizeValue := computer.GetFontSize().Value
		pixels = factor * fontSizeValue
	default:
		pixels = length_(computer, value, -1, true).Value
	}
	return pr.Dimension{Value: pixels, Unit: pr.Px}.Tagged()
}

// Compute the “anchor“ property.
func anchor(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	// _value is either "none" or an AttrData
	attrData, ok := _value.(pr.AttrData)
	if !ok {
		return pr.String("")
	}
	if node, ok := computer.element.(*utils.HTMLNode); ok {
		anchorName := node.Get(attrData.Name)
		if anchorName == "" {
			return pr.String("")
		}
		return pr.String(anchorName)
	}
	return pr.String("")
}

// Compute the “link“ property.
func link(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedString)
	if value.S == "none" {
		return pr.TaggedString{}
	} else if value.Tag == pr.Attr {
		if node, ok := computer.element.(*utils.HTMLNode); ok {
			typeAttr, ok := getLinkAttribute(node, value.S, computer.baseUrl)
			if !ok {
				return pr.TaggedString{}
			}
			return typeAttr
		}
	}
	return value
}

// Return ('external', absolute_uri) or
// ('internal', unquoted_fragment_id) or false
func getLinkAttribute(element *utils.HTMLNode, attrName string, baseUrl string) (pr.TaggedString, bool) {
	attrValue := strings.TrimSpace(element.Get(attrName))
	if strings.HasPrefix(attrValue, "#") && len(attrValue) > 1 {
		// Do not require a baseUrl when the value is just a fragment.
		unescaped := utils.Unquote(attrValue[1:])
		return pr.TaggedString{Tag: pr.Internal, S: unescaped}, true
	}

	uri := element.GetUrlAttribute(attrName, baseUrl, true)
	if uri == "" {
		return pr.TaggedString{}, false
	}
	if baseUrl != "" {
		parsed, err := url.Parse(uri)
		if err != nil {
			logger.WarningLogger.Println(err)
			return pr.TaggedString{}, false
		}
		baseParsed, err := url.Parse(baseUrl)
		if err != nil {
			logger.WarningLogger.Println(err)
			return pr.TaggedString{}, false
		}
		if parsed.Scheme == baseParsed.Scheme && parsed.Host == baseParsed.Host && parsed.Path == baseParsed.Path && parsed.RawQuery == baseParsed.RawQuery {
			// Compare with fragments removed
			return pr.TaggedString{Tag: pr.Internal, S: parsed.Fragment}, true
		}
	}
	return pr.TaggedString{Tag: pr.External, S: uri}, true
}

// Compute the “lang“ property.
func lang(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedString)
	if value.Tag == pr.None {
		return pr.TaggedString{}
	}
	if node, ok := computer.element.(*utils.HTMLNode); ok && value.Tag == pr.Attr {
		s := node.Get(value.S)
		if s == "" {
			return pr.TaggedString{}
		}
		return pr.TaggedString{S: s}
	}
	return pr.TaggedString{S: value.S}
}

// Compute the “tab-size“ property.
func tabSize(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if value.Unit == pr.Scalar {
		return value
	}
	return length_(computer, value, -1, false)
}

// Compute the “transform“ property.
func transforms(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.Transforms)
	result := make(pr.Transforms, len(value))
	for index, tr := range value {
		if tr.String == "translate" {
			tr.Dimensions = _lengthOrPercentageTuple2(computer, tr.Dimensions)
		}
		result[index] = tr
	}
	return result
}

// Compute the “vertical-align“ property.
func verticalAlign(computer *ComputedStyle, _ pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	// Use +/- half an em for super and sub, same as Pango.
	// (See the SUPERSUBRISE constant in pango-markup.c)
	var out pr.TaggedDim
	switch value.Tag {
	case pr.Baseline, pr.Middle, pr.TextTop, pr.TextBottom, pr.Top, pr.Bottom:
		out.Tag = value.Tag
	case pr.Super:
		out.Value = computer.GetFontSize().Value * 0.5
		out.Unit = pr.Scalar
	case pr.Sub:
		out.Value = computer.GetFontSize().Value * -0.5
		out.Unit = pr.Scalar
	default:
		out.Unit = pr.Scalar
		if value.Unit == pr.Perc {
			height, _ := text.StrutLayout(computer, computer.textContext)
			out.Value = height * value.Value / 100
		} else {
			out.Value = length_(computer, value, -1, true).Value
		}
	}
	return out
}

// Compute the “word-spacing“ property.
func wordSpacing(computer *ComputedStyle, name pr.KnownProp, _value pr.CssProperty) pr.CssProperty {
	value := _value.(pr.TaggedDim)
	if value.Tag == pr.Normal {
		return pr.TaggedDim{}
	}
	return length_(computer, value, -1, false)
}
