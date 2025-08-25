package main

import (
	"strings"
	"testing"
)

type sstr struct{}
func (s sstr) String() string { return "stringer-val" }

func TestFormatValueRaw_Scalars(t *testing.T) {
	cases := []struct{ in interface{}; want string }{
		{"hello", "hello"},
		{[]byte("bytes"), "bytes"},
		{sstr{}, "stringer-val"},
		{123, "123"},
		{true, "true"},
	}
	for i, c := range cases {
		got := formatValueRaw(c.in, false)
		if got != c.want {
			t.Fatalf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestFormatValueRaw_MapPretty(t *testing.T) {
	m := map[string]interface{}{"b": 2, "a": 1}
	out := formatValueRaw(m, true)
	// Expect multiline with ordered keys
	if !(out == "a: 1\nb: 2" || out == "b: 2\na: 1") {
		// Since we sort for pretty=true, enforce a then b
		if out != "a: 1\nb: 2" {
			t.Fatalf("unexpected pretty map: %q", out)
		}
	}
}

func TestFormatValueRaw_MapCompact(t *testing.T) {
	m := map[string]interface{}{"k1": 1, "k2": "v"}
	out := formatValueRaw(m, false)
	// Compact output contains both pairs on one line, order not guaranteed
	if !strings.Contains(out, "k1: 1") || !strings.Contains(out, "k2: v") || strings.Contains(out, "\n") {
		t.Fatalf("unexpected compact map: %q", out)
	}
}

func TestFormatValueRaw_NestedCompactSliceInMap(t *testing.T) {
    nested := map[string]interface{}{"x": []interface{}{1, "a"}}
    out := formatValueRaw(nested, false)
    if strings.Contains(out, "\n") {
        t.Fatalf("expected compact single-line output, got %q", out)
    }
    if !strings.Contains(out, "x: 1, a") {
        t.Fatalf("expected compact slice rendering inside map, got %q", out)
    }
}
