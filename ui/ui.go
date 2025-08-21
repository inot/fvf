package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"fvf/search"
)

// ValueFetcher returns a string to display for the value of a given path.
// It should return a pretty-printed JSON or a human readable representation.
// When not available or on error, it can return a message string and/or error.
type ValueFetcher func(path string) (string, error)

// Run launches an interactive TUI similar to fzf.
// - Shows a query line at the top
// - Filters provided items by substring match on Path (case-insensitive)
// - Arrow keys to move, Enter to print the selected entry to stdout
// - Esc/Ctrl-C to quit without printing
func Run(items []search.FoundItem, printValues bool, fetcher ValueFetcher) error {
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	defer s.Fini()

	query := ""
	filtered := make([]search.FoundItem, len(items))
	copy(filtered, items)
	cursor := 0
	offset := 0 // scroll offset
	previewCache := make(map[string]string)
	previewErr := make(map[string]error)

	draw := func() {
		s.Clear()
		w, h := s.Size()

		// Query line
		prompt := "> " + query
		putLine(s, 0, 0, prompt)

		// Hint/status line
		status := fmt.Sprintf("%d/%d  (Up/Down to move, Enter to select, Esc to quit)", len(filtered), len(items))
		putLine(s, 0, 1, status)

		// Layout: left list and right preview separated by a vertical bar
		// Header (rows 0-1) spans full width; content starts at row 2
		contentTop := 2
		maxRows := h - contentTop
		if maxRows < 1 {
			s.Show()
			return
		}

		leftW := w / 2
		if leftW < 20 {
			leftW = w - 30
			if leftW < 10 {
				leftW = w // tiny screens: no preview
			}
		}
		if leftW > w {
			leftW = w
		}
		rightX := leftW

		// Draw separator if we have room for preview
		if rightX < w {
			for y := 0; y < h; y++ {
				s.SetContent(rightX, y, '│', nil, tcell.StyleDefault)
			}
		}

		// Left: results list
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
			if i+offset == cursor {
				putLineHighlighted(s, 0, contentTop+i, line)
			} else {
				putLine(s, 0, contentTop+i, line)
			}
		}

		// Right: preview pane
		if rightX+1 < w && maxRows > 0 {
			// Fetch preview lazily and cache
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

		s.Show()
	}

	filter := func() {
		q := strings.ToLower(strings.TrimSpace(query))
		if q == "" {
			copy(filtered, items)
			filtered = filtered[:len(items)]
		} else {
			filtered = filtered[:0]
			for _, it := range items {
				if strings.Contains(strings.ToLower(it.Path), q) {
					filtered = append(filtered, it)
				}
			}
			// keep results sorted by path
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

	filter()
	draw()

	for {
		e := s.PollEvent()
		switch ev := e.(type) {
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
				if printValues {
					// Prefer cached/fetched preview if available; otherwise fall back to inline value
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
					fmt.Printf("%s = %s\n", it.Path, out)
				} else {
					fmt.Println(it.Path)
				}
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
					filter()
				}
			case tcell.KeyRune:
				r := ev.Rune()
				// basic editing; ignore control chars
				if r != 0 {
					query += string(r)
					filter()
				}
			}
			draw()
		case *tcell.EventResize:
			s.Sync()
			draw()
		}
	}
}

func putLine(s tcell.Screen, x, y int, text string) {
	st := tcell.StyleDefault
	cx := x
	for _, r := range text {
		s.SetContent(cx, y, r, nil, st)
		cx += runewidth.RuneWidth(r)
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

// drawPreview renders details for the current selection into a right-side pane.
func drawPreview(s tcell.Screen, x, y, w, h int, filtered []search.FoundItem, cursor int, printValues bool, fetched string) {
	if cursor < 0 || cursor >= len(filtered) || w <= 0 || h <= 0 {
		return
	}
	it := filtered[cursor]
	lines := make([]string, 0, h)
	lines = append(lines, it.Path)
	if printValues {
		if fetched != "" {
			for _, ln := range strings.Split(fetched, "\n") {
				lines = append(lines, ln)
			}
		} else if it.Value != nil {
			if b, err := json.MarshalIndent(it.Value, "", "  "); err == nil {
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
