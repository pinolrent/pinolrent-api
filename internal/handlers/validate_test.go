package handlers

import (
	"net/http/httptest"
	"testing"
)

// TestPathID pins the single path-id parser: numbers parse, garbage and
// non-positive ids report the per-resource message.
func TestPathID(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		name    string
		wantID  int64
		wantMsg string
	}{
		{"7", "car", 7, ""},
		{"garbage", "reservation", 0, "invalid reservation id"},
		{"0", "car", 0, "invalid car id"},
		{"-3", "user", 0, "invalid user id"},
	} {
		r := httptest.NewRequest("GET", "/x/"+tc.raw, nil)
		r.SetPathValue("id", tc.raw)
		id, msg := pathID(r, tc.name)
		if id != tc.wantID || msg != tc.wantMsg {
			t.Errorf("pathID(%q, %q) = (%d, %q), want (%d, %q)",
				tc.raw, tc.name, id, msg, tc.wantID, tc.wantMsg)
		}
	}
}
