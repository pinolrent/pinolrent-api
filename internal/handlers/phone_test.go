package handlers

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantMsg string
	}{
		{"empty is allowed", "", "", ""},
		{"spaces only is allowed", "   ", "", ""},

		{"local nine digits", "912345678", "+56912345678", ""},
		{"local with spaces", "9 1234 5678", "+56912345678", ""},
		{"country code without plus", "56912345678", "+56912345678", ""},
		{"already e164", "+56912345678", "+56912345678", ""},
		{"e164 with separators", "+56 9-1234-5678", "+56912345678", ""},
		{"e164 with parens and dots", "+56 (9) 1234.5678", "+56912345678", ""},
		{"surrounding whitespace", "  +56912345678  ", "+56912345678", ""},
		{"other country", "+14155552671", "+14155552671", ""},

		{"letters", "not-a-number", "", "invalid phone"},
		{"plus in the middle", "56+912345678", "", "invalid phone"},
		{"too short", "12345678", "", "invalid phone"},
		{"too long", "1234567890123456789", "", "invalid phone"},
		{"country code zero", "+056912345678", "", "invalid phone"},
		{"over max length", "+5691234567890123456789012345678901234", "", "invalid phone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := normalizePhone(tc.in)
			if msg != tc.wantMsg {
				t.Fatalf("normalizePhone(%q) msg = %q, want %q", tc.in, msg, tc.wantMsg)
			}
			if got != tc.want {
				t.Fatalf("normalizePhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
