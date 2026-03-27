package layout

import (
	"testing"

	pr "github.com/benoitkugler/webrender/css/properties"
	bo "github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/html/tree"
	"github.com/benoitkugler/webrender/utils"
	tu "github.com/benoitkugler/webrender/utils/testutils"
	"github.com/benoitkugler/webrender/utils/testutils/fonts"
)

// Test the HTML presentational hints.

var PHTESTINGCSS, _ = tree.NewCSSDefault(utils.InputString(`
@page {margin: 0; size: 1000px 1000px}
body {margin: 0}
`))

func renderWithPH(t *testing.T, input string, withPH bool, baseUrl string) *bo.PageBox {
	doc, err := tree.NewHTML(utils.InputString(input), baseUrl, nil, "")
	if err != nil {
		t.Fatalf("building tree: %s", err)
	}

	return Layout(doc, []tree.CSS{PHTESTINGCSS}, withPH, fonts.FontConfig)[0]
}

func TestNoPh(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	// Test both CSS and non-CSS rules
	page := renderWithPH(t, `
	<hr size=100 />
	<table align=right width=100><td>0</td></table>
	`, false, "")
	html := unpack1(page)
	body := unpack1(html)
	hr, table := unpack2(body)
	if hr.Box().BorderHeight() == Fl(100) {
		t.Fatal("ht")
	}
	tu.AssertEqualG(t, table.Box().PositionX, Fl(0))
}

func TestPhPage(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	page := renderWithPH(t, `
      <body marginheight=2 topmargin=3 leftmargin=5
            bgcolor=red text=blue />
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	tu.AssertEqual(t, body.Box().MarginTop, Fl(2))
	tu.AssertEqual(t, body.Box().MarginBottom, Fl(2))
	tu.AssertEqual(t, body.Box().MarginLeft, Fl(5))
	tu.AssertEqual(t, body.Box().MarginRight, Fl(0))
	tu.AssertEqualG(t, body.Box().Style.GetBackgroundColor(), pr.NewColor(1, 0, 0, 1))
	tu.AssertEqualG(t, body.Box().Style.GetColor(), pr.NewColor(0, 0, 1, 1))
}

func TestPhFlow(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <pre wrap></pre>
      <center></center>
      <div align=center></div>
      <div align=middle></div>
      <div align=left></div>
      <div align=right></div>
      <div align=justify></div>
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	pre, center, div1, div2, div3, div4, div5 := unpack7(body)
	tu.AssertEqualG(t, pre.Box().Style.GetWhiteSpace(), pr.PreWrap)
	tu.AssertEqualG(t, center.Box().Style.GetTextAlignAll(), pr.String("center"))
	tu.AssertEqualG(t, div1.Box().Style.GetTextAlignAll(), pr.String("center"))
	tu.AssertEqualG(t, div2.Box().Style.GetTextAlignAll(), pr.String("center"))
	tu.AssertEqualG(t, div3.Box().Style.GetTextAlignAll(), pr.String("left"))
	tu.AssertEqualG(t, div4.Box().Style.GetTextAlignAll(), pr.String("right"))
	tu.AssertEqualG(t, div5.Box().Style.GetTextAlignAll(), pr.String("justify"))
}

func TestPhPhrasing(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <style>@font-face {
        src: url(weasyprint.otf); font-family: weasyprint
      }</style>
      <br clear=left>
      <br clear=right />
      <br clear=both />
      <br clear=all />
      <font color=red face=weasyprint size=7></font>
      <Font size=4></Font>
      <font size=+5 ></font>
      <font size=-5 ></font>
    `, true, baseUrl)
	html := unpack1(page)
	body := unpack1(html)
	line1, line2, line3, line4, line5 := unpack5(body)
	br1 := unpack1(line1)
	br2 := unpack1(line2)
	br3 := unpack1(line3)
	br4 := unpack1(line4)
	font1, font2, font3, font4 := unpack4(line5)
	tu.AssertEqualG(t, br1.Box().Style.GetClear(), pr.String("left"))
	tu.AssertEqualG(t, br2.Box().Style.GetClear(), pr.String("right"))
	tu.AssertEqualG(t, br3.Box().Style.GetClear(), pr.String("both"))
	tu.AssertEqualG(t, br4.Box().Style.GetClear(), pr.String("both"))
	tu.AssertEqualG(t, font1.Box().Style.GetColor(), pr.NewColor(1, 0, 0, 1))
	tu.AssertEqualG(t, font1.Box().Style.GetFontFamily(), pr.Strings{"weasyprint"})
	tu.AssertEqualG(t, font1.Box().Style.GetFontSize(), pr.FToV(1.5*2*16))
	tu.AssertEqualG(t, font2.Box().Style.GetFontSize(), pr.FToV(6./5*16))
	tu.AssertEqualG(t, font3.Box().Style.GetFontSize(), pr.FToV(1.5*2*16))
	tu.AssertEqualG(t, font4.Box().Style.GetFontSize(), pr.FToV(8./9*16))
}

