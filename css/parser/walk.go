package parser

// This file centralises knowledge of which token types carry child tokens
// and how to rebuild them. Every consumer that needs to inspect or rewrite a
// token tree (variable resolution, calc() evaluation, keyword substitution, …)
// should go through Children / WithChildren / TransformTokens rather than
// enumerating the compound token types itself. When a new compound token type
// is added to the tokenizer, updating the two switches below makes every walker
// handle it automatically.

// Children returns the child tokens of a compound token — FunctionBlock,
// ParenthesesBlock, SquareBracketsBlock or CurlyBracketsBlock — and true.
// For leaf tokens it returns (nil, false).
func Children(token Token) ([]Token, bool) {
	switch t := token.(type) {
	case FunctionBlock:
		return t.Arguments, true
	case ParenthesesBlock:
		return t.Arguments, true
	case SquareBracketsBlock:
		return t.Arguments, true
	case CurlyBracketsBlock:
		return t.Arguments, true
	default:
		return nil, false
	}
}

// WithChildren returns a copy of a compound token with its children replaced,
// preserving the token's position and (for functions) its name. Non-compound
// tokens are returned unchanged.
func WithChildren(token Token, children []Token) Token {
	switch t := token.(type) {
	case FunctionBlock:
		t.Arguments = children
		return t
	case ParenthesesBlock:
		t.Arguments = children
		return t
	case SquareBracketsBlock:
		t.Arguments = children
		return t
	case CurlyBracketsBlock:
		t.Arguments = children
		return t
	default:
		return token
	}
}

// TransformTokens walks a token tree pre-order, calling visit on each node.
//
// For every token, visit returns (replacement, changed, stop):
//   - stop == true:  replacement is used as-is and the node's children are NOT
//     visited. changed reports whether replacement differs from the input.
//   - stop == false: the node is descended into — if it is a compound token its
//     children are transformed recursively and it is rebuilt (only when a child
//     actually changed); leaf tokens are returned unchanged.
//
// TransformTokens returns the resulting token and whether anything changed.
// This lets callers skip re-parsing when a tree is untouched.
func TransformTokens(token Token, visit func(Token) (replacement Token, changed, stop bool)) (Token, bool) {
	repl, changed, stop := visit(token)
	if stop {
		return repl, changed
	}
	kids, ok := Children(token)
	if !ok {
		return token, false
	}
	newKids := make([]Token, len(kids))
	anyChanged := false
	for i, k := range kids {
		nk, ch := TransformTokens(k, visit)
		newKids[i] = nk
		anyChanged = anyChanged || ch
	}
	if !anyChanged {
		return token, false
	}
	return WithChildren(token, newKids), true
}
