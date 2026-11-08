# Daffy Duck

A read-only Linux health CLI, written in Go. Investigate overheating, freezes,
resource contention, and slow boots without applying speculative “optimizations.”
Designed for Zorin OS and Ubuntu; other Linux distributions may expose fewer checks.

## Build

Requires Go 1.24 or newer. No third-party Go dependencies.

```sh
go build -o bin/daffy ./cmd/daffy
./bin/daffy scan
```

## Commands

```sh
daffy scan                         # One-time findings and recommendations
daffy watch --interval 5s          # Repeated, interruptible observations
daffy report --json                # One JSON report on stdout
daffy watch --json                 # One JSON report per line (NDJSON)
daffy doctor                       # Capabilities, tools, and permissions
daffy --help
daffy --version
```

`--sample 1s` controls the utilization sampling window. `--timeout 3s` bounds
each optional external command, not the entire scan. A scan may take longer
than the watch interval; watch must not overlap scans. Stop with Ctrl-C.

Completed scans exit 0 even when they contain critical findings: this is a
diagnostic tool, not a pass/fail health gate. Execution failures exit 1; invalid
usage exits 2. Interrupted commands exit 130.

## Coverage

| Area | Evidence |
| --- | --- |
| Cooling | Hardware temperature sensors and their available limits; historical and sampled thermal-throttling counters |
| CPU and workloads | Sampled CPU and per-process utilization, busy processes, concurrent Java/build workloads |
| Memory | Available memory, swap usage/activity, Linux pressure-stall information, logged OOM kills |
| Storage | Root filesystem capacity, disk activity and I/O pressure, device identification, logged storage errors, optional SMART |
| Graphics | PCI GPU identity and active driver, logged GPU faults; NVIDIA/Nouveau compatibility review |
| System | Failed system services, current and previous boot kernel diagnostics, boot timing, current power profile |

Sources include `/proc`, `/sys`, `/etc/os-release`, and optional system tools.
Run `daffy doctor` to see what is available. Missing tools, inaccessible logs,
and unsupported sensors produce **unknown**, never a fabricated healthy result.
Optional tools include `journalctl`, `systemctl`, `systemd-analyze`,
`powerprofilesctl`, `sensors`, `lspci`, and `smartctl`. Daffy does not install them.

### Distribution adapters

Ubuntu (`ID=ubuntu`) and Zorin OS (`ID=zorin`) have explicit adapters selected
from `/etc/os-release`. They share Linux collectors and Ubuntu-family package
guidance for missing optional tools in `doctor`; no packages are installed.
Zorin is selected by its own ID, not treated as Ubuntu because of `ID_LIKE`.
Other distributions and missing or invalid identities use a generic Linux
adapter without Ubuntu-specific package guidance.

JSON `system` metadata includes `distro_id`, `distro_adapter`, and, when
available, `distro_version`. The existing `os` display name is preserved.
`doctor` also identifies the selected adapter. Supporting another distribution
means adding an adapter to the registry, not branching inside shared collectors.

## Safety and privacy

- No stress tests, process killing, automatic repairs, package installation,
  fan changes, privilege escalation, or system-setting changes.
- Basic scans need no root access. `--privileged` opts into additional checks
  such as SMART **only when already running as root**; it never invokes sudo.
  Root gives the process broad access, so enable it only when needed.
- Default output excludes process command-line arguments, environment variables,
  usernames, hostnames, drive serial numbers, and raw journal messages.
- `--include-sensitive` opts into bounded raw log evidence. Logs can contain
  personal data; review output before sharing. Reports are not uploaded anywhere.
- Reports go to stdout. To save one explicitly:

  ```sh
  daffy report --json > health-report.json
  ```

Temperature limits differ between devices. Daffy uses exposed hardware limits
where possible; fallback thresholds are advisory. Nonzero throttling counters
prove past throttling, not necessarily current throttling. Correlated workloads
are evidence of load, not proof of the cause of a freeze. No logged errors does
not establish hardware health. SMART is supplementary, not a guarantee.

**If temperatures approach the hardware critical limit, stop heavy work. If
they remain high while idle, shut down and inspect cooling hardware. Software
tuning cannot repair a loose heatsink, blocked airflow, or a failing fan.**

## Development and verification

```sh
go test ./...
go vet ./...
go test -race ./...
```

Collectors have fixture-driven tests for missing/malformed data, thresholds,
sampling, privacy, and cancellation. External-command integrations use fake
executors to test errors and permissions without root or real hardware faults.
CLI tests exercise routing, output, validation, and cancellation. Live scans
are smoke tests only: they do not reproduce freezes or validate every device.

### Container smoke tests

```sh
docker build --target ubuntu -t daffy-ubuntu .
docker run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  -e EXPECTED_DISTRO_ID=ubuntu -e EXPECTED_DISTRO_ADAPTER=ubuntu \
  --entrypoint /bin/sh daffy-ubuntu /usr/local/bin/container-smoke

docker build --target zorin-fixture -t daffy-zorin-fixture .
docker run --rm --read-only --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  -e EXPECTED_DISTRO_ID=zorin -e EXPECTED_DISTRO_ADAPTER=zorin \
  --entrypoint /bin/sh daffy-zorin-fixture /usr/local/bin/container-smoke
```

CI covers Ubuntu 22.04 and 24.04 plus Zorin 17/18 **identity fixtures** on their
corresponding Ubuntu bases. Defaults are Ubuntu 24.04 and the Zorin 18 fixture;
use `--build-arg UBUNTU_VERSION=22.04 --build-arg ZORIN_VERSION=17` for the
older fixture. See [Zorin's base versions](https://zorin.com/os/details/).

The Zorin target is not a real Zorin installation: it substitutes test
`os-release` metadata in Ubuntu userspace to verify adapter selection. Images
run as a non-root user without privileged host mounts. Python is included only
to assert smoke-test JSON, not as an application dependency.

Containers share the host kernel and may lack systemd, readable journals,
sensors, devices, and a desktop session. Unknown findings are expected; these
checks validate execution and report contracts, not machine health. Validate
real Zorin services and desktop behavior in a VM or installation, and hardware
diagnostics on the target hardware before claiming those are verified.

Structure:

```text
cmd/daffy/          entry point and signal handling
internal/cli/       commands and human/machine-readable presentation
internal/health/    report model, Linux collectors, optional integrations
```

JSON reports include a schema version, collection timestamp, duration, system
identifiers, and findings. Findings carry a stable ID, area, severity, summary,
evidence, recommendation, and confidence. Severity is one of `critical`,
`warning`, `info`, `ok`, or `unknown`; consumers should tolerate new finding IDs.
