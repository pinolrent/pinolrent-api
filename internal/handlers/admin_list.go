package handlers

import "net/http"

// listPage is the shared skeleton of the admin list endpoints: paginate,
// build the WHERE body, count the matches and stream the requested page into
// the {items,total,limit,offset} envelope they all return. build appends fixed
// clauses with bound params and returns a validation message ("" when valid);
// scan maps one row into an item. head and tail are the fixed SQL around the
// WHERE body — tail carries GROUP BY/ORDER BY, so every value from the query
// string still travels as a bound parameter inside f.
func listPage[T any](a *API, w http.ResponseWriter, r *http.Request, countSQL, head, tail string, build func(*filter) string, scan func(rowScanner) (T, error)) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	var f filter
	if errMsg := build(&f); errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var total int64
	// #nosec G202 -- the WHERE body comes from fixed filter fragments with
	// placeholders; every value from the query string is bound as a parameter.
	if err := a.DB.QueryRowContext(r.Context(),
		countSQL+" WHERE "+f.where(), f.args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- same as the count above.
	rows, err := a.DB.QueryContext(r.Context(),
		head+" WHERE "+f.where()+" "+tail+" LIMIT ? OFFSET ?", f.page(limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	items := make([]T, 0, limit)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			serverError(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
