# Read code components

Read a repository's indexed components and their dependencies:

```sh
sourceant architecture acme/billing
sourceant architecture acme/billing --depth 2
```

Components follow directory boundaries. Their identifiers remain the same when an unrelated component grows. Depth is between 1 and 4; tests are excluded unless `--tests` is supplied.

Export a baseline:

```sh
sourceant architecture acme/billing --depth 2 --json > architecture.json
```

After the repository is indexed again, compare it with that baseline:

```sh
sourceant architecture acme/billing --baseline architecture.json
```

The comparison uses the baseline's repository, depth, and test selection. It reports added, removed, and modified components and dependencies. Incomplete snapshots are refused because missing code cannot establish that a dependency was removed.

These commands read the current index. They do not trigger indexing or compare Git commits. The agent's schedule or the Repositories page updates the index. No model is called to group or compare components.
