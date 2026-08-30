# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0-beta.2] - 2026-08-30

First release, versioned alongside the core it reads.

### Added

- `sourceant install` puts a core on this machine and writes down which one, so
  the agent knows what to start. As a container today, or as a Python package
  once the core is published as one
- `sourceant status` says whether the agent and the indexer are running
- `sourceant repos` lists what is indexed here, and `sourceant graph` says what
  the indexer found in one of them
- `sourceant ui` opens the graph in a browser, and says the agent is not running
  rather than opening a browser on an error page
- `--json` on any command prints the agent's own answer, for anything that reads
  rather than looks
- `SOURCEANT_AGENT_URL` names the agent to talk to, and `--agent` overrides it
  for one command
