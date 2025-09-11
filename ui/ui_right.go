package ui

import (
	"fmt"
	"fvf/search"
	"github.com/gdamore/tcell/v2"
)

// fetchPreviewAndPolicies retrieves the preview value (with cache) and policies for the current selection.
func fetchPreviewAndPolicies(
	filtered []search.FoundItem,
	cursor int,
	printValues bool,
	fetcher ValueFetcher,
	policyFetcher PolicyFetcher,
	previewCache map[string]string,
	previewErr map[string]error,
) (string, []string) {
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

		if policyFetcher != nil {
			if p, err := policyFetcher(p); err == nil {
				policies = p
			}
		}
	}
	return val, policies
}

// drawRightPane invokes drawPreview to render the right pane content
func drawRightPane(
	s tcell.Screen,
	x, y, w, maxRows int,
	filtered []search.FoundItem,
	cursor int,
	printValues bool,
	jsonPreview bool,
	val string,
	policies []string,
	wrap bool,
) {
	drawPreview(s, x, y, w, maxRows, filtered, cursor, printValues, jsonPreview, val, policies, wrap, false)
}
