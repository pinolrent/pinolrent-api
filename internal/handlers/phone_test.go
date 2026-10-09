package handlers

import "testing"

func TestNormalizePhone(t *testing.T) {
	// The defaults under test: Nicaragua (+505, 8 national digits).
	const cc = "505"
	const natLen = 8

	cases := []struct {
		name    string
		in      string
		want    string
		wantMsg string
	}{
		{"empty is allowed", "", "", ""},
		{"spaces only is allowed", "   ", "", ""},

		{"local eight digits", "81234567", "+50581234567", ""},
		{"local with separators", "8123-4567", "+50581234567", ""},
		{"local with spaces", "8123 4567", "+50581234567", ""},
		{"country code without plus", "50581234567", "+50581234567", ""},
		{"already e164", "+50581234567", "+50581234567", ""},
		{"e164 with separators", "+505 8123-4567", "+50581234567", ""},
		{"e164 with parens and dots", "+505 (8123) 4567.89", "+5058123456789", ""},
		{"surrounding whitespace", "  +50581234567  ", "+50581234567", ""},
		{"other country", "+14155552671", "+14155552671", ""},
		{"nine digits is not a local number", "912345678", "", "invalid phone"},
		{"seven digits is not a local number", "1234567", "", "invalid phone"},

		{"letters", "not-a-number", "", "invalid phone"},
		{"plus in the middle", "505+81234567", "", "invalid phone"},
		{"too short", "1234567890", "", "invalid phone"},
		{"too long", "1234567890123456789", "", "invalid phone"},
		{"country code zero", "+050581234567", "", "invalid phone"},
		{"other country code without plus", "56912345678", "", "invalid phone"},
		{"over max length", "+5058123456790123456789012345678901234", "", "invalid phone"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := normalizePhone(tc.in, cc, natLen)
			if msg != tc.wantMsg {
				t.Fatalf("normalizePhone(%q) msg = %q, want %q", tc.in, msg, tc.wantMsg)
			}
			if got != tc.want {
				t.Fatalf("normalizePhone(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizePhoneConfigurable pins the deployment knob: reconfiguring the
// pair changes what a bare number means, which is the whole point of moving
// the country out of the code.
func TestNormalizePhoneConfigurable(t *testing.T) {
	// Chile as configured before: +56, 9 national digits.
	got, msg := normalizePhone("912345678", "56", 9)
	if msg != "" || got != "+56912345678" {
		t.Fatalf("chile local = %q, %q, want +56912345678, \"\"", got, msg)
	}
	// And a Nicaraguan bare number no longer fits it.
	if _, msg := normalizePhone("81234567", "56", 9); msg == "" {
		t.Fatal("nicaraguan local accepted under the chile configuration")
	}
}
