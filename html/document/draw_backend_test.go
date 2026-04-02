package document

import (
	"github.com/benoitkugler/webrender/backend"
	"github.com/benoitkugler/webrender/css/parser"
	"github.com/benoitkugler/webrender/matrix"
	"github.com/benoitkugler/webrender/utils"
)

// in memory dummy implementation for tests

var _ backend.Document = &recordingDrawer{}

type recordingDrawer struct {
	texts [][]backend.TextDrawing
}

func (dr *recordingDrawer) AddPage(left, top, right, bottom fl) backend.Page {
	return dr
}

func (dr recordingDrawer) CreateAnchors(anchors [][]backend.Anchor) {
}

func (dr recordingDrawer) SetAttachments(as []backend.Attachment) {
}

func (dr recordingDrawer) EmbedFile(id string, a backend.Attachment) {
}

func (dr recordingDrawer) SetTitle(title string) {
}

func (dr recordingDrawer) SetMetadata(_ string, _ utils.DocumentMetadata) {
}

func (dr recordingDrawer) SetBookmarks([]backend.BookmarkNode) {
}

func (dr recordingDrawer) AddInternalLink(x, y, w, h fl, anchorName string) {
}

func (dr recordingDrawer) AddExternalLink(x, y, w, h fl, url string) {
}

func (dr recordingDrawer) AddFileAnnotation(x, y, w, h fl, id string) {
}

func (dr recordingDrawer) GetBoundingBox() (left, top, right, bottom fl) {
	return 0, 0, 10, 10
}

func (dr recordingDrawer) SetBoundingBox(left, top, right, bottom fl) {
}

func (dr recordingDrawer) SetMediaBox(left, top, right, bottom fl) {
}

func (dr recordingDrawer) SetTrimBox(left, top, right, bottom fl) {
}

func (dr recordingDrawer) SetBleedBox(left, top, right, bottom fl) {
}

func (dr *recordingDrawer) OnNewStack(f func()) {
	f()
}

func (dr recordingDrawer) Rectangle(x fl, y fl, width fl, height fl) {
}

func (dr recordingDrawer) Clip(evenOdd bool) {
}

func (dr recordingDrawer) SetAlpha(alpha fl, stroke bool) {
}

func (dr recordingDrawer) SetColorRgba(color parser.RGBA, stroke bool) {
}

func (dr recordingDrawer) SetLineWidth(width fl) {
}

func (dr recordingDrawer) SetDash(dashes []fl, offset fl) {
}

func (dr recordingDrawer) Paint(op backend.PaintOp) {
}

func (dr recordingDrawer) GetTransform() matrix.Transform {
	return matrix.Identity()
}

func (dr recordingDrawer) Transform(mt matrix.Transform) {
}

func (dr recordingDrawer) MoveTo(x fl, y fl) {
}

func (dr recordingDrawer) LineTo(x fl, y fl) {
}

func (dr recordingDrawer) CubicTo(x1, y1, x2, y2, x3, y3 fl) {
}

func (dr recordingDrawer) ClosePath() {
}

func (dr recordingDrawer) SetTextPaint(p backend.PaintOp) {
}

func (dr recordingDrawer) SetBlendingMode(mode string) {
}

func (dr *recordingDrawer) DrawText(text []backend.TextDrawing) {
	dr.texts = append(dr.texts, text)
}

func (dr recordingDrawer) AddFont(backend.Font, []byte) *backend.FontChars {
	return &backend.FontChars{Cmap: make(map[backend.GID][]rune), Extents: make(map[backend.GID]backend.GlyphExtents)}
}

func (dr *recordingDrawer) NewGroup(x, y, width, height fl) backend.Canvas {
	return dr
}

func (dr recordingDrawer) DrawRasterImage(img backend.RasterImage, width, height fl) {
}

func (dr recordingDrawer) DrawGradient(gradient backend.GradientLayout, width, height fl) {
}

func (dr recordingDrawer) DrawWithOpacity(opacity fl, group backend.Canvas) {
}

func (dr recordingDrawer) SetStrokeOptions(opt backend.StrokeOptions) {
}

func (dr recordingDrawer) SetColorPattern(backend.Canvas, fl, fl, matrix.Transform, bool) {
}

func (dr recordingDrawer) SetAlphaMask(mask backend.Canvas) {
}

func (dr recordingDrawer) State() backend.GraphicState {
	return dr
}
