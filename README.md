# <img src="assets/logo.svg" alt="qbitctl" width="418">

**qbitctl** is an interactive terminal UI, command-line tool, and Go library for controlling qBittorrent through its Web API.

- Query torrent properties from the CLI or inspect them in the TUI’s details table
- Update torrent settings such as tags, category, limits, and share rules
- Start, pause, move, remove, and delete torrents
- View qBittorrent server info and transfer statistics
- Search through qBittorrent's configured plugins and add results by ID
- Manage the queue, search results, and server stats in an interactive TUI
- Store credentials locally or override them from the command line
- Self-update from GitHub releases
- Embed as a library with configurable I/O, context support, and no hardcoded stdout/stderr

## TUI walkthrough

The walkthrough opens the grouped keyboard shortcuts and searchable command
palette, changes the startup preference in Config, and shows its confirmation
clear after five seconds. It previews a color scheme and opens the torrent
details table, then searches for Ubuntu and sorts and filters the results.
After searching for Debian, it adds the earlier Ubuntu result by ID and checks
server status. Recent IDs remain usable for up to 24 hours with the same
qBittorrent URL and user. The recording uses a local mock qBittorrent server
and sample data.

![qbitctl TUI walkthrough showing shortcuts, configuration, torrent details, and search](assets/demo.gif)

[Replayable terminal recording](assets/demo.cast) · [Recording script](scripts/record_demo.py)

Replay in your terminal with `asciinema play assets/demo.cast`.

## Compatibility

Requires **qBittorrent 5.0.0** or later (WebAPI v2.11.0+). Earlier versions used different endpoint names (`/api/v2/torrents/pause` and `/api/v2/torrents/resume`) that are not supported.

Supports both the older HTTP 200 login response and the HTTP 204 response used by qBittorrent 5.2 and later, including their session cookies and automatic session renewal.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | bash
```

The [installer](scripts/install.sh) downloads the latest release for your
platform, verifies its published SHA-256 checksum, and installs `qbitctl`:

- **Regular user:** `~/.local/bin/qbitctl`. If that directory isn't on your
  `PATH`, the installer adds it in your shell's startup file (`~/.zshrc`,
  `~/.bashrc` or, on macOS, `~/.bash_profile`, fish's `conf.d`, or
  `~/.profile`). Open a new terminal afterwards.
- **Root, or a user who can already write to `/usr/local/bin`:**
  `/usr/local/bin/qbitctl`, with no startup file changes.

The installer never runs `sudo` itself. For a system-wide install, run it as
root:

```bash
curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | sudo bash
```

A root-owned install then needs `sudo qbitctl update` to update. A per-user
install updates without `sudo`.

| Variable | Effect |
|---|---|
| `QBITCTL_VERSION` | Install a specific release, such as `1.3.1`, instead of the latest |
| `QBITCTL_INSTALL_DIR` | Install into this directory instead of the default |
| `QBITCTL_NO_MODIFY_PATH=1` | Leave shell startup files unchanged |

Set variables on `bash`, not `curl`:

```bash
curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | QBITCTL_VERSION=1.3.1 bash
```

To read the script before running it, download it with
`curl -fsSLO https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh`,
then run `bash install.sh`.

The installer supports macOS, Linux, FreeBSD, NetBSD, OpenBSD, and DragonFly
on the architectures that `uname -m` reports as `x86_64`/`amd64`,
`aarch64`/`arm64`, or `armv7l`. For Windows, illumos, Solaris, Plan 9, and
NetBSD on ARM (which reports `evbarm`), download the matching asset and its
`.sha256` file from the [releases page](https://github.com/naterator/qbitctl/releases/latest).

ARMv7 (`armv7l`) is the minimum for the prebuilt 32-bit Linux asset. ARMv6
devices such as the original Pi Zero and Pi 1 need a build for their CPU.
GitHub CLI users can also verify the installed binary against its release with
`gh release verify-asset <release-tag> ~/.local/bin/qbitctl --repo naterator/qbitctl`
([reference](https://cli.github.com/manual/gh_release_verify-asset)).

Once installed, keep it up to date with:

```bash
qbitctl update
```

qbitctl checks the SHA-256 checksum and verifies GitHub's signed release attestation before replacing the executable. Self-update works without a separate CLI or sign-in. The release must be immutable and published with all assets already attached. On Windows, the replaced executable is kept as `qbitctl.exe.old` until the next launch removes it.

## Build from source

Requires Go 1.27.4 or newer.

```bash
go build -o qbitctl ./cmd/qbitctl
```

For a stripped static binary:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o qbitctl ./cmd/qbitctl
```

