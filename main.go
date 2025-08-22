package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"fvf/search"
	"fvf/ui"

	vault "github.com/hashicorp/vault/api"
	"golang.org/x/term"
)

// Version information. Overwrite via -ldflags "-X main.version=... -X main.commit=... -X main.date=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type options struct {
	startPath   string
	kv2         bool
	kv1         bool
	forceKV2    bool
	match       string
	namePart    string
	printValues bool
	maxDepth    int
	jsonOut     bool
	timeout     time.Duration
	interactive bool
	showVersion bool
	paths       []string
}

func main() {
	opts := parseFlags()
	search.SetNamePart(opts.namePart)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	client, err := search.NewVaultClient()
	if err != nil {
		fatal(err)
	}

	if err := search.CheckConnection(ctx, client); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot connect to Vault:", err)
		os.Exit(1)
	}

	matcher, err := buildMatcher(opts.match)
	if err != nil {
		fatal(err)
	}

	items, err := collectItems(ctx, client, opts, matcher)
	if err != nil {
		fatal(err)
	}

	if opts.interactive {
		if err := runInteractive(items, opts, client); err != nil {
			fatal(err)
		}
		return
	}

	if err := printItems(items, opts); err != nil {
		fatal(err)
	}
}

func parseFlags() options {
	// Delegate to the args-based parser for testability
	return parseFlagsWithArgs(os.Args[1:])
}

// parseFlagsWithArgs builds a local FlagSet to allow deterministic tests.
func parseFlagsWithArgs(args []string) options {
	var opts options
	fs := flag.NewFlagSet("fvf", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	// multi-paths as a simple comma-separated string flag
	pathsRaw := fs.String("paths", "", "Comma-separated list of start paths, e.g. kv/app1/,kv/app2/")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "fvf %s (commit %s, built %s)\n\n", version, commit, date)
		fmt.Fprintf(os.Stderr, "Usage: fvf [-path <mount/inner/>] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Note: Running with no flags starts Interactive mode by default.\n\n")
		fs.PrintDefaults()
	}

	fs.StringVar(&opts.startPath, "path", "", "Start path to recurse, e.g. secret/ or secret/app/ (default: all KV mounts)")
	fs.BoolVar(&opts.kv2, "kv2", true, "Assume KV v2 (default). If unsure, leave as-is.")
	fs.BoolVar(&opts.kv1, "kv1", false, "Assume KV v1 (overrides -kv2 and skips detection)")
	fs.BoolVar(&opts.forceKV2, "force-kv2", false, "Force KV v2 and skip auto-detection")
	fs.StringVar(&opts.match, "match", "", "Optional regex to match full logical path")
	fs.StringVar(&opts.namePart, "name", "", "Case-insensitive substring to match secret name (last segment)")
	fs.BoolVar(&opts.printValues, "values", false, "Print values (interactive preview when stdout is a TTY)")
	fs.IntVar(&opts.maxDepth, "max-depth", 0, "Maximum recursion depth (0 = unlimited)")
	fs.BoolVar(&opts.jsonOut, "json", false, "Output JSON array instead of lines")
	fs.DurationVar(&opts.timeout, "timeout", 30*time.Second, "Total timeout for the operation")
	fs.BoolVar(&opts.interactive, "interactive", false, "Interactive TUI filter (like fzf): type to filter, Enter prints secret value")
	fs.BoolVar(&opts.showVersion, "version", false, "Print version information and exit")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			// Help was requested; usage already printed by fs.Parse.
			os.Exit(0)
		}
		// Other parsing errors: show usage with the error message.
		usageAndExit(err.Error())
	}

	// Default/interactive determination is factored for testing
	opts.interactive = determineInteractive(opts, len(args), term.IsTerminal(int(os.Stdout.Fd())))

	if opts.showVersion {
		fmt.Printf("fvf %s (commit %s, built %s)\n", version, commit, date)
		os.Exit(0)
	}

	// finalize multi-paths from comma-separated input
	if *pathsRaw != "" {
		for _, p := range strings.Split(*pathsRaw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				opts.paths = append(opts.paths, p)
			}
		}
	}

	if strings.TrimSpace(opts.startPath) == "" {
		return opts
	}
	if opts.startPath == "" {
		usageAndExit("-path is required")
	}
	return opts
}

// determineInteractive computes whether to run in interactive mode given inputs.
func determineInteractive(opts options, argsLen int, stdoutIsTTY bool) bool {
	// No flags -> interactive by default
	if argsLen == 0 {
		return true
	}
	// If values or json are requested and stdout is a terminal, prefer interactive TUI
	if stdoutIsTTY && (opts.printValues || opts.jsonOut) {
		return true
	}
	return opts.interactive
}

