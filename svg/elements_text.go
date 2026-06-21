package svg

import (
	"math"
	"strings"

	"github.com/benoitkugler/webrender/backend"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/text"
	drawText "github.com/benoitkugler/webrender/text/draw"
)

// text tags
type textSpan struct {
	style  pr.Properties
	text   string
	rotate []Fl // angles in degrees

	x, y, dx, dy []Value

	letterSpacing, textLength Value
	lengthAdjust              bool // true for spacingAndGlyphs

	textAnchor, displayAnchor anchor

	baseline baseline

	isText          bool // only true for tag 'text'
	textBoundingBox Rectangle

	// manualShift, when non-zero, is added to every character's X
	// position by textSpan.draw. svg.go's drawNode sets this on each
	// textSpan in the subtree of a <text> element with text-anchor
	// middle/end, after measuring the union width of the subtree.
	// This produces the equivalent of a post-draw Translate without
	// needing a real group buffer.
	manualShift Fl
}

func newTextSpan(node *cascadedNode) (drawable, error) {
	var out textSpan

	out.text = string(node.text)
	out.style = pr.InitialValues.Copy()

	family := "sans-serif"
	if f, has := node.attrs["font-family"]; has {
		family = f
	}
	// Strip CSS quoting from each comma-separated family name. Values
	// flowing through the `font` shorthand expander come back serialized
	// with quotes (e.g. `font: 2px 'weasyprint'` → font-family: `"weasyprint"`).
	familyList := strings.Split(family, ",")
	for i, f := range familyList {
		f = strings.TrimSpace(f)
		if len(f) >= 2 {
			if (f[0] == '"' && f[len(f)-1] == '"') || (f[0] == '\'' && f[len(f)-1] == '\'') {
				f = f[1 : len(f)-1]
			}
		}
		familyList[i] = f
	}
	out.style.SetFontFamily(familyList)

	if w, has := node.attrs["font-weight"]; has {
		out.style.SetFontWeight(pr.IntString{Int: parseFontWeight(w)})
	}

	if s, has := node.attrs["font-style"]; has {
		out.style.SetFontStyle(pr.String(s))
	}

	// get rotations and translations
	var err error
	out.x, err = parseValues(node.attrs["x"])
	if err != nil {
		return nil, err
	}
	out.y, err = parseValues(node.attrs["y"])
	if err != nil {
		return nil, err
	}

	out.dx, err = parseValues(node.attrs["dx"])
	if err != nil {
		return nil, err
	}
	out.dy, err = parseValues(node.attrs["dy"])
	if err != nil {
		return nil, err
	}

	out.rotate, err = parsePoints(node.attrs["rotate"], nil, false)
	if err != nil {
		return nil, err
	}

	out.letterSpacing, err = parseValue(node.attrs["letter-spacing"])
	if err != nil {
		return nil, err
	}
	out.textLength, err = parseValue(node.attrs["textLength"])
	if err != nil {
		return nil, err
	}

	out.textAnchor = parseAnchor(node.attrs["text-anchor"])
	out.displayAnchor = parseAnchor(node.attrs["display-anchor"])

	baseline, has := node.attrs["dominant-baseline"]
	if !has {
		baseline = node.attrs["alignment-baseline"]
	}
	out.baseline = parseBaseline(baseline)

	out.lengthAdjust = node.attrs["lengthAdjust"] == "spacingAndGlyphs"

	out.isText = node.tag == "text"
	out.textBoundingBox = emptyBbox

	return &out, nil
}

