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

## Install

Download the latest release for your platform:

```bash
curl -Lo qbitctl "https://github.com/naterator/qbitctl/releases/latest/download/qbitctl-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
chmod +x qbitctl
sudo mv qbitctl /usr/local/bin/
```

Once installed, keep it up to date with:

```bash
qbitctl selfupdate
```

## Build from source

Requirements: Go 1.26+

```bash
go build -o qbitctl ./cmd/qbitctl
```

For a stripped static binary:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o qbitctl ./cmd/qbitctl
```

The version is injected at build time via `-ldflags`. The Makefile handles this automatically:

```bash
make            # build with version from git describe
make run        # go run
make test       # go test ./...
make check      # test + vet + fmt-check
make fmt        # gofmt -w
```

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

qbitctl needs qBittorrent Web API credentials. They are resolved in this order:

1. `-c/--config <path>` (explicit config file)
2. `$XDG_CONFIG_HOME/qbitctl/config.json`
3. `./config.json`
4. Command-line overrides with `-u/--url`, `-U/--user`, `-p/--pass`

If `config.json` contains a cleartext password, qbitctl rewrites it in place with the encrypted `enc:v1:...` format.

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
```

#### show (aliases: s, sh)

Show detailed info for a single torrent:

```bash
qbitctl show 9c328901
qbitctl show 9c328901 -j    # output as JSON
qbitctl show 9c328901 -J    # raw server JSON response
qbitctl show 9c328901 -t '{{.Name}}'
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

```bash
qbitctl remove 9c328901        # remove torrent, keep data
qbitctl delete 9c328901        # remove torrent and delete data
```

### Control commands

```bash
qbitctl start 9c328901         # (alias: st)
qbitctl pause 9c328901         # (aliases: p, pa)
qbitctl force-start 9c328901   # (aliases: f, fo, fs)
```

### Setup

```bash
qbitctl config                 # interactive setup (aliases: c, co)
qbitctl config write           # non-interactive (requires -p)
```

### Additional commands

```bash
qbitctl selfupdate             # download and install latest release (aliases: su, up, update, upgrade)
qbitctl selfupdate -d          # check for updates without installing
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
import "github.com/naterator/qbitctl/pkg/client"

app, err := client.NewClient(&client.Options{
    URL:  "http://localhost:8080",
    User: "admin",
    Pass: "secret",
    Hash: "9c328901",
}, true)
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
rc := app.ShowSingleTorrentInfo()
rc = app.SetField("category", "linux")
rc = app.StartTorrent()
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
