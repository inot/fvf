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
    "golang.org/x/term"
    vault "github.com/hashicorp/vault/api"
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
    var opts options
    // multi-paths as a simple comma-separated string flag
    pathsRaw := flag.String("paths", "", "Comma-separated list of start paths, e.g. kv/app1/,kv/app2/")
    // Custom usage to include version header
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "fvf %s (commit %s, built %s)\n\n", version, commit, date)
        fmt.Fprintf(os.Stderr, "Usage: fvf [-path <mount/inner/>] [flags]\n\n")
        fmt.Fprintf(os.Stderr, "Note: Running with no flags starts Interactive mode by default.\n\n")
        flag.PrintDefaults()
    }
    flag.StringVar(&opts.startPath, "path", "", "Start path to recurse, e.g. secret/ or secret/app/ (default: all KV mounts)")
    flag.BoolVar(&opts.kv2, "kv2", true, "Assume KV v2 (default). If unsure, leave as-is.")
    flag.BoolVar(&opts.kv1, "kv1", false, "Assume KV v1 (overrides -kv2 and skips detection)")
    flag.BoolVar(&opts.forceKV2, "force-kv2", false, "Force KV v2 and skip auto-detection")
    flag.StringVar(&opts.match, "match", "", "Optional regex to match full logical path")
    flag.StringVar(&opts.namePart, "name", "", "Case-insensitive substring to match secret name (last segment)")
    flag.BoolVar(&opts.printValues, "values", false, "Also read and print values as JSON")
    flag.IntVar(&opts.maxDepth, "max-depth", 0, "Maximum recursion depth (0 = unlimited)")
    flag.BoolVar(&opts.jsonOut, "json", false, "Output JSON array instead of lines")
    flag.DurationVar(&opts.timeout, "timeout", 30*time.Second, "Total timeout for the operation")
    flag.BoolVar(&opts.interactive, "interactive", false, "Interactive TUI filter (like fzf): type to filter, Enter to print selection")
    flag.BoolVar(&opts.showVersion, "version", false, "Print version information and exit")
    flag.Parse()

    // Default: if no flags provided, start in interactive mode
    if len(os.Args) == 1 {
        opts.interactive = true
    }

    // If values are requested and stdout is a terminal, prefer interactive TUI
    // (so `fvf -values` launches TUI with lazy preview instead of dumping everything)
    if opts.printValues && term.IsTerminal(int(os.Stdout.Fd())) && !opts.jsonOut {
        opts.interactive = true
    }

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
    mounts, err := client.Sys().ListMountsWithContext(ctx)
    if err != nil {
        var respErr *vault.ResponseError
        if errors.As(err, &respErr) && respErr.StatusCode == 403 {
            printGreenHint("fvf: permission denied listing mounts (sys/mounts). Use -path to target a known mount. If your mount is KV v1, add -kv1.")
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
        b, _ := json.MarshalIndent(val, "", "  ")
        return string(b), nil
    }
    return ui.Run(items, opts.printValues, fetcher)
}

func printItems(items []search.FoundItem, opts options) error {
    if opts.jsonOut {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(items)
    }
    for _, it := range items {
        if opts.printValues {
            b, _ := json.Marshal(it.Value)
            fmt.Printf("%s = %s\n", it.Path, string(b))
        } else {
            fmt.Println(it.Path)
        }
    }
    return nil
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
