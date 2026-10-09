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
// be turned into a wa.me link without guessing the country later. A bare
// national number (defaultCountryLen digits) is completed with +defaultCC;
// a country code typed without "+" is accepted when it matches defaultCC;
// anything else must already be international. Empty input is valid and
// returns "" — callers decide whether the number is required. The second
// return is the validation error message ("" when valid), like the other
// normalize/validate helpers.
func normalizePhone(raw, defaultCC string, defaultCountryLen int) (string, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	if len(s) > maxPhoneLen {
		return "", "invalid phone"
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
			return "", "invalid phone"
		}
	}
	s = b.String()

	switch {
	case strings.HasPrefix(s, "+"):
		// already international
	case len(s) == defaultCountryLen: // national number, e.g. 8123-4567 in Nicaragua
		s = "+" + defaultCC + s
	case len(s) == len(defaultCC)+defaultCountryLen && strings.HasPrefix(s, defaultCC):
		// country code without "+", e.g. 50581234567
		s = "+" + s
	default:
		return "", "invalid phone"
	}

	if !phoneRe.MatchString(s) {
		return "", "invalid phone"
	}
	return s, ""
}

// waLink builds a wa.me link with a prefilled message. The stored number is
// already canonical E.164, so only the leading + has to go. Spaces are encoded
// as %20 instead of + so the message survives any query-string parser.
func waLink(phone, text string) string {
	msg := strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
	return "https://wa.me/" + strings.TrimPrefix(phone, "+") + "?text=" + msg
}
