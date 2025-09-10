package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// RenderAll draws the entire UI frame and returns header button bounds.
func RenderAll(
	s tcell.Screen,
	printValues bool,
	fetcher ValueFetcher,
	policyFetcher PolicyFetcher,
	status StatusProvider,
	uiState *UIState,
) (copyBtnX, copyBtnY, copyBtnW, toggleBtnX, toggleBtnY, toggleBtnW int) {
	copyBtnX, copyBtnY, copyBtnW = -1, -1, 0
	toggleBtnX, toggleBtnY, toggleBtnW = -1, -1, 0

	s.Clear()
	w, h := s.Size()

	prompt := "> " + uiState.Query
	putLine(s, 0, 0, prompt)

	wrapState := "off"
	if uiState.PreviewWrap {
		wrapState = "on"
	}
	mouseState := "off"
	if uiState.MouseEnabled {
		mouseState = "on"
	}
	help := fmt.Sprintf("%d/%d  (Up/Down: move, Enter: select, Tab: wrap[%s], m: mouse[%s], Esc: quit)", len(uiState.Filtered), len(uiState.Items), wrapState, mouseState)
	putLine(s, 0, 1, help)

	contentTop := 2
	// Reserve 1 line for status bar at the bottom
	maxRows := h - contentTop - 1
	if maxRows < 1 {
		drawStatusBar(s, 0, h-1, w, status)
		s.Show()
		return
	}

	leftW := computeLeftWidth(w)
	rightX := leftW

	drawVerticalSeparator(s, rightX, h)

	if uiState.Cursor < uiState.Offset {
		uiState.Offset = uiState.Cursor
	}
	if uiState.Cursor >= uiState.Offset+maxRows {
		uiState.Offset = uiState.Cursor - maxRows + 1
	}
	drawLeftList(s, contentTop, leftW, w, uiState.Filtered, strings.TrimSpace(uiState.Query), uiState.Cursor, uiState.Offset, maxRows)

	if rightX+1 < w && maxRows > 0 {
		var val string
		var policies []string
		if len(uiState.Filtered) > 0 && uiState.Cursor >= 0 && uiState.Cursor < len(uiState.Filtered) {
			p := uiState.Filtered[uiState.Cursor].Path
			if cached, ok := uiState.PreviewCache[p]; ok {
				val = cached
			} else if fetcher != nil && printValues {
				if v, err := fetcher(p); err == nil {
					val = v
					uiState.PreviewCache[p] = v
				} else {
					msg := fmt.Sprintf("(error fetching values) %v", err)
					uiState.PreviewCache[p] = msg
					uiState.PreviewErr[p] = err
					val = msg
				}
			}

			// Fetch policies if policy fetcher is available
			if policyFetcher != nil {
				if p, err := policyFetcher(p); err == nil {
					policies = p
				}
			}
		}
		drawPreview(s, rightX+1, contentTop, w-(rightX+1), maxRows, uiState.Filtered, uiState.Cursor, printValues, uiState.JSONPreview, val, policies, uiState.PreviewWrap)

		// Remember current fetched value for header copy button
		uiState.CurrentFetchedVal = val

		// Draw per-secret copy buttons (right-aligned) when values are shown
		uiState.PerLineCopyBtns = uiState.PerLineCopyBtns[:0]
		if printValues {
			kv := toKVFromLines(val)
			if len(kv) > 0 {
				// If JSON preview is active, ensure header copy uses JSON text
				if uiState.JSONPreview {
					if isLikelyJSON(val) {
						uiState.CurrentFetchedVal = val
					} else {
						if b, err := json.MarshalIndent(kv, "", "  "); err == nil {
							uiState.CurrentFetchedVal = string(b)
						}
					}
				}
				// Recompute layout similar to drawPreview's top section
				headerHeight := 1
				separatorHeight := 1
				availableHeight := maxRows - headerHeight - separatorHeight
				if availableHeight < 0 {
					availableHeight = 0
				}
				secretsHeight := availableHeight / 2
				secretsY := contentTop + headerHeight + separatorHeight

				// Determine the visual line indices for each key depending on preview mode
				// Build the same secrets lines that drawPreview would render for the top section
				headerX := rightX + 1
				paneW := w - headerX
				var visualLines []string
				if uiState.JSONPreview {
					// When JSON preview is active, secrets are rendered as JSON text
					if isLikelyJSON(val) {
						visualLines = strings.Split(val, "\n")
					} else {
						// We render KV as pretty JSON when jsonPreview is ON in drawPreview
						if b, err := json.MarshalIndent(kv, "", "  "); err == nil {
							visualLines = strings.Split(string(b), "\n")
						}
					}
				}
				// Fallback to table lines (non-JSON preview)
				if len(visualLines) == 0 {
					visualLines = renderKVTable(kv)
					// Apply the same wrapping used by drawPreview for table mode
					if uiState.PreviewWrap && len(visualLines) > 1 {
						head := visualLines[:1]
						body := visualLines[1:]
						body = wrapTableLines(body, paneW)
						visualLines = append(head, body...)
					}
				}

				// Precompute button X position aligned to the right side of the pane
				baseLabel := "[copy]"
				copiedLabel := "[OK]"
				baseW := runewidth.StringWidth(baseLabel)
				copiedW := runewidth.StringWidth(copiedLabel)
				btnW := baseW
				if copiedW > btnW {
					btnW = copiedW
				}
				bx := headerX + paneW - btnW
				if bx < headerX {
					bx = headerX
				}

				// For each visible line in the secrets section, detect its key and place one button
				searchLimit := secretsHeight
				if searchLimit > len(visualLines) {
					searchLimit = len(visualLines)
				}
				for i := 0; i < searchLimit; i++ {
					ln := visualLines[i]
					var key string
					if uiState.JSONPreview {
						// Extract key from JSON line pattern: optional spaces + "key":
						// Simple heuristic: find first '"', then next '"', and ensure following ':' exists
						if p1 := strings.Index(ln, "\""); p1 != -1 {
							if p2 := strings.Index(ln[p1+1:], "\""); p2 != -1 {
								candidate := ln[p1+1 : p1+1+p2]
								rest := ln[p1+1+p2+1:]
								if strings.Contains(rest, ":") {
									key = candidate
								}
							}
						}
					} else {
						// Table mode: take left side before ':' and trim spaces (handles padding)
						if idx := strings.Index(ln, ":"); idx != -1 {
							key = strings.TrimSpace(ln[:idx])
						}
					}
					if key == "" {
						continue
					}
					valToCopy, ok := kv[key]
					if !ok {
						continue
					}
					y := secretsY + i

					lbl := baseLabel
					if until, ok := uiState.PerKeyFlash[key]; ok && time.Now().Before(until) {
						lbl = copiedLabel
					}
					if pad := btnW - runewidth.StringWidth(lbl); pad > 0 {
						lbl = lbl + strings.Repeat(" ", pad)
					}
					putLine(s, bx, y, lbl)
					uiState.PerLineCopyBtns = append(uiState.PerLineCopyBtns, PerLineCopyBtn{X: bx, Y: y, W: btnW, Key: key, Val: valToCopy})
				}
			}
		}

		// Draw header buttons (right aligned)
		if printValues {
			headerX := rightX + 1
			headerY := contentTop
			paneW := w - headerX
			copyBtnX, copyBtnY, copyBtnW, toggleBtnX, toggleBtnY, toggleBtnW = drawHeaderButtons(s, headerX, headerY, paneW, uiState.JSONPreview, uiState.CopyFlashUntil)
		} else {
			copyBtnX, copyBtnY, copyBtnW = -1, -1, 0
			toggleBtnX, toggleBtnY, toggleBtnW = -1, -1, 0
		}
	}

	// Draw bottom status bar
	drawStatusBar(s, 0, h-1, w, status)

	s.Show()
	return
}
