package handlers

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// Field length caps. Per-request body size is bounded separately by
// maxBodyBytes; these caps reject oversized individual fields before they
// reach the database. The email cap lives in config.MaxEmailLen, shared with
// the ADMIN_EMAILS allow-list.
const (
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt silently truncates beyond this
	maxNameLen     = 200
	maxURLLen      = 2048
)

// lenBetween reports whether s length is within [minLen, maxLen] inclusive.
// maxLen <= 0 means "no upper bound".
func lenBetween(s string, minLen, maxLen int) bool {
	if len(s) < minLen {
		return false
	}
	if maxLen > 0 && len(s) > maxLen {
		return false
	}
	return true
}

// pathID parses the {id} path value for the named resource ("car",
// "reservation", ...). Like the other validators it returns "" when valid
// and an error message when not, so the per-resource noun stays in one
// place. Zero and negative ids never exist, so they are invalid input
// (400), not missing rows (404).
func pathID(r *http.Request, name string) (int64, string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, "invalid " + name + " id"
	}
	return id, ""
}

// queryID parses an optional integer query param ("user_id", "owner_id",
// ...). It reports whether the param was present; like the other validators
// it returns "" when valid and an error message when not. Absent means no
// filter, garbage and non-positive ids are invalid input (400), the same rule
// as pathID.
func queryID(r *http.Request, key string) (id int64, present bool, errMsg string) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return 0, false, ""
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 1 {
		return 0, true, "invalid " + key
	}
	return id, true, ""
}

// uploadExtensions are the file extensions served from /uploads/, derived
// from uploadExtByType (uploads_files.go) plus ".jpeg": the sniffer maps
// image/jpeg to ".jpg", while ".jpeg" stays an accepted alias in URLs and
// query filters.
var uploadExtensions = func() map[string]bool {
	m := map[string]bool{".jpeg": true}
	for _, ext := range uploadExtByType {
		m[ext] = true
	}
	return m
}()

// validURL accepts absolute http(s) URLs and local /uploads/ paths. Local
// paths must be a bare basename with an image extension; anything else
// (traversal, subdirectories, other extensions) is rejected.
func validURL(s string) bool {
	if rest, ok := cutUploadPrefix(s); ok {
		if rest == "" || strings.ContainsAny(rest, `/\`) || strings.Contains(rest, "..") {
			return false
		}
		return uploadExtensions[strings.ToLower(filepath.Ext(rest))]
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

func cutUploadPrefix(s string) (string, bool) {
	const prefix = "/uploads/"
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
