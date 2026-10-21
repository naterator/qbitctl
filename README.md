# qbitctl

**qbitctl** is a Go library and CLI tool for controlling qBittorrent through its HTTP API.

Use it as a **standalone command-line tool** to manage torrents from the terminal, or import `pkg/client` as a **Go library** to build qBittorrent automation into your own applications.

- Query and display torrent properties
- Update torrent settings such as tags, category, limits, and share rules
- Start, pause, move, remove, and delete torrents
- View qBittorrent server info and transfer statistics
- Store credentials locally or override them from the command line
- Self-update from GitHub releases
- Embed as a library with configurable I/O, context support, and no hardcoded stdout/stderr

## Compatibility

Requires **qBittorrent 5.0.0** or later (WebAPI v2.11.0+). Earlier versions used different endpoint names (`/api/v2/torrents/pause` and `/api/v2/torrents/resume`) that are not supported.

Supports both the older HTTP 200 login response and the HTTP 204 response used by qBittorrent 5.2 and later, including their session cookies and automatic session renewal.

## Install

Download the latest release for your platform:

```bash
curl -Lo qbitctl "https://github.com/naterator/qbitctl/releases/latest/download/qbitctl-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x qbitctl
sudo mv qbitctl /usr/local/bin/
```

Once installed, keep it up to date with:

```bash
qbitctl update
```

Self-update checks SHA-256 and verifies the downloaded release asset with GitHub release attestations using the GitHub CLI (`gh`). Install `gh` before using `qbitctl update`. The release must be immutable and published with all assets already attached. Mutable releases have no release attestation and cannot pass this verification.

## Build from source

Requirements: Go 1.26.10+

```bash
go build -o qbitctl ./cmd/qbitctl
```

For a stripped static binary:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o qbitctl ./cmd/qbitctl
```

The version is injected at build time via `-ldflags`. The Makefile handles this automatically with `VERSION`, defaulting to `dev-<short-sha>`:

```bash
make            # build with VERSION or dev-<short-sha>
make run        # go run
make test       # go test ./...
make check      # test + vet + fmt-check
make fmt        # gofmt -w
```

Build every Go-supported `amd64` and `arm64` target except Android and iOS:

```bash
VERSION=v1.2.3 bash scripts/build.sh
```

Binaries and individual SHA-256 files are written to `dist/`. Windows binaries use `.exe`; other targets use `qbitctl-<os>-<arch>`. The target list comes from `go tool dist list` for the installed Go toolchain. Android and iOS require separate SDK/CGO builds.

CI and release workflows follow the [notes workflow templates](https://github.com/naterator/notes/tree/main/.github/workflows), with every job running on `ubuntu-24.04`. CI runs formatting, vet, race tests, a native CLI/API smoke check, a vulnerability check, and all release cross-builds.

To publish releases that support verified updates:

1. Enable **Release immutability** in the repository's GitHub settings before publishing new releases.
2. Create and push a version tag, or manually run **Draft release assets** with an existing tag. Both `vMAJOR.MINOR.PATCH` and `MAJOR.MINOR.PATCH` are accepted.
3. The workflow runs tests, builds and checksums every target, checks the native release binary, and attaches the assets to a draft release. It can resume an existing draft but refuses to change an already published release.
4. Review and publish the completed draft. GitHub locks the tag and assets and creates the release attestation. Check a downloaded asset with `gh release verify-asset <exact-tag> <asset-path> --repo naterator/qbitctl`.

See GitHub's [immutable release publishing guidance](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases). Attaching binaries after publication is incompatible with immutable releases.

### Upgrading from 1.0.2 or 1.0.3

These versions incorrectly add `v` to unprefixed tags before verification, so a release tagged `1.0.3` is looked up as `v1.0.3`. This is fixed in source by preserving the exact published tag for verification. Existing installations can either install a release manually once, or update to a newer **`v`-prefixed immutable release** using their existing update command (`selfupdate` in 1.0.2, `update` in 1.0.3). Renaming a tag alone does not supply the missing release attestation.

## Project layout

```text
cmd/qbitctl/     Cobra CLI, command definitions, terminal helpers
pkg/client/      qBittorrent API client library (importable by external projects)
Makefile         Build, test, and format targets
```

## Quick start

```bash
qbitctl config                    # set up credentials
qbitctl list                      # list all torrents
qbitctl info                      # show server info and transfer stats
qbitctl show 9c328901             # show detailed info for one torrent
qbitctl add ./ubuntu.torrent      # add a torrent
qbitctl start 9c328901            # start a torrent
```

## Authentication

qbitctl needs qBittorrent Web API credentials. It loads the first matching config file in this order, then applies command-line overrides:

1. `-c/--config <path>` (explicit config file)
2. `$XDG_CONFIG_HOME/qbitctl/config.json`
3. `./config.json`

Command-line overrides use `-u/--url`, `-U/--user`, and `-p/--pass`.

If `config.json` contains a cleartext password, qbitctl rewrites it in place with the encrypted `enc:v...` format.

### Interactive config

```bash
qbitctl config
```

Prompts for URL (default `http://localhost:8080`), username (default `admin`), password, and save path.

