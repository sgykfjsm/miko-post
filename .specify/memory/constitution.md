<!--
Sync Impact Report
==================
Version change: (uninitialized template) → 1.0.0
Bump rationale: Initial ratification. All placeholder tokens replaced with concrete,
project-specific governance derived from the approved MVP design document
(docs/design.md, normative; docs/design.ja.md is a translation).

Modified principles:
  [PRINCIPLE_1_NAME] → I. Sink Independence (NON-NEGOTIABLE)
  [PRINCIPLE_2_NAME] → II. One Posting Core, Thin Entry Points
  [PRINCIPLE_3_NAME] → III. Structured, Secret-Free Observability
  [PRINCIPLE_4_NAME] → IV. Fail-Visible, Fail-Safe Behavior
  [PRINCIPLE_5_NAME] → V. Explicit, XDG-Conventional Configuration
  (added)            → VI. Data Preservation

Added sections:
  Additional Constraints (was [SECTION_2_NAME])
  Development Workflow and Quality Gates (was [SECTION_3_NAME])

Removed sections: none

Deferred TODOs: none
-->

# miko-post Constitution

## Core Principles

### I. Sink Independence (NON-NEGOTIABLE)

Every enabled sink MUST be invoked concurrently and MUST run to completion independently of
every other sink. A failure, timeout, or panic in one sink MUST NOT cancel, suppress,
short-circuit, or alter the outcome of any other sink. The orchestrator MUST wait for and
aggregate every enabled sink result before returning; it MUST NOT return on first error.
Disabled sinks are not invoked and MUST NOT count as failures. Every sink invocation MUST be
bounded by its own independently configurable overall timeout, and a timed-out sink MUST yield
a failure result rather than an abort of the post.

Rationale: The tool exists to preserve a thought in more than one place. A design where one
destination can silently take down another destroys the only property that justifies having
two sinks.

### II. One Posting Core, Thin Entry Points

The CLI and the GUI MUST be thin entry points over a single shared posting service. Message
validation, sink selection, submission, result aggregation, and logging MUST live in the shared
core, never duplicated per interface. Within the GUI, buttons and keyboard shortcuts MUST share
the same validation and submission paths. Interface code MAY differ only in argument parsing,
presentation, and lifecycle. Posting core, individual sinks, configuration, GUI, and logging
MUST remain separate responsibilities.

Rationale: Duplicated submission logic is how two interfaces of the same tool silently diverge
in what they accept, what they send, and what they record.

### III. Structured, Secret-Free Observability

All application logging MUST be JSON Lines, one independently valid JSON object per line, using
stable event names and a per-post correlation identifier so concurrent sink events can be
reassembled. Every sink failure from a single post MUST be logged; logging MUST NOT stop after
the first error. Secrets — in particular the Telegram bot token — MUST NEVER appear in logs,
UI text, CLI output, error messages, or configuration dumps. Message bodies MAY be omitted on
success, but when any sink fails the original message MUST be recorded so the post can be
reconstructed.

Rationale: The failure mode this tool must survive is a lost message discovered hours later.
Logs are the record of record, and they are useless if they are unparseable, incomplete, or
unsafe to share.

### IV. Fail-Visible, Fail-Safe Behavior

Partial success MUST be visible. Both interfaces MUST identify every failed sink and give a
short, safe, human-readable reason for each, plus the path to the detailed log. Detailed
diagnostics, stack traces, and internal error chains belong in logs, not in user-facing output.
The process MUST exit `0` only when all enabled sinks succeeded, and `1` for input,
configuration, startup, or any sink failure. Startup MUST fail with an actionable message —
rather than proceeding — when configuration is invalid, when configuration is only partially
valid, or when every sink is disabled. Input validation MUST run before any sink is invoked.

Rationale: A tool that reports success on a half-delivered message is worse than one that
fails loudly, because it removes the user's chance to retry.

