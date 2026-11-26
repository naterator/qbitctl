# <img src="assets/logo.svg" alt="qbitctl" width="418">

Control qBittorrent from your terminal. qbitctl has an interactive TUI, a CLI
for scripts, and a Go client library. It works with qBittorrent 5.0 or later.

## Install

On macOS, Linux, or a supported BSD, run this in your terminal.

```bash
curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | bash
```

On Windows, download `install.ps1`, sign in with `gh auth login`, and run
the script in PowerShell 7 or later. It verifies the release ZIP before
installing to `%LOCALAPPDATA%\qbitctl`. Use `-AddToPath` to add that directory
to your user PATH. [Download install.ps1](https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.ps1)

```powershell
.\install.ps1 -AddToPath
```

## Quick start

Open the TUI and enter your qBittorrent Web UI URL, username, and password when
prompted. To set up a named connection from the CLI, run `qbitctl profile add NAME`.

```bash
qbitctl tui
```

Once you're connected, you can use the same settings from the CLI.

```bash
qbitctl list
qbitctl info
qbitctl search "Ubuntu 24.04"
qbitctl add ./ubuntu.torrent
```

## TUI walkthrough

Use `Tab` to switch between Queue, Search, Server, and Config. Press `?` for
shortcuts or `Ctrl+P` for actions that fit the current tab and selection.
Mark torrents with `Space` to edit them together. Press Enter in the Queue to
open live torrent details. Use `2` for Trackers, `3` for Files, and `4` for
Peers. Press `t` in the Queue to open Trackers directly. In Trackers, `a`
adds, `e` edits, `d` removes, and `v` reveals full URLs. In Files, `F2`
renames the selected file and `F3` renames its folder. Press `o`
in the queue to sort by size, ratio, ETA, or either transfer speed. The
queue detail pane shows connected and swarm counts when the server reports them.
The queue refreshes every five seconds while transferring and every 15
seconds when idle. The command palette can pause automatic refresh, and
`r` still refreshes on demand. Use the queue filters to select a state,
category, or tag. Copy a selected torrent's hash or magnet link from the
command palette when your terminal supports OSC 52. Set
`qbitctl config tui-default set true` to open the TUI when you run `qbitctl` by itself.

The Server tab shows global limits and the active speed mode. Switch modes
to edit the other preset. The Peers view can ban a selected peer after you
confirm it.

Config groups settings under **Connection**, **View**, and **Search**, with a
**Files** section for the config and search cache paths. Use
↑/↓ or `j`/`k` to move and Enter to change the selected setting. Under View,
**Data units** switches between human-readable sizes and speeds (the
default) and exact bytes and bytes per second. qbitctl saves the choice for
both the TUI and CLI. **Color scheme** applies to the TUI and CLI terminal
output across all server profiles in the same config file. Both start with
**Default**. The CLI honors `NO_COLOR`, and JSON and other structured output
stay plain. View also has options for extra queue columns and a fixed
refresh interval.

On the Search tab, the query and current provider are just below the tabs.
Press `p` (**Providers**) to choose **Enabled search plugins** (the default),
**All search plugins**, or an individual enabled plugin. Disabled plugins appear
grayed out at the bottom of the picker. The choice is saved in `config.json`
and used for TUI and CLI searches. If a saved plugin is no longer available or
enabled, qbitctl uses **Enabled search plugins**. Press `m` in the picker to
manage the server's plugins. Press `/` to start a search.

![qbitctl walkthrough showing Config, torrent details with Trackers, and the search provider picker](assets/demo.gif)

[Terminal recording](assets/demo.cast) · [Recording script](scripts/record_demo.py)

## Commands

Run `qbitctl --help` for the command list or `qbitctl help <command>` for its
flags and aliases. Commands that take a torrent hash also accept an
unambiguous prefix.

| Command | What it does |
| --- | --- |
| `tui` | Open the interactive terminal UI. |
| `info`, `list`, `show`, `get` | Read server stats, list torrents, or inspect a specific torrent. |
| `files`, `peers`, `trackers` | Inspect torrent contents and swarm connections, set file priorities, or manage trackers. |
| `search`, `add` | Search installed plugins or add a file, magnet, URL, or search result. |
| `transfer` | Inspect speed limits, edit the active limits, or switch between normal and alternate mode. |
| `categories`, `tags` | Manage server categories and tags or change torrent tag assignments. |
| `set`, `move` | Change torrent settings or move data on the server. |
| `start`, `pause`, `force` | Control one or more torrents. |
| `recheck`, `reannounce`, `rename`, `queue` | Verify data, refresh tracker announcements, rename a torrent, or change its place in the queue. |
| `remove`, `delete` | Remove a torrent. `delete` also removes its files. |
| `config` | Inspect and change saved preferences. |
| `profile` | Set up and manage server connections. |
| `update`, `version`, `completion` | Update qbitctl, show its version, or generate shell completions. |

