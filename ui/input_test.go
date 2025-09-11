package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
	"fvf/search"
)

func newSimScreen(t *testing.T) tcell.Screen {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init screen: %v", err) }
	return s
}

func TestHandleKey_NavigationAndBackspace(t *testing.T) {
	s := newSimScreen(t)
	defer s.Fini()

	items := []search.FoundItem{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	filtered := append([]search.FoundItem(nil), items...)
	query := "abc"
	cursor := 1
	offset := 0
	uiState := &UIState{PreviewWrap: false, MouseEnabled: true}
	apply := func() {}

	// Backspace should shorten query and mark redraw
	shouldRedraw, shouldQuit := HandleKey(s, tcell.NewEventKey(tcell.KeyBackspace2, 0, 0), &items, &filtered, &query, &cursor, &offset, map[string]string{}, nil, uiState, apply, nil)
	if shouldQuit || !shouldRedraw {
		t.Fatalf("expected redraw and no quit on backspace")
	}
	if len(query) != 2 {
		t.Fatalf("expected query len 2 after backspace, got %d", len(query))
	}

	// Up should move cursor up
	shouldRedraw, shouldQuit = HandleKey(s, tcell.NewEventKey(tcell.KeyUp, 0, 0), &items, &filtered, &query, &cursor, &offset, map[string]string{}, nil, uiState, apply, nil)
	if shouldQuit || !shouldRedraw || cursor != 0 {
		t.Fatalf("expected cursor=0 after KeyUp, got %d", cursor)
	}

	// Down should move cursor down
	shouldRedraw, shouldQuit = HandleKey(s, tcell.NewEventKey(tcell.KeyDown, 0, 0), &items, &filtered, &query, &cursor, &offset, map[string]string{}, nil, uiState, apply, nil)
	if shouldQuit || !shouldRedraw || cursor != 1 {
		t.Fatalf("expected cursor=1 after KeyDown, got %d", cursor)
	}
}

func TestHandleKey_Toggles(t *testing.T) {
	s := newSimScreen(t)
	defer s.Fini()

	items := []search.FoundItem{{Path: "a"}}
	filtered := append([]search.FoundItem(nil), items...)
	query := ""
	cursor := 0
	offset := 0
	uiState := &UIState{PreviewWrap: false, MouseEnabled: true}
	apply := func() {}

	// Tab toggle
	_, _ = HandleKey(s, tcell.NewEventKey(tcell.KeyTAB, 0, 0), &items, &filtered, &query, &cursor, &offset, map[string]string{}, nil, uiState, apply, nil)
	if !uiState.PreviewWrap {
		t.Fatalf("expected PreviewWrap=true after TAB")
	}

	// Left Arrow toggle for mouse
	_, _ = HandleKey(s, tcell.NewEventKey(tcell.KeyLeft, 0, 0), &items, &filtered, &query, &cursor, &offset, map[string]string{}, nil, uiState, apply, nil)
	if uiState.MouseEnabled {
		t.Fatalf("expected MouseEnabled=false after Left Arrow")
	}
}

func TestHandleMouse_WheelScroll(t *testing.T) {
	s := newSimScreen(t)
	defer s.Fini()

	filtered := []search.FoundItem{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	cursor := 1
	offset := 0
	uiState := &UIState{MouseEnabled: true}

	ev := tcell.NewEventMouse(0, 0, tcell.WheelUp, 0)
	redraw := HandleMouse(s, ev, &filtered, &cursor, &offset, uiState, -1, -1, 0, -1, -1, 0, nil)
	if !redraw || cursor != 0 { t.Fatalf("wheel up should move cursor to 0; cursor=%d", cursor) }

	ev = tcell.NewEventMouse(0, 0, tcell.WheelDown, 0)
	redraw = HandleMouse(s, ev, &filtered, &cursor, &offset, uiState, -1, -1, 0, -1, -1, 0, nil)
	if !redraw || cursor != 1 { t.Fatalf("wheel down should move cursor to 1; cursor=%d", cursor) }
}
