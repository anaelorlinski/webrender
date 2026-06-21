package validation

import (
	"testing"

	pa "github.com/benoitkugler/webrender/css/parser"
)

// HasVar must detect var() nested inside every compound token type, not just
// functions and parentheses. Regression test: var() inside square/curly
// brackets used to be missed because the walker enumerated only two of the
// four compound types.
func TestHasVarAllCompoundTypes(t *testing.T) {
	cases := []struct {
		css  string
		want bool
	}{
		{"var(--x)", true},
		{"calc(var(--x) + 1px)", true},         // function
		{"calc((var(--x) - var(--y)) * 0.5)", true}, // parentheses
		{"[var(--x)]", true},                   // square brackets
		{"foo([var(--x)])", true},              // function → square brackets
		{"{ color: var(--x) }", true},          // curly brackets
		{"foo(({var(--x)}))", true},            // deeply mixed nesting
		{"calc(1px + 2px)", false},             // no var
		{"foo([bar] (baz))", false},            // compound, no var
		{"red", false},
	}
	for _, tc := range cases {
		tok := pa.ParseOneComponentValue(pa.Tokenize([]byte(tc.css), true))
		if got := HasVar(tok); got != tc.want {
			t.Errorf("HasVar(%q) = %v, want %v", tc.css, got, tc.want)
		}
	}
}
