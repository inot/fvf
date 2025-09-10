package ui

import (
	"time"
	"strings"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// drawHeaderButtons draws header buttons aligned to the right of the right pane header line.
// It draws a [json]/[tbl] toggle (left) and a [copy] button (right). Returns the button bounds
// for click handling.
func drawHeaderButtons(
	s tcell.Screen,
	headerX, headerY, paneW int,
	jsonPreview bool,
	copyFlashUntil time.Time,
) (copyX, copyY, copyW, toggleX, toggleY, toggleW int) {
	// Copy button label/width
	copyBase := "[copy]"
	copyOk := "[OK]"
	copyW = runewidth.StringWidth(copyBase)
	if w := runewidth.StringWidth(copyOk); w > copyW {
		copyW = w
	}
	label := copyBase
	if !copyFlashUntil.IsZero() && time.Now().Before(copyFlashUntil) {
		label = copyOk
	}
	if pad := copyW - runewidth.StringWidth(label); pad > 0 {
		label = label + strings.Repeat(" ", pad)
	}

	// Toggle button
	toggleLabel := "[json]"
	if jsonPreview {
		toggleLabel = "[tbl]"
	}
	toggleW = runewidth.StringWidth(toggleLabel)

	// Layout: [toggle] [space] [copy] aligned to right
	bxCopy := headerX + paneW - copyW
	bxToggle := bxCopy - 1 - toggleW
	if bxToggle < headerX {
		bxToggle = headerX
		bxCopy = bxToggle + toggleW + 1
	}
	putLine(s, bxToggle, headerY, toggleLabel)
	putLine(s, bxCopy, headerY, label)

	return bxCopy, headerY, copyW, bxToggle, headerY, toggleW
}
