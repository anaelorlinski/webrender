package images

import (
	"fmt"
	"os"
	"testing"

	"github.com/benoitkugler/webrender/css/properties"
	"github.com/benoitkugler/webrender/svg"
	"github.com/benoitkugler/webrender/utils"
)

func TestLoadLocalImages(t *testing.T) {
	paths := []string{
		"../resources_test/blue.jpg",
		"../resources_test/icon.png",
		"../resources_test/pattern.gif",
		"../resources_test/pattern.svg",
	}
	for _, path := range paths {
		url, err := utils.PathToURL(path)
		if err != nil {
			t.Fatal(err)
		}
		out, err := getImageFromUri(utils.DefaultUrlFetcher, false, url, "", properties.SBoolFloat{String: "none"})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("%T\n", out)
	}
}

func TestSVGDisplayedSize(t *testing.T) {
	f, err := os.Open("../resources_test/pattern.svg")
	if err != nil {
		t.Fatal(err)
	}
	img, err := svg.Parse(f, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, h := img.DisplayedSize()
	if w != (svg.Value{V: 4, U: svg.Px}) {
		t.Fatalf("unexpected width %v", w)
	}
	if h != (svg.Value{V: 4, U: svg.Px}) {
		t.Fatalf("unexpected height %v", h)
	}
}

func TestExpandColorHints(t *testing.T) {
	black := Color{R: 0, G: 0, B: 0, A: 1}
	white := Color{R: 1, G: 1, B: 1, A: 1}

	// No hints: input returned unchanged.
	c := []Color{black, white}
	p := []properties.Fl{0, 10}
	oc, op := expandColorHints(c, p, []bool{false, false})
	if len(oc) != 2 || oc[0] != black || oc[1] != white {
		t.Fatalf("no-hint case altered stops: %v %v", oc, op)
	}

	// colorNearest returns the expanded sample color closest to position pos.
	colorNearest := func(colors []Color, positions []properties.Fl, pos properties.Fl) Color {
		var best Color
		bestD := properties.Fl(1e9)
		for i, p := range positions {
			if d := absFl(p - pos); d < bestD {
				bestD, best = d, colors[i]
			}
		}
		return best
	}

	// Hint exactly at the midpoint must reproduce plain linear interpolation:
	// the sample at the middle of the span is the 50% blend (gray).
	c = []Color{black, {}, white}
	p = []properties.Fl{0, 5, 10} // hint at 5 == midpoint of [0,10]
	oc, op = expandColorHints(c, p, []bool{false, true, false})
	if len(oc) != len(op) {
		t.Fatalf("length mismatch: %d colors, %d positions", len(oc), len(op))
	}
	mid := colorNearest(oc, op, 5)
	if absFl(mid.R-0.5) > 0.05 || absFl(mid.G-0.5) > 0.05 || absFl(mid.B-0.5) > 0.05 {
		t.Errorf("midpoint hint should give ~0.5 gray, got %v", mid)
	}

	// A hint pushed toward the end (75%) biases the transition: at the span
	// midpoint the color should still be darker than 0.5, because the 50%
	// blend now sits at 75% of the way from start to end.
	c = []Color{black, {}, white}
	p = []properties.Fl{0, 7.5, 10}
	oc, op = expandColorHints(c, p, []bool{false, true, false})
	if atHalf := colorNearest(oc, op, 5); atHalf.R >= 0.5 {
		t.Errorf("hint at 75%% should keep span-midpoint dark (<0.5), got R=%v", atHalf.R)
	}
}

func absFl(v properties.Fl) properties.Fl {
	if v < 0 {
		return -v
	}
	return v
}
