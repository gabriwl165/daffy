# Built-in analysis plugins

Daffy plugins are explicitly invoked, read-only domain analyses. They do not run
as part of the default health scan. The first built-ins are Docker and Bazel.

```sh
daffy plugins list
daffy plugins list --json
daffy docker
daffy docker disk --json
daffy docker runtime
daffy docker config
daffy bazel --path "$HOME/.cache/bazel" --timeout 30s
daffy bazel disk --path /selected/output --exclude /selected/output/skip
```

The longer `daffy plugins run <name>` form remains supported with identical
behavior. Top-level aliases are explicitly reserved for these built-ins; the
registry does not automatically create root commands.

Without an operation, the command selects that plugin's default operations. Docker's
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
  Selected state fields also identify a recorded OOM kill or a stopped container
  with a nonzero exit code. Restart counts are historical observations, not proof
  of a current restart loop. Findings can include suggested next steps; Daffy
  does not read container logs, change limits, or restart containers.

Raw inspection documents, environment variables, command arguments and raw
command stderr are not included in reports. Container names and identifiers can
still be sensitive. Docker daemon access is highly privileged even when the
commands issued by Daffy are read-only. Command time, output and inspected
container counts are bounded; incomplete collection must be visible in reports.
Known command failures distinguish a missing Docker CLI, denied daemon access,
an unreachable daemon, and unsupported operations with fixed diagnostic text.
Classification is best-effort; unfamiliar or localized errors remain generic.
Raw stderr is used only for bounded internal classification, never emitted.

Use an explicit context to keep daemon selection visible, or select containers
by exact name or an unambiguous ID prefix of at least 12 characters:

```sh
daffy docker config --context default --container web --container worker
daffy plugins run docker runtime --container web --json
```

Container selection requires an explicit `config` or `runtime` operation; disk
accounting remains daemon-wide. Unknown or ambiguous selectors are errors.
Configuration includes stopped containers, but runtime statistics include only
running matches. An empty running selection never falls back to all containers.
The context name is reported without exposing its daemon endpoint; without
`--context`, Docker's normal CLI/environment selection remains authoritative.

Compose projects can be selected by their Docker label, without invoking Compose
or reading a project file:

```sh
daffy docker config --project shop
daffy docker runtime --project shop --json
```

Configuration reports expose only the Compose project/service labels and include
project totals, unhealthy counts, and restarting counts. Standalone containers
remain visible in unfiltered configuration reports. Project and container
selectors intersect when combined; project selection also requires an explicit
`config` or `runtime` operation. Names and allowlisted label values may be sensitive.

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

`--timeout` is a per-operation budget (default: 15 seconds). Operations run
sequentially, each with a fresh budget: a slow Docker disk check does not prevent
runtime and configuration checks from running. A bundle can therefore take longer
than the specified timeout. Use, for example, `daffy docker disk --timeout 60s`
to give disk accounting more time; Docker commands use the operation's remaining
budget rather than a separate fixed command limit.

An independent operation failure does not discard successful results from other
operations. Interrupting the command stops further collection; unavailable and
incomplete outcomes remain visible.

Timeout diagnostics distinguish a check that ran out of time from a check that
was never started because the parent command was cancelled or its deadline
expired. Docker timeout issues identify the read-only command without exposing
container arguments or raw stderr. JSON timeout issues add `elapsed_ms` and,
when available, `command`; operation statuses and schema version remain unchanged.
The final error summarizes timeouts rather than repeating the same context error
for every affected check.

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
