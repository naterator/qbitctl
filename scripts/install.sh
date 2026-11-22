#!/usr/bin/env bash
# Install the latest qbitctl release:
#
#   curl -fsSL https://raw.githubusercontent.com/naterator/qbitctl/main/scripts/install.sh | bash
#
# The binary goes to /usr/local/bin when running as root or when that
# directory is already writable, and to ~/.local/bin otherwise. A per-user
# install adds its directory to PATH in the shell's startup file. The installer
# never runs sudo itself; pipe it to `sudo bash` for a system-wide install.
#
# Environment:
#   QBITCTL_VERSION         Release tag to install, such as 1.3.1 (default: latest)
#   QBITCTL_INSTALL_DIR     Directory for the binary (overrides the default)
#   QBITCTL_NO_MODIFY_PATH  Set to 1 to leave shell startup files unchanged
#   QBITCTL_DOWNLOAD_FORMAT Force xz, gz, or raw (default: smallest available)
#   QBITCTL_RELEASES_URL    Release base URL, for mirrors and tests
#
# Everything runs inside main, called on the last line, so a truncated
# download cannot execute a partial script.
set -euo pipefail

path_marker='# Added by the qbitctl installer'

say() {
  printf 'qbitctl-install: %s\n' "$*"
}

die() {
  printf 'qbitctl-install: error: %s\n' "$*" >&2
  exit 1
}

detect_os() {
  local os
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    darwin|dragonfly|freebsd|linux|netbsd|openbsd) printf '%s\n' "$os" ;;
    *) die "unsupported operating system: $os. Download a release asset from https://github.com/naterator/qbitctl/releases" ;;
  esac
}

detect_arch() {
  local machine
  machine=$(uname -m)
  case "$machine" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    armv7|armv7l) printf 'arm\n' ;;
    *) die "unsupported architecture: $machine. Download a release asset from https://github.com/naterator/qbitctl/releases" ;;
  esac
}

release_download_url() {
  local releases=${QBITCTL_RELEASES_URL:-https://github.com/naterator/qbitctl/releases}
  releases=${releases%/}
  local version=${QBITCTL_VERSION:-}
  if [[ -z "$version" ]]; then
    printf '%s/latest/download\n' "$releases"
    return
  fi
  if [[ ! "$version" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    die "QBITCTL_VERSION must look like 1.2.3 or v1.2.3, got: $version"
  fi
  printf '%s/download/%s\n' "$releases" "$version"
}

choose_install_dir() {
  # QBITCTL_SYSTEM_BIN_DIR exists only so tests can stand in for /usr/local/bin.
  local system_dir=${QBITCTL_SYSTEM_BIN_DIR:-/usr/local/bin}
  if [[ -n "${QBITCTL_INSTALL_DIR:-}" ]]; then
    printf '%s\n' "${QBITCTL_INSTALL_DIR%/}"
  elif [[ "$(id -u)" == 0 ]]; then
    printf '%s\n' "$system_dir"
  elif [[ -d "$system_dir" && -w "$system_dir" ]]; then
    printf '%s\n' "$system_dir"
  else
    [[ -n "${HOME:-}" ]] || die "HOME is not set; set QBITCTL_INSTALL_DIR"
    printf '%s/.local/bin\n' "$HOME"
  fi
}

# Print the download formats to try, smallest first. Releases publish .xz only
# for systems whose base install ships xz, .gz for every non-Windows system,
# and the raw binary always.
download_formats() {
  local os=$1
  case "${QBITCTL_DOWNLOAD_FORMAT:-}" in
    "") ;;
    xz|gz|raw)
      local tool=${QBITCTL_DOWNLOAD_FORMAT/gz/gzip}
      if [[ "$tool" != raw ]] && ! command -v "$tool" >/dev/null 2>&1; then
        die "QBITCTL_DOWNLOAD_FORMAT=$QBITCTL_DOWNLOAD_FORMAT needs $tool, which is not installed"
      fi
      printf '%s\n' "$QBITCTL_DOWNLOAD_FORMAT"
      return
      ;;
    *) die "QBITCTL_DOWNLOAD_FORMAT must be xz, gz, or raw, got: $QBITCTL_DOWNLOAD_FORMAT" ;;
  esac
  case "$os" in
    linux|freebsd|netbsd|dragonfly)
      if command -v xz >/dev/null 2>&1; then printf 'xz\n'; fi
      ;;
  esac
  if command -v gzip >/dev/null 2>&1; then printf 'gz\n'; fi
  printf 'raw\n'
}

# Quiet on purpose: a missing compressed asset is expected for older
# releases, and the caller reports what happened.
fetch() {
  curl --fail --location --retry 3 --retry-delay 1 --silent \
    --output "$2" "$1"
}

verify_checksum() {
  local dir=$1 asset=$2 checksum_asset=$3
  # Check that the checksum file names exactly this asset before invoking the
  # platform checksum tool, so a malformed file cannot name another path.
  if ! LC_ALL=C awk -v asset="$asset" '
    NF == 0 { next }
    {
      records++
      if (NF == 2 && length($1) == 64 && $1 !~ /[^[:xdigit:]]/ && $2 == asset) {
        matching++
      }
    }
    END { exit records == 1 && matching == 1 ? 0 : 1 }
  ' "$dir/$checksum_asset"; then
    die "checksum file does not name $asset"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$dir" && sha256sum -c "$checksum_asset" >/dev/null 2>&1) || die "checksum mismatch for $asset"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$dir" && shasum -a 256 -c "$checksum_asset" >/dev/null 2>&1) || die "checksum mismatch for $asset"
  else
    die "neither sha256sum nor shasum is available to verify the download"
  fi
}

