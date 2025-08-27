package ui

import (
    "testing"
    "github.com/mattn/go-runewidth"
    "strings"
)

func TestToKVFromLines(t *testing.T) {
    in := "a: 1\n b : two \n c:three:ignored\nnope\n"
    kv := toKVFromLines(in)
    if kv["a"] != "1" {
        t.Fatalf("a expected 1, got %q", kv["a"])
    }
    if kv["b"] != "two" {
        t.Fatalf("b expected two, got %q", kv["b"])
    }
    if _, ok := kv["c"]; !ok {
        // c should be captured as key with value "three:ignored"
        t.Fatal("expected key c present")
    }
    if kv["c"] != "three:ignored" {
        t.Fatalf("c expected 'three:ignored', got %q", kv["c"])
    }
}

func TestToKVFromMap(t *testing.T) {
    m := map[string]interface{}{"x": 1, "y": "z"}
    kv := toKVFromMap(m)
    if kv["x"] != "1" || kv["y"] != "z" {
        t.Fatalf("unexpected kv: %#v", kv)
    }
}

func TestRenderKVTable_NoTruncationHere(t *testing.T) {
    kv := map[string]string{"long_key": "a very very long value"}
    lines := renderKVTable(kv)
    if len(lines) != 1 {
        t.Fatalf("expected 1 line, got %d", len(lines))
    }
    // renderKVTable should not truncate; truncation/wrapping is handled in drawPreview.
    if runewidth.StringWidth(lines[0]) <= 10 {
        t.Fatalf("unexpected truncation at this stage, got %q (w=%d)", lines[0], runewidth.StringWidth(lines[0]))
    }
}

func TestRenderKVTableSortedKeys(t *testing.T) {
    kv := map[string]string{"b": "2", "a": "1", "c": "3"}
    lines := renderKVTable(kv)
    if len(lines) != 3 {
        t.Fatalf("expected 3 lines, got %d", len(lines))
    }
    // Extract keys left of the first ':' and trim padding
    keys := make([]string, 0, len(lines))
    for _, ln := range lines {
        parts := strings.SplitN(ln, ":", 2)
        if len(parts) == 0 {
            t.Fatalf("unexpected table line format: %q", ln)
        }
        keys = append(keys, strings.TrimSpace(parts[0]))
    }
    want := []string{"a", "b", "c"}
    for i := range want {
        if keys[i] != want[i] {
            t.Fatalf("sorted keys mismatch: got %v want %v", keys, want)
        }
    }
}

func TestRenderKVTable_PEMMultiline(t *testing.T) {
    pem := "-----BEGIN CERTIFICATE-----\nABCD1234EFGH5678IJKL9012MNOP3456QRST7890UVWX\n-----END CERTIFICATE-----"
    kv := map[string]string{"cert": pem}
    lines := renderKVTable(kv)
    if len(lines) < 3 {
        t.Fatalf("expected multi-line render for PEM, got %d lines: %#v", len(lines), lines)
    }
    // First should contain key and BEGIN
    if !strings.Contains(lines[0], "cert") || !strings.Contains(lines[0], "-----BEGIN CERTIFICATE-----") {
        t.Fatalf("unexpected first line: %q", lines[0])
    }
    // Last should be END line
    if !strings.Contains(lines[len(lines)-1], "-----END CERTIFICATE-----") {
        t.Fatalf("missing END footer: last=%q", lines[len(lines)-1])
    }
}

func TestWrapTableLines_TableAligned(t *testing.T) {
    // Construct a line with a short key and a very long value
    key := "token"
    val := strings.Repeat("x", 50)
    line := key + ": " + val
    // Width small so it wraps
    w := 12
    out := wrapTableLines([]string{line}, w)
    if len(out) < 2 {
        t.Fatalf("expected wrapped output with multiple lines, got %d: %#v", len(out), out)
    }
    // Determine value column start (len("token: ") == 7)
    pad := strings.Repeat(" ", len(key)+2)
    for i := 1; i < len(out); i++ {
        if !strings.HasPrefix(out[i], pad) {
            t.Fatalf("wrapped continuation not aligned under value column: %q (want prefix %q)", out[i], pad)
        }
    }
}

func TestIsLikelyJSON(t *testing.T) {
    if !isLikelyJSON("{\"a\":1}") || !isLikelyJSON("[]") {
        t.Fatal("expected json strings to be detected")
    }
    if isLikelyJSON("not json") {
        t.Fatal("did not expect non-json to be detected")
    }
}

func TestMakeSeparator(t *testing.T) {
    sep := makeSeparator(5)
    if sep != "-----" {
        t.Fatalf("unexpected separator: %q", sep)
    }
}

func TestSplitPEMish_HeaderBodyFooter(t *testing.T) {
    s := "-----BEGIN CERTIFICATE-----\nAAAABBBBCCCCDDDD\nEEEEFFFF\n-----END CERTIFICATE-----"
    lines := splitPEMish(s)
    if len(lines) < 3 {
        t.Fatalf("expected at least header/body/footer, got %v", lines)
    }
    if lines[0] != "-----BEGIN CERTIFICATE-----" {
        t.Fatalf("bad header: %q", lines[0])
    }
    if lines[len(lines)-1] != "-----END CERTIFICATE-----" {
        t.Fatalf("bad footer: %q", lines[len(lines)-1])
    }
}

func TestSplitPEMish_LongBase64(t *testing.T) {
    // 128 chars of base64-ish
    body := strings.Repeat("A", 128)
    lines := splitPEMish(body)
    if len(lines) != 2 { // 128 / 64 = 2 lines
        t.Fatalf("expected 2 chunks for 128 chars, got %d: %v", len(lines), lines)
    }
    if len(lines[0]) != 64 || len(lines[1]) != 64 {
        t.Fatalf("unexpected chunk sizes: %d, %d", len(lines[0]), len(lines[1]))
    }
}

func TestToKVFromLines_PEMContinuation(t *testing.T) {
    in := "cert: -----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\nnext: v"
    kv := toKVFromLines(in)
    if !strings.Contains(kv["cert"], "-----BEGIN CERTIFICATE-----") || !strings.Contains(kv["cert"], "-----END CERTIFICATE-----") {
        t.Fatalf("pem not captured as single multi-line value: %q", kv["cert"]) 
    }
    if kv["next"] != "v" {
        t.Fatalf("expected next=v, got %q", kv["next"]) 
    }
}

func TestRenderKVTable_GenericMultiline(t *testing.T) {
    kv := map[string]string{"notes": "line1\nline2\nline3"}
    lines := renderKVTable(kv)
    if len(lines) != 3 {
        t.Fatalf("expected 3 lines (key+2 continuations), got %d: %v", len(lines), lines)
    }
    if !strings.Contains(lines[0], "line1") || strings.TrimSpace(lines[1]) != "line2" || strings.TrimSpace(lines[2]) != "line3" {
        t.Fatalf("unexpected multiline formatting: %v", lines)
    }
}
