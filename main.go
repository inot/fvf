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

    "vf/search"
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
            sub, err := search.WalkVault(ctx, client.Logical(), mnt, kv2, opts.maxDepth, matcher, opts.printValues)
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
        items, err = search.WalkVault(ctx, client.Logical(), opts.startPath, kv2, opts.maxDepth, matcher, opts.printValues)
        if err != nil {
            fatal(err)
        }
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
    flag.StringVar(&opts.startPath, "path", "", "Start path to recurse, e.g. secret/ or secret/app/ (default: all KV mounts)")
    flag.BoolVar(&opts.kv2, "kv2", false, "Assume KV v2 (used if detection fails)")
    flag.BoolVar(&opts.forceKV2, "force-kv2", false, "Force KV v2 and skip auto-detection")
    flag.StringVar(&opts.match, "match", "", "Optional regex to match full logical path")
    flag.StringVar(&opts.namePart, "name", "", "Case-insensitive substring to match secret name (last segment)")
    flag.BoolVar(&opts.printValues, "values", false, "Also read and print values as JSON")
    flag.IntVar(&opts.maxDepth, "max-depth", 0, "Maximum recursion depth (0 = unlimited)")
    flag.BoolVar(&opts.jsonOut, "json", false, "Output JSON array instead of lines")
    flag.DurationVar(&opts.timeout, "timeout", 30*time.Second, "Total timeout for the operation")
    flag.Parse()

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
    fmt.Fprintf(os.Stderr, "\nUsage: vf [-path <mount/inner/>] [flags]\n\n")
    flag.PrintDefaults()
    os.Exit(2)
}

func fatal(err error) {
    fmt.Fprintln(os.Stderr, "Error:", err)
    os.Exit(1)
}
