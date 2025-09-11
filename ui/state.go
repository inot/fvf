package ui

import (
	"time"
	"sort"
	"strings"
	"fvf/search"
)

// UIState aggregates all mutable UI runtime state. Over time, runStreamImpl
// will be refactored to read/write through this struct instead of local vars.
// This enables us to move rendering and input handling to dedicated modules
// without threading dozens of parameters around.
//
type UIState struct {
	// Data
	Items    []search.FoundItem
	Filtered []search.FoundItem
	Query    string
	Cursor   int
	Offset   int

	// Preview/cache
	PreviewCache map[string]string
	PreviewErr   map[string]error

	// Buttons and flash
	PerLineCopyBtns []PerLineCopyBtn
	PerKeyFlash     map[string]time.Time

	// Header buttons
	HeaderCopyBtn  ButtonBounds
	HeaderToggleBtn ButtonBounds
	HeaderRevealBtn ButtonBounds
	CopyFlashUntil time.Time
	CurrentFetchedVal string

	// Flags
	PreviewWrap  bool
	MouseEnabled bool
	PrintValues  bool
	JSONPreview  bool
	RevealAll    bool
}

// ApplyFilter filters Items into Filtered based on Query and normalizes Cursor/Offset.
func (st *UIState) ApplyFilter() {
    q := st.Query
    if q == "" {
        st.Filtered = append(st.Filtered[:0], st.Items...)
    } else {
        lq := strings.ToLower(strings.TrimSpace(q))
        st.Filtered = st.Filtered[:0]
        for _, it := range st.Items {
            if strings.Contains(strings.ToLower(it.Path), lq) {
                st.Filtered = append(st.Filtered, it)
            }
        }
    }
    // Sort filtered list by path for stable order
    sort.Slice(st.Filtered, func(i, j int) bool { return st.Filtered[i].Path < st.Filtered[j].Path })

    if st.Cursor >= len(st.Filtered) {
        st.Cursor = len(st.Filtered) - 1
    }
    if st.Cursor < 0 {
        st.Cursor = 0
    }
    st.Offset = 0
}
// ButtonBounds represents a clickable rectangular region.
type ButtonBounds struct {
	X int
	Y int
	W int
}

// PerLineCopyBtn contains the hitbox and payload for a per-key copy button.
type PerLineCopyBtn struct {
	X, Y, W  int
	Key, Val string
}
