# Verification and limits

## Claims and evidence

| Claim | Verification owner and evidence |
| --- | --- |
| CPU, memory, pressure, temperature, and throttling findings reflect Linux counters | Core collector lane: synthetic procfs/sysfs fixtures, parser and delta tests, cancellation tests |
| Missing capabilities cannot masquerade as healthy observations | Core/integration lanes: absent, malformed, inaccessible, and reset-counter cases |
| Optional commands stay bounded and do not require privilege escalation | Integration lane: bounded-output and timeout tests, fake executor, explicit root/SMART gating |
| Default reports do not export sensitive raw logs | Integration/core lanes: privacy fixtures; orchestrator: live JSON inspection |
| Commands have consistent human/JSON output and interrupt behavior | CLI lane: injected collector, command routing, output errors, cancellation, and sanitization tests |
| Disk profiling respects filesystem boundaries and reports partial coverage | Disk lane: mount fixtures, exclusions, cancellation, symlinks, hard links, and bounded issue tests |
| Disk accounting and cleanup advice are observational, not deletion guarantees | Disk lane: allocation fixtures, hotspot rankings, scoped cleanup rules; orchestrator: controlled-tree JSON smoke test |
| Plugin bundles select only the requested plugin's default operations and retain partial results | Plugin core lane: registry validation, operation selection, failure, and cancellation tests |
| Docker analysis uses structured CLI evidence without exposing raw inspection secrets | Docker lane: native-format fixtures, fake runner, malformed/missing evidence, command limits, and privacy tests |
| Bazel analysis requires an explicit directory and does not invoke Bazel | Bazel lane: invalid-root fixtures, injected scanner configuration, coverage, and cancellation tests |
| The combined application builds and runs on Zorin | Orchestrator: formatting, full tests, race detector, vet, build, live doctor/scan/report/watch smoke checks |

All runtime checks are observational. Do not heat the machine intentionally,
force an OOM, fault a disk, kill workloads, or change system configuration to
exercise a detector. Reproducible fixtures establish those code paths instead.

## Interpretation limits

- A sensor value is an observation, not a cooling-system diagnosis. Hardware
  limits may be absent, incorrect, or specific to a non-CPU device.
- Throttling counters are cumulative within their kernel-defined lifetime.
  Only an increase during a valid sample demonstrates sampled activity.
- Process CPU shares describe the sample, not lifetime usage; short-lived
  processes can disappear between reads. Java alone does not prove Gradle.
- PSI averages describe stalled workloads, not the exact cause of stalls.
- Device counters show activity, not whether an SSD is reliable.
- Journal searches cover bounded recent messages from the selected boots,
  not all historic events. Permissions and retention affect visibility.
- A GPU driver compatibility recommendation is not evidence of a GPU fault.
- Firmware-reported boot timing may be inaccurate and does not describe
  interactive slowness after login. Startup service durations may overlap.
- SMART can be unavailable, partially supported, or healthy before a failure.
- Live smoke tests cannot establish that intermittent freezes are resolved.

## Release checks

```sh
test -z "$(gofmt -l cmd internal)"
go vet ./...
go test -race ./...
go build -o bin/daffy ./cmd/daffy
./bin/daffy --help
./bin/daffy plugins list --json
./bin/daffy doctor
./bin/daffy scan --sample 100ms --timeout 1s
./bin/daffy report --json --sample 100ms --timeout 1s
```

For `watch`, verify that SIGINT stops the process without leaving an observer
running, with exit code 130. Do not use `--privileged` or `--include-sensitive`
for routine live verification. Saved local reports should not be committed.

Plugin tests do not require installed Docker or Bazel. A fixture-backed Docker
CLI on a temporary `PATH` can establish end-to-end bundle routing without daemon
access. Such evidence verifies invocation and interpretation, not compatibility
with every Docker daemon/version or a real workload. Live Docker checks, if
requested, must use an already authorized daemon; never start containers or
change daemon configuration merely to verify the analyzer. See
[plugin scope and limits](plugins.md).

For an authorized Docker daemon, exercise the new options read-only:

```sh
./bin/daffy docker config --context default --json --timeout 5s
./bin/daffy docker runtime --samples 2 --interval 100ms --json --timeout 5s
./bin/daffy docker runtime --project daffy-validation-no-match --json --timeout 5s
./bin/daffy docker disk --details --json --timeout 5s
```

Use a valid context name for the selected machine. An empty daemon establishes
empty-selection and routing behavior, not real workload health or CPU accuracy.
A slow disk query must produce an explicit incomplete report, not a healthy empty
result. The verbose disk fixtures are source-derived/synthetic for CLI v29.1.3,
not captured proof of every Docker version. SIGINT during repeated sampling must
exit 130 and preserve the completed sample count. Keep local reports private.

## Docker profiling regression evidence

- Crash fixtures distinguish exit 137 from a recorded OOM kill, exclude retained
  stopped-container health from current failures, and verify opt-in evidence
  privacy, separate lifecycle counts, scoped stderr forwarding, combined output
  limits, cancellation, and partial evidence preservation.
- Memory fixtures exercise decimal/binary units, non-finite and overflow values,
  usage versus capacity, absolute-byte ranking, limits/headroom/OOM/unknown state,
  inspect identity mismatches, selector/context propagation, sampled pressure,
  and preservation on cancellation. Growth observations are not leak diagnosis.
- Storage fixtures cover image references and filters, rounded size sorting,
  malformed numbers, effective Docker context precedence, capacity and inode
  pressure, warning preservation, and local-verifier rejection cases. Linux
  filesystem checks require matching socket owner, mount namespace, and root;
  inaccessible or ambiguous daemon storage must remain unavailable.
- CLI fixtures verify typed option routing, concise versus verbose output,
  sanitization, JSON compatibility, and operation observation before slow storage.

These tests do not prove compatibility with every Docker version, event retention
policy, logging driver, cgroup arrangement, or remote host. Do not manufacture
crashes, OOM conditions, or storage pressure for a live smoke check. Investigation
is explicitly sensitive: logs and health-check output may contain secrets.
See [Docker troubleshooting](docker-profiling.md) for the command workflow.

The integrated profiling stack was checked with formatting, `go vet ./...`,
`go test -race ./...`, and a native build. Cross-compilation passed for Darwin
ARM64/AMD64, Windows AMD64, and Linux ARM64. A production-binary smoke test with a
temporary fixture Docker executable established default privacy, opt-in stdout
and stderr logs plus health evidence, sampled memory, unused-image filtering,
remote-capacity rejection, and runtime output before delayed disk completion.
No real Docker daemon, containers, or images were changed during verification;
fixture evidence does not replace native-platform or real-daemon compatibility
testing.
