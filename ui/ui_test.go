package ui

import (
	"testing"
	"github.com/mattn/go-runewidth"
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

func TestRenderKVTableWidthTruncation(t *testing.T) {
	kv := map[string]string{"long_key": "a very very long value"}
	lines := renderKVTable(kv, 10)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	// Ensure displayed width does not exceed 10 columns
	if runewidth.StringWidth(lines[0]) > 10 {
		t.Fatalf("line not truncated to width, got %q (w=%d)", lines[0], runewidth.StringWidth(lines[0]))
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
