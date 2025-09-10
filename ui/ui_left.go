package ui

import (
	"fvf/search"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// computeLeftWidth calculates the width of the left list pane with sane minimums.
func computeLeftWidth(w int) int {
	leftW := w / 2
	if leftW < 20 {
		leftW = w - 30
		if leftW < 10 {
			leftW = w
		}
	}
	if leftW > w {
		leftW = w
	}
	return leftW
}

// drawVerticalSeparator draws a thin vertical bar that separates left and right panes.
func drawVerticalSeparator(s tcell.Screen, x, h int) {
	if x <= 0 {
		return
	}
	w, _ := s.Size()
	if x >= w {
		return
	}
	for y := 0; y < h; y++ {
		s.SetContent(x, y, '│', nil, tcell.StyleDefault)
	}
}

// drawLeftList renders the list of results with highlighting and selection.
func drawLeftList(s tcell.Screen, contentTop, leftW, w int, filtered []search.FoundItem, q string, cursor, offset, maxRows int) {
	for i := 0; i < maxRows && i+offset < len(filtered); i++ {
		it := filtered[i+offset]
		line := it.Path
		avail := leftW
		if avail <= 0 {
			avail = w
		}
		if runewidth.StringWidth(line) > avail {
			line = runewidth.Truncate(line, avail, "…")
		}
		if i+offset == cursor {
			base := tcell.StyleDefault.Reverse(true)
			match := base.Bold(true)
			putLineWithHighlights(s, 0, contentTop+i, line, q, base, match)
		} else {
			base := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
			match := tcell.StyleDefault.Foreground(tcell.ColorWhite)
			putLineWithHighlights(s, 0, contentTop+i, line, q, base, match)
		}
	}
}
