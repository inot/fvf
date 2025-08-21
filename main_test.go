package main

import (
	"context"
	"reflect"
	"regexp"
	"sort"
	"testing"

	vault "github.com/hashicorp/vault/api"
	"fvf/search"
)

// fakeLogical implements LogicalAPI for testing
type fakeLogical struct {
	list map[string]*vault.Secret
	read map[string]*vault.Secret
}

func TestWalk_MaxDepth(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"secret":       {Data: map[string]interface{}{"keys": []interface{}{"a/", "b"}}},
			"secret/a":     {Data: map[string]interface{}{"keys": []interface{}{"c"}}},
			// note: no further for secret/a/c
		},
		read: map[string]*vault.Secret{
			"secret/b": {Data: map[string]interface{}{"k": "v"}},
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
	_, err := search.ReadSecret(context.Background(), f, "kv", "bad", true)
	if err == nil {
		t.Fatal("expected error for unexpected v2 data shape")
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
			"secret": {Data: map[string]interface{}{"keys": []interface{}{"a", "b/"}}},
			"secret/b": {Data: map[string]interface{}{"keys": []interface{}{"c"}}},
		},
		read: map[string]*vault.Secret{
			"secret/a": {Data: map[string]interface{}{"k": "v"}},
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
			"kv/metadata": {Data: map[string]interface{}{"keys": []interface{}{"app/"}}},
			"kv/metadata/app": {Data: map[string]interface{}{"keys": []interface{}{"cfg", "sub/"}}},
			"kv/metadata/app/sub": {Data: map[string]interface{}{"keys": []interface{}{"leaf"}}},
		},
		read: map[string]*vault.Secret{
			"kv/data/app/cfg": {Data: map[string]interface{}{"data": map[string]interface{}{"a": "b"}}},
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
