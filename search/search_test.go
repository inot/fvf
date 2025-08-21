package search

import (
	"context"
	"reflect"
	"regexp"
	"sort"
	"testing"

	vault "github.com/hashicorp/vault/api"
)

// fakeLogical implements LogicalAPI for testing within the search package
type fakeLogical struct {
	list map[string]*vault.Secret
	read map[string]*vault.Secret
}

func (f *fakeLogical) ListWithContext(_ context.Context, p string) (*vault.Secret, error) {
	if s, ok := f.list[p]; ok {
		return s, nil
	}
	return nil, nil
}

func (f *fakeLogical) ReadWithContext(_ context.Context, p string) (*vault.Secret, error) {
	if s, ok := f.read[p]; ok {
		return s, nil
	}
	return nil, nil
}

func TestWalk_MaxDepth_pkg(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"secret":   {Data: map[string]interface{}{"keys": []interface{}{"a/", "b"}}},
			"secret/a": {Data: map[string]interface{}{"keys": []interface{}{"c"}}},
		},
		read: map[string]*vault.Secret{
			"secret/b":   {Data: map[string]interface{}{"k": "v"}},
			"secret/a/c": {Data: map[string]interface{}{"x": 1}},
		},
	}
	SetNamePart("")
	items, err := WalkVault(context.Background(), f, "secret", false, 1, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []FoundItem{{Path: "secret/b"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func TestReadSecret_ErrorShapes_pkg(t *testing.T) {
	f := &fakeLogical{
		read: map[string]*vault.Secret{
			"kv/data/bad": {Data: map[string]interface{}{"oops": 1}},
		},
	}
	_, err := ReadSecret(context.Background(), f, "kv", "bad", true)
	if err == nil {
		t.Fatal("expected error for unexpected v2 data shape")
	}
	_, err = ReadSecret(context.Background(), f, "kv", "missing", true)
	if err == nil {
		t.Fatal("expected error for nil secret")
	}
}

func TestHandleLeaf_ListNilTriggersRead_pkg(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{},
		read: map[string]*vault.Secret{
			"secret/x": {Data: map[string]interface{}{"k": "v"}},
		},
	}
	SetNamePart("")
	items, err := WalkVault(context.Background(), f, "secret/x", false, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []FoundItem{{Path: "secret/x"}}
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func TestSplitMount_pkg(t *testing.T) {
	m, in := SplitMount("secret/app/config")
	if m != "secret" || in != "app/config" {
		t.Fatalf("unexpected: mount=%q inner=%q", m, in)
	}
	m, in = SplitMount("secret")
	if m != "secret" || in != "" {
		t.Fatalf("unexpected: mount=%q inner=%q", m, in)
	}
}

func TestAPIPaths_pkg(t *testing.T) {
	if got := ListAPIPath("kv", "app", true); got != "kv/metadata/app" {
		t.Fatalf("kv2 list path got %q", got)
	}
	if got := ReadAPIPath("kv", "app", true); got != "kv/data/app" {
		t.Fatalf("kv2 read path got %q", got)
	}
	if got := ListAPIPath("secret", "app", false); got != "secret/app" {
		t.Fatalf("kv1 list path got %q", got)
	}
	if got := ReadAPIPath("secret", "app", false); got != "secret/app" {
		t.Fatalf("kv1 read path got %q", got)
	}
}

func TestNameAndRegexMatch_pkg(t *testing.T) {
	SetNamePart("conf")
	if !NameOrRegexMatch("config", "secret/app/config", nil) {
		t.Fatal("expected name match")
	}
	SetNamePart("x")
	re := regexp.MustCompile(`^secret/.*/config$`)
	if !NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected regex match")
	}
	SetNamePart("con")
	re = regexp.MustCompile(`^secret/app/.*$`)
	if !NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected both filters to match")
	}
	SetNamePart("bad")
	if !NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("expected match with regex even if name filter fails")
	}
	re = regexp.MustCompile(`^other/.*$`)
	if NameOrRegexMatch("config", "secret/app/config", re) {
		t.Fatal("did not expect match when neither name nor regex match")
	}
	SetNamePart("")
}

func TestWalkVault_KV1_pkg(t *testing.T) {
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
	SetNamePart("")
	items, err := WalkVault(context.Background(), f, "secret", false, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []FoundItem{{Path: "secret/a"}, {Path: "secret/b/c"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
}

func TestWalkVault_KV2_pkg(t *testing.T) {
	f := &fakeLogical{
		list: map[string]*vault.Secret{
			"kv/metadata":        {Data: map[string]interface{}{"keys": []interface{}{"app/"}}},
			"kv/metadata/app":    {Data: map[string]interface{}{"keys": []interface{}{"cfg", "sub/"}}},
			"kv/metadata/app/sub": {Data: map[string]interface{}{"keys": []interface{}{"leaf"}}},
		},
		read: map[string]*vault.Secret{
			"kv/data/app/cfg":      {Data: map[string]interface{}{"data": map[string]interface{}{"a": "b"}}},
			"kv/data/app/sub/leaf": {Data: map[string]interface{}{"data": map[string]interface{}{"z": 9}}},
		},
	}
	SetNamePart("cfg")
	items, err := WalkVault(context.Background(), f, "kv", true, 0, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	expect := []FoundItem{{Path: "kv/app/cfg"}}
	if !reflect.DeepEqual(items, expect) {
		t.Fatalf("got %#v want %#v", items, expect)
	}
	SetNamePart("")
	items, err = WalkVault(context.Background(), f, "kv", true, 0, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Value == nil || items[1].Value == nil {
		t.Fatalf("expected 2 items with values, got %#v", items)
	}
}
