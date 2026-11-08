# Daffy

A read-only system health CLI, written in Go. Investigate overheating, freezes,
resource contention, and slow boots without applying speculative “optimizations.”
Designed for Zorin OS and Ubuntu; other Linux distributions may expose fewer checks.
macOS support covers Intel and Apple Silicon and exposes fewer signals than Linux.

## Install on Linux or macOS

Linux on x86-64 or ARM64 and macOS on Intel or Apple Silicon can install a prebuilt
binary without Go or root access. The installer requires `curl`, `tar`, and
`sha256sum` on Linux or `shasum` on macOS, plus standard system utilities.
Windows installation is not supported.

Download the installer, inspect it, then run it:

```sh
curl --fail --show-error --location --proto '=https' --proto-redir '=https' \
  https://raw.githubusercontent.com/gabriwl165/daffy/main/setup.sh \
  --output setup.sh
less setup.sh
sh setup.sh
```

By default this installs only `daffy` into `$HOME/.local/bin`. It never invokes
`sudo`, installs optional diagnostic tools, or changes shell configuration.
If that directory is not on your PATH, add it for the current shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
daffy --version
daffy doctor
daffy scan
```

To keep the PATH change, add the export to your shell's startup file yourself.
For a custom destination, use an absolute, writable directory:

```sh
INSTALL_DIR="$HOME/bin" sh setup.sh
```

Re-run the installer to update. To install a specific retained build, copy its
`build-<full-commit-SHA>` tag from [Releases](https://github.com/gabriwl165/daffy/releases):

```sh
sh setup.sh --version 'build-<full-commit-SHA>'
```

Replace the placeholder with the actual tag. To uninstall the default install:

```sh
rm -- "$HOME/.local/bin/daffy"
```

## Build from source

Requires Go 1.24 or newer. Darwin disk traversal uses `golang.org/x/sys/unix`.

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
daffy disk                         # Linux: space hotspots across eligible local mounts
daffy disk /home --format json     # Narrow the scope; flags may follow the path
daffy docker                       # Docker's default diagnostic bundle
daffy docker disk                  # Docker disk accounting only
daffy docker runtime               # Docker runtime snapshot
daffy docker config                # Docker configuration checks
daffy bazel --path DIR              # Inspect an explicit output/cache directory
daffy --help
daffy --version
```

`--sample 1s` controls the utilization sampling window. `--timeout 3s` bounds
each optional external command, not the entire scan. A scan may take longer
than the watch interval; watch must not overlap scans. Stop with Ctrl-C.

Completed scans exit 0 even when they contain critical findings: this is a
diagnostic tool, not a pass/fail health gate. Execution failures exit 1; invalid
usage exits 2. Interrupted commands exit 130.

Use `daffy docker` and `daffy bazel` directly; no `plugins run` prefix is needed.
Use `daffy plugins list` to discover available analyses. See
[analysis plugins](docs/plugins.md) for scope, safety, and extension architecture.
These analyses run only when explicitly requested.

<details>
<summary>Legacy command compatibility</summary>

The older `plugins run` forms and `disk scan` remain supported with the same
handlers, options, and exit codes. Prefer the short commands shown above.

</details>

### Disk space profiling

`daffy disk [path]` is an explicit, read-only filesystem traversal, separate from
the default health scan. On Linux, omit the path to discover eligible local
filesystems in the current mount namespace, or supply a directory to narrow the
scan. On macOS, an explicit APFS directory is required:

```sh
daffy disk --exclude /srv/archive --time-budget 2m  # Linux whole-machine scan
daffy disk "$HOME" --top 10                       # Explicit directory scope
daffy disk /var --format json                     # Linux directory scope
```

The report ranks files and directory trees by observed allocated space, reports
filesystem capacity and inodes, and suggests conservative cleanup actions for
known locations. It never runs those actions. Allocated bytes are not guaranteed
reclaimable bytes; parent and child directory totals overlap.

Whole-machine scans can be expensive. Symlinks are not followed, excluded mounts
are not traversed, and permission failures are reported as incomplete coverage.
A time budget returns partial results with a nonzero exit code; cancellation
also preserves partial results. Budgets are checked between I/O operations, not
guaranteed to interrupt blocked kernel calls. Paths can reveal personal data.
See [disk profiling](docs/disk-profiling.md) for scope, accounting, portability,
and safety limits.

## Coverage

The table below describes Linux coverage. The macOS provider reports
kernel/CPU identity, sampled system CPU utilization, physical memory capacity,
immediately free memory, allocated swap usage, and root filesystem capacity when
available. RAM page size comes from `vm_stat`; speculative pages are reported
separately. Immediately free memory is not an estimate of all available or
reclaimable memory, and swap allocation is not swap activity or memory pressure.
CPU sampling uses two `top` samples
and rounds the requested interval up to whole seconds; failed samples stay unknown.
Disk activity uses the second `iostat` interval and reports per-device KB/transfer,
transfers/second, and MB/second for at most 32 devices. These rates do not establish
drive health, utilization, or latency. Processes, pressure, thermals, and swap
activity remain explicitly unknown; capacity alone does not establish system health.
Linux-only integrations and package recommendations are not run on macOS.
Windows cross-compiles but has no supported native health or disk provider.

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

Use typed enum constants for closed application vocabularies, concrete structs
for fixed data shapes, and named configuration structs for application APIs.
Keep maps for genuinely dynamic collections and interfaces for useful external
boundaries such as writers and contexts. Preserve CLI and JSON compatibility
when refactoring. See [the stack review](docs/refactor-review.md) for rationale
and the project-layout assessment.

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

CI runs the suite and installer fixtures on native Intel and Apple Silicon Macs,
verifies runner architectures, and gates releases on both jobs. Release builds
include Darwin AMD64 and ARM64. Cross-builds alone do not prove runtime support.
Installer fixture tests additionally require Python 3; the installer itself does not.

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
internal/health/    shared reports, OS-selected providers, Linux integrations
internal/disk/      shared scanner, OS-specific mounts and safe traversal
internal/distribution/ typed distro profiles and tool identities
  ubuntu/           Ubuntu-specific profile construction
  zorin/            Zorin-specific profile construction
```

Distribution packages contain OS policy, not performance profiling. Health
composes their concrete profiles through an exact-ID registry, with generic
Linux fallback. Zorin reuses Ubuntu-family package guidance without duplicating
shared procfs/sysfs diagnostics. Add another profile package and register it at
this composition boundary; no distro switch belongs in the shared collectors.

See [the PSI performance review](docs/pressure-performance.md) for parser
complexity, benchmark methodology, and limitations.

JSON reports include a schema version, collection timestamp, duration, system
identifiers, and findings. Findings carry a stable ID, area, severity, summary,
evidence, recommendation, and confidence. Severity is one of `critical`,
`warning`, `info`, `ok`, or `unknown`; consumers should tolerate new finding IDs.
