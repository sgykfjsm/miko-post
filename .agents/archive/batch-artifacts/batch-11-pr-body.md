## Summary

Batch 11 — **US6 settings resolution**. The CLI and window front doors now resolve settings through one sequence and refuse, before anything can post, settings that cannot be used. It also takes the two issues scoped into this batch on 2026-09-24: #119 (a two-destinations-succeeding test through the CLI front door) and #130 (load-time upper bounds on the rotation keys).

This PR also carries the PR #134 merge record (`13379f8`), following the batch-N-record-in-batch-N+1 pattern.

## Batch

- Batch: `Batch 11 — US6 settings resolution`
- Objective: FR-005, FR-006, FR-007, FR-018, FR-030 and FR-058 met at both front doors, and FR-072's rotation keys bounded from above (#130).

## Related issues

No closing keywords, by design and as in #133: each issue is closed explicitly after merge with a comment naming the merge commit.

- T075–T083 → #76, #77, #78, #79, #80, #81, #82, #83, #84 — all delivered here
- #119 — option B, delivered here (acceptance items 2–4; item 1 was recorded on 2026-09-24)
- #130 — delivered here (a load-time bound, with the clamp kept deliberately)

## Changes

- **FR-006 (T079)**: `cli.Parse` records whether `-c`/`--config` was given (`flag.Visit`) and returns `cli.ErrConfigWithoutMessage` instead of a window request; stderr carries the contract's required text, as `mp: --config is only available when posting from CLI`. `ModeWindow` and a settings override can no longer meet.
- **FR-007 (T075, T080)**: `-h`/`--help` parse to `ModeHelp`; `cmd/mp` prints `cli.Help` to stdout and exits 0. The text is `contracts/cli-interface.md`'s, with the path from `config.DefaultConfigPath`. If no default can be resolved, help still prints and says so. `ErrHelpNotAvailable` is deleted.
- **FR-018, FR-058 (T077, T081, T083)**: `Settings.RequireDestination` / `config.ErrNoDestinationEnabled` (kept out of `Validate`, since the document is valid). `app.LoadSettings` resolves, loads and applies it. Both front doors call it, so every failure returns before a logger, sink or service exists.
- **FR-030 (T082, T083)**: `gui.Run` opens `errorWindow` on any settings failure. It shows the message and the resolved path and has no message field. With no resolvable path (`$HOME` and `XDG_CONFIG_HOME` both unset), no window is opened and the Fyne app is never constructed; stderr gets the error and the exit status is 1 (DEC-H2). Quit, the close box, Esc and Cmd+Q all close it; Quit is a `commandButton` because the driver delivers Esc to the focused widget, not the canvas. The failure branch is `startupFailure`, which prints the error to stderr, shows only the error window, runs the event loop and then exits 1.
- **FR-005 (T076, T078)**: already structural — `gui.Run` takes no path. That is now pinned by a signature test in `cmd/mp`, and `windowSettings` is tested to read the default path.
- **#130**: `config.MaxRotateSizeMiB` / `MaxRotateAfterDays` are the wrap points, the same rule #109 set for the timeouts. The `internal/logging` clamp stays, documented as defence in depth for callers that bypass `Load`.
- **#119**: `cli.Run` delegates to `run(…, newService)`, and `export_test.go` exposes it. The test supplies the service constructor. `TestBothDestinationsSucceedingExitsZero` builds the service with the real `app.Sinks` and `post.New`, swapping the whole chat sink for a succeeding stand-in, so `app.NewService`'s own one-line body is not exercised there.
- **Extra, accepted as DEC-H1**: `-c ""` with a message is now refused (`cli.ErrEmptyConfigPath`) instead of silently posting with the default settings. No numbered requirement asks for this; it is a dispatch-table row in `contracts/cli-interface.md` with its rationale.
- Docs: `contracts/cli-interface.md` "Not yet implemented" table replaced by a "Verification boundary" note (#119 acceptance item 4, plus the error window). `contracts/config-schema.md` rotation rows and paragraph rewritten, `data-model.md` notes where the FR-018 rule lives, and `tasks.md` T075–T083 are ticked, with delivery notes where the delivery differs from the task text.

## Validation

- [x] `make check` (gofmt, vet, `go test -race ./...`) green across all ten packages
- [x] Coverage: `internal/cli` 100.0%, `internal/app` 99.6%, `internal/config` 99.1%
- [x] **Mutation**: 39 of 39 killed after fix pass 1. After fix pass 2, all 37 distinct mutants are killed; the list is in `state.yaml` `mutants_fix_pass_2`. Two are retired: `ew-nopathfallback`, whose code DEC-H2 removed, and the seam-bypass mutant, which is excluded from the harness because by construction it sends to the real Bot API. This includes the `sinks[:1]` truncation in `app.Sinks`, killed in `internal/cli` (#119's acceptance), and a regression reintroducing FR-006's window, which now fails in about 32 s instead of hanging.
- [x] Native darwin/arm64, **re-run on the final tree** (HEAD `25a4f1d` + fix passes 1–2): `mp --help` prints the resolved path and exits 0; `mp -c ./c.toml` prints the required text and exits 1.
- [x] Native: `mp` with an all-disabled default config keeps the startup-error window open, writes the actionable message to stderr, and creates no log file (no post was set up). With `HOME` and `XDG_CONFIG_HOME` both unset it exits 1 and writes nothing in the working directory (DEC-H2). With `HOME` unset but `XDG_CONFIG_HOME` set, the path resolves, the window opens, and Fyne still writes `Library/` and `fyne/` into the working directory. The posting window already did that in the same environment before this batch; it is recorded as a follow-up rather than fixed here.
- [ ] **Not verified natively**: dismissing the error window by any real keypress or click, and the exit status that follows. osascript was blocked on Accessibility permission, and the observed exit 1 came from termination, not dismissal. The native check is owned by T091 / #92 (the quickstart FR-030 scenario). Dismissal is covered headlessly for Quit, the close box, Esc (focused and unfocused) and Cmd+Q, and exit 1 after dismissal is covered by `TestAStartupFailureShowsTheErrorWindowAndExitsOne`.

## Out of scope

- Batch 12 (T084–T091).
- #115, #118, #101 and the other open follow-ups.

## Review

Staged review, `review-and-fix`. **Cycle 0**: the contract was valid; findings were CON-001–004, COR-001–003 and ADV-001–003. **Fix pass 1** addressed all of them:

- **Esc was dead in the real driver** (COR-001/ADV-002). The focused Quit button swallowed Esc, and the test bypassed focus routing.
- **`gui.Run`'s failure branch had no test at all** (ADV-001/COR-002/CON-003).
- The `-c ""` row was missing from the contract (CON-001).
- The records were stale (CON-002).
- The rotation message claimed a harm the #126 clamp had already prevented, and there was no compatibility note (ADV-003).
- An FR-006 regression hung the process tests (COR-003).
- The T037/T039 notes were stale (CON-004).

**Cycle 1** re-reviewed the full diff; the contract was valid. Its findings were:

- COR-001: the path argument to the error window was unasserted.
- ADV-002 and ADV-003: two tests would reach the real Bot API under the very regressions they guard against.
- CON-002: the durable docs overstated what was verified for native dismissal.
- COR-002: the rotation rationale described the wrong overflow mechanism. A wrapped threshold is disabled or becomes arbitrary; it never rotates on every write.
- ADV-001: with `HOME` and `XDG_CONFIG_HOME` both unset, the window path wrote Fyne storage into the working directory.
- CON-003 and ADV-004: doc fixes.

**Fix pass 2** addressed all of these. Cycle 2: re-review of the full diff after the decisions.

## Maintainer decisions (2026-09-24)

1. **DEC-H1**: refusing `-c ""` with a message is **accepted** (CON-001). It is recorded as a dispatch-table row in `contracts/cli-interface.md`.
2. **DEC-H2**: with no resolvable settings path, the window front door **prints to stderr and exits 1 without a window** (ADV-001). The Fyne app is never constructed, so nothing is written relative to the working directory.

## Post-merge closure notes

- **#83**: dismissal and exit 1 are verified headlessly only. The native check is owned by T091 / #92.
- **#119**: the test supplies the service constructor and swaps the whole chat sink, not only the network call.
- **#130**: the closing comment must correct the issue's own rationale. A wrapped threshold disables rotation or becomes arbitrary (`2^44+1` MiB wraps to 1 MiB); it never rotates on every write. Values above the limit are now refused at load.
- **#126**: its comment carries the same wrong mechanism. That is recorded as a follow-up, not fixed here.

**Cycle 2** re-reviewed the full diff; the contract was valid and correctness found nothing required. Adversarial found:

- ADV-002: the PR body and records overstated DEC-H2 as "no HOME". This is reworded.
- ADV-001: with `HOME` unset but `XDG_CONFIG_HOME` set, Fyne still writes into the working directory. This is recorded as a follow-up covering both windows, since the posting window did the same before this batch.

The non-blocking COR-001, a stale test comment, is also fixed.

## Notes for reviewers

- `TestWindowStartupRejectsInvalidDefaultSettings` (cmd/mp) is **removed, not rewritten**. Since FR-030 the binary waits on a window there, so a process test would hang. Its coverage has moved to `internal/gui` (`startupFailure` and the error window) and `internal/app`; the reason is recorded in `main_test.go`.
- **Compatibility**: a `rotate_size_mib` or `rotate_after_days` above its limit used to load and be clamped to "effectively never". It is now refused at load time with the limit to use instead. This is documented in `contracts/config-schema.md`.
- Project records are reconciled in this PR: `state.yaml` `in_progress`, `project-status.md` and `work-log.md`.
- `Render`'s `noDestinationsLine` is now unreachable from `Run` and kept for an empty `Report`.
- The #119 test replaces the chat sink by name after `app.Sinks` builds it, and fails if there was nothing to replace.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
