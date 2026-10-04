package handlers

import (
	"context"
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
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/x/"+tc.raw, nil)
		r.SetPathValue("id", tc.raw)
		id, msg := pathID(r, tc.name)
		if id != tc.wantID || msg != tc.wantMsg {
			t.Errorf("pathID(%q, %q) = (%d, %q), want (%d, %q)",
				tc.raw, tc.name, id, msg, tc.wantID, tc.wantMsg)
		}
	}
}

// TestQueryID pins the single query-int parser: absent means no filter,
// numbers parse, garbage and non-positive ids are invalid like pathID.
func TestQueryID(t *testing.T) {
	for _, tc := range []struct {
		raw         string
		wantID      int64
		wantPresent bool
		wantMsg     string
	}{
		{"", 0, false, ""},
		{"7", 7, true, ""},
		{"garbage", 0, true, "invalid user_id"},
		{"0", 0, true, "invalid user_id"},
		{"-3", 0, true, "invalid user_id"},
	} {
		r := httptest.NewRequestWithContext(context.Background(), "GET", "/x?user_id="+tc.raw, nil)
		id, present, msg := queryID(r, "user_id")
		if id != tc.wantID || present != tc.wantPresent || msg != tc.wantMsg {
			t.Errorf("queryID(%q) = (%d, %v, %q), want (%d, %v, %q)",
				tc.raw, id, present, msg, tc.wantID, tc.wantPresent, tc.wantMsg)
		}
	}
}
