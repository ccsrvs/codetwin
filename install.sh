#!/usr/bin/env bash
#
# install.sh — install or update the codetwin CLI from a GitHub release.
#
# One-line install:
#
#   curl -fsSL https://raw.githubusercontent.com/ccsrvs/codetwin/main/install.sh | bash
#
# Or, from a checkout:  ./install.sh
#
# Re-running updates an existing install in place: if `codetwin` is already on
# PATH, the new binary lands in the same directory. Otherwise it installs to
# ~/.local/bin (override with CODETWIN_BIN_DIR).
#
# Installs the latest release by default. Pin or roll back with CODETWIN_VERSION:
#
#   CODETWIN_VERSION=v0.3.1 curl -fsSL .../install.sh | bash
#
set -euo pipefail

REPO="ccsrvs/codetwin"
BIN_NAME="codetwin"

err() { printf 'install: %s\n' "$*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || err "curl is required"

# --- platform detection ----------------------------------------------------
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac
case "$os" in
  linux|darwin) ;;
  *) err "unsupported OS: $os (Windows binaries are attached to each release; see the README)" ;;
esac

# --- resolve the release tag -----------------------------------------------
# CODETWIN_VERSION pins a specific tag; otherwise ask the API for the latest.
# The tag is part of the asset filename, so we need it either way.
tag="${CODETWIN_VERSION:-}"
if [ -z "$tag" ]; then
  api="https://api.github.com/repos/${REPO}/releases/latest"
  tag="$(curl -fsSL "$api" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)" \
    || err "could not reach the GitHub API to resolve the latest release"
  [ -n "$tag" ] || err "no published release found for $REPO (set CODETWIN_VERSION to pin a tag)"
fi

# --- destination: reuse an existing install, else ~/.local/bin -------------
dest_dir="${CODETWIN_BIN_DIR:-}"
if [ -z "$dest_dir" ]; then
  existing="$(command -v "$BIN_NAME" 2>/dev/null || true)"
  if [ -n "$existing" ]; then
    # Resolve symlinks where possible so we replace the real file.
    real="$(readlink -f "$existing" 2>/dev/null || echo "$existing")"
    dest_dir="$(dirname "$real")"
    echo "Found existing $BIN_NAME at $real — updating in place."
  else
    dest_dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dest_dir"

# --- download ---------------------------------------------------------------
asset="${BIN_NAME}-${tag}-${os}-${arch}"
url="https://github.com/${REPO}/releases/download/${tag}/${asset}"

tmp="$(mktemp "${dest_dir}/.${BIN_NAME}-install-XXXXXX")" \
  || err "cannot write to $dest_dir (permission denied?)"
trap 'rm -f "$tmp"' EXIT

echo "Downloading $BIN_NAME $tag ($os/$arch)…"
curl -fsSL "$url" -o "$tmp" \
  || err "download failed for $asset (no prebuilt binary for $os/$arch in $tag?)"
chmod 0755 "$tmp"

# Sanity-check the download runs before swapping it into place, so a truncated
# or wrong-platform file can't replace a working install.
"$tmp" --version >/dev/null 2>&1 || err "downloaded binary failed to run"

mv "$tmp" "${dest_dir}/${BIN_NAME}"
trap - EXIT

echo "Installed $BIN_NAME $("${dest_dir}/${BIN_NAME}" --version) -> ${dest_dir}/${BIN_NAME}"
case ":$PATH:" in
  *":$dest_dir:"*) ;;
  *) echo "note: $dest_dir is not on your PATH — add it to run '$BIN_NAME' directly." ;;
esac

# Most people drive codetwin through a coding agent — point them at the
# built-in skill installer. Claude Code user scope covers every project;
# other agents and project scope are under `agent-install --list`.
cat <<EOF

Next: let your coding agent drive $BIN_NAME. For Claude Code (user scope, all projects):

    $BIN_NAME agent-install claude --scope user

Other agents (cursor, copilot, ...) and project scope: $BIN_NAME agent-install --list
EOF
