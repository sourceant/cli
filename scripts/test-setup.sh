#!/bin/sh
# Drives both install paths the way somebody would: set the machine up, start
# it, and ask the API a question. A path that installs but never answers is not
# installed.
#
# The core comes from its real release. The agent comes from a release too,
# unless AGENT_BINARY names a build to serve instead, which is what makes this
# runnable before the agent has one.
set -eu

VERSION="${VERSION:-$(cat VERSION)}"
AGENT_BINARY="${AGENT_BINARY:-}"
RUNTIMES="${RUNTIMES:-python docker}"
# Extra flags for setup, so a run can name a local image or wheel instead of
# reaching for what a release published.
SETUP_ARGS="${SETUP_ARGS:-}"

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
served=""
agent_pid=""

cleanup() {
    [ -n "$agent_pid" ] && kill "$agent_pid" 2>/dev/null || true
    [ -n "$served" ] && kill "$served" 2>/dev/null || true
    docker rm -f "$(docker ps -aqf name=sourceant-core- 2>/dev/null)" 2>/dev/null || true
    rm -rf "$work"
}
trap cleanup EXIT

log() { printf '\n== %s\n' "$*"; }
die() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# An agent release, or a local build dressed as one.
serve_agent() {
    plat="linux-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
    mkdir -p "$work/serve/v$VERSION"
    cp "$AGENT_BINARY" "$work/sourceant-agent-$VERSION-$plat"
    chmod +x "$work/sourceant-agent-$VERSION-$plat"
    tar -czf "$work/serve/v$VERSION/sourceant-agent-$VERSION-$plat.tar.gz" \
        -C "$work" "sourceant-agent-$VERSION-$plat"
    (cd "$work/serve" && python3 -m http.server 8971 --bind 127.0.0.1 >/dev/null 2>&1) &
    served=$!
    export SOURCEANT_DOWNLOAD_BASE="http://127.0.0.1:8971"
    # The port has to answer before an install asks it for anything.
    for _ in $(seq 1 20); do
        curl -fsS -o /dev/null "http://127.0.0.1:8971/" 2>/dev/null && return 0
        sleep 0.5
    done
    die "the stub agent release never came up"
}

answers() {
    for _ in $(seq 1 "$2"); do
        curl -fsS -o /dev/null "$1" 2>/dev/null && return 0
        sleep 1
    done
    return 1
}

check() {
    runtime=$1
    home="$work/home-$runtime"
    log "$runtime: setting up"
    SOURCEANT_INSTALL_HOME="$home" "$root/sourceant" setup --runtime "$runtime" $SETUP_ARGS

    [ -f "$home/config.json" ] || die "$runtime: nothing was written down"
    grep -q "\"runtime\": \"$runtime\"" "$home/config.json" ||
        die "$runtime: config.json names another runtime"
    [ -x "$home/bin/sourceant-agent" ] || die "$runtime: no agent was installed"

    log "$runtime: starting the agent"
    SOURCEANT_INSTALL_HOME="$home" "$home/bin/sourceant-agent" >"$work/agent-$runtime.log" 2>&1 &
    agent_pid=$!

    answers "http://127.0.0.1:8930/health" 120 ||
        die "$runtime: the agent never answered. $(tail -5 "$work/agent-$runtime.log")"

    # The agent answers before the core does. Waiting only for the agent asks
    # about a core that has not finished starting and calls it down.
    log "$runtime: waiting for the core"
    health=""
    for _ in $(seq 1 180); do
        health=$(curl -fsS "http://127.0.0.1:8930/health" 2>/dev/null || true)
        case "$health" in *'"core_up":true'*) break ;; esac
        sleep 1
    done
    printf '  %s\n' "$health"
    case "$health" in
        *'"core_up":true'*) ;;
        *) die "$runtime: the core never came up. $(tail -8 "$work/agent-$runtime.log")" ;;
    esac

    log "$runtime: asking the API"
    curl -fsS "http://127.0.0.1:8930/api/repositories" >/dev/null ||
        die "$runtime: the API did not answer for repositories"

    kill "$agent_pid" 2>/dev/null || true
    agent_pid=""
    printf '  PASS: %s installs, starts, and answers\n' "$runtime"
}

[ -x "$root/sourceant" ] || die "build the CLI first: make build"
[ -n "$AGENT_BINARY" ] && serve_agent

for runtime in $RUNTIMES; do
    check "$runtime"
done

printf '\nPASS: %s\n' "$RUNTIMES"
