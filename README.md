# <img src="assets/logo.svg" alt="qbitctl" width="418">

Control qBittorrent from your terminal. qbitctl has an interactive TUI, a CLI
for scripts, and a Go client library. It works with qBittorrent 5.0 or later.

## Install

On macOS, Linux, or a supported BSD, run this in your terminal.

```bash
curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | bash
```

On Windows, download the `.exe` for your architecture and put it on your
PATH. Manual downloads are available for every build. [GitHub releases](https://github.com/naterator/qbitctl/releases/latest)

## Quick start

Open the TUI and enter your qBittorrent Web UI URL, username, and password when
prompted. For text setup, use `qbitctl config`.

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
shortcuts or `Ctrl+P` for the command palette. Mark torrents with `Space` to
edit them together. Press Enter in the Queue to open live torrent details.
The details and the queue refresh every five seconds. Set
`qbitctl tui-default on` to open the TUI when you run `qbitctl` by itself.

On the Search tab, the query and current provider are just below the tabs.
Press `p` (**Providers**) to choose **Enabled search plugins** (the default),
**All search plugins**, or an individual enabled plugin. Disabled plugins appear
grayed out at the bottom of the picker. The choice is saved in `config.json`
and used for TUI and CLI searches. If a saved plugin is no longer available or
enabled, qbitctl uses **Enabled search plugins**. Press `/` to start a search.

![qbitctl TUI showing setup, the search provider picker, and torrent controls](assets/demo.gif)

[Terminal recording](assets/demo.cast) · [Recording script](scripts/record_demo.py)

## Commands

Run `qbitctl --help` for the command list or `qbitctl help <command>` for its
flags and aliases. Commands that take a torrent hash also accept an
unambiguous prefix.

| Command | What it does |
| --- | --- |
| `tui` | Open the interactive terminal UI. |
| `info`, `list`, `show`, `get` | Read server stats, list torrents, or inspect a specific torrent. |
| `search`, `add` | Search installed plugins or add a file, magnet, URL, or search result. |
| `set`, `move` | Change torrent settings or move data on the server. |
| `start`, `pause`, `force-start` | Control one or more torrents. |
| `remove`, `delete` | Remove a torrent. `delete` also removes its files. |
| `config`, `tui-default` | Set up the connection or change what bare `qbitctl` opens. |
| `update`, `version`, `completion` | Update qbitctl, show its version, or generate shell completions. |

```bash
qbitctl list --fields hash,name,progress
qbitctl search "Ubuntu 24.04" --sort seeders-desc --min-seeders 10 --limit 5
qbitctl add --search-id 123456789012
qbitctl set seedtime-limit 3d 9c328901
qbitctl pause 9c328901 fedcba98
qbitctl completion bash
```

Search uses your saved provider and waits up to 30 seconds by default.
Run `qbitctl search --list-plugins` to see what your server has
installed. Use `--plugin` with a name or ID, join several with `|` in a quoted
value, or use `--plugin all` to include disabled plugins. Change the wait with
`--timeout`.

Copy the ID from a search result into `add --search-id`. IDs last up to 24
hours for the same server and user.

Use `--json` for structured output or `--server-json` for the raw API response
where offered. `list` and `show` also support `--fields` and `--template`.
Use one output format at a time.

## Authentication

`qbitctl config` tests the connection before saving it. The usual config is
`~/.config/qbitctl/config.json`. Set `XDG_CONFIG_HOME` to change its base path
or use `--config` to pick a file. qbitctl falls back to `./config.json` when
it can't find the default file.

Saved passwords are encrypted with a key in your OS keyring. On macOS that
key lives in your default Keychain. Over SSH, qbitctl can use your unlocked
login Keychain while your macOS login session is active. For a headless setup,
supply the same `QBITCTL_MASTER_KEY` when saving the config and on later runs.

To use a different server or account, pass `--url` and `--user` with an
explicit password. URLs can include a reverse-proxy path prefix. For scripts,
pass the password on stdin or set `QBITCTL_PASSWORD` for that invocation.
Use `config write` to save settings from a script.

```bash
printf '%s\n' "$QBT_PASS" | qbitctl -u https://nas.example/qbt/ -U admin --pass-stdin list
```

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