```bash
qbitctl list --state downloading --category linux --fields hash,name,progress
qbitctl search "Ubuntu 24.04" --sort seeders-desc --min-seeders 10 --limit 5
qbitctl add --search-id 123456789012 --category linux --stopped
qbitctl set seedtime-limit 3d 9c328901 fedcba98
qbitctl files 9c328901
qbitctl files priority 9c328901 skip 2 3
qbitctl queue top 9c328901 fedcba98
qbitctl pause 9c328901 fedcba98
qbitctl transfer mode alternate
qbitctl transfer set download 524288
qbitctl categories create linux /downloads/linux --download-path /incomplete
qbitctl tags add linux 9c328901
qbitctl completion bash
```

Use `qbitctl batch commands.txt` to run one command per line with a shared
login. Pipe the commands into `qbitctl batch` to read them from stdin.
A batch stops on the first error unless you pass `--continue-on-error`.

Use `set category <name> <hash>` to assign a category to a torrent.
`tags remove <tag> <hash>` detaches a tag from a torrent. `tags delete <tag>`
deletes it from the server catalog.

Use `qbitctl list --fields hash-full,name` to export full torrent hashes.
The existing `hash` field uses an eight-character prefix.

When both stdin and stdout are terminals, `qbitctl delete` previews the
targets on stderr and asks you to type `DELETE`. Use `--yes` to skip the
prompt. Redirected invocations and batch commands continue without a prompt.

Use `qbitctl show <hash> --fields name,seeds,leechers,queue-position`
for a compact view. Fields the server omits are blank. `dl-path` and its
existing alias `download-path` show the content path. Use
`temp-download-path` for qBittorrent's separate download directory.

Use `trackers <hash>` for hostnames and status, or `--full-url` when you need
the exact announce URL. The `add`, `edit`, and `remove` tracker subcommands
take exact URLs. The `set` fields for share-limit policy are
`inactive-seedtime-limit`, `share-limit-action` (qBittorrent 5.2+), and
`share-limits-mode` (5.3+).

Search uses your saved provider and waits up to 30 seconds by default.
Run `qbitctl search --list-plugins` to see what your server has
installed. Use `--plugin` with a name or ID, join several with `|` in a quoted
value, or use `--plugin all` to include disabled plugins. Change the wait with
`--timeout`.

Run `qbitctl search plugins list` or use `install <https-url>`,
`enable <id>`, `disable <id>`, `uninstall <id>`, and `update` under
`qbitctl search plugins`. Disabling your default plugin temporarily falls
back to the enabled group. Re-enabling it restores the saved choice.

Copy the ID from a search result into `add --search-id`. Run
`qbitctl search --recent` to print the latest cached results without
connecting. IDs last up to 24 hours for the same server and user.
Add options include `--category`, `--save-path`, `--tags`, and `--stopped`.
Run `qbitctl add --help` or press `F2` in the TUI add form for
content layout, a separate download path, stop conditions, piece priorities,
and share limits. These overrides require qBittorrent 5.2 or later for
plugin-downloaded search results.

Use `--json` for structured output or `--server-json` for the raw API response
where offered. `list` and `show` also support `--fields` and `--template`.
Use one output format at a time.

Use `--units bytes` or `--units human` to override the saved display units
for one command. JSON and template output keep their original values.
Use `--http-timeout 90s` to override the per-request budget, or set
`"http_timeout": "90s"` in `config.json`. Search jobs keep their own
`search --timeout` budget.

```bash
qbitctl --units bytes info
qbitctl --units human search "Ubuntu 24.04"
```

## Authentication

`qbitctl profile add NAME` tests the connection before saving it. The usual config is
`~/.config/qbitctl/config.json`. Set `XDG_CONFIG_HOME` to change its base path
or use `--config` to pick a file. qbitctl falls back to `./config.json` when
it can't find the default file.

