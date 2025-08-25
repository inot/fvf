package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"fvf/search"
)

// ValueFetcher returns a string to display for the value of a given path.
// It should return a pretty-printed JSON or a human readable representation.
// When not available or on error, it can return a message string and/or error.
type ValueFetcher func(path string) (string, error)

// StatusProvider supplies left, middle, right strings for the bottom status bar.
// Example: left = token lifetime, middle = server, right = version
type StatusProvider func() (left string, middle string, right string)

func putLine(s tcell.Screen, x, y int, text string) {
	st := tcell.StyleDefault
	cx := x
	for _, r := range text {
		s.SetContent(cx, y, r, nil, st)
		cx += runewidth.RuneWidth(r)
	}
}

// drawStatusBar renders a status bar on a single line with left, middle, and right aligned segments.
func drawStatusBar(s tcell.Screen, x, y, w int, status StatusProvider) {
	if w <= 0 || status == nil {
		return
	}
	left, middle, right := status()

	st := tcell.StyleDefault.Reverse(true)

	// calculate widths
	lw := runewidth.StringWidth(left)
	mw := runewidth.StringWidth(middle)
	rw := runewidth.StringWidth(right)

	// Right-aligned start pos
	rStart := w - rw
	if rStart < 0 {
		right = runewidth.Truncate(right, w, "…")
		rw = runewidth.StringWidth(right)
		rStart = w - rw
	}

	// Center middle
	mStart := (w - mw) / 2
	if mStart < 0 {
		mStart = 0
	}

	// Ensure middle does not overlap right
	if mStart+mw > rStart {
		avail := rStart - mStart - 1
		if avail < 0 {
			avail = 0
		}
		middle = runewidth.Truncate(middle, avail, "…")
		mw = runewidth.StringWidth(middle)
	}

	// Ensure left does not overlap middle
	if lw > mStart-1 {
		avail := mStart - 1
		if avail < 0 {
			avail = 0
		}
		left = runewidth.Truncate(left, avail, "…")
		lw = runewidth.StringWidth(left)
	}

	// Clear line with style
	for cx := 0; cx < w; cx++ {
		s.SetContent(x+cx, y, ' ', nil, st)
	}

	// Draw left
	cx := x
	for _, r := range left {
		s.SetContent(cx, y, r, nil, st)
		cx += runewidth.RuneWidth(r)
	}
	// Draw middle
	cx = x + mStart
	for _, r := range middle {
		s.SetContent(cx, y, r, nil, st)
		cx += runewidth.RuneWidth(r)
	}
	// Draw right
	cx = x + rStart
	for _, r := range right {
		s.SetContent(cx, y, r, nil, st)
		cx += runewidth.RuneWidth(r)
	}
}

// putLineWithHighlights renders text with baseStyle and highlights all case-insensitive
// occurrences of query using matchStyle. Handles wide runes properly.
func putLineWithHighlights(s tcell.Screen, x, y int, text, query string, baseStyle, matchStyle tcell.Style) {
	rs := []rune(text)
	if query == "" {
		cx := x
		for _, r := range rs {
			s.SetContent(cx, y, r, nil, baseStyle)
			cx += runewidth.RuneWidth(r)
		}
		return
	}
	qr := []rune(query)
	// Lowercase copies for case-insensitive matching
	lrs := make([]rune, len(rs))
	for i, r := range rs {
		lrs[i] = unicode.ToLower(r)
	}
	lqr := make([]rune, len(qr))
	for i, r := range qr {
		lqr[i] = unicode.ToLower(r)
	}

	cx := x
	for i := 0; i < len(rs); {
		matched := false
		if i+len(lqr) <= len(lrs) {
			ok := true
			for j := 0; j < len(lqr); j++ {
				if lrs[i+j] != lqr[j] {
					ok = false
					break
				}
			}
			if ok {
				// draw match
				for j := 0; j < len(qr); j++ {
					r := rs[i+j]
					s.SetContent(cx, y, r, nil, matchStyle)
					cx += runewidth.RuneWidth(r)
				}
				i += len(qr)
				matched = true
			}
		}
		if !matched {
			r := rs[i]
			s.SetContent(cx, y, r, nil, baseStyle)
			cx += runewidth.RuneWidth(r)
			i++
		}
	}
}