### Non-interactive config write

```bash
qbitctl -u http://localhost:8080 -U admin -p secret config write
qbitctl -u http://localhost:8080 -U admin -p secret config write -o ./config.json
```

## Commands

Every command supports short aliases for quick usage. Run `qbitctl help <command>` to see aliases and flags for any command.

### Global flags

```text
-c, --config <path>   Alternate config file path
-u, --url <url>       qBittorrent WebUI URL
-U, --user <user>     qBittorrent username
-p, --pass <pass>     qBittorrent password
-v, --version         Show version
-h, --help            Show help
```

### View commands

#### info (aliases: i, in)

Show qBittorrent server version and transfer statistics:

```bash
qbitctl info
qbitctl info -j             # output as JSON
qbitctl info -J             # raw server JSON response
```

#### list (aliases: l, li, ls)

List all torrents sorted by progress (most complete first):

```bash
qbitctl list
qbitctl list -j             # output as JSON
qbitctl list -J             # raw server JSON response
qbitctl list -t '{{range .}}{{.Hash}} {{.Name}}{{"\n"}}{{end}}'
qbitctl list --fields hash,name,progress
```

#### show (aliases: s, sh)

Show detailed info for a single torrent:

```bash
qbitctl show 9c328901
qbitctl show 9c328901 -j    # output as JSON
qbitctl show 9c328901 -J    # raw server JSON response
qbitctl show 9c328901 -t '{{.Name}}'
qbitctl show 9c328901 --fields name,state,ratio --no-header
```

#### get (alias: g)

Read one torrent property:

```bash
qbitctl get name 9c328901
qbitctl get progress 9c328901
qbitctl get tracker-list 9c328901
qbitctl get name 9c328901 -j    # output as JSON
qbitctl get name 9c328901 -J    # raw server JSON response
```

Available fields: `autotmm`, `category`, `dl-limit`, `dl-path`, `dl-speed`, `downloaded`, `eta`, `hash`, `name`, `private`, `progress`, `ratio`, `ratio-limit`, `seedtime`, `seedtime-limit`, `seqdl`, `size`, `state`, `superseed`, `tags`, `tracker`, `tracker-list`, `up-limit`, `up-speed`, `uploaded`

### Change commands

#### add (aliases: a, ad)

```bash
qbitctl add ./ubuntu.torrent
qbitctl add 'magnet:?xt=urn:btih:...'
```

#### set (alias: se)

Update one torrent property:

```bash
qbitctl set category Linux 9c328901
qbitctl set tags Linux,ISO 9c328901
qbitctl set up-limit 1024 9c328901
qbitctl set dl-limit 2048 9c328901
qbitctl set ratio-limit 2.0 9c328901
qbitctl set seedtime-limit 3600 9c328901
qbitctl set seqdl true 9c328901
qbitctl set autotmm false 9c328901
qbitctl set superseed true 9c328901
```

Available fields: `autotmm`, `category`, `dl-limit`, `ratio-limit`, `seedtime-limit`, `seqdl`, `superseed`, `tags`, `up-limit`

Input formats:

