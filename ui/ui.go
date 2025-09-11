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

// RunStream is a small wrapper that delegates to the internal implementation.
// Kept minimal to improve readability and testability.
func RunStream(itemsCh <-chan search.FoundItem, printValues bool, jsonPreview bool, fetcher ValueFetcher, policyFetcher PolicyFetcher, status StatusProvider, quit <-chan struct{}, activity chan<- struct{}) error {
    return runStreamImpl(itemsCh, printValues, jsonPreview, fetcher, policyFetcher, status, quit, activity)
}

// It mirrors the old Run() behavior, including lazy preview fetching when printValues is true.
// quit: when a value arrives, the UI exits gracefully.
// activity: UI sends an event on any user interaction (keys/mouse) to help the caller detect idleness.
func runStreamImpl(itemsCh <-chan search.FoundItem, printValues bool, jsonPreview bool, fetcher ValueFetcher, policyFetcher PolicyFetcher, status StatusProvider, quit <-chan struct{}, activity chan<- struct{}) error {
    s, err := tcell.NewScreen()
    if err != nil {
        return err
    }
    if err := s.Init(); err != nil {
        return err
    }
    // Enable mouse by default; user can toggle with Left Arrow
    s.EnableMouse()
    defer s.DisableMouse()
    defer s.Fini()

    finished := false
    defer func() {
        if !finished {
            s.Fini()
        }
    }()

    // Initialize consolidated UI state
    uiState := &UIState{
        Items:         make([]search.FoundItem, 0, 1024),
        Filtered:      make([]search.FoundItem, 0, 1024),
        Query:         "",
        Cursor:        0,
        Offset:        0,
        PreviewCache:  make(map[string]string),
        PreviewErr:    make(map[string]error),
        PerKeyFlash:   make(map[string]time.Time),
        PreviewWrap:   false,
        MouseEnabled:  true,
        PrintValues:   printValues,
        JSONPreview:   jsonPreview,
    }

    // Per-secret copy buttons (drawn in redraw) and flash state keyed by secret key
    type copyBtn struct {
        X, Y, W  int
        Key, Val string
    }
    uiState.PerLineCopyBtns = uiState.PerLineCopyBtns[:0]
    uiState.PerKeyFlash = make(map[string]time.Time)

    // Header full-secret copy button state
    copyBtnX, copyBtnY, copyBtnW := -1, -1, 0
    uiState.CopyFlashUntil = time.Time{}
    uiState.CurrentFetchedVal = ""

    // Header toggle button [json]/[tbl]
    toggleBtnX, toggleBtnY, toggleBtnW := -1, -1, 0

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
        copyBtnX, copyBtnY, copyBtnW, toggleBtnX, toggleBtnY, toggleBtnW = RenderAll(
            s,
            printValues,
            fetcher,
            policyFetcher,
            status,
            uiState,
        )
    }

    applyFilter := func() { uiState.ApplyFilter() }

    // receive items and trigger redraws
    go func() {
        for it := range itemsCh {
            uiState.Items = append(uiState.Items, it)
            q := strings.ToLower(strings.TrimSpace(uiState.Query))
            if q == "" || strings.Contains(strings.ToLower(it.Path), q) {
                uiState.Filtered = append(uiState.Filtered, it)
                sort.Slice(uiState.Filtered, func(i, j int) bool { return uiState.Filtered[i].Path < uiState.Filtered[j].Path })
            }
            s.PostEvent(tcell.NewEventInterrupt(nil))
        }
        s.PostEvent(tcell.NewEventInterrupt(nil))
    }()

    uiState.ApplyFilter()
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
            shouldRedraw, shouldQuit := HandleKey(s, ev, &uiState.Items, &uiState.Filtered, &uiState.Query, &uiState.Cursor, &uiState.Offset, uiState.PreviewCache, fetcher, uiState, applyFilter, activity)
            if shouldQuit {
                return nil
            }
            if shouldRedraw {
                redraw()
            }
        case *tcell.EventResize:
            s.Sync()
            redraw()
        case *tcell.EventMouse:
            shouldRedraw := HandleMouse(s, ev, &uiState.Filtered, &uiState.Cursor, &uiState.Offset, uiState, copyBtnX, copyBtnY, copyBtnW, toggleBtnX, toggleBtnY, toggleBtnW, activity)
            if shouldRedraw {
                redraw()
            }
        }
        // Check for external quit
        if shouldQuit.Load() {
            return nil
        }
    }
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
                // In table mode, render JSON object as a padded key-value table for alignment
                var obj map[string]interface{}
                if err := json.Unmarshal([]byte(fetched), &obj); err == nil {
                    kv := toKVFromMap(obj)
                    secretsLines = append(secretsLines, renderKVTable(kv)...)
                } else {
                    // Fallback to readable JSON lines
                    secretsLines = append(secretsLines, toLinesFromJSONText(fetched)...)
                }
			} else {
				kv := toKVFromLines(fetched)
				if len(kv) > 0 {
					if jsonPreview {
						// Render KV as pretty JSON when jsonPreview is ON
						if b, err := json.MarshalIndent(kv, "", "  "); err == nil {
							secretsLines = append(secretsLines, strings.Split(string(b), "\n")...)
						} else {
							secretsLines = append(secretsLines, renderKVTable(kv)...)
						}
					} else {
						secretsLines = append(secretsLines, renderKVTable(kv)...)
					}
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
