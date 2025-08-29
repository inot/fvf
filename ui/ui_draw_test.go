package ui

import (
	"testing"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"fvf/search"
	"strings"
)

// Test drawPreview renders basic elements without panic and respects width
func TestDrawPreview_RendersPathAndSeparator(t *testing.T) {
    s := tcell.NewSimulationScreen("UTF-8")
    if err := s.Init(); err != nil {
        t.Fatalf("init sim screen: %v", err)
    }
    defer s.Fini()

    filtered := []search.FoundItem{{Path: "secret/foo", Value: map[string]interface{}{"a": 1, "b": 2}}}
    w := 40
    h := 6
    drawPreview(s, 0, 0, w, h, filtered, 0, true, false, "", []string{"default"}, false)

    // Extract first two lines to check path and separator
    checkLine := func(y int) string {
        // Reconstruct by reading the contents via GetContent char by char.
        line := make([]rune, 0, w)
        for x := 0; x < w; x++ {
            ch, _, _, _ := s.GetContent(x, y)
            if ch == 0 {
                ch = ' '
            }
            line = append(line, ch)
        }
        return string(line)
    }

    ln0 := checkLine(0)
    if !containsRunes(ln0, []rune("secret/foo")) {
        t.Fatalf("expected path on first line, got: %q", ln0)
    }
    ln1 := checkLine(1)
    // Expect separator of '-' at least a few chars
    cnt := 0
    for _, r := range ln1 {
        if r == '-' { cnt++ }
    }
    if cnt < 3 {
        t.Fatalf("expected separator dashes on second line, got: %q", ln1)
    }
}

// When wrap is ON in -values (non-JSON) mode, wrapped lines should align under the value column
func TestDrawPreview_TableWrapAlignment_WrapOn(t *testing.T) {
    s := tcell.NewSimulationScreen("UTF-8")
    if err := s.Init(); err != nil { t.Fatalf("init sim screen: %v", err) }
    defer s.Fini()

    val := map[string]interface{}{
        "short": "x",
        "long":  strings.Repeat("L", 50),
    }
    filtered := []search.FoundItem{{Path: "secret/foo", Value: val}}
    w := 15
    h := 8
    drawPreview(s, 0, 0, w, h, filtered, 0, true, false, "", []string{"default"}, true)

    readLine := func(y int) string {
        line := make([]rune, 0, w)
        for x := 0; x < w; x++ {
            ch, _, _, _ := s.GetContent(x, y)
            if ch == 0 { ch = ' ' }
            line = append(line, ch)
        }
        return string(line)
    }

    l2 := readLine(2)
    idx := strings.Index(l2, ": ")
    if idx <= 0 { t.Fatalf("expected key: value format, got: %q", l2) }
    pad := strings.Repeat(" ", idx+2)

    // Next line should start with padding aligning under the value column
    l3 := readLine(3)
    if !strings.HasPrefix(l3, pad) {
        t.Fatalf("continuation not aligned: %q (expected prefix len=%d)", l3, len(pad))
    }
}

// When wrap is OFF, long lines are truncated and should not produce aligned continuation lines
func TestDrawPreview_TableWrapAlignment_WrapOff(t *testing.T) {
    s := tcell.NewSimulationScreen("UTF-8")
    if err := s.Init(); err != nil { t.Fatalf("init sim screen: %v", err) }
    defer s.Fini()

    val := map[string]interface{}{
        "short": "x",
        "long":  strings.Repeat("L", 50),
    }
    filtered := []search.FoundItem{{Path: "secret/foo", Value: val}}
    w := 15
    h := 8
    drawPreview(s, 0, 0, w, h, filtered, 0, true, false, "", []string{"default"}, false)

    readLine := func(y int) string {
        line := make([]rune, 0, w)
        for x := 0; x < w; x++ {
            ch, _, _, _ := s.GetContent(x, y)
            if ch == 0 { ch = ' ' }
            line = append(line, ch)
        }
        return string(line)
    }

    l2 := readLine(2)
    // Expect truncation ellipsis in wrap-off mode for the long value line
    if !containsRunes(l2, []rune("…")) {
        t.Fatalf("expected truncation ellipsis in wrap-off mode, got: %q", l2)
    }

    idx := strings.Index(l2, ": ")
    if idx <= 0 { t.Fatalf("expected key: value format, got: %q", l2) }
    pad := strings.Repeat(" ", idx+2)
    l3 := readLine(3)
    if strings.HasPrefix(l3, pad) {
        t.Fatalf("did not expect continuation in wrap-off mode: %q", l3)
    }
}

func containsRunes(s string, sub []rune) bool {
	// naive rune contains to avoid importing strings; ensure width behavior unchanged
	rs := []rune(s)
	for i := 0; i+len(sub) <= len(rs); i++ {
		ok := true
		for j := range sub {
			if rs[i+j] != sub[j] { ok = false; break }
		}
		if ok { return true }
	}
	return false
}

func TestPutLineRespectsRunewidth(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("init sim screen: %v", err)
	}
	defer s.Fini()

	putLine(s, 0, 0, "a")
	putLine(s, 1, 0, "中") // double-width

	// The next glyph should start at x=1+width('中') = 3
	wA := runewidth.RuneWidth('a')
	wZhong := runewidth.RuneWidth('中')
	if wA != 1 || wZhong < 2 {
		t.Fatalf("unexpected rune widths: a=%d, 中=%d", wA, wZhong)
	}
}
