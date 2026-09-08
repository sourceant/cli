# Versions

The CLI, agent, and core are versioned independently.

Select the agent and core releases during setup. Replace `AGENT_VERSION` and `CORE_VERSION` with the releases you want:

```bash
sourceant setup --agent-version AGENT_VERSION --core-version CORE_VERSION
```

Use `--image` for a specific core container or `--from` for a Python installation source.

`sourceant stop` needs agent shutdown support, introduced in agent `1.0.0-beta.3`. The agent defines core compatibility.
