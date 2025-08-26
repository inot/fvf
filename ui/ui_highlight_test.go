package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
)

func TestPutLineWithHighlights_MatchesAndStyles(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	base := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	match := tcell.StyleDefault.Foreground(tcell.ColorWhite).Bold(true)

	putLineWithHighlights(s, 0, 0, "Hello hello HeLLo", "hello", base, match)

	// Verify that some cells used the match style (we check attributes by reading contents)
	matchedCount := 0
	for x := 0; x < 20; x++ {
		_, _, style, _ := s.GetContent(x, 0)
		fg, _, _ := style.Decompose()
		if fg == tcell.ColorWhite {
			matchedCount++
		}
	}
	if matchedCount == 0 {
		t.Fatalf("expected some highlighted cells, got 0")
	}
}
