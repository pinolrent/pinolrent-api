package handlers

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Field length caps. Per-request body size is bounded separately by
// maxBodyBytes; these caps reject oversized individual fields before they
// reach the database.
const (
	maxEmailLen    = 254 // RFC 5321
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

// uploadExtensions are the file extensions served from /uploads/, kept in
// sync with uploadExtByType in uploads.go.
var uploadExtensions = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

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
