package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "regexp"
    "strings"
    "time"

    "fvf/search"
    "fvf/ui"
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
    forceKV2    bool
    match       string
    namePart    string
    printValues bool
    maxDepth    int
    jsonOut     bool
    timeout     time.Duration
    interactive bool
    showVersion bool
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

    // Quick connectivity check before doing any work
    if err := search.CheckConnection(ctx, client); err != nil {
        fmt.Fprintln(os.Stderr, "Cannot connect to Vault:", err)
        os.Exit(1)
    }

    var matcher *regexp.Regexp
    if opts.match != "" {
        matcher, err = regexp.Compile(opts.match)
        if err != nil {
            fatal(err)
        }
    }

    var items []search.FoundItem
    if strings.TrimSpace(opts.startPath) == "" {
        // Search across all KV mounts
        mounts, err := client.Sys().ListMountsWithContext(ctx)
        if err != nil {
            fatal(fmt.Errorf("cannot list mounts (provide -path to search a known mount): %w", err))
        }
        for mntPath, m := range mounts {
            if m.Type != "kv" {
                continue
            }
            mnt := strings.TrimSuffix(mntPath, "/")
            kv2 := opts.kv2
            if !opts.forceKV2 {
                if v, ok := m.Options["version"]; ok && v == "2" {
                    kv2 = true
                } else {
                    kv2 = false
                }
            }
            // In interactive mode, avoid fetching values during the walk; fetch lazily in the UI
            valuesDuringWalk := opts.printValues && !opts.interactive
            sub, err := search.WalkVault(ctx, client.Logical(), mnt, kv2, opts.maxDepth, matcher, valuesDuringWalk)
            if err != nil {
                fatal(fmt.Errorf("error walking mount %s: %w", mnt, err))
            }
            items = append(items, sub...)
        }
    } else {
        // Determine KV version if not forced for the specific path
        kv2 := opts.kv2
        if !opts.forceKV2 {
            if v, ok := search.DetectKV2(ctx, client, opts.startPath); ok {
                kv2 = v
            }
        }
        valuesDuringWalk := opts.printValues && !opts.interactive
        items, err = search.WalkVault(ctx, client.Logical(), opts.startPath, kv2, opts.maxDepth, matcher, valuesDuringWalk)
        if err != nil {
            fatal(err)
        }
    }

    if opts.interactive {
        // Lazy value fetcher used by the preview pane and Enter output when -values is set
        fetcher := func(path string) (string, error) {
            mnt, inner := search.SplitMount(path)
            // Determine kv2 for this mount/path
            kv2 := opts.kv2
            if !opts.forceKV2 {
                if v, ok := search.DetectKV2(ctx, client, mnt); ok {
                    kv2 = v
                }
            }
            val, err := search.ReadSecret(ctx, client.Logical(), mnt, inner, kv2)
            if err != nil {
                return "", err
            }
            b, _ := json.MarshalIndent(val, "", "  ")
            return string(b), nil
        }
        // Launch interactive UI similar to fzf: type to filter, up/down to navigate, Enter to select.
        if err := ui.Run(items, opts.printValues, fetcher); err != nil {
            fatal(err)
        }
        return
    }

    if opts.jsonOut {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        if err := enc.Encode(items); err != nil {
            fatal(err)
        }
        return
    }

    for _, it := range items {
        if opts.printValues {
            b, _ := json.Marshal(it.Value)
            fmt.Printf("%s = %s\n", it.Path, string(b))
        } else {
            fmt.Println(it.Path)
        }
    }
}

func parseFlags() options {
    var opts options
    // Custom usage to include version header
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "fvf %s (commit %s, built %s)\n\n", version, commit, date)
        fmt.Fprintf(os.Stderr, "Usage: fvf [-path <mount/inner/>] [flags]\n\n")
        fmt.Fprintf(os.Stderr, "Note: Running with no flags starts Interactive mode by default.\n\n")
        flag.PrintDefaults()
    }
    flag.StringVar(&opts.startPath, "path", "", "Start path to recurse, e.g. secret/ or secret/app/ (default: all KV mounts)")
    flag.BoolVar(&opts.kv2, "kv2", false, "Assume KV v2 (used if detection fails)")
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
