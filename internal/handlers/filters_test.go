package handlers

import (
	"reflect"
	"testing"
)

func TestFilterWhere(t *testing.T) {
	var f filter
	if got := f.where(); got != "1=1" {
		t.Fatalf("empty where() = %q, want 1=1", got)
	}
	f.add("user_id = ?", int64(7))
	f.add("read_at IS NULL")
	if got := f.where(); got != "user_id = ? AND read_at IS NULL" {
		t.Fatalf("where() = %q", got)
	}
	if got := f.params(); !reflect.DeepEqual(got, []any{int64(7)}) {
		t.Fatalf("params() = %v", got)
	}
	paged := f.page(50, 10)
	if !reflect.DeepEqual(paged, []any{int64(7), 50, 10}) {
		t.Fatalf("page() = %v", paged)
	}
	// Paging must not mutate the filter: reusing it appends once.
	if got := f.params(); !reflect.DeepEqual(got, []any{int64(7)}) {
		t.Fatalf("params() after page() = %v", got)
	}
}

func TestFilterUnread(t *testing.T) {
	var f filter
	if !f.unread("") || len(f.params()) != 0 {
		t.Fatal("empty unread must be a no-op")
	}
	if !f.unread("true") || f.where() != "read_at IS NULL" {
		t.Fatalf("where() = %q", f.where())
	}
	var g filter
	if !g.unread("FALSE") || g.where() != "read_at IS NOT NULL" {
		t.Fatalf("where() = %q", g.where())
	}
	var bad filter
	if bad.unread("maybe") {
		t.Fatal("unread(maybe) must be invalid")
	}
}
