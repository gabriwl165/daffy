# Stack review and refactoring decisions

## Scope

Reviewed the application introduced by PRs #1, #2, #3, #5, and #6: Linux
collectors, optional command integrations, CLI, distribution selection, and
container verification. Refactors are stacked above #6 rather than rewriting
the existing review branches.

## Findings

| Area | Finding | Direction |
| --- | --- | --- |
| Report model | Severity, confidence, and area use unrestricted strings | Use typed, string-backed enums without changing JSON values |
| System metadata | A map represents a known set of fields | Use a tagged struct, preserving required and optional JSON keys |
| Finding creation | Several adjacent strings and variadic evidence obscure call-site intent | Pass a named struct with an evidence slice |
| Collector policy | Fixed maps encode recommendations, confidence, and journal categories | Use typed policy and explicit fixed data shapes |
| Distribution adapters | An interface describes data-only implementations | Prefer concrete descriptors while retaining distro-specific registration and generic fallback |
| External commands | Positional timeout/tool parameters and variadic arguments obscure configuration | Pass command configuration explicitly |
| Journal boots | Independent argument and label strings can disagree | Derive both from one typed boot selection |
| CLI | Command strings and repeated collector-option construction scatter policy | Use command enums and one options conversion |
| Entry point | The runner takes a long list of positional arguments | Pass a run configuration struct |

## Boundaries

- Enum constants describe closed application vocabularies. External values
  such as device names, process names, versions, paths, and command output are
  not enums. Unknown external distribution IDs must continue to fall back.
- Dynamic maps keyed by PIDs, sensors, counters, or devices remain maps.
- Standard interfaces such as `io.Writer` and `context.Context`, and injected
  function seams used by tests, remain useful boundaries.
- Go types change inside this repository's `internal` packages. The external
  CLI syntax, exit codes, JSON field names, finding IDs, recommendations, and
  safety/privacy behavior must remain stable.
- Core and optional-integration confidence policies differ today. This is a
  refactor, not an opportunity to silently unify their behavior.

## Layout

The [project-layout reference](https://github.com/golang-standards/project-layout)
is community guidance, not an official Go standard. The existing `cmd/daffy`,
`internal/cli`, `internal/health`, `docs`, `scripts`, and `testdata` directories
already fit the application. Adding `pkg`, empty layers, or moving files only
to imitate a template would not improve the design.

## Evidence path

The pre-refactor repository test suite passed. Focused health and CLI tests
cover each refactor; JSON contract tests protect names, string enum encoding,
and optional fields. Final integration verification uses formatting, vet,
race-enabled tests, build, live CLI smoke checks, and the four existing Docker
smoke configurations. Container limitations documented in the README still
apply: Zorin identity fixtures are not Zorin installations or hardware tests.

## Final verification

The reconciled refactor passed formatting, `go vet ./...`, `go test -race ./...`,
build with version injection, shell syntax, and `git diff --check`.
All four read-only/non-root Docker smoke cases passed: Ubuntu 22.04/24.04 and
Zorin 17/18 identity fixtures on their corresponding Ubuntu bases.

Before/after live comparisons confirmed identical help text and doctor JSON,
the same report top-level keys, system metadata, and finding IDs. A live scan
completed, watch emitted valid NDJSON, and SIGINT exited 130. Dynamic readings
and timestamps were not compared for equality and do not establish machine
health. Fixture tests additionally protect policy precedence, confidence,
journal evidence ordering, command configuration, option forwarding, metadata
omission/order, duration boundaries, and cancellation behavior.
