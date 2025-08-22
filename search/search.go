package search

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	vault "github.com/hashicorp/vault/api"
)

// FoundItem is a discovered secret path with optional value
type FoundItem struct {
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// ListMountsWithFallback attempts to list mounts using the standard API, and if
// permission is denied (403), it falls back to the internal UI endpoint
// sys/internal/ui/mounts, which is often permitted to less privileged users.
func ListMountsWithFallback(ctx context.Context, c *vault.Client) (map[string]*vault.MountOutput, error) {
	mounts, err := c.Sys().ListMountsWithContext(ctx)
	if err == nil {
		return mounts, nil
	}
	var respErr *vault.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != 403 {
		return nil, err
	}

	// Fallback: internal UI endpoint
	sec, ierr := c.Logical().ReadWithContext(ctx, "sys/internal/ui/mounts")
	if ierr != nil {
		return nil, err // return original error for context
	}
	if sec == nil || sec.Data == nil {
		return nil, err
	}
    // Some Vault versions nest the actual mounts map under the "data" key,
    // and then again under a "mounts" key. Others group mounts into sections
    // like "secret" or "system". Merge mounts from all shapes.
    root := sec.Data
    if inner, ok := root["data"].(map[string]interface{}); ok {
        root = inner
    }

    out := make(map[string]*vault.MountOutput)

    // Helper to merge a mounts-like map into out.
    mergeMounts := func(m map[string]interface{}) {
        for k, v := range m {
            mm, ok := v.(map[string]interface{})
            if !ok {
                continue
            }
            // Heuristic: treat entries that have either a type or options as mount entries
            if _, hasType := mm["type"]; !hasType {
                if _, hasOpts := mm["options"]; !hasOpts {
                    continue
                }
            }
            mo := &vault.MountOutput{Options: map[string]string{}}
            if t, ok := mm["type"].(string); ok {
                mo.Type = t
            }
            if opts, ok := mm["options"].(map[string]interface{}); ok {
                for okk, ov := range opts {
                    if s, ok := ov.(string); ok {
                        mo.Options[okk] = s
                    }
                }
            }
            out[k] = mo
        }
    }

    // 1) If there's an explicit mounts map, merge it first
    if mountsMap, ok := root["mounts"].(map[string]interface{}); ok {
        mergeMounts(mountsMap)
    }
    // 2) If root itself looks like a mounts map, merge it
    mergeMounts(root)
    // 3) Some servers structure mounts under section keys (e.g., "secret"). Merge all section maps.
    for _, v := range root {
        if section, ok := v.(map[string]interface{}); ok {
            mergeMounts(section)
        }
    }

    return out, nil
}

// LogicalAPI is the minimal surface used from Vault's logical client.
type LogicalAPI interface {
	ListWithContext(ctx context.Context, path string) (*vault.Secret, error)
	ReadWithContext(ctx context.Context, path string) (*vault.Secret, error)
}

// CurrentNamePart is the case-insensitive substring used to match the last path segment.
// Set via CLI before walking.
var CurrentNamePart string

// SetNamePart sets the -name filter value.
func SetNamePart(s string) { CurrentNamePart = s }

// NameOrRegexMatch returns true if, based on provided filters, the base name or the full path matches.
// If neither filter is provided, match all. If both provided, OR semantics.
func NameOrRegexMatch(baseName, logicalPath string, matcher *regexp.Regexp) bool {
	nameProvided := CurrentNamePart != ""
	regexProvided := matcher != nil
	switch {
	case !nameProvided && !regexProvided:
		return true // no filters -> match all
	case nameProvided && !regexProvided:
		return nameMatch(baseName)
	case !nameProvided && regexProvided:
		return matcher.MatchString(logicalPath)
	default: // both provided -> OR semantics
		return nameMatch(baseName) || matcher.MatchString(logicalPath)
	}
}

func nameMatch(base string) bool {
	if CurrentNamePart == "" {
		return false
	}
	b := strings.ToLower(base)
	q := strings.ToLower(CurrentNamePart)
	return strings.Contains(b, q)
}

// SplitMount splits the provided path into mount and inner parts.
func SplitMount(p string) (mount string, inner string) {
	p = strings.TrimPrefix(p, "/")
	parts := strings.SplitN(p, "/", 2)
	mount = parts[0]
	if len(parts) > 1 {
		inner = parts[1]
	}
	inner = strings.TrimPrefix(inner, "/")
	return
}

func joinNonEmpty(elem ...string) string {
	var xs []string
	for _, e := range elem {
		if e == "" {
			continue
		}
		xs = append(xs, e)
	}
	return strings.Join(xs, "/")
}

// ListAPIPath returns the listing path given mount and inner for kv version
func ListAPIPath(mount, inner string, kv2 bool) string {
	if kv2 {
		// v2: use metadata for listing
		return path.Clean(joinNonEmpty(mount, "metadata", inner))
	}
	return path.Clean(joinNonEmpty(mount, inner))
}

// ReadAPIPath returns the read path given mount and inner for kv version
func ReadAPIPath(mount, inner string, kv2 bool) string {
	if kv2 {
		// v2: data path for read
		return path.Clean(joinNonEmpty(mount, "data", inner))
	}
	return path.Clean(joinNonEmpty(mount, inner))
}

// ReadSecret reads a secret and returns its data payload depending on kv version
func ReadSecret(ctx context.Context, logical LogicalAPI, mount, inner string, kv2 bool) (interface{}, error) {
	readPath := ReadAPIPath(mount, inner, kv2)
	sec, err := logical.ReadWithContext(ctx, readPath)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, fmt.Errorf("no data at %s", readPath)
	}
	if kv2 {
		// In some cases an empty secret may have a nil or missing data field.
		if raw, exists := sec.Data["data"]; !exists || raw == nil {
			return map[string]interface{}{}, nil
		}
		if data, ok := sec.Data["data"].(map[string]interface{}); ok {
			return data, nil
		}
		// If the payload isn't a map (unexpected), treat as empty rather than erroring out.
		return map[string]interface{}{}, nil
	}
	return sec.Data, nil
}

