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
	if msg := f.unread(""); msg != "" || len(f.params()) != 0 {
		t.Fatalf("empty unread must be a no-op, got %q", msg)
	}
	if msg := f.unread("true"); msg != "" || f.where() != "read_at IS NULL" {
		t.Fatalf("where() = %q msg = %q", f.where(), msg)
	}
	var g filter
	if msg := g.unread("FALSE"); msg != "" || g.where() != "read_at IS NOT NULL" {
		t.Fatalf("where() = %q msg = %q", g.where(), msg)
	}
	var bad filter
	if msg := bad.unread("maybe"); msg == "" {
		t.Fatal("unread(maybe) must be invalid")
	}
}
