# Disk space profiling

Disk profiling is a separate, explicitly requested operation. It does not add a
filesystem traversal to the existing health scan, and it never deletes files or
runs cleanup commands.

## Reading the CLI report

```sh
daffy disk "$HOME" --progress
daffy disk /var --top 10 --time-budget 30s
daffy disk "$HOME" --json --progress > disk-report.json
```

Text output separates coverage, diagnostic findings, filesystem capacity, ranked
files/directories, recommendations, and scan issues. Capacity bars are accompanied
by numeric percentages; color and an interactive terminal are not required.
Rankings use allocated bytes. Parent and child directory sizes overlap and must
not be added together. Capacity remains filesystem-wide even for a selected path.

`--progress` is optional and writes counters and the current directory only to
stderr, at most once per second after the first second. It does not promise an
ETA or a percentage of the tree: the total entry count is unknown until scanning
finishes. Without the flag, JSON mode stays silent on stderr during successful
scans. The JSON schema and partial-report/exit-code behavior are unchanged.

Start with capacity and inode warnings, then inspect coverage before interpreting
the rankings. To investigate a hotspot, rerun the command with that directory as
the path. If filesystem consumption is not explained by the scan, consider
permissions, deleted-but-open files, snapshots, shared blocks, reserved space,
and metadata; these are possible explanations, not diagnoses established by a
directory walk. No extra privileged checks or cleanup commands run automatically.

## Scope and safety

With no path, the profiler discovers eligible local filesystems in the current
Linux mount namespace. This is not necessarily the host's complete filesystem
view when running in a container. Virtual filesystems, network mounts, and
duplicate mount aliases are excluded by policy. The report exposes skipped
mounts and incomplete coverage rather than treating inaccessible data as empty.

Filesystem eligibility uses recognized local filesystem types; unknown types
are conservatively skipped. Mounts under `/media` and `/run/media` are treated
as removable media. This is a mount-path heuristic, not hardware identification:
an external disk mounted elsewhere can still be scanned. Nested overlay mounts
are excluded; an overlay mounted at `/` is supported for container use.
Ambiguous stacked mountpoints are retained as excluded boundaries rather than
causing unrelated scans to fail.

An optional path narrows the scan. Exclusions narrow it further. Symlinks are not
followed and file contents are not read. Run with ordinary user permissions;
Daffy never invokes `sudo`. Permission failures, disappearing files, and other
I/O errors can make the report partial.

`coverage.complete` means the selected, policy-eligible traversal finished without
observed I/O failures or interruption. It does not mean every host path was read:
intentional mount skips and configured exclusions remain outside coverage.
Issue details and cleanup candidates are bounded; counters expose omitted errors.

A time budget and cancellation are checked between filesystem operations.
Neither can guarantee interruption of an already blocked kernel operation.
Scanning is observational, not a filesystem snapshot: concurrent writes,
renames, and mount changes can affect results.

## Interpreting space

The diagnostic analyzer flags filesystem-wide available capacity and inode counts
at or below 10% as warnings, and at or below 5% as critical. Unknown totals and
invalid free counts are not interpreted as exhaustion. These are investigation
thresholds, not predictions of when a filesystem will fill. A scoped directory
scan still reports capacity for its containing filesystem; the two scopes must
not be compared as though they measure the same thing.

Incomplete traversal is a warning. Intentional mount-policy skips and configured
exclusions are informational and do not by themselves mean traversal failed.
Diagnostics are derived from the existing report without another filesystem walk
or external commands: O(M + I) time and O(M + I) worst-case result space, for M
reported mounts and I retained issues. The JSON report contract is unchanged.

- **Apparent bytes** describe logical file lengths.
- **Allocated bytes** describe blocks attributed to filesystem entries.
- Hard-linked files are charged once per scan; the directory receiving the
  charge can depend on traversal order.
- Directory totals overlap their descendants: do not add ranked parent and child
  directories together.
- Reflinks, compression, snapshots, reserved space, filesystem metadata, and
  deleted-but-open files can prevent totals from matching filesystem usage.
- Allocated bytes are **not a guarantee of reclaimable space**.

Cleanup candidates are conservative advice for known cache or log locations,
not authorization to delete arbitrary old or large files. Review the explanation,
risk, and suggested native command first. Disk reports contain paths, which may
reveal usernames or personal filenames; review them before sharing.

## Implementation boundaries

### Initial macOS scope

On Apple Silicon, select an explicit directory on APFS, for example
`daffy disk "$HOME"`. Whole-system scans are unsupported until APFS System/Data
volume alias accounting is validated. Root components must not be symlinks:
macOS paths such as `/tmp` or `/var` may need their physical `/private/...` path.
Do not select a system-volume root to work around the whole-system restriction.

Traversal opens each directory relative to its parent descriptor with no-follow
flags. Symlinks and special files are inspected as metadata, not followed or
opened for contents. Discovered mounts remain traversal boundaries, including
unsupported filesystem types. Mount changes during a scan are not a snapshot.

APFS capacity belongs to a volume; volumes may share a container's free space.
Do not sum volume capacities as independent disks. Clones, shared extents, and
snapshots also mean observed allocated blocks are not unique physical or
reclaimable bytes. Linux cleanup guidance is not applicable on macOS.

### Shared contracts

Portable typed configuration, reports, and a scanner interface are separated
from platform-specific mount discovery, metadata, and capacity accounting.
Additional operating systems can implement the platform boundary without
changing the command's report contract. Unsupported platforms must report that
fact explicitly rather than return an empty successful scan.

Traversal uses batched directory reads and bounded hotspot lists. Directory
totals are aggregated once in postorder, not by walking every file's ancestors.
Hard-link identities are typed device/inode keys rather than generic string
sets. Resource usage still depends on directory depth, mount count, and the
number of multiply linked files; this is not a constant-memory operation.

Mount planning sorts mounts and indexes alias roots in per-device tries instead
of comparing every mount pair. Exclusions are component tries, and mount
boundaries use typed path keys. Hotspot selection uses bounded min-heaps; final
sorting is limited to the retained results. Path processing costs still scale
with path length. Tests and benchmarks cover wide/deep trees and large mount
tables; benchmark timings are observations, not performance guarantees.

The optional scanner progress callback receives typed counters and the active
directory at directory/batch boundaries. Intermediate updates are throttled to
250 ms, with initial and final updates even on interruption. It runs
synchronously: callers must keep it cheap and nonblocking. No per-entry path
formatting or timing is added. Progress uses O(1) additional state and does not
add a second walk; it cannot report while a filesystem call is blocked.

Hotspot replacement repairs the heap root once rather than popping and pushing
two boxed entries. Ranking remains O(log K) per accepted replacement and O(K)
retained state, with final O(K log K) ordering, for top-list limit K. This is a
constant-factor/allocation improvement, not a new asymptotic bound. Existing
mount/exclusion indexing and single-postorder directory aggregation are retained.