### V. Explicit, XDG-Conventional Configuration

Configuration MUST be TOML resolved from XDG conventions, with state and logs under the XDG
state path. The `[sink.telegram]` and `[sink.obsidian]` section names are normative. Application
behavior — exit codes, UTF-8/LF encoding, sink concurrency, the Obsidian line transformation,
and the Telegram format-fallback algorithm — MUST remain application behavior and MUST NOT be
exposed as configuration options. Only the Telegram bot token supports an environment-variable
override. An explicitly selected configuration file applies to CLI posting only and MUST NEVER
change the configuration used by the GUI. User-facing help MUST show paths resolved for the
current environment, not unresolved variable expressions.

Rationale: Configurable behavior is behavior that can silently differ between two runs of the
same tool. Keeping the algorithm out of the config file keeps the semantics one thing.

### VI. Data Preservation

Appends to a user's notes MUST open the target in append mode and MUST NEVER rewrite, truncate,
or reorder existing content. Written text MUST be UTF-8 with LF line endings. Rotated logs MUST
be retained; the application MUST NOT automatically delete or expire any user data or log file.
Destructive behavior MUST NOT be introduced without an explicit amendment to this constitution.

Rationale: This tool writes into a personal knowledge vault that the user did not ask it to
manage. Its write privilege is strictly additive.

## Additional Constraints

- **Language and toolchain**: Go, with the GUI built on Fyne. A single binary MUST provide both
  the CLI and GUI entry points.
- **Normative source**: `docs/design.md` is normative. `docs/design.ja.md` is a translation; if
  the two differ, the English document takes precedence. Specifications MUST NOT contradict the
  approved design document without an explicit, recorded decision.
- **Scope discipline**: Work explicitly listed as out of scope for a version MUST NOT be
  implemented in that version, even opportunistically. For v0.1 this includes image posting, a
  configuration GUI, retry queues, stdin input, automatic HTTP retries, per-invocation sink
  flags, automatic log retention, and additional sink types.
- **Retries**: The MarkdownV2-to-plain-text path is a format fallback, not a transport retry.
  General automatic HTTP retry MUST NOT be introduced without an amendment.
- **Distribution**: Installation targets the Go toolchain via `go install`. Platform bundles,
  code signing, and notarization are outside the v0.1 distribution requirement.

## Development Workflow and Quality Gates

- Every acceptance criterion in the governing specification MUST have a corresponding automated
  test or an explicitly recorded justification for why it is verified manually.
- Concurrency, timeout, and partial-failure behavior MUST be covered by tests that assert both
  sinks ran and both results were reported — not merely that the happy path returns success.
- Tests MUST assert that no secret appears in produced log lines or user-facing output.
- Sink implementations MUST be testable without contacting live external services.
- Changes MUST NOT weaken an existing principle to make a task easier; the amendment procedure
  below is the only route to a behavior change that conflicts with this document.

## Governance

This constitution supersedes other conventions and ad-hoc practices in this repository. When a
specification, plan, task, or review comment conflicts with a principle here, this document
wins and the conflicting artifact MUST be corrected.

**Amendments**: Any change to a principle MUST be recorded as an edit to this file with an
updated Sync Impact Report, a version bump, and a stated rationale. Amendments that remove or
redefine a principle in a backward-incompatible way require explicit human approval before the
dependent specifications are updated.

**Versioning**: This constitution follows semantic versioning. MAJOR for backward-incompatible
governance or principle removal/redefinition; MINOR for a newly added principle or materially
expanded guidance; PATCH for clarifications, wording, and non-semantic refinements.

**Compliance review**: Every plan and every pull request MUST be checked against these
principles before merge. Added complexity MUST be justified against the principle it serves.
Unresolved ambiguity MUST be surfaced explicitly rather than resolved by silent assumption.

**Version**: 1.0.0 | **Ratified**: 2026-09-01 | **Last Amended**: 2026-09-01
