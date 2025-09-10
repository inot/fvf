package ui

import (
	"time"
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
	CopyFlashUntil time.Time
	CurrentFetchedVal string

	// Flags
	PreviewWrap  bool
	MouseEnabled bool
	PrintValues  bool
	JSONPreview  bool
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
