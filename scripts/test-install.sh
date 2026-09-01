#!/bin/sh
# Runs install.sh against a stub release inside a bare Ubuntu container, so the
# script is exercised on a machine with nothing on it rather than on a laptop
# that already has everything.
set -eu

IMAGE="${IMAGE:-ubuntu:24.04}"
VERSION="9.9.9"

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# A release as the Go action builds one: a binary named for its version and
# platform, alone in a gzipped tar.
for plat in linux-amd64 linux-arm64; do
    mkdir -p "$work/serve/v$VERSION"
    printf '#!/bin/sh\necho "sourceant %s"\n' "$VERSION" > "$work/sourceant-$VERSION-$plat"
    chmod +x "$work/sourceant-$VERSION-$plat"
    tar -czf "$work/serve/v$VERSION/sourceant-$VERSION-$plat.tar.gz" \
        -C "$work" "sourceant-$VERSION-$plat"
done

cp "$root/scripts/install.sh" "$work/install.sh"

docker run --rm \
    -v "$work:/w:ro" \
    -e SOURCEANT_VERSION="$VERSION" \
    -e SOURCEANT_DOWNLOAD_BASE="file:///w/serve" \
    "$IMAGE" sh -c '
        set -eu
        apt-get update -qq >/dev/null 2>&1
        apt-get install -y -qq curl >/dev/null 2>&1
        sh /w/install.sh
        command -v sourceant >/dev/null || { echo "sourceant is not on PATH"; exit 1; }
        [ -x /usr/local/bin/sourceant ] || { echo "not executable"; exit 1; }
        out=$(sourceant)
        case "$out" in
            *9.9.9*) ;;
            *) echo "ran but said: $out"; exit 1 ;;
        esac
        echo "PASS: installed, on PATH, executable, runs"
    '
