# fvf

A fast interactive finder for HashiCorp Vault KV secrets. Think “fzf for Vault”.

- Interactive TUI to filter secrets across KV v1/v2.
- Lazy value preview in the right pane.
- Script-friendly plain or JSON output.

## Requirements

- Go 1.20+ (module currently targets Go 1.24)
- HashiCorp Vault access
- Environment:
  - VAULT_ADDR (e.g., <https://vault.example.com:8200>)
  - VAULT_TOKEN (or export a token from your auth flow)

## Install / Build

- Host build:

  ```sh
  make build
  ```

  Outputs: `dist/fvf`

- Cross-compile (macOS arm64, Linux amd64/arm64, Windows amd64):

  ```sh
  make build-all
  ```

  Outputs in `dist/`:
  - `fvf-darwin-arm64`
  - `fvf-linux-amd64`
  - `fvf-linux-arm64`
  - `fvf-windows-amd64.exe`

## Versioning

- If [./version] exists, its contents define the version (whitespace trimmed; trailing dots removed).
- Else falls back to `git describe`, else `0.1.0`.
- Build embeds:
  - `main.version`, `main.commit`, `main.date`

Check version:

```sh
./fvf -version
```

## Usage

- No flags: interactive TUI

  ```sh
  ./fvf
  ```

  - Type to filter; Up/Down to navigate; Enter prints selection.
  - Right pane shows value preview (when available).

- Interactive with values:

  ```sh
  ./fvf -values
  ```

  - If stdout is a TTY, launches TUI with lazy value preview and fetch-on-select.
  - If stdout is NOT a TTY (e.g., piping), prints all paths with values.

- Specific path:

  ```sh
  ./fvf -path secret/
  ./fvf -path kv/
  ./fvf -path kv/app/
  ```

- Name and regex filters:

  ```sh
  ./fvf -name conf
  ./fvf -match '^secret/.*/config$'
  ```

- JSON output:

  ```sh
  ./fvf -json
  ```

- KVv2 control:

  ```sh
  ./fvf -kv2
  ./fvf -force-kv2
  ```

- Depth and timeout:

  ```sh
  ./fvf -max-depth 2 -timeout 45s
  ```

### Flags

- -path string          Start path to recurse (default: all KV mounts)
- -kv2                  Assume KV v2 (used if detection fails)
- -force-kv2            Force KV v2 and skip auto-detection
- -match string         Regex on full logical path
- -name string          Substring match on last path segment
- -values               Print values (interactive when stdout is a TTY)
- -max-depth int        Max recursion depth (0 = unlimited)
- -json                 Output JSON array
- -timeout duration     Total timeout (default 30s)
- -interactive          Force interactive TUI
- -version              Print version and exit

## Makefile targets

- build — host build into `dist/fvf`
- build-all — cross-compile for macOS arm64, Linux amd64/arm64, Windows amd64
- macos-arm64, linux-amd64, linux-arm64, windows-amd64 — individual targets
- clean — remove `dist/`

Override version metadata at build:

```sh
VERSION=1.2.3 COMMIT=$(git rev-parse --short HEAD) DATE=$(date -u +%Y-%m-%d) make build
```

## Notes

- TTY-aware behavior for `-values`:
  - TTY stdout → TUI with preview
  - Non-TTY stdout → prints values for all matches
- KV v2 detection per mount unless `-force-kv2`.

## License

MIT