The version is injected at build time via `-ldflags`. The Makefile handles
this automatically with `VERSION`, which defaults to `dev-<short-sha>`.

```bash
make            # Build with VERSION or dev-<short-sha>
make check      # Run the quick test, vet, and formatting checks
make test-race  # Run the tests with the race detector
make smoke      # Build and exercise the CLI and TUI
make ci         # Run the native CI checks locally
```

Build the release targets declared in [scripts/build.sh](scripts/build.sh):

```bash
VERSION=v1.2.3 bash scripts/build.sh
```

Binaries and individual SHA-256 files are written to `dist/`. Windows binaries use `.exe`; other targets use `qbitctl-<os>-<arch>`. The release target list is checked against the installed Go toolchain before building.

CI and release packaging run on `ubuntu-26.04`, with a focused `windows-2025` job for self-update recovery. CI runs formatting, vet, race tests, CLI and terminal smoke tests, a qBittorrent API compatibility matrix, a vulnerability scan, and all release cross-builds.

To publish releases that support verified updates:

1. Enable **Release immutability** in the repository's GitHub settings before publishing new releases.
2. Create a signed version tag with `git tag -s` and push it, or manually run **Draft release assets** with an existing signed tag. Both `vMAJOR.MINOR.PATCH` and `MAJOR.MINOR.PATCH` are accepted.
3. The workflow waits for the latest push-triggered CI run for the exact release commit to pass before cross-building assets. It checks the native release binary and verifies the tag again before attaching assets to a draft. It can resume an existing draft but refuses to change an already published release.
4. Review and publish the completed draft. GitHub locks the tag and assets and creates the release attestation for self-updates.

See GitHub's [immutable release publishing guidance](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases). Attaching binaries after publication is incompatible with immutable releases.

## Project layout

```text
cmd/qbitctl/             Cobra CLI, self-update, and terminal helpers
internal/configfile/     Locked, atomic config and cache file updates
internal/qbtest/         In-memory qBittorrent API fixture
internal/releaseattest/  Sigstore and TUF release attestation verification
internal/searchresults/  Shared search sorting, filtering, and size parsing
internal/tui/            Interactive queue, search, server, and configuration
pkg/client/              Importable qBittorrent API client library
scripts/                 Build, smoke, compatibility, and demo scripts
Makefile                 Build, test, CI, and formatting targets
```

## Quick start

```bash
qbitctl config                    # set up credentials
qbitctl list                      # list all torrents
qbitctl info                      # show server info and transfer stats
qbitctl tui                       # open the interactive workspace
qbitctl search "Ubuntu 24.04"     # search with the Jackett plugin
qbitctl show 9c328901             # show detailed info for one torrent
qbitctl add ./ubuntu.torrent      # add a torrent
qbitctl start 9c328901            # start a torrent
```

## Authentication

qbitctl needs qBittorrent Web API credentials. It loads the first matching config file in this order, then applies command-line overrides:

1. `-c/--config <path>` (explicit config file)
2. `$XDG_CONFIG_HOME/qbitctl/config.json` (defaults to `~/.config/qbitctl/config.json`)
3. `./config.json`

Override the server URL with `-u/--url` and the username with `-U/--user`. Supply an explicit password when overriding the URL to point at a different server origin. Server URLs can include a reverse-proxy path prefix. Use an HTTP or HTTPS URL without embedded credentials, a query, or a fragment.

