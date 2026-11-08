# Built-in analysis plugins

Daffy plugins are explicitly invoked, read-only domain analyses. They do not run
as part of the default health scan. The first built-ins are Docker and Bazel.

```sh
daffy plugins list
daffy plugins list --json
daffy plugins run docker
daffy plugins run docker disk --json
daffy plugins run docker runtime
daffy plugins run docker config
daffy plugins run bazel --path "$HOME/.cache/bazel" --timeout 30s
daffy plugins run bazel disk --path /selected/output --exclude /selected/output/skip
```

Without an operation, `run` selects that plugin's default operations. Docker's
defaults are disk accounting, a runtime snapshot, and container configuration
checks. Bazel's default is a filesystem-space analysis; an explicit directory is
required. Daffy never discovers a Bazel cache by starting a Bazel server.

## Scope and interpretation

### Docker

The Docker CLI must be installed and have permission to query its selected
daemon. Daffy does not invoke `sudo` or silently fall back to a local socket.
The selected daemon can be remote; results are not necessarily measurements of
the machine running Daffy. Keep Docker context configuration stable during a run.
The bundle is a sequence of observations, not an atomic snapshot.

- **Disk:** preserves the category sizes and reclaimable estimates reported by
  `docker system df`. These are formatted display values, not exact byte totals.
  Shared image layers must not be added as independent reclaimable space.
- **Runtime:** takes a one-shot `docker stats` observation of running containers.
  CPU, memory and I/O values retain Docker's display units and rounding.
- **Configuration:** examines selected container inspection fields for diagnostic
  findings. Missing resource limits and privileged execution are contextual
  observations, not proof of a security incident or performance problem.

Raw inspection documents, environment variables, command arguments and raw
command stderr are not included in reports. Container names and identifiers can
still be sensitive. Docker daemon access is highly privileged even when the
commands issued by Daffy are read-only. Command time, output and inspected
container counts are bounded; incomplete collection must be visible in reports.

### Bazel

Supply a local Bazel output/cache directory with `--path`. Daffy reports observed
filesystem usage under that directory using its disk scanner. It does not prove
that the directory is a Bazel cache, measure remote caches, or equate all observed
bytes with safely reclaimable space. Paths in reports can be sensitive.

This operation does not invoke Bazel, run builds, clean outputs or analyze build
traces. The underlying scanner's existing traversal and ranking limitations still
apply. Exclusions are explicit paths; selecting a directory does not establish a
container or sandbox boundary.

## Execution and output

`--timeout` is an overall deadline for the selected operation or default bundle.
Operations run sequentially. An independent operation failure does not discard
successful results from other operations. Cancellation stops further collection;
unavailable and incomplete outcomes remain visible.

Text is the default; `--json` emits a versioned report with typed operation
details. Findings do not cause a nonzero exit status. Partial reports (including
filesystem coverage gaps) and execution failures exit 1,
invalid usage exits 2, and interruption exits 130. Partial reports are emitted
before reporting execution errors. A report with missing evidence is not a
healthy result.

## Architecture

`internal/plugins` owns the `Analyze` contract, typed descriptors, registry,
runner and report structs. Built-ins implement the contract in
`internal/plugins/docker` and `internal/plugins/bazel`. CLI bootstrap explicitly
constructs dependencies and registers the built-ins; no `init()` registration or
mutable global registry is used.

The registry stores explicit entries, validates descriptors, and resolves
operation names within their plugin. Consequently Docker and Bazel can both
expose `disk`. Default selection applies only to the requested plugin. Requests,
option capabilities and result payloads use ordinary typed structs, not generic
string-keyed property bags.

Plugins collect through injected boundaries and never own terminal output or
process exit codes. Tests use fake command runners, fake scanners and filesystem
fixtures rather than requiring a Docker daemon or a Bazel build.

These are built-in plugins, not dynamically loaded Go modules or executable
plugins. External installation, trace analysis and automatic remediation are
outside this first implementation.
