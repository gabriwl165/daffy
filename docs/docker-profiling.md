# Docker troubleshooting

Daffy's Docker profiler is read-only. It does not restart containers, change
resource limits, delete images, or prune volumes. Commands query the daemon
selected by Docker's context and environment, which can be remote.

## Start with the default report

```sh
daffy docker
daffy docker --verbose
daffy docker --json
```

The concise report puts actionable container findings before resource and storage
summaries. Repeated missing-memory-limit notices are grouped. Full configuration
inventory is available with `--verbose`; JSON retains structured details and full
identifiers. Completed container checks can be displayed before slow disk
accounting finishes. Collection gaps remain visible rather than looking like a
healthy empty result. Each operation has its own timeout budget.

## Investigate a failing container

```sh
daffy docker config --container api
daffy docker config --container api --investigate --log-tail 50 --timeout 30s
daffy docker config --project shop --investigate --json --timeout 60s
```

Configuration distinguishes current restarting/unhealthy state, a recorded OOM
kill, stopped nonzero exits, and historical restarts. A stopped container's retained
unhealthy state is not evidence of a currently failing health check. Exit code 137
alone does not establish OOM; a restart count alone does not establish a current
crash loop.

`--investigate` explicitly permits potentially sensitive recent application logs
and health-check output. Do not publish these reports without reviewing them for
secrets. Collection is bounded by time, output size, and container count. Lifecycle
events have limited daemon retention: missing events do not prove the absence of
crashes. Start, die, and restart observations are separate counts, not a diagnosis
of the underlying application fault. Incomplete evidence is reported explicitly.

## Investigate memory

```sh
daffy docker runtime --container api --memory-details --samples 10 --interval 1s \
  --memory-threshold 80 --runtime-sort memory --timeout 30s
```

Memory diagnostics correlate observed current, first, and peak usage with
configured limits and recorded OOM state. Absolute-memory ranking is useful when
containers have different limits. Percentage denominators distinguish a
container limit from daemon-host memory; missing configuration remains unknown.
Unlimited containers do not have a private memory budget just because Docker
prints a percentage.

Growth is measured only over valid observations in this sampling window. Rising
usage does not establish a leak, and a short peak is not sustained pressure.
Missing containers are not zero samples. Docker's reported memory is not an
application heap profile; use language-specific tooling to investigate allocation
and retention after identifying a suspect container.

## Investigate images and filesystem pressure

```sh
daffy docker disk --details --timeout 60s
daffy docker disk --image-filter unused --disk-sort unique-size --timeout 60s
daffy docker disk --image-filter dangling --disk-sort size --json --timeout 60s
daffy docker disk --host-capacity --timeout 60s
```

Image filters are `all`, `unused`, `dangling`, and `referenced`. Sort modes are
`size`, `unique-size`, and `name`. Image filtering or sorting enables detailed
accounting. References include stopped containers; unknown reference counts do
not qualify as unused. Untagged/dangling and unreferenced are different concepts.
An unused image may still be wanted for rollback.

Reported sizes are rounded Docker accounting, not exact reclaimable bytes. Shared
image layers must not be summed as independently reclaimable storage. Image
creation time is not last-use time. Volume link counts and cache in-use flags are
not deletion-safety guarantees; Daffy does not recommend deleting volume data.

Host capacity is available only when the daemon filesystem can be verified as
local and accessible. Remote, VM-backed, inaccessible, and ambiguous daemon
filesystems are reported unavailable, never replaced by the client's disk usage.
Available space and inode pressure are separate observations.

If disk accounting times out, retry it independently with a larger `--timeout`.
That increases the collection budget; it does not repair a slow daemon. Earlier
container observations and any successfully collected storage evidence survive
independent failures. All observations are non-atomic and can change during a run.
