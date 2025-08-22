package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"fvf/search"

	vault "github.com/hashicorp/vault/api"
)

// fakeLogical implements LogicalAPI for testing
type fakeLogical struct {
	list map[string]*vault.Secret
	read map[string]*vault.Secret
}

func TestParseFlagsWithArgs_DefaultInteractive(t *testing.T) {
    // No args -> interactive by default
    got := parseFlagsWithArgs([]string{})
    if !got.interactive {
        t.Fatal("expected interactive=true when no args")
    }
}

func TestParseFlagsWithArgs_PathsParsing(t *testing.T) {
    args := []string{"-paths", "kv/app1/,kv/app2/", "-json"}
    got := parseFlagsWithArgs(args)
    if len(got.paths) != 2 || got.paths[0] != "kv/app1/" || got.paths[1] != "kv/app2/" {
        t.Fatalf("unexpected paths parsed: %#v", got.paths)
    }
    if !got.jsonOut {
        t.Fatal("expected jsonOut=true")
    }
}

func TestDetermineInteractive(t *testing.T) {
    // values + tty -> interactive
    if !determineInteractive(options{printValues: true}, 2, true) {
        t.Fatal("expected interactive when -values and tty")
    }
    // values + non-tty -> not forced
    if determineInteractive(options{printValues: true}, 2, false) {
        t.Fatal("did not expect interactive when -values and non-tty by default")
    }
    // explicit interactive flag respected
    if !determineInteractive(options{interactive: true}, 2, false) {
        t.Fatal("expected interactive when flag explicitly set")
    }
}

func TestWalk_MaxDepth(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"secret":   {Data: map[string]interface{}{"keys": []interface{}{"a/", "b"}}},
			"secret/a": {Data: map[string]interface{}{"keys": []interface{}{"c"}}},
			// note: no further for secret/a/c
		},
		read: map[string]*vault.Secret{
			"secret/b":   {Data: map[string]interface{}{"k": "v"}},
			"secret/a/c": {Data: map[string]interface{}{"x": 1}},
		},
	}
	search.SetNamePart("")
	items, err := search.WalkVault(context.Background(), f, "secret", false, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	// With maxDepth=1, only leaf at depth 1 should appear (secret/b), not secret/a/c
	expect := []search.FoundItem{{Path: "secret/b"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func TestReadSecret_ErrorShapes(t *testing.T) {
	f := &fakeLogical{
		read: map[string]*vault.Secret{
			// present but wrong shape for kv2 (no nested data key)
			"kv/data/bad": {Data: map[string]interface{}{"oops": 1}},
		},
	}
	// kv2 wrong shape
	val, err := search.ReadSecret(context.Background(), f, "kv", "bad", true)
	if err != nil {
		t.Fatalf("did not expect error for tolerant v2 shape handling, got: %v", err)
	}
	if m, ok := val.(map[string]interface{}); !ok || len(m) != 0 {
		t.Fatalf("expected empty map for unexpected shape, got %#v", val)
	}
	// nil secret
	_, err = search.ReadSecret(context.Background(), f, "kv", "missing", true)
	if err == nil {
		t.Fatal("expected error for nil secret")
	}
}

func TestHandleLeaf_ListNilTriggersRead(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			// simulate List returning nil by omitting entries
			// so recurse treats mount+inner as leaf
		},
		read: map[string]*vault.Secret{
			"secret/x": {Data: map[string]interface{}{"k": "v"}},
		},
	}
	search.SetNamePart("")
	items, err := search.WalkVault(context.Background(), f, "secret/x", false, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []search.FoundItem{{Path: "secret/x"}}
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func (f *fakeLogical) ListWithContext(_ context.Context, p string) (*vault.Secret, error) {
	if s, ok := f.list[p]; ok {
		return s, nil
	}
	// not found -> simulate no list support / leaf
	return nil, nil
}

func (f *fakeLogical) ReadWithContext(_ context.Context, p string) (*vault.Secret, error) {
	if s, ok := f.read[p]; ok {
		return s, nil
	}
	return nil, nil
}

func TestSplitMount(t *testing.T) {
	m, in := search.SplitMount("secret/app/config")
	if m != "secret" || in != "app/config" {
		t.Fatalf("unexpected: mount=%q inner=%q", m, in)
	}
	m, in = search.SplitMount("secret")
	if m != "secret" || in != "" {
		t.Fatalf("unexpected: mount=%q inner=%q", m, in)
	}
}

func TestAPIPaths(t *testing.T) {
	if got := search.ListAPIPath("kv", "app", true); got != "kv/metadata/app" {
		t.Fatalf("kv2 list path got %q", got)
	}
	if got := search.ReadAPIPath("kv", "app", true); got != "kv/data/app" {
		t.Fatalf("kv2 read path got %q", got)
	}
	if got := search.ListAPIPath("secret", "app", false); got != "secret/app" {
		t.Fatalf("kv1 list path got %q", got)
	}
	if got := search.ReadAPIPath("secret", "app", false); got != "secret/app" {
		t.Fatalf("kv1 read path got %q", got)
	}
}

func TestNameAndRegexMatch(t *testing.T) {
	search.SetNamePart("conf")
	if !search.NameOrRegexMatch("config", "secret/app/config", nil) {
		t.Fatal("expected name match")
	}
	search.SetNamePart("x")
	re := regexp.MustCompile(`^secret/.*/config$`)
	if !search.NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected regex match")
	}
	search.SetNamePart("con")
	re = regexp.MustCompile(`^secret/app/.*$`)
	if !search.NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected both filters to match")
	}
	// With OR semantics, regex alone should still match even if name does not
	search.SetNamePart("bad")
	if !search.NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected match with regex even if name filter fails")
	}
	// Negative case: neither name nor regex matches
	re = regexp.MustCompile(`^other/.*$`)
	if search.NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("did not expect match when neither name nor regex match")
	}
	search.SetNamePart("") // reset
}

