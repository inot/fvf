package ui

import (
	"testing"
	"fvf/search"
)

func TestUIState_ApplyFilter_EmptyQuery(t *testing.T) {
	st := &UIState{
		Items:   []search.FoundItem{{Path: "a"}, {Path: "b"}},
		Query:   "",
		Cursor:  0,
		Offset:  0,
		Filtered: make([]search.FoundItem, 0, 2),
	}
	st.ApplyFilter()
	if len(st.Filtered) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(st.Filtered))
	}
	if st.Cursor != 0 || st.Offset != 0 {
		t.Fatalf("expected cursor/offset 0/0, got %d/%d", st.Cursor, st.Offset)
	}
}

func TestUIState_ApplyFilter_WithQuery(t *testing.T) {
	st := &UIState{
		Items: []search.FoundItem{{Path: "foo/bar"}, {Path: "baz/qux"}, {Path: "foo/baz"}},
		Query: "foo",
		Filtered: make([]search.FoundItem, 0, 3),
	}
	st.ApplyFilter()
	if len(st.Filtered) != 2 {
		t.Fatalf("expected 2 filtered for query 'foo', got %d", len(st.Filtered))
	}
	// Ensure order is sorted by path
	if st.Filtered[0].Path > st.Filtered[1].Path {
		t.Fatalf("expected sorted order, got %v", st.Filtered)
	}
}
