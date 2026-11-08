# Verification and limits

## Claims and evidence

| Claim | Verification owner and evidence |
| --- | --- |
| CPU, memory, pressure, temperature, and throttling findings reflect Linux counters | Core collector lane: synthetic procfs/sysfs fixtures, parser and delta tests, cancellation tests |
| Missing capabilities cannot masquerade as healthy observations | Core/integration lanes: absent, malformed, inaccessible, and reset-counter cases |
| Optional commands stay bounded and do not require privilege escalation | Integration lane: bounded-output and timeout tests, fake executor, explicit root/SMART gating |
| Default reports do not export sensitive raw logs | Integration/core lanes: privacy fixtures; orchestrator: live JSON inspection |
| Commands have consistent human/JSON output and interrupt behavior | CLI lane: injected collector, command routing, output errors, cancellation, and sanitization tests |
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
