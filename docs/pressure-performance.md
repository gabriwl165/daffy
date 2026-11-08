# PSI parser review

## Complexity

`corePressure` reads three fixed Linux pressure-stall information resources:
CPU, memory, and I/O. Iterating resources, lines, and fields does not make it
cubic: each field belongs to one line in one resource, rather than repeatedly
traversing the same input. Work is **O(B)** in total input bytes, with memory
for input and normalized evidence. Linux PSI files are normally tiny.

The optimization target is allocation and parsing overhead, not a different
asymptotic complexity. Parallel file reads would add coordination cost without
evidence that it benefits these three small reads.

## Contracts

- Typed constants identify resources, record kinds, and consumed PSI fields.
- Named policy defines the minimum field count and warning threshold.
- Missing/malformed data remains unknown, never healthy.
- Finite `avg10` values must be within 0–100; values at least 20 warn.
- Evidence retains normalized whitespace and resource ordering.
- Shared PSI collection stays in `health`; distribution package policy does
  not duplicate Linux kernel parsers.

## Reproduce measurements

```sh
go test ./internal/health -run 'Pressure|PSI'
go test ./internal/health -run '^$' -bench Pressure -benchmem -count=3
```

Benchmarks compare the previous split/fields/join parser with the new parser,
using CPU/memory examples and larger synthetic input. They measure parsing
only, not filesystem latency, the complete scan, or real application speedup.

## Local measurements

Linux/amd64, Intel i5-9400F, three benchmark runs on the final parser:

| Input | Bytes/op, old → new | Allocations/op, old → new | Median ns/op, old → new |
| --- | --- | --- | --- |
| CPU PSI | 352 → 144 | 7 → 3 | 4,450 → 3,143 |
| Memory PSI | 368 → 160 | 7 → 3 | 4,332 → 2,443 |
| 512 synthetic records | ~102,004 → ~51,554 | 1,035 → 521 | 572,857 → 716,843 |

Allocation reductions were consistent. Timing is noisy: the larger synthetic
case was slower in this run, despite lower allocation. This is not a universal
latency improvement or an end-to-end scan speedup claim. The intended tiny PSI
inputs benefit from fewer temporary slices and preallocated evidence builders;
both implementations remain linear. Differential tests compare parser results
and evidence with the previous implementation, including malformed inputs.
