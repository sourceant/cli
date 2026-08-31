#!/bin/sh
# Installs the SourceAnt CLI. It is the only thing anybody fetches by hand:
# `sourceant setup` brings down the agent and the core after this.
set -eu

REPO="sourceant/cli"
BIN="sourceant"
VERSION="${SOURCEANT_VERSION:-latest}"
INSTALL_DIR="${SOURCEANT_INSTALL_DIR:-/usr/local/bin}"
# Where releases are served from. Overridden so this can be tested without
# reaching GitHub.
DOWNLOAD_BASE="${SOURCEANT_DOWNLOAD_BASE:-https://github.com/$REPO/releases/download}"
API_BASE="${SOURCEANT_API_BASE:-https://api.github.com/repos/$REPO/releases}"

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl
need tar

platform() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux | darwin) ;;
        *) die "no build is published for $os" ;;
    esac
    arch=$(uname -m)
    case "$arch" in
        x86_64 | amd64) arch=amd64 ;;
        aarch64 | arm64) arch=arm64 ;;
        *) die "no build is published for $arch" ;;
    esac
    printf '%s-%s' "$os" "$arch"
}

resolve() {
    if [ "$VERSION" != "latest" ]; then
        printf '%s' "${VERSION#v}"
        return
    fi
    # The tag, read without jq so this works on a machine with nothing on it.
    tag=$(curl -fsSL "$API_BASE/latest" |
        sed -n 's/.*"tag_name" *: *"\([^"]*\)".*/\1/p' | head -1)
    [ -n "$tag" ] || die "could not tell which version is latest"
    printf '%s' "${tag#v}"
}

main() {
    plat=$(platform)
    version=$(resolve)
    asset="$BIN-$version-$plat.tar.gz"
    url="$DOWNLOAD_BASE/v$version/$asset"

    log "Installing $BIN $version for $plat"

    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT

    curl -fsSL "$url" -o "$tmp/$asset" || die "could not download $url"
    tar -xzf "$tmp/$asset" -C "$tmp"
    # The archive holds one file named for the version and platform.
    [ -f "$tmp/$BIN-$version-$plat" ] || die "the archive did not hold $BIN"
    chmod +x "$tmp/$BIN-$version-$plat"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$tmp/$BIN-$version-$plat" "$INSTALL_DIR/$BIN"
    elif command -v sudo >/dev/null 2>&1; then
        sudo mv "$tmp/$BIN-$version-$plat" "$INSTALL_DIR/$BIN"
    else
        die "$INSTALL_DIR is not writable and sudo is not here. Set SOURCEANT_INSTALL_DIR"
    fi

    log ""
    log "Installed $INSTALL_DIR/$BIN"
    log ""
    log "Next: sourceant setup   # brings down the agent and the core"
    log "      sourceant ui      # starts it and opens the view"
}

main "$@"
