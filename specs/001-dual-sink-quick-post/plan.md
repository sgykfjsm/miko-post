# Implementation Plan: miko-post v0.1 — Dual-Sink Quick Post

**Branch**: `001-dual-sink-quick-post` | **Date**: 2026-09-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-dual-sink-quick-post/spec.md`

## Summary

`miko-post` sends one short message to two independent destinations — Telegram and an Obsidian
daily note — from either a CLI or a minimal Fyne window, and records structured JSONL diagnostics
so a failed post can be reconstructed later.

The technical approach follows directly from the constitution's first two principles. A single Go
module builds one binary (`cmd/mp`) that dispatches to a thin CLI or a thin GUI, both of which
call **one** shared posting service in `internal/post`. That service validates the message, starts
every enabled sink concurrently, waits for all of them, and aggregates their results — never
returning on first error and never letting one sink cancel another. Sinks satisfy a two-method
interface, so the orchestrator owns the per-sink timeout, the result shaping, and the event
vocabulary in one place rather than once per sink.

Phase 0 resolved the five deferrals the specification left to planning (A-007 – A-011) and settled
the library choices. The dependency set is deliberately small — Fyne, go-toml/v2, and ULID, with
`log/slog` covering diagnostics — because two constitutional requirements (no automatic log
deletion, no automatic transport retry) are things popular libraries do **by default**.

## Technical Context

**Language/Version**: Go 1.24 declared in `go.mod`; developed against `go1.27.0 darwin/arm64` (R-002)

**Primary Dependencies**: Fyne v2.8.1 (GUI), `pelletier/go-toml/v2` v2.4.3 (settings),
`oklog/ulid/v2` v2.1.2 (correlation ids), stdlib `log/slog` + `net/http` (R-003 – R-008)

**Storage**: Plain files only — a TOML settings file, an append-only Obsidian daily note, and a
rotating JSONL log. No database.

**Testing**: stdlib `testing`, `net/http/httptest`, `t.TempDir()`, `fyne.io/fyne/v2/test` for
headless widget tests, plus `go test -race` on the orchestrator (R-011)

**Target Platform**: macOS (arm64 and amd64). A-003 scopes the keyboard contract to macOS for v0.1.

**Project Type**: Single-module Go desktop/CLI application distributed via `go install`

**Performance Goals**: None beyond interactive responsiveness. A-004 establishes a single user
posting at low frequency; the only latency bounds are the configured timeouts (30 s per HTTP
request, 60 s per sink).

**Constraints**: Sinks must be testable without contacting live services; no secret may appear in
any log line or user-facing string; appends must be strictly additive; no automatic deletion of
any user data or log file.

**Scale/Scope**: 76 functional requirements, 6 user stories, 2 sinks, 2 front doors, ~12 stable
log events. Rare concurrent posts must remain separable and must not corrupt the note (A-004).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Gate | Status | How the design satisfies it |
|---|---|---|---|
| **I. Sink Independence** (NON-NEGOTIABLE) | No sink can cancel, suppress, or short-circuit another; orchestrator aggregates all results; each sink independently timeout-bounded | **PASS** | Each sink runs in its own goroutine with its **own** `context.WithTimeout` derived from `context.Background()`, not from a shared cancellable parent — so no sibling's failure or timeout can propagate. `AwaitAll` collects every result via `sync.WaitGroup` before returning. Verified under `-race`. |
| **II. One Posting Core, Thin Entry Points** | Validation, sink selection, submission, aggregation, logging live in the core only | **PASS** | `internal/post.Service` is the sole submission path. `internal/cli` and `internal/gui` differ only in argument parsing, presentation, and lifecycle. Within the GUI, the Send control and `Cmd+Enter` both call one `submit()` (FR-023). |
| **III. Structured, Secret-Free Observability** | JSONL, stable event names, per-post correlation id, every failure logged, no secrets | **PASS** | `slog` JSON handler; event names as typed constants with a test asserting the full set; ULID `message_id`; the token is a `Secret` type whose `LogValue()`/`String()`/`MarshalJSON()` redact, reachable only via one `Reveal()` call site. |
| **IV. Fail-Visible, Fail-Safe** | Partial success visible; short safe reasons plus log path; exit `0` only on full success; startup fails on invalid or all-disabled settings; validation before any sink | **PASS** | `SinkResult` structurally separates display `Reason` from diagnostic `Err`. Exit status is computed in one place from the aggregate. Settings load runs the all-disabled check before any sink is constructed. |
| **V. Explicit, XDG-Conventional Configuration** | TOML under XDG; namespaced section names; behavior not configurable; only the token has an env override; `--config` never affects the GUI; help shows resolved paths | **PASS** | Strict TOML decoding rejects the non-normative top-level sections outright (R-004). The GUI is constructed with the default resolved path and never receives the `--config` value — enforced by the call signature, not by convention. |
| **VI. Data Preservation** | Append-only notes, UTF-8/LF, rotated logs retained, nothing auto-deleted | **PASS** | `O_APPEND|O_WRONLY` (+`O_CREATE` only when permitted), never `O_TRUNC`. The hand-written rotating writer has no deletion path at all, and the collision rule appends a disambiguator rather than overwriting (R-006). |

**Additional constraints**: Go + Fyne, single binary — satisfied. `docs/design.md` normative — the
plan contradicts it nowhere. Scope discipline — the spec's Out of Scope list is carried into
`tasks.md` as an explicit non-goal. Retries — the formatting fallback is the *only* second attempt
and is gated behind the narrow predicate in [contracts/telegram-sink.md](./contracts/telegram-sink.md).
Distribution via `go install` — the module path in R-001 makes that work.

**Result: PASS, no violations.** Complexity Tracking is therefore empty.

**Post-Phase-1 re-check: PASS.** The design added no dependency, abstraction, or configuration
surface that any principle argues against. Two decisions were made *because* of the constitution
rather than merely consistent with it: rejecting `lumberjack` (its defaults delete rotated logs,
which principle VI forbids) and rejecting a Telegram SDK (its built-in retry and rate-limit
policies conflict with FR-041 and the Additional Constraints).

## Project Structure

### Documentation (this feature)

```text
specs/001-dual-sink-quick-post/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── checklists/
│   └── requirements.md  # Spec quality checklist (16/16)
├── contracts/           # Phase 1 output
│   ├── cli-interface.md
│   ├── gui-interface.md
│   ├── config-schema.md
│   ├── log-events.md
│   ├── telegram-sink.md
│   └── obsidian-sink.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
go.mod                              # module github.com/sgykfjsm/miko-post

