package draw

import (
	"bytes"
	"fmt"

	"github.com/benoitkugler/webrender/backend"
	pr "github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/matrix"
	"github.com/benoitkugler/webrender/text"
	"github.com/benoitkugler/webrender/utils"
	"github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"
)

var _ backend.Font = (*gotextFont)(nil)

type gotextFont struct {
	face *font.Face
	id   font.FontID
	size int
}

func (f *gotextFont) Origin() text.FontOrigin { return text.FontOrigin(f.id) }

func (f *gotextFont) Description() backend.FontDescription {
	extents, _ := f.face.FontHExtents()
	meta := f.face.Describe()
	upem := float32(f.face.Upem())

	out := backend.FontDescription{
		Family: meta.Family,
		Size:   f.size,
		Style:  fontStyle(meta.Aspect.Style),
		Weight: int(meta.Aspect.Weight),
	}
	if f.size != 0 {
		out.Ascent = extents.Ascender * upem / utils.Fl(f.size)
		out.Descent = extents.Descender * upem / utils.Fl(f.size)
	}

	out.IsOpentype = true
	// out.IsOpentypeOpentype = face.Type == truetype.TypeOpenType // FIXME : requires recent go-text

	return out
}

func fontStyle(s font.Style) text.FontStyle {
	switch s {
	case font.StyleNormal:
		return text.FSty_Normal
	case font.StyleItalic:
		return text.FSty_Italic
	default:
		return text.FSty_Normal
	}
}