func TestPhLists(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <ol>
        <li type=A></li>
        <li type=1></li>
        <li type=a></li>
        <li type=i></li>
        <li type=I></li>
      </ol>
      <ul>
        <li type=circle></li>
        <li type=disc></li>
        <li type=square></li>
      </ul>
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	ol, ul := unpack2(body)
	oli1, oli2, oli3, oli4, oli5 := unpack5(ol)
	uli1, uli2, uli3 := unpack3(ul)
	tu.AssertEqualG(t, oli1.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "upper-alpha"})
	tu.AssertEqualG(t, oli2.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "decimal"})
	tu.AssertEqualG(t, oli3.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "lower-alpha"})
	tu.AssertEqualG(t, oli4.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "lower-roman"})
	tu.AssertEqualG(t, oli5.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "upper-roman"})
	tu.AssertEqualG(t, uli1.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "circle"})
	tu.AssertEqualG(t, uli2.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "disc"})
	tu.AssertEqualG(t, uli3.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "square"})
}

func TestPhListsTypes(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)
	page := renderWithPH(t, `
      <ol type=A></ol>
      <ol type=1></ol>
      <ol type=a></ol>
      <ol type=i></ol>
      <ol type=I></ol>
      <ul type=circle></ul>
      <ul type=disc></ul>
      <ul type=square></ul>
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	ol1, ol2, ol3, ol4, ol5, ul1, ul2, ul3 := unpack8(body)
	tu.AssertEqualG(t, ol1.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "upper-alpha"})
	tu.AssertEqualG(t, ol2.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "decimal"})
	tu.AssertEqualG(t, ol3.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "lower-alpha"})
	tu.AssertEqualG(t, ol4.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "lower-roman"})
	tu.AssertEqualG(t, ol5.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "upper-roman"})
	tu.AssertEqualG(t, ul1.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "circle"})
	tu.AssertEqualG(t, ul2.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "disc"})
	tu.AssertEqualG(t, ul3.Box().Style.GetListStyleType(), pr.CounterStyleID{Name: "square"})
}

func TestPhTables(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <table align=left rules=none></table>
      <table align=right rules=groups></table>
      <table align=center rules=rows></table>
      <table border=10 cellspacing=3 bordercolor=green>
        <thead>
          <tr>
            <th valign=top></th>
          </tr>
        </thead>
        <tr>
          <td nowrap><h1 align=right></h1><p align=center></p></td>
        </tr>
        <tr>
        </tr>
        <tfoot align=justify>
          <tr>
            <td></td>
          </tr>
        </tfoot>
      </table>
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	wrapper1, wrapper2, wrapper3, wrapper4 := unpack4(body)
	tu.AssertEqualG(t, wrapper1.Box().Style.GetFloat(), pr.String("left"))
	tu.AssertEqualG(t, wrapper2.Box().Style.GetFloat(), pr.String("right"))
	tu.AssertEqualG(t, wrapper3.Box().Style.GetMarginLeft(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, wrapper3.Box().Style.GetMarginRight(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, unpack1(wrapper1).Box().Style.GetBorderLeftStyle(), pr.String("hidden"))
	tu.AssertEqualG(t, wrapper1.Box().Style.GetBorderCollapse(), pr.String("collapse"))
	tu.AssertEqualG(t, unpack1(wrapper2).Box().Style.GetBorderLeftStyle(), pr.String("hidden"))
	tu.AssertEqualG(t, wrapper2.Box().Style.GetBorderCollapse(), pr.String("collapse"))
	tu.AssertEqualG(t, unpack1(wrapper3).Box().Style.GetBorderLeftStyle(), pr.String("hidden"))
	tu.AssertEqualG(t, wrapper3.Box().Style.GetBorderCollapse(), pr.String("collapse"))

	table4 := unpack1(wrapper4)
	tu.AssertEqualG(t, table4.Box().Style.GetBorderTopStyle(), pr.String("outset"))
	tu.AssertEqualG(t, table4.Box().Style.GetBorderTopWidth(), pr.FToV(10))
	tu.AssertEqualG(t, table4.Box().Style.GetBorderSpacing(), pr.Point{pr.FToD(3), pr.FToD(3)})
	r, g, b, _ := table4.Box().Style.GetBorderLeftColor().RGBA.Unpack()
	tu.Assert(t, g > r && g > b)
	headGroup, rowsGroup, footGroup := unpack3(table4)
	head := unpack1(headGroup)
	th := unpack1(head)
	tu.AssertEqualG(t, th.Box().Style.GetVerticalAlign(), pr.TagToV(pr.Top))
	line1, _ := unpack2(rowsGroup)
	td := unpack1(line1)
	tu.AssertEqualG(t, td.Box().Style.GetWhiteSpace(), pr.Nowrap)
	tu.AssertEqualG(t, td.Box().Style.GetBorderTopWidth(), pr.FToV(1))
	tu.AssertEqualG(t, td.Box().Style.GetBorderTopStyle(), pr.String("inset"))
	h1, p := unpack2(td)
	tu.AssertEqualG(t, h1.Box().Style.GetTextAlignAll(), pr.String("right"))
	tu.AssertEqualG(t, p.Box().Style.GetTextAlignAll(), pr.String("center"))
	foot := unpack1(footGroup)
	tr := unpack1(foot)
	tu.AssertEqualG(t, tr.Box().Style.GetTextAlignAll(), pr.String("justify"))
}

func TestPhHr(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <hr align=left>
      <hr align=right />
      <hr align=both color=red />
      <hr align=center noshade size=10 />
      <hr align=all size=8 width=100 />
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	hr1, hr2, hr3, hr4, hr5 := unpack5(body)
	tu.AssertEqual(t, hr1.Box().MarginLeft, Fl(0))
	tu.AssertEqualG(t, hr1.Box().Style.GetMarginRight(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, hr2.Box().Style.GetMarginLeft(), pr.TagToV(pr.Auto))
	tu.AssertEqual(t, hr2.Box().MarginRight, Fl(0))
	tu.AssertEqualG(t, hr3.Box().Style.GetMarginLeft(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, hr3.Box().Style.GetMarginRight(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, hr3.Box().Style.GetColor(), pr.NewColor(1, 0, 0, 1))
	tu.AssertEqualG(t, hr4.Box().Style.GetMarginLeft(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, hr4.Box().Style.GetMarginRight(), pr.TagToV(pr.Auto))
	tu.AssertEqualG(t, hr4.Box().BorderHeight(), Fl(10))
	tu.AssertEqualG(t, hr4.Box().Style.GetBorderTopWidth(), pr.FToV(5))
	tu.AssertEqualG(t, hr5.Box().BorderHeight(), Fl(8))
	tu.AssertEqual(t, hr5.Box().Height, Fl(6))
	tu.AssertEqual(t, hr5.Box().Width, Fl(100))
	tu.AssertEqualG(t, hr5.Box().Style.GetBorderTopWidth(), pr.FToV(1))
}

func TestPhEmbedded(t *testing.T) {
	defer tu.CaptureLogs().AssertNoLogs(t)

	page := renderWithPH(t, `
      <object data="data:image/svg+xml,<svg></svg>"
              align=top hspace=10 vspace=20></object>
      <img src="data:image/svg+xml,<svg></svg>" alt=text
              align=right width=10 height=20 />
      <embed src="data:image/svg+xml,<svg></svg>" align=texttop />
    `, true, "")
	html := unpack1(page)
	body := unpack1(html)
	line := unpack1(body)
	object, _, img, embed, _ := unpack5(line)
	tu.AssertEqualG(t, embed.Box().Style.GetVerticalAlign(), pr.TagToV(pr.TextTop))
	tu.AssertEqualG(t, object.Box().Style.GetVerticalAlign(), pr.TagToV(pr.Top))
	tu.AssertEqual(t, object.Box().MarginTop, Fl(20))
	tu.AssertEqual(t, object.Box().MarginLeft, Fl(10))
	tu.AssertEqualG(t, img.Box().Style.GetFloat(), pr.String("right"))
	tu.AssertEqual(t, img.Box().Width, Fl(10))
	tu.AssertEqual(t, img.Box().Height, Fl(20))
}