qbitctl encrypts saved credentials with a private master key from the OS keyring or `QBITCTL_MASTER_KEY`. On a headless host, supply a stable secret through the environment before saving credentials and on later runs. If qbitctl cannot read an existing saved password, run `qbitctl config` to enter it again.

Each authenticated command uses its own Web UI session. qbitctl attempts to
log out when the command finishes, including after errors or Ctrl-C. A failed
logout leaves the command's output and exit status intact.

### Supplying the password

Use `--pass-stdin` or `QBITCTL_PASSWORD` to keep the Web UI password out of
process arguments. `QBITCTL_MASTER_KEY` is the separate key used to encrypt
and decrypt saved passwords.

| Precedence | Source |
|---|---|
| 1 | `--pass-stdin` or `-p/--pass`. Use only one. |
| 2 | A non-empty `QBITCTL_PASSWORD` environment variable. |
| 3 | The saved password for the configured server origin. |

`--pass-stdin` reads the first line from piped or redirected input, up to
4096 bytes. Bare `qbitctl`, `qbitctl config`, and `qbitctl tui` reserve stdin
for interactive input. An exported `QBITCTL_PASSWORD` can be sent to the
server selected by `--url` or the active config, so prefer setting it for
each invocation.

```bash
printf '%s\n' "$QBT_PASS" | qbitctl -u http://nas:8080 -U admin --pass-stdin list
QBITCTL_PASSWORD="$QBT_PASS" qbitctl -u http://nas:8080 -U admin list
```

### macOS Keychain

qbitctl keeps the encrypted Web UI password in `config.json` and stores the
encryption key in Keychain under service `qbitctl`, account `master-key`. On
first save it uses your default user Keychain. If the save reports that
Keychain is unavailable, check that your default Keychain exists and is
unlocked in Keychain Access, then retry `qbitctl config`.

For a fresh setup without Keychain access, set `QBITCTL_MASTER_KEY` to a stable
secret before running `qbitctl config` and supply the same secret on later runs.
An existing `enc:v2` config requires the key that encrypted it.

### Interactive config

```bash
qbitctl config
```

Prompts for URL, username, password, and save path. The URL and username default to the saved values, or to `http://localhost:8080` and `admin` when nothing valid is saved. Use `qbitctl --config /path/to/config.json config` to choose the save path in advance. The text setup checks authentication and API compatibility before saving, and stops with an error if input ends before every prompt is answered.

### Non-interactive config write

```bash
printf '%s\n' "$QBT_PASS" | qbitctl -u http://localhost:8080 -U admin --pass-stdin config write
QBITCTL_PASSWORD="$QBT_PASS" qbitctl -u http://localhost:8080 -U admin config write -o ./config.json
```

## Commands

Run `qbitctl help <command>` to see its flags and any aliases.

### Global flags

```text
-c, --config <path>   Alternate config file path
-u, --url <url>       qBittorrent WebUI URL
-U, --user <user>     qBittorrent username
-p, --pass <pass>     qBittorrent password (visible in process arguments)
    --pass-stdin      Read the qBittorrent password from standard input
-v, --version         Show version
-h, --help            Show help
```

### View commands

#### tui

`qbitctl tui` opens the workspace in an interactive terminal using the same
config as the other commands. The Config tab guides you through first-time
setup and lets you change the server address or credentials later. A blank
password field keeps the saved password for the same server. Enter a password
when switching to a different server. The Config tab shows whether a password
is saved and masks new passwords as you type. The workspace opens immediately
and shows its progress while it connects. Press `Esc` during the connection to
cancel and open Config. If the connection fails, Config opens with the error.
The exception is when there is no usable saved config and the URL, username,
and password all came from `--url`, `--user`, and `-p` or `QBITCTL_PASSWORD`.
In that case the TUI exits with the error.

- Press `Tab` or `1`, `2`, `3`, and `4` to switch between Queue, Search,
  Server, and Config.
