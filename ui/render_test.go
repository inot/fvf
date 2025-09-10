package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
)

func TestRenderAll_BasicFrame(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	st := &UIState{
		Items:        nil,
		Filtered:     nil,
		Query:        "abc",
		PreviewWrap:  false,
		MouseEnabled: true,
		PreviewCache: make(map[string]string),
		PreviewErr:   make(map[string]error),
	}

	copyX, copyY, copyW, toggleX, toggleY, toggleW := RenderAll(s, false, nil, nil, func() (string, string, string) { return "L", "M", "R" }, st)
	// Header buttons should be disabled when printValues=false
	if copyW != 0 || toggleW != 0 {
		t.Fatalf("expected no header buttons when printValues=false; got copyW=%d, toggleW=%d", copyW, toggleW)
	}
	_ = copyX; _ = copyY; _ = toggleX; _ = toggleY
}