// RunStream launches the interactive TUI and progressively receives items from a channel.
// It mirrors the old Run() behavior, including lazy preview fetching when printValues is true.
// quit: when a value arrives, the UI exits gracefully.
// activity: UI sends an event on any user interaction (keys/mouse) to help the caller detect idleness.
func RunStream(itemsCh <-chan search.FoundItem, printValues bool, fetcher ValueFetcher, status StatusProvider, quit <-chan struct{}, activity chan<- struct{}) error {
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	defer s.Fini()

	finished := false
	defer func() {
		if !finished {
			s.Fini()
		}
	}()

	items := make([]search.FoundItem, 0, 1024)
	query := ""
	filtered := make([]search.FoundItem, 0, 1024)
	cursor := 0
	offset := 0
	previewCache := make(map[string]string)
	previewErr := make(map[string]error)

	// quit signal handling: wake event loop when requested to exit
	var shouldQuit atomic.Bool
	if quit != nil {
		go func() {
			<-quit
			shouldQuit.Store(true)
			// interrupt the event wait to allow graceful exit
			s.PostEvent(tcell.NewEventInterrupt(nil))
		}()
	}

	redraw := func() {
		s.Clear()
		w, h := s.Size()

		prompt := "> " + query
		putLine(s, 0, 0, prompt)

		help := fmt.Sprintf("%d/%d  (Up/Down to move, Enter to select, Esc to quit)", len(filtered), len(items))
		putLine(s, 0, 1, help)

		contentTop := 2
		// Reserve 1 line for status bar at the bottom
		maxRows := h - contentTop - 1
		if maxRows < 1 {
			s.Show()
			return
		}

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
		rightX := leftW

		if rightX < w && maxRows > 0 {
			for y := 0; y < h; y++ {
				s.SetContent(rightX, y, '│', nil, tcell.StyleDefault)
			}
		}

		if cursor < offset {
			offset = cursor
		}
		if cursor >= offset+maxRows {
			offset = cursor - maxRows + 1
		}
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
			// Highlight query matches: base gray, matches white; selected line reversed
			q := strings.TrimSpace(query)
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

		if rightX+1 < w && maxRows > 0 {
			var val string
			if len(filtered) > 0 && cursor >= 0 && cursor < len(filtered) {
				p := filtered[cursor].Path
				if cached, ok := previewCache[p]; ok {
					val = cached
				} else if fetcher != nil && printValues {
					if v, err := fetcher(p); err == nil {
						val = v
						previewCache[p] = v
					} else {
						msg := fmt.Sprintf("(error fetching values) %v", err)
						previewCache[p] = msg
						previewErr[p] = err
						val = msg
					}
				}
			}
			drawPreview(s, rightX+1, contentTop, w-(rightX+1), maxRows, filtered, cursor, printValues, val)
		}

		// Draw bottom status bar
		drawStatusBar(s, 0, h-1, w, status)

		s.Show()
	}

	applyFilter := func() {
		q := strings.ToLower(strings.TrimSpace(query))
		if q == "" {
			filtered = append(filtered[:0], items...)
		} else {
			filtered = filtered[:0]
			for _, it := range items {
				if strings.Contains(strings.ToLower(it.Path), q) {
					filtered = append(filtered, it)
				}
			}
			sort.Slice(filtered, func(i, j int) bool { return filtered[i].Path < filtered[j].Path })
		}
		if cursor >= len(filtered) {
			cursor = len(filtered) - 1
		}
		if cursor < 0 {
			cursor = 0
		}
		offset = 0
	}

	// receive items and trigger redraws
	go func() {
		for it := range itemsCh {
			items = append(items, it)
			q := strings.ToLower(strings.TrimSpace(query))
			if q == "" || strings.Contains(strings.ToLower(it.Path), q) {
				filtered = append(filtered, it)
				sort.Slice(filtered, func(i, j int) bool { return filtered[i].Path < filtered[j].Path })
			}
			s.PostEvent(tcell.NewEventInterrupt(nil))
		}
		s.PostEvent(tcell.NewEventInterrupt(nil))
	}()

	applyFilter()
	redraw()

	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventInterrupt:
			redraw()
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
				return nil
			}
			switch ev.Key() {
			case tcell.KeyEnter:
				if len(filtered) == 0 {
					return nil
				}
				it := filtered[cursor]
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
				if out == "" {
					out = "{}"
				}
				finished = true
				s.Fini()
				fmt.Println(out)
				return nil
			case tcell.KeyUp:
				if cursor > 0 {
					cursor--
				}
			case tcell.KeyDown:
				if cursor < len(filtered)-1 {
					cursor++
				}
			case tcell.KeyPgUp:
				cursor -= 10
				if cursor < 0 {
					cursor = 0
				}
			case tcell.KeyPgDn:
				cursor += 10
				if cursor >= len(filtered) {
					cursor = len(filtered) - 1
				}
			case tcell.KeyHome:
				cursor = 0
			case tcell.KeyEnd:
				cursor = len(filtered) - 1
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if len(query) > 0 {
					query = query[:len(query)-1]
					applyFilter()
				}
			case tcell.KeyRune:
				r := ev.Rune()
				if r != 0 {
					query += string(r)
					applyFilter()
				}
			}
			if activity != nil {
				select {
				case activity <- struct{}{}:
				default:
				}
			}
			redraw()
		case *tcell.EventResize:
			s.Sync()
			redraw()
		case *tcell.EventMouse:
			// ignore for now
			if activity != nil {
				select {
				case activity <- struct{}{}:
				default:
				}
			}
		}
		// Check for external quit
		if shouldQuit.Load() {
			return nil
		}
	}
}