- Open **Color scheme** in Config to preview Default or one of 16 other
  palettes. Press `Enter` to save or `Esc` to return to the previous palette.
- Press `Ctrl+P` or `:` to open the command palette. Type to find a command,
  use the arrow keys to move, and press `Enter` to run it.
- Press `?` for the grouped keyboard shortcut reference. Use the arrow keys
  or Page Up and Page Down to scroll in smaller terminals. Press `Esc` to return.
- In Queue, `/` filters by name, hash, category, tags, or state. Use the arrows
  or `j` and `k` to move, `Enter` for details, and `Space` to mark torrents for
  batch actions. The palette includes start, pause, force-start, move, edit,
  trackers, remove, and delete. Removing torrents and deleting data require
  confirmation. Edit applies to marked torrents, or to the focused torrent
  when no rows are marked.
- Torrent details use a two-column layout with right-aligned labels, a visual
  divider, and left-aligned values. Long values wrap within the value column,
  and the view remains scrollable in smaller terminals.
- In Queue, press `o` or choose **Sort queue** in the palette to sort by name,
  progress, or state in either direction. Choose **Active first**
  to restore the default order. Press `s` to start selected torrents.
- The Queue uses incremental updates. It falls back to full refreshes for
  the rest of the session if the server cannot supply usable sync data.
- In Search, `/` opens the search form. It defaults to Jackett and 30 seconds.
  Press `Enter` on a result to open the add dialog, then `Enter` to submit it.
  Press `Esc` to cancel a running search.
- Press `s` to sort by seeders or size in either direction, or restore search
  order. Press `f` to filter by text (title, source, or ID), minimum seeders,
  or size. Use `Tab` to move between fields. Sizes accept bytes or units
  such as `500 MB` and `2 GiB`. Leave either size bound blank for an open-ended
  range. The results update as you type. `Enter` applies the filters and `Esc`
  discards edits. Press `Esc` on the results list to clear the applied filters.
  Text filters apply to the current search.
- Footer confirmations, hints, and errors clear after about five seconds. A
  newer message replaces the current one and receives its own timeout, while
  active progress indicators continue until the operation finishes.
- Press `a` to add a torrent file, magnet link, or URL. The add form offers a category, a destination on the qBittorrent host, and an option to add it stopped. Press `r` to refresh and `q` to quit.

The TUI saves preferences in the active config file, including when you use
`--config` to select a different file.

```json
{
  "ui": {
    "tui_default": false,
    "theme": "default",
    "queue": { "sort": "progress-desc" },
    "search": {
      "sort": "seeders-desc",
      "min_seeders": "10",
      "min_size": "500 MB",
      "max_size": "4 GiB"
    }
  }
}
```

On the first TUI launch with the default config or its `./config.json`
fallback, qbitctl imports missing preferences from `tui-queue.json` and
`tui-search.json`. It removes each old file after its settings have been
saved to `config.json`.

#### tui-default

Run `qbitctl tui-default on` to open the TUI when you run bare `qbitctl`
in an interactive terminal. Run `qbitctl tui-default off` to show CLI help
once configured, or `qbitctl tui-default status` to read the setting.
You can also change it in the TUI's Config tab. Explicit subcommands run
directly when credentials are configured. On first use in a terminal,
qbitctl offers TUI or text setup. Text setup resumes the requested command.
TUI setup opens the workspace once the connection succeeds.

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

#### search

Search through qBittorrent's installed search plugins. The default plugin is
`jackett`, and qbitctl waits 30 seconds for the search to finish by default. Use
`--timeout` with a Go duration such as `2m` to adjust the limit. At the limit,
qbitctl returns the results found so far and prints a notice to stderr. With
`--json`, stdout is still an array, including `[]` when there are no results.
qbitctl attempts to delete its search job before logging out, including after Ctrl-C.
Each result includes the name, size, seeder and leecher counts, and source
hostname. Sizes are in bytes in JSON and piped output. Downloadable results
also include an ID in the table and in the JSON `id` field. Use that ID with
`qbitctl add --search-id`.

