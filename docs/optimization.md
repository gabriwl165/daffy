# Read-only optimization

`daffy optimize` turns a sampled health report into prioritized recommendations.
It does not clean files, stop applications, change settings, install software,
or save history. Existing diagnostic commands retain their behavior.

```sh
daffy optimize
daffy optimize --json
daffy optimize --sample 2s --timeout 5s
```

The first version reuses native health collection. It does not recursively scan
storage, invoke workload plugins, or inspect application usage history.
Recommendations include their source findings, confidence, and a suggested
next step. Suggested commands are text only: Daffy does not execute them.
No recommendations means no actionable recommendations from the available
observations, not that the machine is healthy.

## Evidence and limitations

- Warning and critical findings for known signals can produce recommendations.
  Activity alone is not saturation; historical throttling is not current
  throttling. Workload correlation is not proof of causation.
- Named capabilities describe the observations available in this sample, not
  full platform support. Temperature availability is independent of throttle
  counter availability. Missing and unknown signals are not healthy results.
- Legacy health findings cannot reliably distinguish missing permissions,
  dependencies, and unsupported hardware. The planner reports unavailable data
  without guessing which of those caused it.
- Temperature limits vary by hardware. Reduce heavy work near critical limits;
  if temperatures stay high at idle, shut down and inspect cooling. Software
  cannot repair a blocked vent, loose heatsink, or failing fan.
- Storage capacity does not establish what is safe to delete or how much space
  cleanup would reclaim. Run an explicit scoped disk analysis before deciding.
- Process CPU observations do not identify unnecessary background applications.
  There is no foreground tracking, recurring-use analysis, or persistent agent.
- Default privacy controls still apply. This command does not accept
  `--include-sensitive` or `--privileged` and requests neither from the health
  collector. Reports go to stdout only.

## Architecture guidelines

Keep the sequence **observe → explain → propose** read-only. Future approval,
execution, and verification stages must be separately introduced and reviewed.

`internal/optimize` owns concrete plan, capability, source-evidence, and
recommendation structs plus pure analysis of supplied reports. It performs no
I/O. Use named attributes and typed closed vocabularies, not generic string-keyed
models. Keep health collection in `internal/health`, filesystem traversal in
`internal/disk`, distro policy in `internal/distribution`, workload knowledge in
plugins, and command orchestration in `internal/cli`.

Linux should share kernel collectors across distributions. Add distribution
profiles for package guidance and explicit policy rather than branching inside
collectors. Detect service managers and optional tools independently; do not
assume systemd. Keep generic Linux fallback behavior conservative.

macOS and Windows need native collection implementations behind Go build
constraints. Prefer supported APIs and documented tools. Do not force capability
parity or use OS display names to infer sensor availability. Linux currently has
the broadest coverage; macOS has partial health coverage; Windows does not yet
have a supported native health provider. Cross-compilation is not runtime proof.

## Follow-up stacks

Ship each independently reviewable layer in small dependent GitHub PRs:

1. **Local history:** opt-in storage, explicit retention and size limits,
   user-only permissions/ACLs, schema migrations, inspection and deletion.
   Aggregate measurements; do not retain arguments, environments, or document
   paths by default. Sparse history must remain insufficient evidence.
2. **Native capability expansion:** fill macOS process and thermal gaps where
   supported; introduce Windows diagnostics and native verification. Validate
   Intel and Apple Silicon separately and real Linux service/desktop behavior
   beyond distro identity fixtures.
3. **Recurring workload analysis:** correlate repeated CPU, memory, and I/O
   consumption with reliably observed application state. Background does not
   mean unnecessary, and inactivity does not authorize termination.
4. **Approved actions:** introduce a narrow allowlist only after preview,
   permissions, stale-target checks, cancellation, partial failure, verification,
   and rollback contracts exist. Revalidate process identity and file targets
   immediately before action. No arbitrary generated shell commands, silent
   elevation, universal cache deletion, or fan tuning.
5. **Optional agent:** explicit native installation and removal, bounded
   resource use, and separately opted-in collection or execution policies.

## Verification contract

Pure planner fixtures establish recommendation selection, deterministic order,
deduplication, evidence preservation, missing data behavior, and input isolation.
CLI tests establish validation before collection, option forwarding, JSON shape,
human-output sanitization, cancellation, writer errors, and absence of mutation
dependencies. Existing scan/report contracts remain covered by their tests.

Run formatting, `go vet ./...`, `go test -race ./...`, and builds before release.
Native smoke tests establish command integration only, not diagnostic accuracy
on every device. Containers and cross-builds cannot establish hardware thermal
correctness or native Windows support.