func putLineHighlighted(s tcell.Screen, x, y int, text string) {
    st := tcell.StyleDefault.Reverse(true)
    cx := x
    for _, r := range text {
        s.SetContent(cx, y, r, nil, st)
        cx += runewidth.RuneWidth(r)
    }
}

func makeSeparator(w int) string {
	return strings.Repeat("-", w)
}

func isLikelyJSON(s string) bool {
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

func toKVFromLines(s string) map[string]string {
	kv := make(map[string]string)
	for _, ln := range strings.Split(s, "\n") {
		if kvPair := strings.SplitN(ln, ":", 2); len(kvPair) == 2 {
			kv[strings.TrimSpace(kvPair[0])] = strings.TrimSpace(kvPair[1])
		}
	}
	return kv
}

func toKVFromMap(m map[string]interface{}) map[string]string {
	kv := make(map[string]string)
	for k, v := range m {
		kv[k] = fmt.Sprintf("%v", v)
	}
	return kv
}

func renderKVTable(kv map[string]string, w int) []string {
    // Stable lexical order of keys for deterministic table view
    keys := make([]string, 0, len(kv))
    for k := range kv { keys = append(keys, k) }
    sort.Strings(keys)

    maxK := 0
    for _, k := range keys {
        if len(k) > maxK { maxK = len(k) }
    }

    lines := make([]string, 0, len(keys))
    for _, k := range keys {
        v := kv[k]
        line := fmt.Sprintf("%-*s: %s", maxK, k, v)
        if runewidth.StringWidth(line) > w {
            line = runewidth.Truncate(line, w, "…")
        }
        lines = append(lines, line)
    }
    return lines
}

func drawPreview(s tcell.Screen, x, y, w, h int, filtered []search.FoundItem, cursor int, printValues bool, fetched string) {
	if cursor < 0 || cursor >= len(filtered) || w <= 0 || h <= 0 {
		return
	}
	it := filtered[cursor]
	lines := make([]string, 0, h)
	lines = append(lines, it.Path)
	// Separator between path and values
	if h > 1 {
		sep := makeSeparator(w)
		lines = append(lines, sep)
	}
	if printValues {
		if fetched != "" {
			if isLikelyJSON(fetched) {
				// Show JSON as-is when in JSON mode
				for _, ln := range strings.Split(fetched, "\n") {
					lines = append(lines, ln)
				}
			} else {
				// Try to render a key/value table from fetched lines like "k: v"
				kv := toKVFromLines(fetched)
				if len(kv) > 0 {
					for _, ln := range renderKVTable(kv, w) {
						lines = append(lines, ln)
					}
				} else {
					for _, ln := range strings.Split(fetched, "\n") {
						lines = append(lines, ln)
					}
				}
			}
		} else if it.Value != nil {
			if m, ok := it.Value.(map[string]interface{}); ok {
				kv := toKVFromMap(m)
				for _, ln := range renderKVTable(kv, w) {
					lines = append(lines, ln)
				}
			} else if b, err := json.MarshalIndent(it.Value, "", "  "); err == nil {
				for _, ln := range strings.Split(string(b), "\n") {
					lines = append(lines, ln)
				}
			}
		} else {
			lines = append(lines, "")
			lines = append(lines, "(no values to preview)")
			lines = append(lines, "Tip: run with -values or rely on lazy fetch")
		}
	} else {
		lines = append(lines, "")
		lines = append(lines, "(no values to preview)")
		lines = append(lines, "Tip: run with -values to include secret values")
	}
	// Render within the pane bounds, truncating long lines.
	for i := 0; i < h && i < len(lines); i++ {
		ln := lines[i]
		if runewidth.StringWidth(ln) > w {
			ln = runewidth.Truncate(ln, w, "…")
		}
		putLine(s, x, y+i, ln)
	}
}
