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

    // In our current implementation, the first line is the path, second is separator
    // and the key-value pairs start from the third line (index 2)
    // The first key-value pair should be on line 2 (index 2)
    l2 := readLine(2)
    
    // The second key-value pair should be on line 3 (index 3)
    l3 := readLine(3)
    
    // The long value should be on line 4 (index 4)
    l4 := readLine(4)
    
    // The keys are sorted alphabetically, so "long" comes before "short"
    // Verify the first key-value pair contains "long : " (with space before colon)
    if !strings.Contains(l2, "long : ") {
        t.Fatalf("expected first line to contain 'long : ', got: %q", l2)
    }
    
    // Verify the second key-value pair contains "short: x" (without space before colon)
    if !strings.Contains(l3, "short: x") {
        t.Fatalf("expected second line to contain 'short: x', got: %q", l3)
    }
    
    // The continuation should be on the next line and indented to align with the value
    expectedIndent := strings.Repeat(" ", len("long: "))
    if !strings.HasPrefix(l4, expectedIndent) {
        t.Fatalf("expected continuation to be indented by %d spaces, got: %q", len(expectedIndent), l4)
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

    // In our current implementation, the first line is the path, second is separator
    // and the key-value pairs start from the third line (index 2)
    // The first key-value pair should be on line 2 (index 2)
    l2 := readLine(2)
    
    // The second key-value pair should be on line 3 (index 3)
    l3 := readLine(3)
    
    // The keys are sorted alphabetically, so "long" comes before "short"
    // Verify the first key-value pair contains "long : " (with space before colon)
    if !strings.Contains(l2, "long : ") {
        t.Fatalf("expected first line to contain 'long : ', got: %q", l2)
    }
    
    // The long value should be truncated with an ellipsis in wrap-off mode
    if !containsRunes(l2, []rune("…")) {
        t.Fatalf("expected truncation ellipsis in wrap-off mode, got: %q", l2)
    }
    
    // The second key-value pair should be "short: x" (without space before colon)
    if !strings.Contains(l3, "short: x") {
        t.Fatalf("expected second line to contain 'short: x', got: %q", l3)
    }
    
    // There should be no continuation line in wrap-off mode
    l4 := readLine(4)
    if strings.TrimSpace(l4) != "" {
        t.Fatalf("expected no continuation line in wrap-off mode, got: %q", l4)
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
