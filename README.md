# SourceAnt CLI

The command a person types. It reads the code graph the SourceAnt agent keeps on this machine.

```
$ sourceant repos
REPOSITORY       PATH
acme/billing     /home/you/work/billing

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

Two ways to have it. `--runtime docker` pulls the published image, and is what works today. `--runtime python` builds a virtual environment and pip installs the core, for when the core is published as a package; until then it says so rather than recording something that will not start.

Both put the index in the same place, `$XDG_DATA_HOME/sourceant`, so it does not matter which one indexed it. The container runs as whoever installed, so what it writes there belongs to them.

`sourceant ui` starts the agent and opens the view.

| Variable | Default | Meaning |
|---|---|---|
| `SOURCEANT_AGENT_URL` | `http://127.0.0.1:8930` | The agent to talk to |

`--agent`, `--timeout` and `--json` override it per command.

## Commands

| Command | What it does |
|---|---|
| `sourceant setup` | Put the agent and a core on this machine |
| `sourceant status` | Whether the agent and the indexer are running |
| `sourceant repos` | Repositories indexed on this machine |
| `sourceant graph <repository>` | What the indexer found in one of them |
| `sourceant ui` | Open the graph in a browser |
| `sourceant version` | What this build is |

`--json` prints the agent's own answer, for anything that wants to read it rather than look at it.

## Building

```bash
make qa      # fmt-check, vet, lint, test
make build
```

## Licence

MIT.
