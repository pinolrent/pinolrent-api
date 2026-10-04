package handlers

import "strings"

// filter accumulates WHERE fragments with their bound arguments, so list
// endpoints share one way to build filtered queries: every value from the
// query string travels as a parameter, never interpolated. Clauses are fixed
// internal fragments; adding a clause appends its args in order.
type filter struct {
	clauses []string
	args    []any
}

// add appends a fixed WHERE fragment with its bound arguments.
func (f *filter) add(clause string, args ...any) {
	f.clauses = append(f.clauses, clause)
	f.args = append(f.args, args...)
}

// where joins the accumulated fragments. With no clauses it matches every
// row, so callers always have a WHERE body.
func (f filter) where() string {
	if len(f.clauses) == 0 {
		return "1=1"
	}
	return strings.Join(f.clauses, " AND ")
}

// params returns the bound arguments in clause order.
func (f filter) params() []any {
	return f.args
}

// page returns the bound arguments with limit/offset appended, copying so
// the filter stays reusable after paving.
func (f filter) page(limit, offset int) []any {
	out := make([]any, 0, len(f.args)+2)
	return append(append(out, f.args...), limit, offset)
}

// unread appends the read filter shared by the notification listings. Like
// paginate, it returns "" when the value is valid and an error message when
// it is not: "" (no filter), "true" (unread only) or "false" (read only).
func (f *filter) unread(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "true":
		f.add("read_at IS NULL")
		return ""
	case "false":
		f.add("read_at IS NOT NULL")
		return ""
	}
	return "invalid unread"
}
