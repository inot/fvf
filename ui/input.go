package ui

import (
	"encoding/json"
	"fmt"
	"time"

	"fvf/search"

	"github.com/gdamore/tcell/v2"
)

// HandleKey processes a key event, mutating state and returning flags for redraw/quit.
func HandleKey(
	s tcell.Screen,
	ev *tcell.EventKey,
	items *[]search.FoundItem,
	filtered *[]search.FoundItem,
	query *string,
	cursor *int,
	offset *int,
	previewCache map[string]string,
	fetcher ValueFetcher,
	uiState *UIState,
	applyFilter func(),
	activity chan<- struct{},
) (shouldRedraw bool, shouldQuit bool) {
	if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
		return false, true
	}
	shouldRedraw = true
	switch ev.Key() {
	case tcell.KeyEnter:
		if len(*filtered) == 0 {
			return false, true
		}
		it := (*filtered)[*cursor]
		out := ""
		if fetcher != nil {
			if v, ok := previewCache[it.Path]; ok {
				out = v
			} else {
				if v, err := fetcher(it.Path); err == nil {
					previewCache[it.Path] = v
					out = v
				} else {
					out = fmt.Sprintf("(error fetching values) %v", err)
				}
			}
		} else if it.Value != nil {
			b, _ := json.Marshal(it.Value)
			out = string(b)
		}
		// Match printed output to current preview mode
		if uiState.JSONPreview {
			if isLikelyJSON(out) {
				// keep
			} else {
				kv := toKVFromLines(out)
				if len(kv) > 0 {
					if b, err := json.MarshalIndent(kv, "", "  "); err == nil {
						out = string(b)
					}
				}
			}
		} else {
			// Ensure table output in table mode
			if isLikelyJSON(out) {
				lines := toLinesFromJSONText(out)
				out = joinLines(lines)
			}
		}
		if out == "" {
			out = "{}"
		}
		// finalize
		s.Fini()
		fmt.Println(out)
		return false, true
	case tcell.KeyUp:
		if *cursor > 0 {
			*cursor--
			uiState.RevealAll = false
		}
	case tcell.KeyDown:
		if *cursor < len(*filtered)-1 {
			*cursor++
			uiState.RevealAll = false
		}
	case tcell.KeyPgUp:
		*cursor -= 10
		if *cursor < 0 {
			*cursor = 0
		}
		uiState.RevealAll = false
	case tcell.KeyPgDn:
		*cursor += 10
		if *cursor >= len(*filtered) {
			*cursor = len(*filtered) - 1
		}
		uiState.RevealAll = false
	case tcell.KeyHome:
		*cursor = 0
		uiState.RevealAll = false
	case tcell.KeyEnd:
		*cursor = len(*filtered) - 1
		uiState.RevealAll = false
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(*query) > 0 {
			*query = (*query)[:len(*query)-1]
			applyFilter()
			uiState.RevealAll = false
		}
	case tcell.KeyLeft:
		// Toggle mouse enablement with Left Arrow
		uiState.MouseEnabled = !uiState.MouseEnabled
		if uiState.MouseEnabled {
			s.EnableMouse()
		} else {
			s.DisableMouse()
		}
	case tcell.KeyRight:
		// Toggle reveal all secret values with Right Arrow
		uiState.RevealAll = !uiState.RevealAll
	case tcell.KeyTAB:
		uiState.PreviewWrap = !uiState.PreviewWrap
	case tcell.KeyRune:
		r := ev.Rune()
		// Some terminals send Tab as a rune instead of KeyTAB.
		if r == '\t' {
			uiState.PreviewWrap = !uiState.PreviewWrap
			break
		}
		if r != 0 {
			*query += string(r)
			applyFilter()
			uiState.RevealAll = false
		}
	}
	if activity != nil {
		select {
		case activity <- struct{}{}:
		default:
		}
	}
	return shouldRedraw, false
}

// HandleMouse processes a mouse event, mutating state and returning whether to redraw.
func HandleMouse(
	s tcell.Screen,
	ev *tcell.EventMouse,
	filtered *[]search.FoundItem,
	cursor *int,
	offset *int,
	uiState *UIState,
	copyBtnX, copyBtnY, copyBtnW int,
	toggleBtnX, toggleBtnY, toggleBtnW int,
	revealBtnX, revealBtnY, revealBtnW int,
	activity chan<- struct{},
) (shouldRedraw bool) {
	if !uiState.MouseEnabled {
		return false
	}
	mx, my := ev.Position()
	btn := ev.Buttons()

	if activity != nil {
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	// Map click position to left list rows
	w, h := s.Size()
	contentTop := 2
	maxRows := h - contentTop - 1
	if maxRows < 1 {
		return false
	}
	leftW := computeLeftWidth(w)

	// Wheel scroll
	if btn&tcell.WheelUp != 0 {
		if *cursor > 0 {
			*cursor--
		}
		return true
	}
	if btn&tcell.WheelDown != 0 {
		if *cursor < len(*filtered)-1 {
			*cursor++
		}
		return true
	}

	// Per-secret copy buttons click (right pane)
	if btn&tcell.Button1 != 0 {
		for _, b := range uiState.PerLineCopyBtns {
			if my == b.Y && mx >= b.X && mx < b.X+b.W {
				_ = copyToClipboard(b.Val)
				uiState.PerKeyFlash[b.Key] = time.Now().Add(1200 * time.Millisecond)
				// schedule a delayed redraw to clear the flash
				go func() {
					time.Sleep(1300 * time.Millisecond)
					s.PostEvent(tcell.NewEventInterrupt(nil))
				}()
				return true
			}
		}
	}

	// Header buttons click
	if btn&tcell.Button1 != 0 {
		// Toggle view button
		if toggleBtnW > 0 && my == toggleBtnY && mx >= toggleBtnX && mx < toggleBtnX+toggleBtnW {
			uiState.JSONPreview = !uiState.JSONPreview
			return true
		}
		// Reveal/Hide button
		if revealBtnW > 0 && my == revealBtnY && mx >= revealBtnX && mx < revealBtnX+revealBtnW {
			uiState.RevealAll = !uiState.RevealAll
			return true
		}
		if copyBtnW > 0 && my == copyBtnY && mx >= copyBtnX && mx < copyBtnX+copyBtnW {
			if uiState.CurrentFetchedVal != "" {
				_ = copyToClipboard(uiState.CurrentFetchedVal)
				uiState.CopyFlashUntil = time.Now().Add(1200 * time.Millisecond)
				go func() {
					time.Sleep(1300 * time.Millisecond)
					s.PostEvent(tcell.NewEventInterrupt(nil))
				}()
				return true
			}
		}
	}

	// Left click: move cursor only
	if btn&tcell.Button1 != 0 {
		if mx >= 0 && mx < leftW && my >= contentTop && my < contentTop+maxRows {
			row := my - contentTop
			newCursor := *offset + row
			if newCursor >= 0 && newCursor < len(*filtered) {
				*cursor = newCursor
				uiState.RevealAll = false
				return true
			}
		}
	}
	return false
}

// joinLines is a tiny helper to avoid importing strings in this file.
func joinLines(lines []string) string {
	out := ""
	for i, ln := range lines {
		if i > 0 {
			out += "\n"
		}
		out += ln
	}
	return out
}
