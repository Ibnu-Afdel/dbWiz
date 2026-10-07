#!/bin/sh
# Install DBWiz from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/Ibnu-Afdel/dbWiz/main/install.sh | sh
#
# Downloads the static binary for your CPU, verifies its SHA-256 against the
# release's checksums.txt, and installs it to ~/.local/bin (no sudo). Override
# with environment variables:
#
#   DBWIZ_VERSION=v1.1.0        install a specific release instead of the latest
#   DBWIZ_INSTALL_DIR=/usr/local/bin   install somewhere else (may need sudo)

set -eu

repo="Ibnu-Afdel/dbWiz"
version="${DBWIZ_VERSION:-latest}"
install_dir="${DBWIZ_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
fail() { printf 'dbwiz install: %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || fail "DBWiz runs on Linux only (this is $(uname -s))."

case "$(uname -m)" in
  x86_64 | amd64) arch="amd64" ;;
  aarch64 | arm64) arch="arm64" ;;
  *) fail "no prebuilt binary for $(uname -m) — build from source: go install github.com/Ibnu-Afdel/dbwiz@latest" ;;
esac

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  fail "need curl or wget"
fi
command -v sha256sum >/dev/null 2>&1 || fail "need sha256sum (coreutils)"

if [ "$version" = "latest" ]; then
  base="https://github.com/$repo/releases/latest/download"
else
  base="https://github.com/$repo/releases/download/$version"
fi
asset="dbwiz-linux-$arch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading $asset ($version)…"
fetch "$base/$asset" "$tmp/$asset" || fail "download failed: $base/$asset"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

say "Verifying checksum…"
(cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c - >/dev/null) ||
  fail "checksum mismatch — the download is corrupt or was tampered with; nothing was installed"

mkdir -p "$install_dir" || fail "can't create $install_dir"
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$install_dir/dbwiz" || fail "can't write to $install_dir (try DBWIZ_INSTALL_DIR or sudo)"

say "Installed $("$install_dir/dbwiz" --version) to $install_dir/dbwiz"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) say ""
     say "Note: $install_dir isn't on your PATH. Add it, e.g. for bash/zsh:"
     say "  echo 'export PATH=\"$install_dir:\$PATH\"' >> ~/.profile" ;;
esac

say ""
say "Run it:          dbwiz"
say "Add a launcher:  dbwiz desktop install"