Saved passwords are encrypted with a key in your OS keyring. On macOS that
key lives in your default Keychain. Over SSH, qbitctl can use your unlocked
login Keychain while your macOS login session is active. For a headless setup,
supply the same `QBITCTL_MASTER_KEY` when saving the config and on later runs.

Use `qbitctl profile list` to see saved connections, `--profile NAME` to pick
one for a command, and `qbitctl profile use NAME` to set the startup default.
Config has separate **Active profile** and **Startup profile** rows. Each
profile has its own search provider, and view preferences apply across
profiles.

To use a different server or account, pass `--url` and `--user` with an
explicit password. URLs can include a reverse-proxy path prefix. For scripts,
pass the password on stdin or set `QBITCTL_PASSWORD` for that invocation.
Use `profile write` to save connection settings without a login check.

```bash
printf '%s\n' "$QBT_PASS" | qbitctl -u https://nas.example/qbt/ -U admin --pass-stdin list
```

For a private certificate authority, add `--ca-file /path/to/ca.pem` to the
command. Save it with `profile add --ca-file`, `profile write`, or in the connection settings in the
TUI. qbitctl adds the bundle to system trust and checks the server hostname.

## Saved preferences

`qbitctl config` lists the available settings. A setting name by itself shows
its commands. Use `get` to print the current value, `set` to save a value, and
`list` to see the choices for settings that offer them.

```bash
qbitctl config tui-default set true
qbitctl config color-scheme list
qbitctl config color-scheme set tookie
qbitctl config data-units get
qbitctl config refresh-interval set adaptive
qbitctl config default-search-plugin list
```

The default search provider is saved per profile. Its list shows enabled and
disabled server plugins in the TUI's order. Set an enabled plugin by ID or
use `enabled` or `all`. The color-scheme list shows the colors for each
named palette.

```bash
printf '%s\n' "$QBT_PASS" | qbitctl profile add example --url https://nas.example/qbt/ --user admin --password -
```

Run `qbitctl profile add` to see its options. Run
`qbitctl profile setup NAME` to re-enter an existing connection's credentials.

## Updates and install options

```bash
qbitctl update
qbitctl update --dry-run
```

The installer checks the published SHA-256 checksum and picks the smallest
archive it can unpack. It normally installs into `~/.local/bin` and adds that
directory to your shell's PATH if needed. If you can write to `/usr/local/bin`,
it installs there. Use the same privilege level when updating. The updater
also checks GitHub's signed release attestation before replacing the binary.

| Installer variable | What it changes |
| --- | --- |
| `QBITCTL_VERSION` | Install a specific release. |
| `QBITCTL_INSTALL_DIR` | Choose where to put the binary. |
| `QBITCTL_NO_MODIFY_PATH=1` | Leave shell startup files alone. |
| `QBITCTL_DOWNLOAD_FORMAT` | Pick `xz`, `gz`, or `raw`. |

The Linux 32-bit ARM binary requires ARMv7. For other platforms and manual
downloads, check the assets and their `.sha256` files. [GitHub releases](https://github.com/naterator/qbitctl/releases/latest)

## Build from source

Use Go 1.27.4 or later.

```bash
go build -o qbitctl ./cmd/qbitctl
```

For Go projects, import `github.com/naterator/qbitctl/pkg/client`. Use
`NewClientContext` to connect and call `Logout` when you're done. The client
takes its output writers and request context from the caller. [Client examples](pkg/client/example_test.go)

### Test

```bash
make ci
make cover
```

The real-server compatibility tests use Docker. See
[CI](.github/workflows/ci.yml) for the qBittorrent versions and commands.

On macOS, you can also test the login Keychain directly.

```bash
QBITCTL_TEST_KEYCHAIN=1 go test ./pkg/client -run '^TestMacMasterSecretKeychain$' -count=1
```

Mutation testing is a manual quarterly check because it edits a disposable
copy and can take substantially longer than the normal test suite. Run it
from a committed checkout with:

```bash
bash scripts/run_mutation_testing.sh
```

The pinned Gremlins v0.6.0 run covers the client setters, validation, and
release-attestation verifier. Its JSON report and log are written under
`mutation-results/`; inspect every `LIVED` mutant before changing tests or
production code.

### Cut a release

Tag the exact commit that passed CI on `main` with a signed, annotated tag.
The [release workflow](.github/workflows/release.yml) builds the binaries and
archives, tests them, and attaches them to a draft. Review the draft and
publish it with release immutability enabled so GitHub signs the asset
attestation.

## License

[BSD 3-Clause](LICENSE)
