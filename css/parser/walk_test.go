package parser

import "testing"

func parseOne(css string) Token {
	return ParseOneComponentValue(Tokenize([]byte(css), true))
}

// Children / WithChildren must cover every compound token type the tokenizer
// can produce, so that generic walkers (var resolution, calc evaluation, …)
// descend into all of them.
func TestChildrenCoversAllCompoundTypes(t *testing.T) {
	for _, tc := range []struct {
		css  string
		kind Kind
	}{
		{"foo(1)", KFunctionBlock},
		{"(1)", KParenthesesBlock},
		{"[1]", KSquareBracketsBlock},
		{"{1}", KCurlyBracketsBlock},
	} {
		tok := parseOne(tc.css)
		if tok.Kind() != tc.kind {
			t.Fatalf("%q: got kind %v, want %v", tc.css, tok.Kind(), tc.kind)
		}
		kids, ok := Children(tok)
		if !ok {
			t.Errorf("%q: Children reported no children for a compound token", tc.css)
		}
		if len(kids) == 0 {
			t.Errorf("%q: expected at least one child token", tc.css)
		}
	}

	// Leaf tokens must report no children.
	for _, css := range []string{"1", "1px", "red", "50%", "'s'"} {
		if _, ok := Children(parseOne(css)); ok {
			t.Errorf("%q: leaf token unexpectedly reported children", css)
		}
	}
}

// WithChildren must preserve identity (kind, name, position) while swapping
// the child list.
func TestWithChildrenPreservesShape(t *testing.T) {
	fn := parseOne("foo(1)")
	pos := fn.Pos()
	repl := WithChildren(fn, []Token{NewNumber(2, pos)})
	got, ok := repl.(FunctionBlock)
	if !ok {
		t.Fatalf("WithChildren changed the token type: %T", repl)
	}
	if got.Name != "foo" {
		t.Errorf("function name not preserved: got %q", got.Name)
	}
	if got.Pos() != pos {
		t.Errorf("position not preserved: got %v want %v", got.Pos(), pos)
	}
	if len(got.Arguments) != 1 {
		t.Fatalf("expected 1 replaced child, got %d", len(got.Arguments))
	}

	// Non-compound token is returned unchanged.
	leaf := parseOne("red")
	if WithChildren(leaf, []Token{NewNumber(1, leaf.Pos())}) != leaf {
		t.Errorf("WithChildren mutated a leaf token")
	}
}

// TransformTokens must reach leaves at every depth and through every compound
// type, and must leave untouched trees unchanged (changed == false).
func TestTransformTokensDepthAndAllTypes(t *testing.T) {
	// Replace every Number n with n+1, nested through function → square →
	// parentheses → curly.
	inc := func(tok Token) (Token, bool, bool) {
		if n, ok := tok.(Number); ok {
			return NewNumber(n.ValueF+1, n.Pos()), true, true
		}
		return tok, false, false
	}

	tok := parseOne("foo([ (bar({7})) ])")
	out, changed := TransformTokens(tok, inc)
	if !changed {
		t.Fatal("expected the tree to change (a nested Number was present)")
	}

	// Walk back down and confirm the deeply-nested 7 became 8.
	var deepest func(Token) (float64, bool)
	deepest = func(tk Token) (float64, bool) {
		if n, ok := tk.(Number); ok {
			return float64(n.ValueF), true
		}
		kids, ok := Children(tk)
		if !ok {
			return 0, false
		}
		for _, k := range kids {
			if v, ok := deepest(k); ok {
				return v, true
			}
		}
		return 0, false
	}
	if v, ok := deepest(out); !ok || v != 8 {
		t.Errorf("nested number not incremented through all compound types: got %v (found=%v)", v, ok)
	}

	// A tree with no matching leaves must be reported unchanged.
	noNums := parseOne("foo([bar])")
	if _, changed := TransformTokens(noNums, inc); changed {
		t.Errorf("expected changed=false for a tree with no numbers")
	}

	// stop=true must prevent descent: increment the outer number but not the
	// one inside the stopped subtree.
	stopAtFn := func(tok Token) (Token, bool, bool) {
		if _, ok := tok.(FunctionBlock); ok {
			return tok, false, true // stop, don't descend
		}
		if n, ok := tok.(Number); ok {
			return NewNumber(n.ValueF+1, n.Pos()), true, true
		}
		return tok, false, false
	}
	// The 5 is a direct child of the parentheses; the 9 is inside foo(...).
	mixed := parseOne("(5 foo(9))")
	out2, changed2 := TransformTokens(mixed, stopAtFn)
	if !changed2 {
		t.Fatal("expected a change (the top-level 5)")
	}
	kids, _ := Children(out2)
	if got := kids[0].(Number).ValueF; got != 6 {
		t.Errorf("top-level number should be incremented: got %v want 6", got)
	}
	// foo(9) must be untouched because we stopped at it.
	for _, k := range kids {
		if fn, ok := k.(FunctionBlock); ok {
			if fn.Arguments[0].(Number).ValueF != 9 {
				t.Errorf("number inside stopped subtree should be unchanged")
			}
		}
	}
}
