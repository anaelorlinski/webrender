// Package tracer provides a function to dump the current layout tree,
// which may be used in debug mode.
package tracer

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/html/boxes"
	"github.com/benoitkugler/webrender/utils"
)

type Tracer struct {
	out *os.File

	indentation int
}

// NewTracer panics if an error occurs.
func NewTracer(outFile string) Tracer {
	f, err := os.Create(outFile)
	if err != nil {
		panic(err)
	}

	return Tracer{out: f}
}

func (tr *Tracer) Indent()   { tr.indentation += 1 }
func (tr *Tracer) DeIndent() { tr.indentation -= 1 }

func FormatMaybeFloat(v properties.MaybeFloat) string {
	if v, ok := v.(properties.Float); ok {
		return strconv.FormatFloat(float64(utils.RoundPrec(fl(v), 1)), 'g', -1, 32)
	}
	return fmt.Sprintf("%v", v)
}

func (t Tracer) print(line string) {
	fmt.Fprintln(t.out, strings.Repeat(" ", t.indentation)+line)
}

func (t Tracer) Dump(line string) { t.print(line) }

func (t Tracer) DumpTree(box boxes.Box, context string) {
	t.print("BOX TREE AT " + context + ":")

	var printer func(box boxes.Box, indent int)
	printer = func(box boxes.Box, indent int) {
		if box == nil {
			t.print(strings.Repeat(" ", indent) + "<nil>")
			return
		}

		line := fmt.Sprintf("%s: %s %s %s %s ; %s %s %s %s ; %s %s %s %s", box.Type(),
			FormatMaybeFloat(box.Box().PositionX),
			FormatMaybeFloat(box.Box().PositionY),
			FormatMaybeFloat(box.Box().Width),
			FormatMaybeFloat(box.Box().Height),

			FormatMaybeFloat(box.Box().MarginBottom),
			FormatMaybeFloat(box.Box().MarginTop),
			FormatMaybeFloat(box.Box().MarginRight),
			FormatMaybeFloat(box.Box().MarginLeft),

			FormatMaybeFloat(box.Box().BorderBottomWidth),
			FormatMaybeFloat(box.Box().BorderTopWidth),
			FormatMaybeFloat(box.Box().BorderRightWidth),
			FormatMaybeFloat(box.Box().BorderLeftWidth),
		)
		t.print(strings.Repeat(" ", indent) + line)
		if tb, ok := box.(*boxes.TextBox); ok {
			t.print(fmt.Sprintf("%q", tb.TextS()))
		}

		for _, child := range box.Box().Children {
			printer(child, indent+1)
		}
	}

	printer(box, 0)

	t.print("END BOX TREE\n")
}
