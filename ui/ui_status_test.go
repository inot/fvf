package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

func readLine(s tcell.Screen, y, w int) string {
	line := make([]rune, 0, w)
	for x := 0; x < w; x++ {
		ch, _, _, _ := s.GetContent(x, y)
		if ch == 0 { ch = ' ' }
		line = append(line, ch)
	}
	return string(line)
}

func TestDrawStatusBar_AlignMiddleWhenSpace(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	w := 40
	h := 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ { s.SetContent(x, y, ' ', nil, tcell.StyleDefault) }
	}

	status := func() (string, string, string) { return "L", "MID", "R" }
	drawStatusBar(s, 0, h-1, w, status)
	ln := readLine(s, h-1, w)

	if !containsRunes(ln, []rune("MID")) {
		t.Fatalf("expected middle text present: %q", ln)
	}
	// Right should be near the end (single char 'R' at the last non-space run)
	if ln[w-1] != 'R' {
		t.Fatalf("expected right-aligned 'R' at end, got %q", string(ln[w-1]))
	}
	// Ensure width is exactly filled visually
	if runewidth.StringWidth(ln) != w {
		t.Fatalf("expected visual width %d, got %d", w, runewidth.StringWidth(ln))
	}
}

func TestDrawStatusBar_TruncatesWhenTight(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil { t.Fatalf("init: %v", err) }
	defer s.Fini()

	w := 20
	h := 2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ { s.SetContent(x, y, ' ', nil, tcell.StyleDefault) }
	}
	status := func() (string, string, string) {
		return "LEFT-LONG-SEGMENT", "MID", "RIGHT-LONG-SEGMENT"
	}
	drawStatusBar(s, 0, h-1, w, status)
	ln := readLine(s, h-1, w)
	// Right should reach end; we don't assert middle presence because it can be truncated under tight space
	if last := ln[w-1]; last == ' ' {
		t.Fatalf("expected right-aligned text to reach end, got space at end: %q", ln)
	}
}
