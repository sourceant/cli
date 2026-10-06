# SourceAnt CLI

The command a person types. It reads the work in a checkout, and the code graph the SourceAnt agent keeps on this machine.

```
$ sourceant review
http://127.0.0.1:8930/reviews/8c2b18b67f4c41efb61efd7d2967ccfd

feat/subtract against main (bd51156), 1 file changed, 2 commits

blocking  calc.py:5  The function 'subtract' has no docstring on its first line.
blocking  calc.py:9  The function 'times' has no docstring on its first line.

Not ready. 2 blocking.

$ sourceant repos
REPOSITORY       READ            PATH
acme/billing     3 minutes ago   /home/you/work/billing
acme/shipping    reading         /home/you/work/shipping

$ sourceant graph acme/billing
2215 nodes, 2006 links

KIND        COUNT
function    895
import      854
class       257
python      179

EDGE     COUNT
defines  1152
imports  854
```

## How the pieces fit

Three processes, each with one job:

| | |
|---|---|
| `sourceant` | this CLI, which talks only to the agent |
| `sourceant-agent` | always running: supervises the indexer, keeps the graph current, serves it |
| the SourceAnt core | Python, owns the grammars and the graph |

The CLI never reaches past the agent. The agent is the process that is always up and the one that knows where the core is listening; going around it would mean learning both.

## Installing

```bash
curl -fsSL https://raw.githubusercontent.com/sourceant/cli/main/scripts/install.sh | sh
sourceant setup
```

`setup` puts the agent and a core on this machine and writes down which one, so the agent knows what to start.

`--runtime docker` pulls the published image. `--runtime python` builds a virtual environment and installs the core's published wheel.

The core and agent versions are independent of the CLI version. By default, Docker uses the `latest` image, Python uses the latest core release, and the agent uses its latest release. Release lookup prefers a stable release and falls back to the newest published prerelease when no stable release exists. The CLI installer uses the same policy.

Pin releases independently:

```bash
sourceant setup --core-version 1.0.0-beta.2 --agent-version 1.0.0-beta.2
```

`--core-version` selects a `v`-prefixed image tag for Docker or a release wheel for Python. Use `--image` for a custom container image or `--from` for a Python package specifier or checkout. Neither can be combined with `--core-version`.

To pin the CLI installer itself:

```bash
curl -fsSL https://raw.githubusercontent.com/sourceant/cli/main/scripts/install.sh | SOURCEANT_VERSION=1.0.0-beta.2 sh
```

Both put the index in the same place, `$XDG_DATA_HOME/sourceant`, so it does not matter which one indexed it. The container runs as whoever installed, so what it writes there belongs to them.

Any command that needs the agent starts one, so nothing has to be started by hand. The usual port is 8930, and where something else holds it the agent takes the next one free and the address is written to `~/.sourceant/config.json` for every later command. `sourceant start` does it on its own, `sourceant stop` shuts the agent and its core down without touching the index or the configuration, and `sourceant status` reports on both without starting anything.

| Variable | Default | Meaning |
|---|---|---|
| `SOURCEANT_AGENT_URL` | `http://127.0.0.1:8930` | The agent to talk to |

`--agent`, `--timeout` and `--json` override it per command.

## Commands

| Command | What it does |
|---|---|
| `sourceant review [path]` | Read what a checkout has that its default branch does not |
| `sourceant setup` | Put the agent and a core on this machine |
| `sourceant stop` | Stop the agent and its Python core or Docker container |
| `sourceant status` | Whether the agent and the indexer are running |
| `sourceant repos` | Repositories indexed on this machine |
| `sourceant graph <repository>` | What the indexer found in one of them |
| `sourceant architecture <repository>` | Indexed components and dependencies; compare an exported baseline with `--baseline` |
| `sourceant ui` | Open the graph in a browser |
| `sourceant start` | Start the agent and the indexer |
| `sourceant update [cli\|agent\|core]...` | Bring this machine up to the current release |
| `sourceant version` | What this build is |

`review` reads the folder you are standing in, committed or not, against the branch the repository defaults to. It exits 2 when a skill blocks the change, so a shell script can use it. `--against <ref>` compares against something else, `--no-model` says what changed without judging it, `--no-wait` prints the link and leaves it running, and `--title` and `--skill` name the change and the skills to read it against.

`update` replaces this command, the agent and the core, or only the parts named. `--check` says what is available and changes nothing. `--to <version>` takes a version other than the newest, for one named part. A machine on a prerelease follows prereleases; `--prerelease` asks for that on a machine that is not. Nothing is written until its checksum matches the release it came from, and each replacement is renamed over the old file, so an interrupted update leaves what was working in place.

`--json` prints the agent's own answer, for anything that wants to read it rather than look at it.

For a remote review, `--dir` (`-d`) selects the local checkout and
`--repository` (`-r`) supplies its `owner/name` identity. `--host` (`-H`)
selects the reviewer server; the CLI supplies the API path. Set
`SOURCEANT_REVIEW_TOKEN` for authentication and optionally
`SOURCEANT_REVIEW_HOST` as the default server.

```bash
sourceant review \
  --dir /workspace/repo \
  --repository acme/example \
  --base "$(git -C /workspace/repo rev-parse HEAD~1)" \
  --head "$(git -C /workspace/repo rev-parse HEAD)" \
  --host https://review.example.com \
  --option discovery-passes=3 \
  --option evaluation-passes=2 \
  --format json
```

`--option` (`-o`) can be repeated for different reviewer settings.

## Building

```bash
make qa      # fmt-check, vet, lint, test
make build
```

## Licence

MIT.