qbitctl keeps download links in a private local cache. IDs remain available for up to 24 hours for the same qBittorrent URL and user. The cache keeps the most recent eight searches per profile, with a limit of 64 searches across profiles.

```bash
qbitctl search "Ubuntu 24.04"
qbitctl search "Ubuntu 24.04" --plugin jackett --timeout 2m
qbitctl search "Ubuntu 24.04" --json
qbitctl search "Ubuntu 24.04" --sort seeders-desc --min-seeders 10 --limit 5
qbitctl search "Ubuntu 24.04" --filter "desktop tracker.example" --min-size 500MB --max-size 2GiB
```

Search results can be ordered with `--sort` (`original`, `seeders-desc`,
`seeders-asc`, `size-desc`, or `size-asc`) and narrowed with `--filter`,
`--min-seeders`, `--min-size`, and `--max-size`. The filter uses
whitespace-separated terms that must match the result name, source, or ID.
Size bounds accept bytes or a unit. `KB`, `MB`, `GB`, `TB`, and `PB` are
decimal, and `KiB`, `MiB`, `GiB`, `TiB`, and `PiB` are binary.
`--limit` applies after filtering and sorting; `0` (the default) shows every
matching result. These options apply to `--json` output too. Every result is
still cached, so an ID from an earlier unfiltered run still works with
`add --search-id`. Piped output keeps the tab-separated byte-size format for
scripts, while an interactive terminal uses aligned columns and binary units
such as `1.4 GiB`.

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

Run a search, then pass the ID of the result you want to download.

```bash
qbitctl search "Ubuntu 24.04"
qbitctl add --search-id 123456789012
```

```bash
qbitctl add ./ubuntu.torrent
qbitctl add 'magnet:?xt=urn:btih:...'
qbitctl add ./ubuntu.torrent --category Linux --save-path /downloads/linux --stopped
```

The category, save path, and stopped options apply to direct file, URL, or magnet adds. Use a save path on the qBittorrent server. Local uploads accept regular files up to 63 MiB. Search result IDs use the server's plugin or default add behavior.

#### set (alias: se)

Update one torrent property:

```bash
qbitctl set category Linux 9c328901
qbitctl set tags Linux,ISO 9c328901
qbitctl set up-limit 1024 9c328901
qbitctl set dl-limit 2048 9c328901
qbitctl set ratio-limit 2.0 9c328901
qbitctl set seedtime-limit 3600 9c328901
qbitctl set seedtime-limit 3d 9c328901
qbitctl set -- seedtime-limit -1 9c328901
qbitctl set seqdl true 9c328901
qbitctl set autotmm false 9c328901
qbitctl set superseed true 9c328901
```

Available fields: `autotmm`, `category`, `dl-limit`, `ratio-limit`, `seedtime-limit`, `seqdl`, `superseed`, `tags`, `up-limit`

Input formats:

```text
up-limit / dl-limit                Bytes/sec, or "k" suffix for KiB (e.g. 512k)
ratio-limit                        Floating point (e.g. 2.0)
seedtime-limit                     Duration (90m, 2h, 3d, 7d12h) or seconds; -1 unlimited, -2 global limit
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
qbitctl config write           # non-interactive (requires a supplied password)
```

### Additional commands

```bash
qbitctl update                 # download and install latest release (aliases: upd, up, upgrade)
qbitctl update -d              # check for updates without installing
qbitctl version                # show version (aliases: v, ve, ver)
qbitctl completion bash        # generate shell completions (alias: com)
qbitctl help                   # help (aliases: h, he)
```

The help view groups commands and flags by section. In a color-capable terminal,
section headings, command names, and descriptions use separate colors. Set
`NO_COLOR=1` for the same help content without styling.

### Shell completion

Install the generated completion script for your shell. `qbitctl completion
powershell` is also available.

