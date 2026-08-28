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
make build
```

Then start the agent. See [sourceant/agent](https://github.com/sourceant/agent).

| Variable | Default | Meaning |
|---|---|---|
| `SOURCEANT_AGENT_URL` | `http://127.0.0.1:8930` | The agent to talk to |

`--agent`, `--timeout` and `--json` override it per command.

## Commands

| Command | What it does |
|---|---|
| `sourceant status` | Whether the agent and the indexer are running |
| `sourceant repos` | Repositories indexed on this machine |
| `sourceant graph <repository>` | What the indexer found in one of them |
| `sourceant version` | What this build is |

`--json` prints the agent's own answer, for anything that wants to read it rather than look at it.

## Building

```bash
make qa      # fmt-check, vet, lint, test
make build
```

## Licence

MIT.