// returns the text bounding box
func (t *textSpan) draw(dst backend.Canvas, attrs *attributes, svg *SVGImage, dims drawingDims) []vertex {
	t.style.SetFontSize(pr.FToV(dims.fontSize))

	splitted := text.SplitFirstLine([]rune(t.text), t.style, svg.textContext, pr.Inf, false, true)
	width, height := Fl(splitted.Width), Fl(splitted.Height)

	// Get rotations and translations
	var xs, ys, dxs, dys []Fl
	for _, v := range t.x {
		xs = append(xs, v.Resolve(dims.fontSize, dims.innerWidth))
	}
	for _, v := range t.y {
		ys = append(ys, v.Resolve(dims.fontSize, dims.innerHeight))
	}
	for _, v := range t.dx {
		dxs = append(dxs, v.Resolve(dims.fontSize, dims.innerWidth))
	}
	for _, v := range t.dy {
		dys = append(dys, v.Resolve(dims.fontSize, dims.innerHeight))
	}

	var yAlign Fl

	letterSpacing := dims.length(t.letterSpacing)
	textLength := dims.length(t.textLength)
	scaleX := Fl(1.)
	if textLength != 0 && t.text != "" {
		// calculate the number of spaces to be considered for the text
		spacesCount := Fl(len(t.text) - 1)
		if t.lengthAdjust {
			// scale letterSpacing up/down to textLength
			widthWithSpacing := width + spacesCount*letterSpacing
			letterSpacing *= textLength / widthWithSpacing
			// calculate the glyphs scaling factor by:
			// - deducting the scaled letterSpacing from textLength
			// - dividing the calculated value by the original width
			spacelessTextLength := textLength - spacesCount*letterSpacing
			scaleX = spacelessTextLength / width
		} else if spacesCount != 0 {
			// adjust letter spacing to fit textLength
			letterSpacing = (textLength - width) / spacesCount
		}
		width = textLength
	}
	ascentL, descentL := dims.fontSize*.8, dims.fontSize*.2

	// align text box vertically
	if t.displayAnchor == middle {
		yAlign = -height / 2
	} else if t.displayAnchor == top {
		// pass
	} else if t.displayAnchor == bottom {
		yAlign = -height
	} else if t.baseline == central {
		yAlign = (ascentL+descentL)/2 - descentL
	} else if t.baseline == ascent {
		yAlign = ascentL
	} else if t.baseline == descent {
		yAlign = -descentL
	}

	// return early when there’s no text,
	// update the cursor position though
	if t.text == "" {
		x0 := svg.cursorPosition.x
		if len(xs) != 0 {
			x0 = xs[0]
		}
		y0 := svg.cursorPosition.y
		if len(ys) != 0 {
			y0 = ys[0]
		}
		var dx0, dy0 Fl
		if len(dxs) != 0 {
			dx0 = dxs[0]
		}
		if len(dys) != 0 {
			dy0 = dys[0]
		}
		svg.cursorPosition = point{x0 + dx0, y0 + dy0}
		return nil
	}

	// Draw letters
	chars := []rune(t.text)
	var (
		bbox   Rectangle
		texts  []backend.TextDrawing
		drawer = drawText.Context{Output: dst, Fonts: svg.textContext.Fonts()}
	)
	for i, r := range chars {
		hasX, hasY := i < len(xs), i < len(ys)

		var angle Fl // en radians
		if i < len(t.rotate) {
			angle = t.rotate[i] * math.Pi / 180
		} else if L := len(t.rotate); L != 0 {
			angle = t.rotate[L-1] * math.Pi / 180
		}

		if hasX && xs[i] != 0 { // x specified
			svg.cursorDPosition.x = 0
		}
		if hasY && ys[i] != 0 { // y specified
			svg.cursorDPosition.y = 0
		}
		if i < len(dxs) {
			svg.cursorDPosition.x += dxs[i]
		}
		if i < len(dys) {
			svg.cursorDPosition.y += dys[i]
		}

		splitted := text.SplitFirstLine([]rune{r}, t.style, svg.textContext, pr.Inf, false, true)
		layout := splitted.Layout
		width, height = Fl(splitted.Width), Fl(splitted.Height)

		x, y := svg.cursorPosition.x, svg.cursorPosition.y
		if hasX {
			x = xs[i]
		}
		if hasY {
			y = ys[i]
		}

		width *= scaleX
		if i != 0 {
			x += letterSpacing
		}
		svg.cursorPosition = point{x + width, y}

		xPosition := x + svg.cursorDPosition.x
		yPosition := y + svg.cursorDPosition.y + yAlign

		pointsBb := Rectangle{
			xPosition, yPosition,
			width, -height,
		}
		if i == 0 {
			bbox = pointsBb
		} else {
			bbox.union(pointsBb)
		}

		layout.ApplyJustification()

		doFill, doStroke := svg.applyPainters(dst, &svgNode{graphicContent: t, attributes: *attrs}, dims)
		dst.State().SetTextPaint(newPaintOp(doFill, doStroke, false))
		texts = append(texts,
			drawer.CreateFirstLine(layout, "none", pr.TaggedString{Tag: pr.None}, scaleX, xPosition, yPosition, angle))
	}

	// Apply text-anchor shift. Three cases:
	//   1. svg.go pre-computed a shift across the <text> subtree
	//      (covers <text text-anchor="..."> with or without nested
	//      tspans, where the outer node's bbox is unknown when this
	//      runs because children haven't drawn yet).
	//   2. This is a <tspan> with its own text-anchor (e.g.
	//      <text><tspan x="10" text-anchor="middle">...). The tspan's
	//      bbox is local; we use its own width.
	//   3. Otherwise (no shift), draw at unshifted positions.
	shift := t.manualShift
	if shift == 0 && (t.textAnchor == middle || t.textAnchor == end) && !t.isText {
		// Case 2: tspan with its own anchor and no override from parent.
		if t.textAnchor == middle {
			shift = -bbox.Width / 2
		} else {
			shift = -bbox.Width
		}
	}
	if shift != 0 {
		for i := range texts {
			texts[i].X += shift
		}
		bbox.X += shift
	}

	dst.OnNewStack(func() {
		dst.DrawText(texts)
	})

	t.textBoundingBox = bbox

	return nil
}