func usageAndExit(msg string) {
	if msg != "" {
		fmt.Fprintln(os.Stderr, "Error:", msg)
	}
	fmt.Fprintf(os.Stderr, "\nfvf %s (commit %s, built %s)\n\n", version, commit, date)
	fmt.Fprintf(os.Stderr, "Usage: fvf [-path <mount/inner/>] [flags]\n\n")
	fmt.Fprintf(os.Stderr, "Note: Running with no flags starts Interactive mode by default.\n\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}

// buildMatcher compiles a regexp pattern if provided, else returns nil.
func buildMatcher(pattern string) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

// valuesDuringWalk returns whether values should be fetched during Walk.
// In interactive mode we avoid fetching to keep the UI responsive.
func valuesDuringWalk(opts options) bool {
	return opts.printValues && !opts.interactive
}

// decideKV2ForMountMeta determines kv2 based on CLI flags and mount metadata.
// If -kv1 is set -> false. If -force-kv2 is set -> opts.kv2. Otherwise use mount Options["version"] == "2".
func decideKV2ForMountMeta(opts options, mountOptions map[string]string) bool {
	if opts.kv1 {
		return false
	}
	if opts.forceKV2 {
		return opts.kv2
	}
	if v, ok := mountOptions["version"]; ok && v == "2" {
		return true
	}
	return false
}

// decideKV2ForPath uses DetectKV2 unless forced by flags.
func decideKV2ForPath(ctx context.Context, client *vault.Client, start string, opts options) bool {
	if opts.kv1 {
		return false
	}
	if opts.forceKV2 {
		return opts.kv2
	}
	if client == nil {
		return opts.kv2
	}
	if v, ok := search.DetectKV2(ctx, client, start); ok {
		return v
	}
	return opts.kv2
}

// collectItems routes to the correct collection strategy.
func collectItems(ctx context.Context, client *vault.Client, opts options, matcher *regexp.Regexp) ([]search.FoundItem, error) {
	if strings.TrimSpace(opts.startPath) == "" && len(opts.paths) == 0 {
		return collectAcrossAllMounts(ctx, client, opts, matcher)
	}
	if len(opts.paths) > 0 {
		return collectForPaths(ctx, client, opts, matcher)
	}
	return collectForSinglePath(ctx, client, opts, matcher)
}

func collectAcrossAllMounts(ctx context.Context, client *vault.Client, opts options, matcher *regexp.Regexp) ([]search.FoundItem, error) {
	mounts, err := search.ListMountsWithFallback(ctx, client)
	if err != nil {
		var respErr *vault.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == 403 {
			printGreenHint("fvf: permission denied listing mounts (sys/mounts). Fallback to sys/internal/ui/mounts also failed. Use -path to target a known mount. If your mount is KV v1, add -kv1.")
			fmt.Fprintln(os.Stderr, "Vault error:", err)
			os.Exit(1)
		}
		printGreenHint("fvf: cannot list mounts (provide -path to search a known mount). If your mount is KV v1, add -kv1.")
		fmt.Fprintln(os.Stderr, "Vault/Client error:", err)
		os.Exit(1)
	}
	var items []search.FoundItem
	for mntPath, m := range mounts {
		if m.Type != "kv" {
			continue
		}
		mnt := strings.TrimSuffix(mntPath, "/")
		kv2 := decideKV2ForMountMeta(opts, m.Options)
		sub, err := search.WalkVault(ctx, client.Logical(), mnt, kv2, opts.maxDepth, matcher, valuesDuringWalk(opts))
		if err != nil {
			return nil, fmt.Errorf("error walking mount %s: %w", mnt, err)
		}
		items = append(items, sub...)
	}
	return items, nil
}

func collectForPaths(ctx context.Context, client *vault.Client, opts options, matcher *regexp.Regexp) ([]search.FoundItem, error) {
	var items []search.FoundItem
	for _, p := range opts.paths {
		kv2 := decideKV2ForPath(ctx, client, p, opts)
		sub, err := search.WalkVault(ctx, client.Logical(), p, kv2, opts.maxDepth, matcher, valuesDuringWalk(opts))
		if err != nil {
			return nil, fmt.Errorf("error walking path %s: %w", p, err)
		}
		items = append(items, sub...)
	}
	return items, nil
}

func collectForSinglePath(ctx context.Context, client *vault.Client, opts options, matcher *regexp.Regexp) ([]search.FoundItem, error) {
	kv2 := decideKV2ForPath(ctx, client, opts.startPath, opts)
	return search.WalkVault(ctx, client.Logical(), opts.startPath, kv2, opts.maxDepth, matcher, valuesDuringWalk(opts))
}

func runInteractive(items []search.FoundItem, opts options, client *vault.Client) error {
	fetcher := func(p string) (string, error) {
		perReqTimeout := 15 * time.Second
		attempt := func() (interface{}, error) {
			reqCtx, cancel := context.WithTimeout(context.Background(), perReqTimeout)
			defer cancel()
			mnt, inner := search.SplitMount(p)
			kv2 := decideKV2ForPath(reqCtx, client, mnt, opts)
			return search.ReadSecret(reqCtx, client.Logical(), mnt, inner, kv2)
		}
		val, err := attempt()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
				val, err = attempt()
			}
		}
		if err != nil {
			return "", err
		}
		// In interactive mode, honor -json by showing pretty JSON in preview.
		if opts.jsonOut {
			if b, err := json.MarshalIndent(val, "", "  "); err == nil {
				return string(b), nil
			}
		}
		// Otherwise return a human-friendly raw representation where strings are unquoted.
		return formatValueRaw(val, true), nil
	}
	// Show preview if either -values or -json is set.
	return ui.Run(items, opts.printValues || opts.jsonOut, fetcher)
}

