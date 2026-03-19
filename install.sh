#!/bin/sh

set -eu

owner="Intina47"
repo="jot"
project="jot"

bin_dir="${INSTALL_DIR:-}"
version="${JOT_VERSION:-}"

usage() {
  cat <<'EOF'
Install jot from GitHub Releases.

Usage:
  install.sh [-b BIN_DIR] [-v VERSION]

Options:
  -b, --bin-dir   Install directory for the jot binary.
  -v, --version   Release version to install, for example 1.5.5.
  -h, --help      Show this help.

Environment:
  INSTALL_DIR     Same as --bin-dir.
  JOT_VERSION     Same as --version.
EOF
}

fail() {
  printf '%s\n' "error: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

expand_home() {
  case "$1" in
    "~")
      printf '%s\n' "$HOME"
      ;;
    "~/"*)
      printf '%s/%s\n' "$HOME" "${1#~/}"
      ;;
    *)
      printf '%s\n' "$1"
      ;;
  esac
}

default_bin_dir() {
  if [ "$(id -u)" -eq 0 ]; then
    printf '%s\n' "/usr/local/bin"
    return
  fi

  for candidate in /usr/local/bin /opt/homebrew/bin; do
    if [ -d "$candidate" ] && [ -w "$candidate" ]; then
      printf '%s\n' "$candidate"
      return
    fi
  done

  printf '%s\n' "$HOME/.local/bin"
}

resolve_tag() {
  if [ -n "$version" ]; then
    case "$version" in
      v*)
        printf '%s\n' "$version"
        ;;
      *)
        printf 'v%s\n' "$version"
        ;;
    esac
    return
  fi

  resolved_url="$(
    curl -fsSIL -o /dev/null -w '%{url_effective}' \
      "https://github.com/$owner/$repo/releases/latest"
  )" || fail "could not resolve the latest jot release"

  tag="${resolved_url##*/}"
  [ -n "$tag" ] || fail "could not determine the latest jot release tag"
  printf '%s\n' "$tag"
}

detect_target() {
  os_name="$(uname -s)"
  arch_name="$(uname -m)"

  case "$os_name" in
    Darwin)
      os="darwin"
      ;;
    Linux)
      os="linux"
      ;;
    *)
      fail "unsupported operating system: $os_name"
      ;;
  esac

  case "$arch_name" in
    x86_64|amd64)
      arch="amd64"
      ;;
    arm64|aarch64)
      arch="arm64"
      ;;
    *)
      fail "unsupported architecture: $arch_name"
      ;;
  esac

  if [ "$os" = "linux" ] && [ "$arch" = "arm64" ]; then
    fail "Linux arm64 binaries are not published yet"
  fi

  printf '%s %s\n' "$os" "$arch"
}

while [ $# -gt 0 ]; do
  case "$1" in
    -b|--bin-dir)
      [ $# -ge 2 ] || fail "missing value for $1"
      bin_dir="$2"
      shift 2
      ;;
    -v|--version)
      [ $# -ge 2 ] || fail "missing value for $1"
      version="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

need_cmd curl
need_cmd tar
need_cmd uname
need_cmd mktemp
need_cmd chmod
need_cmd mkdir
need_cmd id

tag="$(resolve_tag)"
set -- $(detect_target)
os="$1"
arch="$2"

[ -n "$bin_dir" ] || bin_dir="$(default_bin_dir)"
bin_dir="$(expand_home "$bin_dir")"

asset_name="${project}_${tag}_${os}_${arch}.tar.gz"
download_url="https://github.com/$owner/$repo/releases/download/$tag/$asset_name"

tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d -t jot)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM HUP

archive_path="$tmp_dir/$asset_name"
binary_path="$tmp_dir/$project"
install_path="$bin_dir/$project"

printf '%s\n' "Downloading $download_url"
curl -fsSL --retry 3 --output "$archive_path" "$download_url" || fail "download failed"

tar -xzf "$archive_path" -C "$tmp_dir" || fail "archive extraction failed"
[ -f "$binary_path" ] || fail "archive did not contain $project"

mkdir -p "$bin_dir"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "$binary_path" "$install_path"
else
  cp "$binary_path" "$install_path"
  chmod 0755 "$install_path"
fi

printf '%s\n' "Installed $project ${tag#v} to $install_path"

case ":$PATH:" in
  *":$bin_dir:"*)
    ;;
  *)
    printf '%s\n' "$bin_dir is not in your PATH."
    printf '%s\n' "Add it, for example: export PATH=\"$bin_dir:\$PATH\""
    ;;
esac