install_binary() {
  local source=$1 dir=$2
  mkdir -p "$dir" 2>/dev/null || true
  if [[ ! -d "$dir" || ! -w "$dir" ]]; then
    die "cannot write to $dir. Re-run with sudo, or set QBITCTL_INSTALL_DIR to a writable directory"
  fi
  # Copy beside the target and rename, so a running qbitctl is replaced
  # atomically and an interrupted install never leaves a partial binary.
  local staged="$dir/.qbitctl-install.$$"
  cp "$source" "$staged"
  chmod 0755 "$staged"
  mv -f "$staged" "$dir/qbitctl"
}

startup_file() {
  local shell_name
  shell_name=$(basename "${SHELL:-sh}")
  case "$shell_name" in
    zsh) printf '%s/.zshrc\n' "${ZDOTDIR:-$HOME}" ;;
    bash)
      if [[ "$(uname -s)" == Darwin ]]; then
        printf '%s/.bash_profile\n' "$HOME"
      else
        printf '%s/.bashrc\n' "$HOME"
      fi
      ;;
    fish) printf '%s/fish/conf.d/qbitctl.fish\n' "${XDG_CONFIG_HOME:-$HOME/.config}" ;;
    *) printf '%s/.profile\n' "$HOME" ;;
  esac
}

ensure_on_path() {
  local dir=$1
  case ":${PATH:-}:" in
    *":$dir:"*) return ;;
  esac

  local export_hint="export PATH=\"$dir:\$PATH\""
  if [[ "${QBITCTL_NO_MODIFY_PATH:-}" == 1 || "$(id -u)" == 0 ]]; then
    say "$dir is not on PATH. Add it with: $export_hint"
    return
  fi
  if [[ "$dir" == *[\"\$\`\\]* ]]; then
    say "$dir is not on PATH and contains characters this installer will not write to a startup file. Add it yourself."
    return
  fi

  # Prefer $HOME in the written line so the startup file stays portable.
  local shown=$dir
  if [[ "$dir" == "$HOME"/* ]]; then
    shown="\$HOME/${dir#"$HOME"/}"
  fi

  local file line
  file=$(startup_file)
  if [[ "$file" == */fish/conf.d/qbitctl.fish ]]; then
    line="fish_add_path -g \"$shown\""
  else
    line="export PATH=\"$shown:\$PATH\""
  fi

  if [[ -f "$file" ]] && grep -qF "$path_marker" "$file"; then
    say "$file already adds the qbitctl directory to PATH"
  else
    mkdir -p "$(dirname "$file")"
    printf '\n%s\n%s\n' "$path_marker" "$line" >> "$file"
    say "added $dir to PATH in $file"
  fi
  say "open a new terminal, or run: $export_hint"
}

warn_if_shadowed() {
  local installed=$1 found
  found=$(command -v qbitctl 2>/dev/null || true)
  if [[ -n "$found" && "$found" != "$installed" ]]; then
    say "warning: $found comes first on PATH and is a different qbitctl. Remove it or reorder PATH."
  fi
}

main() {
  command -v curl >/dev/null 2>&1 || die "curl is required"

  local os arch asset base dir
  os=$(detect_os)
  arch=$(detect_arch)
  asset="qbitctl-$os-$arch"
  base=$(release_download_url)
  dir=$(choose_install_dir)

  local tmp_dir
  tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/qbitctl-install.XXXXXX")
  # shellcheck disable=SC2064 # Expand now: tmp_dir is local to main.
  trap "rm -rf '$tmp_dir'" EXIT

  # Try the smallest format this machine can unpack first. A release from
  # before compressed assets falls through to the raw binary.
  local formats format download="" last_error=""
  # A separate assignment lets set -e stop on an invalid forced format.
  formats=$(download_formats "$os")
  for format in $formats; do
    download=$asset
    [[ "$format" == raw ]] || download="$asset.$format"
    say "downloading $download"
    if fetch "$base/$download" "$tmp_dir/$download" &&
      fetch "$base/$download.sha256" "$tmp_dir/$download.sha256"; then
      break
    fi
    last_error="could not download $base/$download"
    [[ "$format" == raw ]] || say "$download is not available; trying the next format"
    rm -f "$tmp_dir/$download" "$tmp_dir/$download.sha256"
    download=""
  done
  [[ -n "$download" ]] || die "$last_error"
  verify_checksum "$tmp_dir" "$download" "$download.sha256"

  # Unpack only after the checksum of the downloaded file has been verified.
  local binary="$tmp_dir/qbitctl.unpacked"
  case "$format" in
    xz) xz -dc "$tmp_dir/$download" > "$binary" || die "could not decompress $download" ;;
    gz) gzip -dc "$tmp_dir/$download" > "$binary" || die "could not decompress $download" ;;
    raw) binary="$tmp_dir/$download" ;;
  esac
  [[ -s "$binary" ]] || die "$download unpacked to an empty file"

  install_binary "$binary" "$dir"
  say "installed $asset to $dir/qbitctl"
  ensure_on_path "$dir"
  warn_if_shadowed "$dir/qbitctl"
}

main "$@"