```text
up-limit / dl-limit                Bytes/sec, or "k" suffix for KiB (e.g. 512k)
ratio-limit                        Floating point (e.g. 2.0)
seedtime-limit                     Seconds (e.g. 86400)
seqdl / autotmm / superseed        true or false
```

#### move (aliases: m, mo)

```bash
qbitctl move /downloads/linux 9c328901
```

#### remove (aliases: r, rm) / delete (aliases: d, de)

Remove and delete also accept multiple hashes:

```bash
qbitctl remove 9c328901             # remove torrent, keep data
qbitctl delete 9c328901             # remove torrent and delete data
qbitctl remove 9c328901 fedcba98    # remove multiple torrents
```

### Control commands

Control commands accept one or more hashes:

```bash
qbitctl start 9c328901         # (alias: st)
qbitctl pause 9c328901         # (aliases: p, pa)
qbitctl force-start 9c328901   # (aliases: f, fo, fs)
qbitctl start 9c328901 fedcba98   # start multiple torrents
qbitctl pause 9c32 fedc           # short hashes work too
```

### Setup

```bash
qbitctl config                 # interactive setup (aliases: c, co)
qbitctl config write           # non-interactive (requires -p)
```

### Additional commands

```bash
qbitctl update                 # download and install latest release (aliases: upd, up, upgrade)
qbitctl update -d              # check for updates without installing
qbitctl version                # show version (aliases: v, ve, ver)
qbitctl completion bash        # generate shell completions (alias: com)
qbitctl help                   # help (aliases: h, he)
```

### Output format flags

Commands in the View group support these output flags:

| Flag | Short | Description |
|------|-------|-------------|
| `--json` | `-j` | Output the normal display data as JSON |
| `--server-json` | `-J` | Output the raw qBittorrent API JSON response |
| `--template` | `-t` | Render output with a Go template (list and show only) |
| `--fields` | | Render selected fields as tab-separated rows (list and show only) |
| `--no-header` | | Omit the header row when using `--fields` |

## Torrent hashes

Single-torrent commands accept the hash as a positional argument. Short hash prefixes (8+ characters recommended) are resolved automatically as long as they are unambiguous:

```bash
qbitctl show 9c328901
qbitctl get name 9c3289
qbitctl start 9c32
```

## Exit codes

```text
0  Success
1  Authentication failed
2  API fetch request failed
3  API set request failed
4  Invalid command-line arguments
5  File/auth configuration error
6  Action failed
```

## Library usage

The `pkg/client` package can be used as a standalone Go library. All I/O is configurable through `App.Stdout` and `App.Stderr`, and all requests respect `App.Ctx` for cancellation and timeouts.

```go
import (
    "bytes"
    "context"
    "io"
    "log"
    "time"

    "github.com/naterator/qbitctl/pkg/client"
)

app, err := client.NewClient(&client.Options{
    URL:  "http://localhost:8080",
    User: "admin",
    Pass: "secret",
})
if err != nil {
    log.Fatal(err)
}

// Configure I/O (defaults to os.Stdout/os.Stderr)
app.Stdout = &bytes.Buffer{}
app.Stderr = io.Discard

// Set a context for timeouts
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
app.Ctx = ctx

// Use the API
hash, err := app.ResolveHash("9c328901")
if err != nil {
    log.Fatal(err)
}
if err := app.ShowSingleTorrentInfo(hash); err != nil {
    log.Fatal(err)
}
if err := app.SetField(hash, "category", "linux"); err != nil {
    log.Fatal(err)
}
if err := app.StartTorrent(hash); err != nil {
    log.Fatal(err)
}
```

Key library types:

- `client.App` — authenticated qBittorrent client with configurable I/O
- `client.Options` — connection and authentication options
- `client.TorrentInfo` — torrent metadata (JSON-mapped)
- `client.TransferInfo` — server transfer statistics
- `client.CodedError` — error with exit code for CLI integration
- `client.HiddenInputFunc` — password echo control for interactive config

## Test

```bash
go test ./...              # all tests
go test ./pkg/client/      # client library tests
go test ./cmd/qbitctl      # CLI tests
make check                 # test + vet + fmt-check
```

## License

This project is licensed under the **BSD 3-Clause License**. See [LICENSE](./LICENSE).
