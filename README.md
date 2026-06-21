# Web render

This module implements a static renderer for the HTML, CSS and SVG formats.

It consists for the main part of a Golang port of the awesome [Weasyprint](https://github.com/Kozea/WeasyPrint) python Html to Pdf library.

The project is usable, but you should use it carefully in production; breaking changes may also be committed on the fly.

## Fork: extra features over upstream

This is a fork of `benoitkugler/webrender` adding behaviors and bug fixes
on top of upstream and on top of WeasyPrint where it diverges from the
CSS spec. Filled in as we encounter each one — entries explain what the
fork does, why, and where it lives.

### CSS / parser

#### Dimension units lower-cased at tokenization
- File: `css/parser/tokenizer.go`
- Per WP commit `dbde3d98`, dimension unit identifiers (`PX`, `Px`, `_Red`)
  are case-insensitive. The fork lower-cases them at tokenize time so
  downstream unit tables stay simple.

#### CSS Color Level 4: `rgb()` / `rgba()` interchangeable, alpha as 4th arg
- File: `css/parser/colors.go` (`ParseColor`)
- Both functions accept an optional alpha as the 4th argument — `rgb(r,g,b,a)`
  is valid. Alpha is `Number` only (percentage alpha not supported, matches
  upstream tinycss2).

#### Line-continuations in base64 data URLs
- File: `utils/urls.go` `dataURI.toResource`
- ASCII whitespace inside base64-encoded data URL payloads is stripped before
  decoding. CSS string line-continuations (`\<newline>`) surface as
  whitespace, which broke `@import "data:...;base64,\<NL><sp>..."`.

### CSS / validation

#### CSS Images L4: double-position color stops in gradients
- File: `css/validation/utils.go` `parseColorStops`
- `<color> <position1> <position2>` expands to two stops with the same
  color at each position.

#### `column-gap: <percentage>` accepted at validation
- File: `css/validation/validation.go`
- Per CSS Box Alignment, `column-gap` accepts percentages.

### Layout / flex

#### Step 3.E uses `bottomSpace` for the measurement layout
- File: `html/layout/flex.go`
- Upstream-aligned. Fixes `TestFlexDirectionColumnBreak` and
  `TestFlexDirectionColumnBreakMargin`.

#### Step 3.E uses `parentBox.Width` for column-flex measurement width
- File: `html/layout/flex.go`
- Upstream uses `pr.Inf`, which would let text run as a single line and
  produce an oversized intrinsic height. Fork uses parent's width so
  text wraps correctly when measuring column-flex children.

#### Box-sizing handled before flex layout
- File: `html/layout/flex.go`
- `resolvePercentages` / `adjustBoxSizing` already convert any
  border-box / padding-box CSS height to content-box before flex layout
  runs. The fork removed an extra adjustment that double-counted, fixing
  `TestFlexDirectionColumnBoxSizing`.

#### Step 7 fork "useIntrinsicWidth" for non-stretch column-flex items
- File: `html/layout/flex.go`
- For column-flex items whose `align-self` resolves to neither `stretch`
  nor `normal`, fork lays them out at `min(max-content, available)`
  width (shrink-to-fit). Lets `align-items: center` actually center.

#### `min-height: auto` measurement uses parent's width for column-flex
- File: `html/layout/flex.go`
- When `min-height: auto` resolves the implied content height for a flex
  item, fork lays out at `parentBox.Width` (the cross-axis width the
  item will eventually get) instead of `maxContentWidth(box)`.
  `maxContentWidth(grid)` returns max-of-children, not column-track
  sum, which would force narrow width and spurious wrap, inflating
  min-height.

#### Single `position: relative` branch resolving absolute descendants
- File: `html/layout/flex.go`
- Fork removed a duplicate at end-of-function that caused a
  "placeholder can't have its layout done." panic.

#### Per-line cross-axis gap added during item placement
- File: `html/layout/flex.go`
- For wrap-direction-cross flex, fork adds `crossGap` between flex lines
  during step 14 placement.

#### Backup container size for row-flex when step 15 didn't size
- File: `html/layout/flex.go`
- If `box.Height` is still `auto` for a row-flex container after step 15,
  set it to `sumCross(flexLines) + (n-1)*crossGap`.

#### Min/max-width and min/max-height enforcement on flex container
- File: `html/layout/flex.go`
- Fork explicitly clamps `box.Width` / `box.Height` against min/max-width
  and min/max-height after the placement step.

#### Whitespace-only and contiguous-text handling
- File: `html/boxes/build.go` `flexChildren`
- Whitespace-only text runs are skipped per CSS Flexbox §4.
- Contiguous text runs are wrapped in an anonymous block container flex
  item per the spec.

#### `flex-basis: auto` reads main-size box attribute
- File: `html/layout/flex.go`
- When `flex-basis: auto` resolves to "auto", fork reads the main-size
  box attribute (width or height) since `resolvePercentagesBox` has
  already resolved percentages to pixels.

### Text

#### `SpaceHeight` reads `FontHExtents` directly
- File: `text/engine_gotext.go` `FontConfigurationGotext.SpaceHeight`
- Reads `FontHExtents` directly instead of routing through the HarfBuzz
  shaper, avoiding 1/64-px quantization that disagreed with the wrap
  path and broke `TestColumnSpan3` line-height precision.

#### OT features live on `FontFace`, not `Font`
- Shared `*Font` means `SetFeatures` clobbers; fork snapshots per-face
  at construction so each face has its own feature set.

### Tree / stylesheets

#### `TestUAStylesheet` is a function taking baseURL+fontConfig
- File: `html/tree/tree.go`
- Per-render fontConfig registration so `@font-face`s land in the right
  collection. Fork's helper is `TestUAStylesheet(baseURL, fontConfig)`.

### Tests

#### Verbatim ports of upstream WP / tinycss2 tests
- Layout tests under `html/layout/` and CSS tests under
  `css/validation/` are copied from WeasyPrint and tinycss2 verbatim
  where possible. Test failures often expose real bugs rather than
  noise.

## Scope

The main goal of this module is to process HTML or SVG inputs into laid out documents, ready to be paint, and to be compatible with various output formats (like raster images or PDF files).
To do so, this module uses an abstraction of the output, whose implementation must be provided by an higher level package.

## Outline of the module

From the lower level to the higher level, this module has the following structure :

- the `css` package provides a CSS parser, with property validation and a CSS selector engine (`css/selector`).

- the `svg` package implements a SVG parser and renderer, supporting CSS styling.

- the `html` package implements an HTML renderer

- the `backend` package defines the interfaces which must be implemented by output targets.

The main entry points are the `html/document` package for HTML rendering and the `svg` package if you only need SVG support.

### HTML to PDF: an overview

The `html` package implements a static HTML renderer, which works by :

- parsing the HTML input and fetching CSS files, and cascading the styles. This is implemented in the `html/tree` package

- building a tree of boxes from the HTML structure (package `html/boxes`)

- laying out this tree, that is attributing position and dimensions to the boxes, and performing line, paragraph and page breaks (package `html/layout`)

- drawing the laid out tree to an output. Contrary to the Python library, this step is here performed on an abstract output, which must implement the `backend.Document` interface. This means than the core layout logic could easily be reused for other purposes, such as visualizing html document on a GUI application, or targetting other output file formats.
