# Explicit process control

Diagnostics remain read-only. The `kill` command is an explicit exception: it
sends SIGTERM to a process on Linux or macOS.

```sh
daffy kill pid 12845
daffy kill port 8080
```

PID targets must be greater than 1. Port targets must be between 1 and 65535
and select TCP listening sockets, including IPv4 and IPv6. UDP sockets and
established connections are not selected. A port with multiple process owners
is refused and the discovered PIDs are listed; inspect them before choosing a
specific PID. No match is an execution failure, not a successful termination.

There is no automatic privilege escalation, force-kill fallback, or process-group
signalling. Success means **SIGTERM was sent**, not that the process exited.
A process can handle or ignore SIGTERM. Exit codes are 0 for a sent signal,
1 for execution failure, 2 for invalid usage, and 130 for interruption.

## Platform lookup

- Linux matches listening socket inodes in `/proc/net/tcp` and `/proc/net/tcp6`
  to `/proc/<pid>/fd` links. Discovery is limited to visible process and network
  namespaces. Restricted `/proc` visibility can prevent port-based termination;
  incomplete discovery is refused rather than reported as no owner.
- macOS invokes `/usr/sbin/lsof` for TCP listeners and `/bin/ps` for process-start
  identity. These commands run directly, without a shell, with bounded output
  and timeouts. Missing tools, warnings, and permission errors fail discovery.
  Intel and Apple Silicon share this implementation.

The controller resolves ownership and rechecks the same process identity
immediately before signalling. Linux uses process start ticks; macOS uses `ps`
start time, which has second-level precision. These checks reduce, but do not
eliminate, PID-reuse and ownership races between the last check and the signal.
This is not an atomic port-reservation or process-identity operation.

## Verification

Fixture tests cover lookup parsing and fake-backed tests cover refusal paths.
Owned-child tests signal only processes created by the tests. Native macOS tests
run in CI on Intel and Apple Silicon; cross-compilation alone does not establish
native lookup or signalling behavior.
