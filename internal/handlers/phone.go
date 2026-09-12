package handlers

import (
	"net/url"
	"regexp"
	"strings"
)

// maxPhoneLen caps the raw input before normalization; E.164 allows at most 15
// digits, so anything longer than this is rejected without being parsed.
const maxPhoneLen = 32

// phoneRe is the canonical E.164 shape stored in the database: a leading +, a
// non-zero country code digit, then 7 to 14 more digits.
var phoneRe = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// normalizePhone canonicalizes a phone number to E.164 so the stored value can
// be turned into a wa.me link without guessing the country later. Chilean local
// formats (9 digits, or the 56-prefixed 11-digit form) are completed with +56;
// any other input must already be international. Empty input is valid and
// returns "" — callers decide whether the number is required.
func normalizePhone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", true
	}
	if len(s) > maxPhoneLen {
		return "", false
	}

	// Keep a leading +, drop the separators people type, reject anything else.
	var b strings.Builder
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			// separators, dropped
		default:
			return "", false
		}
	}
	s = b.String()

	switch {
	case strings.HasPrefix(s, "+"):
		// already international
	case len(s) == 9: // Chilean national number, e.g. 9 1234 5678
		s = "+56" + s
	case len(s) == 11 && strings.HasPrefix(s, "56"):
		s = "+" + s
	default:
		return "", false
	}

	if !phoneRe.MatchString(s) {
		return "", false
	}
	return s, true
}

// waLink builds a wa.me link with a prefilled message. The stored number is
// already canonical E.164, so only the leading + has to go. Spaces are encoded
// as %20 instead of + so the message survives any query-string parser.
func waLink(phone, text string) string {
	msg := strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
	return "https://wa.me/" + strings.TrimPrefix(phone, "+") + "?text=" + msg
}
