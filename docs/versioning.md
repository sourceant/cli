# Versions

The CLI, agent, and core are versioned independently.

Select the agent and core releases during setup:

```bash
sourceant setup --agent-version 1.0.0-beta.3 --core-version 1.0.0-beta.2
```

Use `--image` for a specific core container or `--from` for a Python installation source.

CLI `1.0.0-beta.3` needs agent `1.0.0-beta.3` for `sourceant stop`. The agent defines core compatibility.
