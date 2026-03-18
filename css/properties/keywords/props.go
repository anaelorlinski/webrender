package keywords

// HasTablePrefix returns true for all keywords starting
// by "table-"
func (kw Keyword) HasTablePrefix() bool {
	return TableCaption <= kw && kw <= TableRowGroup
}
