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

func putLine(s tcell.Screen, x, y int, text string) {
    st := tcell.StyleDefault
    cx := x
    for _, r := range text {
        s.SetContent(cx, y, r, nil, st)
        cx += runewidth.RuneWidth(r)
    }
}

// RunStream launches the interactive TUI and progressively receives items from a channel.
// It mirrors the old Run() behavior, including lazy preview fetching when printValues is true.
func RunStream(itemsCh <-chan search.FoundItem, printValues bool, fetcher ValueFetcher) error {
    s, err := tcell.NewScreen()
    if err != nil {
        return err
    }
    if err := s.Init(); err != nil {
        return err
    }
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

    redraw := func() {
        s.Clear()
        w, h := s.Size()

        prompt := "> " + query
        putLine(s, 0, 0, prompt)

        status := fmt.Sprintf("%d/%d  (Up/Down to move, Enter to select, Esc to quit)", len(filtered), len(items))
        putLine(s, 0, 1, status)

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
                leftW = w
            }
        }
        if leftW > w {
            leftW = w
        }
        rightX := leftW

        if rightX < w {
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
            if i+offset == cursor {
                putLineHighlighted(s, 0, contentTop+i, line)
            } else {
                putLine(s, 0, contentTop+i, line)
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
        e := s.PollEvent()
        switch ev := e.(type) {
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
            redraw()
        case *tcell.EventResize:
            s.Sync()
            redraw()
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
	lines := make([]string, 0, len(kv))
	maxK := 0
	for k := range kv {
		if len(k) > maxK {
			maxK = len(k)
		}
	}
	for k, v := range kv {
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
