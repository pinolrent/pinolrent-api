package handlers

import (
	"strconv"
	"strings"
)

const maxPricePerDay = 100_000_000

// normalizeCarName trims the name and returns the validation error message
// ("" when valid), so create and patch enforce the same rules.
func normalizeCarName(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "name is required"
	}
	if !lenBetween(s, 1, maxNameLen) {
		return "", "name is too long (max " + strconv.Itoa(maxNameLen) + " characters)"
	}
	return s, ""
}

// validateCarPrice returns the error message for a price outside the allowed
// range in centavos, or "" when valid.
func validateCarPrice(p int64) string {
	if p < 0 {
		return "price_per_day must be >= 0"
	}
	if p > maxPricePerDay {
		return "price_per_day must be <= " + strconv.FormatInt(maxPricePerDay, 10)
	}
	return ""
}

// validateCarPhotoURL returns the error message for an invalid photo URL, or
// "" — which also covers the empty value, meaning "no photo".
func validateCarPhotoURL(u string) string {
	if u == "" {
		return ""
	}
	if len(u) > maxURLLen {
		return "photo_url is too long"
	}
	if !validURL(u) {
		return "invalid photo_url"
	}
	return ""
}