func (ctx Context) createFirstLineGotext(layout text.TextLayoutGotext,
	textOverflow string, blockEllipsis pr.TaggedString, scaleX, x, y, angle pr.Fl,
) backend.TextDrawing {
	fts := ctx.Fonts.(*text.FontConfigurationGotext)
	style := layout.Style

	// var ellipsis string
	// if textOverflow == "ellipsis" || blockEllipsis.Tag != pr.None {
	// 	// assert layout.maxWidth is not nil
	// 	// maxWidth := layout.MaxWidth.V()
	// 	// pl.SetWidth(pango.Unit(text.PangoUnitsFromFloat(pr.Fl(maxWidth))))
	// 	if textOverflow == "ellipsis" {
	// 		pl.SetEllipsize(pango.ELLIPSIZE_END)
	// 	} else {
	// 		ellipsis = blockEllipsis.S
	// 		if blockEllipsis.Tag == pr.Auto {
	// 			ellipsis = "…"
	// 		}
	// 		// Remove last word if hyphenated
	// 		newText := layout.Text()
	// 		if hyph := style.HyphenateCharacter; strings.HasSuffix(string(newText), hyph) {
	// 			lastWordEnd := fts.GetLastWordEnd(newText[:len(newText)-len([]rune(hyph))])
	// 			if lastWordEnd != -1 && lastWordEnd != 0 {
	// 				newText = newText[:lastWordEnd]
	// 			}
	// 		}
	// 		layout.SetText(string(newText) + ellipsis)
	// 	}
	// }

	// firstLine, index := layout.GetFirstLine()
	// if blockEllipsis.Tag != pr.None {
	// 	for index != 0 && index != -1 {
	// 		lastWordEnd := text.GetLastWordEnd(fts, pl.Text[:len(pl.Text)-len([]rune(ellipsis))])
	// 		if lastWordEnd == -1 {
	// 			break
	// 		}
	// 		newText := pl.Text[:lastWordEnd]
	// 		layout.SetText(string(newText) + ellipsis)
	// 		firstLine, index = layout.GetFirstLine()
	// 	}
	// }

	var (
		output        backend.TextDrawing
		lastFont      *font.Font
		lastFontChars *backend.FontChars
		xAdvance      pr.Fl
	)

	fontSize := style.FontDescription.Size
	textRunes := layout.Text()

	output.FontSize = fontSize
	output.ScaleX = scaleX
	output.X, output.Y = x, y
	output.Angle = angle
	output.Text = textRunes

	for _, run := range layout.Line {

		// Pango objects
		// glyphItem := run.Data
		// glyphString := glyphItem.Glyphs
		// offset := glyphItem.Item.Offset

		// Font content
		face := run.Face
		outFont := lastFontChars
		if lastFont != face.Font { // add a new "run"
			location := fts.FontLocation(face.Font)
			content := fts.FontContent(location)
			backendFont := &gotextFont{face, location, run.Size.Round()}
			outFont = ctx.Output.AddFont(backendFont, content)

			lastFontChars = outFont

			outRun := backend.TextRun{Font: backendFont}
			output.Runs = append(output.Runs, outRun)
		} // else use the last one

		runDst := &output.Runs[len(output.Runs)-1]

		runDst.Glyphs = make([]backend.TextGlyph, len(run.Glyphs))
		for i, glyphInfo := range run.Glyphs {
			outGlyph := &runDst.Glyphs[i]
			width := fixedToFloat(glyphInfo.Advance)
			glyph := glyphInfo.GlyphID

			// if glyph == pango.GLYPH_EMPTY || glyph&pango.GLYPH_UNKNOWN_FLAG != 0 {
			// 	outGlyph.Offset = pr.Fl(width) / fontSize
			// 	outGlyph.Glyph = backend.GID(fonts.EmptyGlyph)
			// 	continue
			// }

			outGlyph.Offset = fixedToFloat(glyphInfo.XOffset) / fontSize
			outGlyph.Rise = fixedToFloat(glyphInfo.YOffset)
			outGlyph.Glyph = backend.GID(glyph)

			// Ink bounding box and logical widths in font
			if _, in := outFont.Extents[outGlyph.Glyph]; !in {
				extents, _ := face.GlyphExtents(glyph)
				x1, y1, x2, y2 := extents.XBearing, -extents.YBearing-extents.Height,
					extents.XBearing+extents.Width, -extents.YBearing
				if int(x1) < outFont.Bbox[0] {
					outFont.Bbox[0] = int(x1 / fontSize)
				}
				if int(y1) < outFont.Bbox[1] {
					outFont.Bbox[1] = int(y1 / fontSize)
				}
				if int(x2) > outFont.Bbox[2] {
					outFont.Bbox[2] = int(x2 / fontSize)
				}
				if int(y2) > outFont.Bbox[3] {
					outFont.Bbox[3] = int(y2 / fontSize)
				}
				outFont.Extents[outGlyph.Glyph] = backend.GlyphExtents{
					Width:  int(extents.Width / fontSize),
					Y:      int(extents.YBearing / fontSize),
					Height: int(extents.Height / fontSize),
				}
			}

			// Kerning, word spacing, letter spacing
			outGlyph.Kerning = int(pr.Fl(outFont.Extents[outGlyph.Glyph].Width) - width/fontSize + outGlyph.Offset)

			// Mapping between glyphs and characters
			outGlyph.TextOffset, outGlyph.TextLength = glyphInfo.TextIndex(), glyphInfo.RunesCount()
			if _, in := outFont.Cmap[outGlyph.Glyph]; !in {
				outFont.Cmap[outGlyph.Glyph] = textRunes[outGlyph.TextOffset : outGlyph.TextOffset+outGlyph.TextLength]
			}
			// advance
			outGlyph.XAdvance = xAdvance
			xAdvance += pr.Fl(outFont.Extents[outGlyph.Glyph].Width) + outGlyph.Offset - pr.Fl(outGlyph.Kerning)
		}
	}

	return output
}

func drawEmojiGotext(font_ *gotextFont, glyph backend.GID, extents backend.GlyphExtents,
	fontSize, x, y, xAdvance utils.Fl, dst backend.Canvas,
) {
	face := font_.face
	data := face.GlyphData(font.GID(glyph))

	switch data := data.(type) {
	// TODO: more formats
	case font.GlyphBitmap:
		if data.Format == font.PNG {
			img := backend.RasterImage{
				Content:   bytes.NewReader(data.Data),
				MimeType:  "image/png",
				Rendering: "",
				ID:        utils.Hash(fmt.Sprintf("%p-%d", face, glyph)),
			}

			d := utils.Fl(extents.Width) / 1000
			a := utils.Fl(data.Width) / utils.Fl(data.Height) * d
			f := utils.Fl(-extents.Y-extents.Height)/1000 - fontSize
			f = y + f
			e := xAdvance / 1000
			e = x + e*fontSize

			dst.OnNewStack(func() {
				dst.State().Transform(matrix.New(a, 0, 0, d, e, f))
				dst.DrawRasterImage(img, fontSize, fontSize)
			})
		}
	}
}

func floatToFixed(v pr.Fl) fixed.Int26_6 { return fixed.Int26_6(v * 64) }
func fixedToFloat(v fixed.Int26_6) pr.Fl { return pr.Fl(v) / 64 }