```bash
# bash (requires bash-completion)
mkdir -p ~/.local/share/bash-completion/completions
qbitctl completion bash > ~/.local/share/bash-completion/completions/qbitctl

# zsh: also add `fpath=(~/.zfunc $fpath)` before `compinit` in ~/.zshrc
mkdir -p ~/.zfunc
qbitctl completion zsh > ~/.zfunc/_qbitctl

# fish
mkdir -p ~/.config/fish/completions
qbitctl completion fish > ~/.config/fish/completions/qbitctl.fish
```

Hash completion connects to the server with the active configuration,
including `--config`, `--url`, `--user`, and `QBITCTL_PASSWORD`. It never
prompts or reads stdin, and gives up after three seconds. Completions for
commands, field names, set values, and `.torrent` files for `add` work
without a server connection.

### Output format flags

These flags apply to `info`, `list`, `show`, and `get` as indicated.
`search` also supports `--json` (`-j`). Choose one output format per
invocation. `--no-header` requires `--fields`.

| Flag | Short | Description |
|------|-------|-------------|
| `--json` | `-j` | Output the normal display data as JSON |
| `--server-json` | `-J` | Output the raw qBittorrent API JSON response |
| `--template` | `-t` | Render output with a Go template (list and show only) |
| `--fields` | | Render selected fields as tab-separated rows (list and show only) |
| `--no-header` | | Omit the header row when using `--fields` |

## Torrent hashes

Single-torrent commands accept the hash as a positional argument. Full and short hashes must match a torrent on the server. Short hash prefixes (8+ characters recommended) are resolved automatically as long as they are unambiguous:

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
package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"time"

	"github.com/naterator/qbitctl/pkg/client"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// Set a deadline for login and subsequent requests.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	app, err := client.NewClientContext(ctx, &client.Options{
		URL:  "http://localhost:8080",
		User: "admin",
		Pass: "secret",
	})
	if err != nil {
		return err
	}

	// Release the Web UI session, even if the request context has expired.
	defer app.Logout()

	// Configure I/O (defaults to os.Stdout/os.Stderr)
	app.Stdout = &bytes.Buffer{}
	app.Stderr = io.Discard

	// Use the API
	hash, err := app.ResolveHash("9c328901")
	if err != nil {
		return err
	}
	if err := app.ShowSingleTorrentInfo(hash); err != nil {
		return err
	}
	if err := app.SetField(hash, "category", "linux"); err != nil {
		return err
	}
	return app.StartTorrent(hash)
}
```

Key library types:

- `client.App` — authenticated qBittorrent client with configurable I/O
- `client.Options` — connection and authentication options
- `client.TorrentInfo` — torrent metadata (JSON-mapped)
- `client.TransferInfo` — server transfer statistics
- `client.CodedError` — error with exit code for CLI integration
- `client.HTTPError` — HTTP status and a bounded server error excerpt
- `client.HiddenInputFunc` — password echo control for interactive config

## Test

```bash
go test ./...          # All Go tests
go test ./pkg/client/  # Client library tests
go test ./cmd/qbitctl  # CLI tests
make test-race         # Go tests with the race detector
make smoke             # Native CLI and PTY smoke tests with Python 3
make ci                # The same native checks used in GitHub Actions
```

The compatibility test runs a disposable qBittorrent container and needs
Docker. CI runs it against 5.0.0 and 5.3.0 using the digest-pinned images in
[ci.yml](.github/workflows/ci.yml).

```bash
compat_dir=$(mktemp -d)
go build -trimpath -o "$compat_dir/qbitctl" ./cmd/qbitctl
python3 scripts/integration_qbittorrent.py --binary "$compat_dir/qbitctl" \
  --image 'qbittorrentofficial/qbittorrent-nox:5.3.0-2@sha256:43b2d352cf76330a7ca34c7320b9b74227d6f31e662e0e123d0457206b09a7ea' \
  --version 5.3.0
```

## License

This project is licensed under the **BSD 3-Clause License**. See [LICENSE](./LICENSE).
