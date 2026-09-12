package handlers

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"empty is allowed", "", "", true},
		{"spaces only is allowed", "   ", "", true},

		{"local nine digits", "912345678", "+56912345678", true},
		{"local with spaces", "9 1234 5678", "+56912345678", true},
		{"country code without plus", "56912345678", "+56912345678", true},
		{"already e164", "+56912345678", "+56912345678", true},
		{"e164 with separators", "+56 9-1234-5678", "+56912345678", true},
		{"e164 with parens and dots", "+56 (9) 1234.5678", "+56912345678", true},
		{"surrounding whitespace", "  +56912345678  ", "+56912345678", true},
		{"other country", "+14155552671", "+14155552671", true},

		{"letters", "no-es-un-numero", "", false},
		{"plus in the middle", "56+912345678", "", false},
		{"too short", "12345678", "", false},
		{"too long", "1234567890123456789", "", false},
		{"country code zero", "+056912345678", "", false},
		{"over max length", "+5691234567890123456789012345678901234", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizePhone(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("normalizePhone(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Fatalf("normalizePhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
