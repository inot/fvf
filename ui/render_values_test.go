package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
	"fvf/search"
	"time"
)

func TestRenderAll_PerLineCopyButtons_TableMode(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	// Prepare state with one filtered item and table-like value in cache
	st := &UIState{
		Items:        []search.FoundItem{{Path: "secret/foo"}},
		Filtered:     []search.FoundItem{{Path: "secret/foo"}},
		Cursor:       0,
		Query:        "",
		PreviewWrap:  false,
		MouseEnabled: true,
		JSONPreview:  false,
		PreviewCache: map[string]string{
			"secret/foo": "user: alice\npassword: s3cr3t",
		},
		PreviewErr:   make(map[string]error),
	}

	RenderAll(s, true, nil, nil, func() (string, string, string) { return "L","M","R" }, st)
	if len(st.PerLineCopyBtns) == 0 {
		t.Fatalf("expected per-line copy buttons in table mode, got none")
	}
	// Expect keys present in buttons
	keys := make(map[string]bool)
	for _, b := range st.PerLineCopyBtns { keys[b.Key] = true }
	if !keys["user"] || !keys["password"] {
		t.Fatalf("expected buttons for keys 'user' and 'password', got keys=%v", keys)
	}
}

func TestRenderAll_PerLineCopyButtons_JSONMode(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	st := &UIState{
		Items:        []search.FoundItem{{Path: "secret/foo"}},
		Filtered:     []search.FoundItem{{Path: "secret/foo"}},
		Cursor:       0,
		Query:        "",
		PreviewWrap:  false,
		MouseEnabled: true,
		JSONPreview:  true,
		PreviewCache: map[string]string{
			"secret/foo": "{\"a\":\"x\",\"b\":\"y\"}",
		},
		PreviewErr:   make(map[string]error),
	}

	RenderAll(s, true, nil, nil, func() (string, string, string) { return "L","M","R" }, st)
	if len(st.PerLineCopyBtns) == 0 {
		t.Fatalf("expected per-line copy buttons in JSON mode, got none")
	}
	keys := make(map[string]bool)
	for _, b := range st.PerLineCopyBtns { keys[b.Key] = true }
	if !keys["a"] || !keys["b"] {
		t.Fatalf("expected buttons for keys 'a' and 'b', got keys=%v", keys)
	}
}

func TestHandleMouse_ClickCopyButtonSetsFlash(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	st := &UIState{
		Items:        []search.FoundItem{{Path: "secret/foo"}},
		Filtered:     []search.FoundItem{{Path: "secret/foo"}},
		Cursor:       0,
		JSONPreview:  false,
		MouseEnabled: true,
		PreviewCache: map[string]string{
			"secret/foo": "user: alice\npassword: s3cr3t",
		},
		PreviewErr:   make(map[string]error),
		PerKeyFlash:  make(map[string]time.Time),
	}

	// Render to populate button hitboxes
	RenderAll(s, true, nil, nil, func() (string, string, string) { return "L","M","R" }, st)
	if len(st.PerLineCopyBtns) == 0 { t.Fatalf("need at least one copy button") }
	btn := st.PerLineCopyBtns[0]

	// Click on the first button
	ev := tcell.NewEventMouse(btn.X, btn.Y, tcell.Button1, 0)
	_ = HandleMouse(s, ev, &st.Filtered, &st.Cursor, &st.Offset, st, -1, -1, 0, -1, -1, 0, nil)

	if _, ok := st.PerKeyFlash[btn.Key]; !ok {
		t.Fatalf("expected flash to be set for key %q after click", btn.Key)
	}
}