func printItems(items []search.FoundItem, opts options) error {
	if opts.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	}
	for _, it := range items {
		if opts.printValues {
			// Print values in raw form (unquoted strings). For maps, print concise k: v pairs.
			fmt.Printf("%s = %s\n", it.Path, formatValueRaw(it.Value, false))
		} else {
			fmt.Println(it.Path)
		}
	}
	return nil
}

// formatValueRaw renders Vault secret values in a copy/paste-friendly way:
// - Strings are printed without JSON quotes or escapes
// - Maps are rendered as k: v; if pretty is true, each on its own line; otherwise comma-separated
// - Non-strings fall back to fmt or JSON for complex/nested cases
func formatValueRaw(v interface{}, pretty bool) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []byte:
		return string(vv)
	case fmt.Stringer:
		return vv.String()
	case map[string]interface{}:
		if pretty {
			// multiline
			lines := make([]string, 0, len(vv))
			// stable-ish order by key
			keys := make([]string, 0, len(vv))
			for k := range vv {
				keys = append(keys, k)
			}
			// simple lexical sort
			for i := 0; i < len(keys)-1; i++ {
				for j := i + 1; j < len(keys); j++ {
					if keys[j] < keys[i] {
						keys[i], keys[j] = keys[j], keys[i]
					}
				}
			}
			for _, k := range keys {
				lines = append(lines, fmt.Sprintf("%s: %s", k, scalarToString(vv[k])))
			}
			return strings.Join(lines, "\n")
		}
		// single-line concise k: v, k2: v2
		parts := make([]string, 0, len(vv))
		for k, x := range vv {
			parts = append(parts, fmt.Sprintf("%s: %s", k, scalarToString(x)))
		}
		// no guaranteed order; acceptable for compact display
		return strings.Join(parts, ", ")
	default:
		// Try to keep simple scalars readable
		if s, ok := tryScalar(v); ok {
			return s
		}
		// Fallback to JSON for arbitrary/nested structures
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

func tryScalar(v interface{}) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", true
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", t), true
	}
	return "", false
}

func scalarToString(v interface{}) string {
	if s, ok := tryScalar(v); ok {
		return s
	}
	// If value is a slice of strings or numbers, render compactly
	switch arr := v.(type) {
	case []string:
		return strings.Join(arr, ", ")
	case []interface{}:
		parts := make([]string, 0, len(arr))
		for _, e := range arr {
			if s, ok := tryScalar(e); ok {
				parts = append(parts, s)
			} else if b, err := json.Marshal(e); err == nil {
				parts = append(parts, string(b))
			} else {
				parts = append(parts, fmt.Sprintf("%v", e))
			}
		}
		return strings.Join(parts, ", ")
	}
	// Fallback to JSON for nested or complex types
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// printGreenHint prints a friendly fvf hint message in green when stderr is a TTY,
// and as plain text otherwise. The Vault/raw error should be printed separately.
func printGreenHint(msg string) {
	if term.IsTerminal(int(os.Stderr.Fd())) {
		// ANSI green
		fmt.Fprintf(os.Stderr, "\x1b[32m%s\x1b[0m\n", msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}
