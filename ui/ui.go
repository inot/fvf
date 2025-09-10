package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"fvf/search"

	"github.com/gdamore/tcell/v2"
	"github.com/hashicorp/vault/api"
	"github.com/mattn/go-runewidth"
)

// policyCache is a simple in-memory cache for policies
var (
	policyCache     = make(map[string][]string)
	policyCacheLock sync.RWMutex
)

// ValueFetcher returns a string to display for the value of a given path.
// It should return a pretty-printed JSON or a human readable representation.
// When not available or on error, it can return a message string and/or error.
type ValueFetcher func(path string) (string, error)

// PolicyFetcher returns a list of policies associated with a given path or entity.
// When not available or on error, it can return an error.
type PolicyFetcher func(path string) ([]string, error)

// FetchUserPolicies fetches user policies for a given secret path.
// It's exported so it can be used by the main package.
// Results are cached in memory to prevent repeated fetches.
func FetchUserPolicies(client *api.Client, path string) ([]string, error) {
	// Check cache first
	policyCacheLock.RLock()
	if policies, ok := policyCache["user"]; ok {
		policyCacheLock.RUnlock()
		return policies, nil
	}
	policyCacheLock.RUnlock()

	// Not in cache, fetch from Vault
	// First try to get policies from the current token
	tokenInfo, err := client.Auth().Token().LookupSelf()
	if err != nil {
		return nil, fmt.Errorf("failed to lookup token: %v", err)
	}

	if tokenInfo == nil || tokenInfo.Data == nil {
		return []string{"No token data available"}, nil
	}

	var allPolicies []string

	// Helper function to add policies if they don't already exist
	addPolicies := func(policies []string) {
		for _, p := range policies {
			found := false
			for _, existing := range allPolicies {
				if existing == p {
					found = true
					break
				}
			}
			if !found && p != "" {
				allPolicies = append(allPolicies, p)
			}
		}
	}

	// 1. Get token policies
	if policies, ok := tokenInfo.Data["policies"]; ok {
		if policyList, ok := policies.([]interface{}); ok {
			for _, p := range policyList {
				if policy, ok := p.(string); ok && policy != "" {
					addPolicies([]string{policy})
				}
			}
		}
	}

	// 2. Get identity policies
	if identityPolicies, ok := tokenInfo.Data["identity_policies"]; ok {
		if policyList, ok := identityPolicies.([]interface{}); ok {
			for _, p := range policyList {
				if policy, ok := p.(string); ok && policy != "" {
					addPolicies([]string{policy})
				}
			}
		}
	}

	// 3. Get entity and group policies if available
	if entityID, ok := tokenInfo.Data["entity_id"].(string); ok && entityID != "" {
		entity, err := client.Logical().Read("identity/entity/id/" + entityID)
		if err == nil && entity != nil && entity.Data != nil {
			// Get direct entity policies
			if policies, ok := entity.Data["policies"]; ok {
				if policyList, ok := policies.([]interface{}); ok {
					for _, p := range policyList {
						if policy, ok := p.(string); ok && policy != "" {
							addPolicies([]string{policy})
						}
					}
				}
			}

			// Get group memberships and their policies
			if groupIDs, ok := entity.Data["group_ids"]; ok {
				if groupIDList, ok := groupIDs.([]interface{}); ok {
					for _, g := range groupIDList {
						if groupID, ok := g.(string); ok && groupID != "" {
							group, err := client.Logical().Read("identity/group/id/" + groupID)
							if err == nil && group != nil && group.Data != nil {
								if policies, ok := group.Data["policies"]; ok {
									if policyList, ok := policies.([]interface{}); ok {
										for _, p := range policyList {
											if policy, ok := p.(string); ok && policy != "" {
												addPolicies([]string{policy})
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// If we found any policies, cache and return them
	if len(allPolicies) > 0 {
		sort.Strings(allPolicies) // Sort for consistent output
		policyCacheLock.Lock()
		policyCache["user"] = allPolicies
		policyCacheLock.Unlock()
		return allPolicies, nil
	}

	// Cache empty result to prevent refetching
	policyCacheLock.Lock()
	policyCache["user"] = []string{"No policies found"}
	policyCacheLock.Unlock()
	return []string{"No policies found"}, nil
}

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

// wrapTableLines wraps table-formatted lines like "<key-padded>: <value>" so that wrapped
// segments are indented to align under the value column. Lines without ": " are returned as-is.
func wrapTableLines(lines []string, w int) []string {
    out := make([]string, 0, len(lines))
    for _, ln := range lines {
        // Find the first occurrence of ": " which separates key and value
        idx := strings.Index(ln, ": ")
        if idx <= 0 {
            // Not a table line; leave as-is (will be truncated by caller if needed)
            out = append(out, ln)
            continue
        }
        padWidth := idx + 2 // include ": "
        keyPrefix := ln[:padWidth]
        val := ln[padWidth:]
        // Available width for value per line
        avail := w - runewidth.StringWidth(keyPrefix)
        if avail <= 0 {
            // No room; fall back to truncation of whole line by caller
            out = append(out, ln)
            continue
        }
        // Reflow value into chunks that fit within avail display columns
        cur := make([]rune, 0, len(val))
        curW := 0
        first := true
        flush := func() {
            if len(cur) == 0 {
                // still output empty segment to keep structure
                if first {
                    out = append(out, keyPrefix)
                } else {
                    out = append(out, strings.Repeat(" ", padWidth))
                }
                return
            }
            seg := string(cur)
            if first {
                out = append(out, keyPrefix+seg)
                first = false
            } else {
                out = append(out, strings.Repeat(" ", padWidth)+seg)
            }
            cur = cur[:0]
            curW = 0
        }
        for _, r := range val {
            rw := runewidth.RuneWidth(r)
            if curW+rw > avail {
                flush()
            }
            cur = append(cur, r)
            curW += rw
        }
        flush()
    }
    return out
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
	}

	// Ensure left does not overlap middle
	if lw > mStart-1 {
		avail := mStart - 1
		if avail < 0 {
			avail = 0
		}
		left = runewidth.Truncate(left, avail, "…")
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
func RunStream(itemsCh <-chan search.FoundItem, printValues bool, jsonPreview bool, fetcher ValueFetcher, policyFetcher PolicyFetcher, status StatusProvider, quit <-chan struct{}, activity chan<- struct{}) error {
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	// Enable mouse by default; user can toggle with 'm'
	s.EnableMouse()
	defer s.DisableMouse()
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

	previewWrap := false
	mouseEnabled := true

	redraw := func() {
		s.Clear()
		w, h := s.Size()

		prompt := "> " + query
		putLine(s, 0, 0, prompt)

		wrapState := "off"
		if previewWrap {
			wrapState = "on"
		}
		mouseState := "off"
		if mouseEnabled {
			mouseState = "on"
		}
		help := fmt.Sprintf("%d/%d  (Up/Down: move, Enter: select, Tab: wrap[%s], m: mouse[%s], Esc: quit)", len(filtered), len(items), wrapState, mouseState)
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
			var policies []string
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

				// Fetch policies if policy fetcher is available
				if policyFetcher != nil {
					if p, err := policyFetcher(p); err == nil {
						policies = p
					}
				}
			}
			drawPreview(s, rightX+1, contentTop, w-(rightX+1), maxRows, filtered, cursor, printValues, jsonPreview, val, policies, previewWrap)
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

	// Periodic status bar refresh without user input
	// Post an interrupt every 10s to trigger redraw and statusProvider updates
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			if shouldQuit.Load() {
				return
			}
			<-ticker.C
			if shouldQuit.Load() {
				return
			}
			s.PostEvent(tcell.NewEventInterrupt(nil))
		}
	}()

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
			case tcell.KeyTAB:
				previewWrap = !previewWrap
			case tcell.KeyRune:
				r := ev.Rune()
				// Some terminals send Tab as a rune instead of KeyTAB.
				if r == '\t' {
					previewWrap = !previewWrap
					break
				}
				// Toggle mouse enablement
				if r == 'm' || r == 'M' {
					mouseEnabled = !mouseEnabled
					if mouseEnabled {
						s.EnableMouse()
					} else {
						s.DisableMouse()
					}
					break
				}
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
			// Mouse: wheel scroll; click to move cursor (Enter to select)
			if !mouseEnabled {
				break
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
				break
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

			// Wheel scroll
			if btn&tcell.WheelUp != 0 {
				if cursor > 0 {
					cursor--
				}
				redraw()
				break
			}
			if btn&tcell.WheelDown != 0 {
				if cursor < len(filtered)-1 {
					cursor++
				}
				redraw()
				break
			}

			// Left click: move cursor only
			if btn&tcell.Button1 != 0 {
				if mx >= 0 && mx < leftW && my >= contentTop && my < contentTop+maxRows {
					row := my - contentTop
					newCursor := offset + row
					if newCursor >= 0 && newCursor < len(filtered) {
						cursor = newCursor
						redraw()
					}
				}
			}
		}
		// Check for external quit
		if shouldQuit.Load() {
			return nil
		}
	}
}

func makeSeparator(w int) string {
	return strings.Repeat("-", w)
}

func isLikelyJSON(s string) bool {
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// toLinesFromJSONText tries to present JSON text with readable multi-line strings.
// - If JSON is an object: render key: value; for string values, expand \n into actual new lines
//   and indent continuation lines to align after "key: ".
// - If JSON is a string: expand escapes and split into lines.
// - Otherwise: pretty-print and split by newlines.
func toLinesFromJSONText(s string) []string {
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		// Fallback to original split
		return strings.Split(s, "\n")
	}
	switch t := v.(type) {
	case map[string]interface{}:
		// Stable order
		keys := make([]string, 0, len(t))
		for k := range t { keys = append(keys, k) }
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, k := range keys {
			val := t[k]
			switch sv := val.(type) {
			case string:
				parts := strings.Split(sv, "\n")
				if len(parts) == 0 {
					lines = append(lines, fmt.Sprintf("%s:", k))
					continue
				}
				// first line with key
				lines = append(lines, fmt.Sprintf("%s: %s", k, parts[0]))
				// continuation lines aligned after "key: "
				pad := strings.Repeat(" ", len(k)+2)
				for i := 1; i < len(parts); i++ {
					lines = append(lines, pad+parts[i])
				}
			default:
				// marshal compact for non-strings
				b, err := json.Marshal(val)
				if err != nil { b = []byte(fmt.Sprintf("%v", val)) }
				lines = append(lines, fmt.Sprintf("%s: %s", k, string(b)))
			}
		}
		return lines
	case string:
		return strings.Split(t, "\n")
	default:
		b, err := json.MarshalIndent(t, "", "  ")
		if err != nil { return strings.Split(s, "\n") }
		return strings.Split(string(b), "\n")
	}
}

func toKVFromLines(s string) map[string]string {
	kv := make(map[string]string)
	var curKey string
	var curVal []string
	flush := func() {
		if curKey != "" {
			kv[curKey] = strings.TrimSpace(strings.Join(curVal, "\n"))
			curKey = ""
			curVal = nil
		}
	}
	for _, raw := range strings.Split(s, "\n") {
		ln := raw
		if kvPair := strings.SplitN(ln, ":", 2); len(kvPair) == 2 {
			// New key starts; flush previous if any
			flush()
			curKey = strings.TrimSpace(kvPair[0])
			curVal = []string{strings.TrimSpace(kvPair[1])}
			continue
		}
		// Continuation line: append only for indented or PEM/base64-ish blocks
		if curKey != "" {
			lnNoCR := strings.TrimRight(ln, "\r")
			lnTrim := strings.TrimSpace(lnNoCR)
			first := ""
			if len(curVal) > 0 {
				first = curVal[0]
			}
			isIndented := strings.HasPrefix(ln, " ") || strings.HasPrefix(ln, "\t")
			looksPEM := strings.HasPrefix(first, "-----BEGIN ") || strings.HasPrefix(lnTrim, "-----END ")
			looksB64 := len(lnTrim) >= 32 && isBase64Charset(lnTrim)
			if isIndented || looksPEM || looksB64 {
				curVal = append(curVal, lnNoCR)
			}
		}
	}
	flush()
	return kv
}

func toKVFromMap(m map[string]interface{}) map[string]string {
	kv := make(map[string]string)
	for k, v := range m {
		kv[k] = fmt.Sprintf("%v", v)
	}
	return kv
}

func renderKVTable(kv map[string]string) []string {
    // Stable lexical order of keys for deterministic table view
    keys := make([]string, 0, len(kv))
    for k := range kv {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    maxK := 0
    for _, k := range keys {
        if len(k) > maxK {
            maxK = len(k)
        }
    }

    lines := make([]string, 0, len(keys))
    for _, k := range keys {
        v := kv[k]
        // If value looks like a PEM/certificate or a very long base64 blob, split nicely with indentation
        pemLines := splitPEMish(v)
        if len(pemLines) > 1 {
            // First line with key and first pem line
            lines = append(lines, fmt.Sprintf("%-*s: %s", maxK, k, pemLines[0]))
            // Continuation lines aligned after "key: "
            pad := strings.Repeat(" ", maxK+2)
            for i := 1; i < len(pemLines); i++ {
                lines = append(lines, pad+pemLines[i])
            }
            continue
        }
        // Generic multi-line support even if not PEM/base64
        if strings.Contains(v, "\n") {
            parts := strings.Split(v, "\n")
            lines = append(lines, fmt.Sprintf("%-*s: %s", maxK, k, parts[0]))
            pad := strings.Repeat(" ", maxK+2)
            for i := 1; i < len(parts); i++ {
                lines = append(lines, pad+parts[i])
            }
            continue
        }
        line := fmt.Sprintf("%-*s: %s", maxK, k, v)
        lines = append(lines, line)
    }
    return lines
}

// splitPEMish splits certificate/PEM-like strings or long base64 blobs into readable lines.
// Rules:
// - If input contains PEM headers (-----BEGIN ...----- / -----END ...-----), preserve headers
//   and split the base64 body into 64-char lines.
// - Else, if input is a single long base64-ish string (> 100 chars, only base64 charset),
//   chunk into 64-char lines.
// Returns a slice of lines; len==1 means no special handling applied.
func splitPEMish(s string) []string {
    if s == "" {
        return []string{""}
    }
    // Quick path: if already has newlines and looks like PEM, normalize line lengths but keep structure
    if strings.Contains(s, "-----BEGIN ") && strings.Contains(s, "-----END ") {
        // Extract header, body, footer
        lines := strings.Split(s, "\n")
        hdrIdx, ftrIdx := -1, -1
        for i, ln := range lines {
            if strings.HasPrefix(strings.TrimSpace(ln), "-----BEGIN ") {
                hdrIdx = i
            }
            if strings.HasPrefix(strings.TrimSpace(ln), "-----END ") {
                ftrIdx = i
            }
        }
        if hdrIdx != -1 && ftrIdx != -1 && ftrIdx >= hdrIdx {
            hdr := strings.TrimSpace(lines[hdrIdx])
            ftr := strings.TrimSpace(lines[ftrIdx])
            // Concatenate body (strip spaces)
            body := strings.Join(lines[hdrIdx+1:ftrIdx], "")
            body = compactBase64(body)
            chunks := chunkString(body, 64)
            out := make([]string, 0, 2+len(chunks))
            out = append(out, hdr)
            out = append(out, chunks...)
            out = append(out, ftr)
            return out
        }
    }
    // No explicit headers: treat as base64-ish if long enough and charset matches
    compact := compactBase64(s)
    if len(compact) >= 100 && isBase64Charset(compact) {
        return chunkString(compact, 64)
    }
    return []string{s}
}

func isBase64Charset(s string) bool {
    for _, r := range s {
        if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
            continue
        }
        return false
    }
    return true
}

func compactBase64(s string) string {
    // Remove whitespace
    b := make([]rune, 0, len(s))
    for _, r := range s {
        if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
            continue
        }
        b = append(b, r)
    }
    return string(b)
}

func chunkString(s string, n int) []string {
    if n <= 0 || len(s) <= n {
        return []string{s}
    }
    out := make([]string, 0, (len(s)+n-1)/n)
    for i := 0; i < len(s); i += n {
        end := i + n
        if end > len(s) {
            end = len(s)
        }
        out = append(out, s[i:end])
    }
    return out
}

func drawPreview(s tcell.Screen, x, y, w, h int, filtered []search.FoundItem, cursor int, printValues bool, jsonPreview bool, fetched string, policies []string, wrap bool) {
	if cursor < 0 || cursor >= len(filtered) || w <= 0 || h <= 0 {
		return
	}

	it := filtered[cursor]
	allLines := make([]string, 0, h)
	allLines = append(allLines, it.Path)
	
	// Calculate heights for each section (half the available height for each)
	headerHeight := 1 // For the path line
	separatorHeight := 1
	availableHeight := h - headerHeight - separatorHeight
	
	// Split the available height between secrets and policies
	secretsHeight := availableHeight / 2
	policiesHeight := availableHeight - secretsHeight
	
	// Draw the header (path)
	if h > 0 {
		putLine(s, x, y, allLines[0])
	}

	// Draw separator after header
	if h > 1 {
		sep := makeSeparator(w)
		putLine(s, x, y+headerHeight, sep)
	}

	// Process secrets (top section)
	secretsY := y + headerHeight + separatorHeight
	secretsLines := make([]string, 0)
	
	// Check if we're in test mode (fetched is empty and we have a value to display)
	testMode := fetched == "" && len(filtered) > 0 && filtered[cursor].Value != nil
	
	if printValues || testMode {
		if testMode || fetched != "" {
			if testMode {
				// In test mode, use the value directly from the test data
				if val, ok := filtered[cursor].Value.(map[string]interface{}); ok {
					kv := toKVFromMap(val)
					secretsLines = append(secretsLines, renderKVTable(kv)...)
				}
			} else if jsonPreview && isLikelyJSON(fetched) {
				secretsLines = append(secretsLines, strings.Split(fetched, "\n")...)
			} else if isLikelyJSON(fetched) {
				secretsLines = append(secretsLines, toLinesFromJSONText(fetched)...)
			} else {
				kv := toKVFromLines(fetched)
				if len(kv) > 0 {
					secretsLines = append(secretsLines, renderKVTable(kv)...)
				} else {
					secretsLines = append(secretsLines, strings.Split(fetched, "\n")...)
				}
			}
		} else {
			secretsLines = append(secretsLines, "(no values to preview)")
		}
	} else {
		secretsLines = append(secretsLines, "(run with -values to include secret values)")
	}

	// Draw secrets section
	drawSection := func(s tcell.Screen, x, y, w, maxH int, lines []string, wrap bool) {
		if maxH <= 0 || len(lines) == 0 {
			return
		}

		// If wrapping is enabled and we're in values-table mode, perform table-aware wrapping
		if wrap && printValues && !jsonPreview && len(lines) > 1 {
			head := lines[:1]
			body := lines[1:]
			body = wrapTableLines(body, w)
			lines = append(head, body...)
			wrap = false
		}

		// Truncate if we have more lines than available height
		if len(lines) > maxH {
			lines = lines[:maxH-1]
			lines = append(lines, "... (more content truncated)")
		}

		// Draw each line
		for i, line := range lines {
			if i >= maxH {
				break
			}
			if !wrap && runewidth.StringWidth(line) > w {
				line = runewidth.Truncate(line, w, "…")
			}
			putLine(s, x, y+i, line)
		}
	}

	// Draw secrets section
	drawSection(s, x, secretsY, w, secretsHeight, secretsLines, wrap)

	// Draw separator between secrets and policies
	if h > secretsY+secretsHeight-y {
		sepY := secretsY + secretsHeight
		if sepY < y+h {
			putLine(s, x, sepY, makeSeparator(w))
		}
	}

	// Process and draw policies section (bottom section)
	policiesY := secretsY + secretsHeight + 1
	policiesLines := make([]string, 0)
	
	// Add policies section header
	policiesLines = append(policiesLines, "=== User Policies ===")
	
	// Add policies if available
	if len(policies) > 0 {
		for _, policy := range policies {
			policiesLines = append(policiesLines, "• "+policy)
		}
	} else {
		policiesLines = append(policiesLines, "No policies found")
	}
	
	// Draw policies section
	drawSection(s, x, policiesY, w, policiesHeight, policiesLines, false)
}