// WalkVault recursively walks the given start path and returns matching items
func WalkVault(
	ctx context.Context,
	logical LogicalAPI,
	start string,
	kv2 bool,
	maxDepth int,
	matcher *regexp.Regexp,
	withValues bool,
) ([]FoundItem, error) {
	mount, inner := SplitMount(start)
	var out []FoundItem
	if err := recurse(ctx, logical, mount, inner, kv2, 0, maxDepth, matcher, withValues, &out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func recurse(
	ctx context.Context,
	logical LogicalAPI,
	mount, inner string,
	kv2 bool,
	depth, maxDepth int,
	matcher *regexp.Regexp,
	withValues bool,
	out *[]FoundItem,
) error {
	if maxDepth > 0 && depth > maxDepth {
		return nil
	}

	listPath := ListAPIPath(mount, inner, kv2)
	sec, err := logical.ListWithContext(ctx, listPath)
	if err != nil {
		return err
	}
	if sec == nil || sec.Data == nil {
		// treat as leaf
		return handleLeaf(ctx, logical, mount, inner, kv2, matcher, withValues, out)
	}

	rawKeys, ok := sec.Data["keys"].([]interface{})
	if !ok {
		return fmt.Errorf("unexpected list response at %s", listPath)
	}
	for _, k := range rawKeys {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		key, _ := k.(string)
		if strings.HasSuffix(key, "/") {
			// recurse into subpath only if doing so can yield leaves within maxDepth
			nextDepth := depth + 1
			if maxDepth > 0 && nextDepth >= maxDepth {
				continue
			}
			nextInner := joinNonEmpty(strings.TrimSuffix(inner, "/"), strings.TrimSuffix(key, "/"))
			if err := recurse(ctx, logical, mount, nextInner, kv2, nextDepth, maxDepth, matcher, withValues, out); err != nil {
				return err
			}
		} else {
			// leaf candidate at depth+1
			if maxDepth > 0 && (depth+1) > maxDepth {
				continue
			}
			leafInner := joinNonEmpty(inner, key)
			if err := handleLeaf(ctx, logical, mount, leafInner, kv2, matcher, withValues, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleLeaf(
	ctx context.Context,
	logical LogicalAPI,
	mount, inner string,
	kv2 bool,
	matcher *regexp.Regexp,
	withValues bool,
	out *[]FoundItem,
) error {
	logicalPath := path.Clean(joinNonEmpty(mount, inner))
	base := path.Base(logicalPath)
	if !NameOrRegexMatch(base, logicalPath, matcher) {
		if !withValues {
			return nil
		}
	}

	if withValues {
		val, err := ReadSecret(ctx, logical, mount, inner, kv2)
		if err != nil {
			return err
		}
		if NameOrRegexMatch(base, logicalPath, matcher) {
			*out = append(*out, FoundItem{Path: logicalPath, Value: val})
		}
		return nil
	}

	if NameOrRegexMatch(base, logicalPath, matcher) {
		*out = append(*out, FoundItem{Path: logicalPath})
	}
	return nil
}

// DetectKV2 tries to determine whether the mount for the start path is KV v2.
func DetectKV2(ctx context.Context, c *vault.Client, start string) (bool, bool) {
	mount, _ := SplitMount(start)
	mounts, err := ListMountsWithFallback(ctx, c)
	if err != nil {
		return false, false
	}
	m, ok := mounts[mount+"/"]
	if !ok {
		return false, false
	}
	if m.Type != "kv" {
		return false, true // not a kv engine
	}
	if v, ok := m.Options["version"]; ok && v == "2" {
		return true, true
	}
	return false, true
}

// NewVaultClient creates a vault client from env configuration and optional token discovery
func NewVaultClient() (*vault.Client, error) {
	cfg := vault.DefaultConfig()
	if err := cfg.ReadEnvironment(); err != nil {
		return nil, err
	}
	c, err := vault.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	// Token: prefer env, then fallback to ~/.vault-token. If none, continue without a token
	if tok := os.Getenv("VAULT_TOKEN"); tok != "" {
		c.SetToken(tok)
	} else if home, _ := os.UserHomeDir(); home != "" {
		if b, err := os.ReadFile(path.Join(home, ".vault-token")); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				c.SetToken(t)
			}
		}
	}
	return c, nil
}

// CheckConnection verifies the Vault server is reachable by calling the health endpoint.
func CheckConnection(ctx context.Context, c *vault.Client) error {
	_, err := c.Sys().HealthWithContext(ctx)
	return err
}