cmd/
└── mp/
    └── main.go                     # Entry point: flag parsing, dispatch, single os.Exit

internal/
├── cli/                            # Thin CLI front door: parse, render, exit code
├── gui/                            # Thin Fyne front door
│   ├── window.go                   #   main window, auto-close, result panel
│   ├── entry.go                    #   extended Entry: TypedShortcut + TypedKey (R-003)
│   └── errorwindow.go              #   minimal startup-failure window (FR-030)
├── post/                           # THE posting core
│   ├── message.go                  #   Message, validation (FR-009 – FR-011)
│   ├── service.go                  #   concurrent start, await-all, aggregate
│   ├── result.go                   #   SinkResult, exit-status rule
│   └── sink.go                     #   Sink interface
├── sink/
│   ├── telegram/                   #   HTTP client, formatting-rescue predicate
│   └── obsidian/                   #   path resolution, transformation, append
├── config/                         # XDG resolution, strict decode, validation, Secret type
├── logging/                        # slog JSON logger, event constants, rotating writer
│   ├── rotate.go
│   └── birthtime_darwin.go         #   build-tagged creation time (+ portable fallback)
└── version/                        # ldflags vars with ReadBuildInfo fallback (R-009)

testdata/                           # sample TOML configs, Telegram response fixtures
```

**Structure Decision**: A single Go module with one `cmd/mp` binary and everything else under
`internal/`. The package boundaries are not decorative — they are the mechanism by which
constitution principle II ("posting core, individual sinks, configuration, GUI, and logging MUST
remain separate responsibilities") is enforced by the compiler rather than by review. `internal/`
also prevents any of this becoming an accidental public API, which matters because
`docs/design.md` §6 explicitly marks its `SinkResult` sketch as illustrative rather than a
required Go API.

The two files that look like over-decomposition earn their place: `gui/entry.go` isolates the
extended-widget workaround that FR-021 and FR-022 jointly force (R-003), and
`logging/birthtime_darwin.go` contains the one platform-specific syscall so the rest of the
logging package stays portable and testable (R-006, A-011).

## Complexity Tracking

> No Constitution Check violations. This section is intentionally empty.
