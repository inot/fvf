package ui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func makeSeparator(w int) string {
	return strings.Repeat("-", w)
}

// copyToClipboard copies text to the macOS clipboard using pbcopy.
func copyToClipboard(text string) error {
    cmd := exec.Command("pbcopy")
    cmd.Stdin = strings.NewReader(text)
    return cmd.Run()
}

func isLikelyJSON(s string) bool {
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// toLinesFromJSONText tries to present JSON text with readable multi-line strings.
//   - If JSON is an object: render key: value; for string values, expand \n into actual new lines
//     and indent continuation lines to align after "key: ".
//   - If JSON is a string: expand escapes and split into lines.
//   - Otherwise: pretty-print and split by newlines.
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
		for k := range t {
			keys = append(keys, k)
		}
		sortStrings(keys)
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
				if err != nil {
					b = []byte(fmt.Sprintf("%v", val))
				}
				lines = append(lines, fmt.Sprintf("%s: %s", k, string(b)))
			}
		}
		return lines
	case string:
		return strings.Split(t, "\n")
	default:
		b, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return strings.Split(s, "\n")
		}
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
	sortStrings(keys)

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
//   - If input contains PEM headers (-----BEGIN ...----- / -----END ...-----), preserve headers
//     and split the base64 body into 64-char lines.
//   - Else, if input is a single long base64-ish string (> 100 chars, only base64 charset),
//     chunk into 64-char lines.
//
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

// sortStrings is a tiny local helper to avoid importing sort here just for one call site.
func sortStrings(a []string) {
	// Simple insertion sort (small inputs typical here)
	for i := 1; i < len(a); i++ {
		j := i
		for j > 0 && a[j-1] > a[j] {
			a[j-1], a[j] = a[j], a[j-1]
			j--
		}
	}
}
