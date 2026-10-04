package handlers

import (
	"net/http"
	"strconv"
)

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// paginate extracts the limit/offset query params with defaults. It returns a
// non-empty error message when the caller provided invalid values.
func paginate(r *http.Request) (limit, offset int, errMsg string) {
	limit = defaultPageLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxPageLimit {
			return 0, 0, "invalid limit"
		}
		limit = n
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, 0, "invalid offset"
		}
		if n > 10000 {
			return 0, 0, "invalid offset"
		}
		offset = n
	}
	return limit, offset, ""
}
