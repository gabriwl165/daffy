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
  does not change limits or restart containers. Structured crash diagnostics
  distinguish current restarting/unhealthy state, recorded OOM kills, nonzero
  stopped exits, and historical restarts, with timestamps and restart policy.
  Exit 137 alone is not proof of OOM. Stopped containers' retained health states
  are not counted as currently unhealthy.

The config API supports explicit opt-in crash investigation (`crash_investigation`
and optional `log_tail`, 1–200 lines, default 50). This collects bounded recent
logs, health-check output, and separate die/start/restart event counts from a
fixed 24-hour window. Logs and health output can contain secrets; they are never
included by default. Evidence is limited to 20 diagnostic containers, 64 KiB of
logs per container and 1 MiB of log text overall, plus bounded health entries.
Truncation and unavailable evidence are explicit. Docker's event retention is
limited: history cannot prove a crash loop is absent. Investigation commands
share the config operation's timeout. CLI flags are added in the later CLI PR.

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

Runtime trends are opt-in and bounded:

```sh
daffy docker runtime --samples 5 --interval 500ms --runtime-top 10 \
  --cpu-threshold 150 --memory-threshold 80 --timeout 30s
```

Choose 1–60 samples, an interval of 100ms–1m (default 500ms), and at most
100 ranked containers (`--runtime-top 0` keeps all). Ranking uses peak CPU,
then peak memory percentage, then container ID. Thresholds compare with observed
peaks using `>=`; zero disables that threshold. CPU percentages may exceed 100%
on multicore systems; memory thresholds range from 0–100%. Missing containers
are not zero-valued samples. The original runtime table is the latest snapshot;
the separate trend section retains peak observations across container churn.
Timeout/cancellation preserves collected peaks and the completed sample count.
Runtime-only flags require an explicit `runtime` operation and work with context,
container, and project selection. Without them, collection stays a single snapshot.

Disk drill-down is also opt-in:

```sh
daffy docker disk --details --context default --timeout 60s --json
```

The usual category summary is collected first. A second verbose query selects
image, container, volume, and build-cache details into typed rows; unknown fields
(including raw labels and mountpoints) are discarded. Sizes and `N/A` values
remain Docker-formatted strings, not exact byte accounting. Shared image layers,
volume link counts, and cache in-use fields are not deletion-safety guarantees.
Daffy does not prune resources or recommend deleting volume data.

The verbose parser follows Docker CLI v29.1.3's single-envelope JSON formatter;
other response shapes produce an incomplete report rather than a healthy empty
result. Collection shares the operation's remaining timeout with the summary.
An unavailable or invalid drill-down preserves an already collected summary.
`--details` requires an explicit `disk` operation and cannot combine with container
or project filtering. Disk accounting may be slow on daemons with large caches.

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