func TestWalkVault_KV1(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"secret":   {Data: map[string]interface{}{"keys": []interface{}{"a", "b/"}}},
			"secret/b": {Data: map[string]interface{}{"keys": []interface{}{"c"}}},
		},
		read: map[string]*vault.Secret{
			"secret/a":   {Data: map[string]interface{}{"k": "v"}},
			"secret/b/c": {Data: map[string]interface{}{"x": 1}},
		},
	}
	search.SetNamePart("")
	items, err := search.WalkVault(context.Background(), f, "secret", false, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []search.FoundItem{{Path: "secret/a"}, {Path: "secret/b/c"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func TestWalkVault_KV2(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"kv/metadata":         {Data: map[string]interface{}{"keys": []interface{}{"app/"}}},
			"kv/metadata/app":     {Data: map[string]interface{}{"keys": []interface{}{"cfg", "sub/"}}},
			"kv/metadata/app/sub": {Data: map[string]interface{}{"keys": []interface{}{"leaf"}}},
		},
		read: map[string]*vault.Secret{
			"kv/data/app/cfg":      {Data: map[string]interface{}{"data": map[string]interface{}{"a": "b"}}},
			"kv/data/app/sub/leaf": {Data: map[string]interface{}{"data": map[string]interface{}{"z": 9}}},
		},
	}
	search.SetNamePart("cfg")
	items, err := search.WalkVault(context.Background(), f, "kv", true, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []search.FoundItem{{Path: "kv/app/cfg"}}
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}

	// With values
	search.SetNamePart("")
	items, err = search.WalkVault(context.Background(), f, "kv", true, 0, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	// Expect two entries, with values present
	if len(items) != 2 || items[0].Value == nil || items[1].Value == nil {
		t.Fatalf("expected 2 items with values, got %#v", items)
	}
}

func TestBuildMatcher(t *testing.T) {
	re, err := buildMatcher("")
	if err != nil || re != nil {
		t.Fatalf("expected nil matcher, got %v, err=%v", re, err)
	}
	re, err = buildMatcher("^a.+b$")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if !re.MatchString("axxb") {
		t.Fatal("regex should match")
	}
}

func TestValuesDuringWalk(t *testing.T) {
	if valuesDuringWalk(options{printValues: true, interactive: true}) {
		t.Fatal("interactive should suppress values during walk")
	}
	if !valuesDuringWalk(options{printValues: true, interactive: false}) {
		t.Fatal("non-interactive with -values should fetch during walk")
	}
}

func TestDecideKV2ForMountMeta(t *testing.T) {
	// kv1 flag forces false
	if decideKV2ForMountMeta(options{kv1: true, kv2: true}, map[string]string{"version": "2"}) {
		t.Fatal("kv1 should force false")
	}
	// force-kv2 uses opts.kv2 regardless of mount meta
	if !decideKV2ForMountMeta(options{forceKV2: true, kv2: true}, map[string]string{"version": "1"}) {
		t.Fatal("force-kv2 true should force true")
	}
	if decideKV2ForMountMeta(options{forceKV2: true, kv2: false}, map[string]string{"version": "2"}) {
		t.Fatal("force-kv2 with kv2=false should force false")
	}
	// auto by meta
	if !decideKV2ForMountMeta(options{}, map[string]string{"version": "2"}) {
		t.Fatal("version=2 in meta should return true")
	}
	if decideKV2ForMountMeta(options{}, map[string]string{"version": "1"}) {
		t.Fatal("version=1 in meta should return false")
	}
}

func TestPrintItems_JSONAndLines(t *testing.T) {
	items := []search.FoundItem{{Path: "a", Value: map[string]any{"x": 1}}, {Path: "b"}}

	// JSON path
	var buf bytes.Buffer
	oldStdout := stdOutSwap(&buf)
	if err := printItems(items, options{jsonOut: true}); err != nil {
		t.Fatalf("printItems json err: %v", err)
	}
	stdOutRestore(oldStdout)
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("expected valid JSON, got: %s", buf.String())
	}

	// plain lines
	buf.Reset()
	oldStdout = stdOutSwap(&buf)
	if err := printItems(items, options{printValues: false}); err != nil {
		t.Fatalf("printItems lines err: %v", err)
	}
	stdOutRestore(oldStdout)
	out := buf.String()
	if !strings.Contains(out, "a\n") || !strings.Contains(out, "b\n") {
		t.Fatalf("expected lines with paths, got: %q", out)
	}

	// with values (non-interactive behavior)
	buf.Reset()
	oldStdout = stdOutSwap(&buf)
	if err := printItems(items, options{printValues: true}); err != nil {
		t.Fatalf("printItems values err: %v", err)
	}
	stdOutRestore(oldStdout)
	out = buf.String()
	if !strings.Contains(out, "a = ") || !strings.Contains(out, "b = ") {
		t.Fatalf("expected key=value lines, got: %q", out)
	}
}

// Swap stdout via os.Stdout using a pipe to capture output into a buffer.

// stdOutSwap redirects os.Stdout to the provided buffer.
func stdOutSwap(buf *bytes.Buffer) *osFile {
	old := captureStdoutStart()
	captureStdoutTo(buf)
	return old
}

func stdOutRestore(old *osFile) {
	captureStdoutStop(old)
}

// below is minimal implementation borrowed for testing stdout capture
// without external deps.

// NOTE: We keep these in the _test file to avoid polluting main package API.

// --- platform-agnostic stdout capture ---
// The code below is adapted for tests to capture stdout using os.Pipe().
// It is intentionally lightweight and local to tests.

type osFile struct{ f *os.File }

var savedStdout *os.File
var pipeReader *os.File
var pipeWriter *os.File
var copierDone chan struct{}

func captureStdoutStart() *osFile {
	savedStdout = os.Stdout
	pipeReader, pipeWriter, _ = os.Pipe()
	os.Stdout = pipeWriter
	copierDone = make(chan struct{})
	return &osFile{f: savedStdout}
}

func captureStdoutTo(buf *bytes.Buffer) {
	go func() {
		_, _ = io.Copy(buf, pipeReader)
		close(copierDone)
	}()
}

func captureStdoutStop(old *osFile) {
	_ = pipeWriter.Close()
	<-copierDone
	os.Stdout = old.f
}

// Ensure decideKV2ForPath falls back to opts when DetectKV2 returns !ok
func TestDecideKV2ForPath_Fallback(t *testing.T) {
	// We cannot reliably mock vault.Client.Sys() here.
	// Passing a zero-value *vault.Client will cause DetectKV2 to return (false,false),
	// so decideKV2ForPath should return opts.kv2.
	var c *vault.Client
	got := decideKV2ForPath(context.Background(), c, "any", options{kv2: true})
	if !got {
		t.Fatal("expected fallback to opts.kv2=true when detection not ok")
	}
}
