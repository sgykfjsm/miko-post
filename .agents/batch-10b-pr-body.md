## Summary
The diagnostic log now captures what a failed post needs to be re-sent by hand, records a real
stack for a panic, and tells a GUI user when diagnostics were lost. Two fields that
`contracts/log-events.md` listed as owed are delivered — `message` under FR-068's capture rule and
`stack` under FR-071 — and FR-076's single warning now reaches the GUI front door, which never
produced one.

## Batch
Batch 10b / US5: T066, T067, T071–T074, issues #67, #68, #72–#75.

**This PR carries three commits, not two.** `e0a0c39` is the Batch 10a record reconciliation, which
is not on `origin/main`; `38a45fe` and `159e3dc` are 10b. See *Carried prerequisite* below — that
commit needs a decision before this merges.

## What each task did
- **T071** — `message` capture in `internal/app/recorder.go`. The body is held from
  `MessageReceived` and attached to the records that report a sink's own failure.
- **T072** — **no code change; closed by verification.** `message_len` and `message_bytes` were
  already emitted with R-010's semantics. Rather than assert that from reading, three mutants were
  built — a byte count under `message_len`, a rune count under `message_bytes`, and the two swapped
  — and all three are killed by the existing `TestASuccessfulPostWritesTheContractsRecords`. The
  residual T040 recorded for this task ("stays open for whichever other records need them") is
  answered rather than dropped: none do, because the counts describe the post, `message_received`
  is emitted once per post, and every record shares its `message_id`.
- **T073** — `stack`. The trace is captured where the panic is recovered, in `internal/post`, and
  read back through the new `post.Traced` interface.
- **T074** — FR-076's warning for the GUI. `internal/gui` never called `Logger.Degraded()`, so
  diagnostics could fail for a whole session with the user never told (issue #75).
- **T066 / T067** — the tests, in `internal/app/capture_test.go`,
  `internal/app/degraded_path_test.go` and `internal/gui/degraded_test.go`.

## The defect this batch's own review found
The first implementation attached the body to **every** failure-shaped record, including the
formatting-fallback records. Those are emitted from inside `Send`, before the post's outcome exists,
and FR-039's plaintext rescue means a failed markdown attempt is routinely followed by a *successful*
one — so `telegram_markdown_failed` is a failure-shaped record inside a post that fully succeeded.

**Every rescued post therefore wrote the user's private message body into the log**, breaching
FR-068's privacy half on the most ordinary path there is: the rescue exists because Telegram rejects
ordinary punctuation. The contract stage caught it (CON-001); a failing test was written first to
confirm it rather than take it on trust, then the fix. `TestARescuedPostIsASuccessAndRecordsNoBody`
and `TestAFailedRescueRecordsTheBody` are the two halves, and the regression mutant — putting the
body back on those records — is killed. This is **DEC-G1a**.

## Decisions
Defined as structured blocks in `.agents/state.yaml` under `in_progress` 10b, where DEC-A1 through
DEC-F4 live, and summarised here. (The first draft defined them only in this file; `state.yaml`
records that 10a's PR description was replaced wholesale after review, so seven rationales were one
rewrite away from being unreachable — contract finding CON-009.)

- **DEC-G1** — the body goes on every record that reports **a sink's own outcome** as a failure (the
  `*_failed` events from `SinkFinished`), and on the terminal record **only when no failure record
  could carry it**. SC-008 asks for a failed post to be re-sendable "without consulting any other
  source", and the per-sink failure record already names the destination, the error type and the
  detail. The terminal fallback covers the one shape producing no per-sink record: a sink whose
  `Name` panics resolves to a sentinel the vocabulary does not cover, so `SinkStarted` returns early
  and the terminal record is the post's only record. Repetition is bounded by the number of
  destinations that actually failed — one record each, plus the terminal record only when there were
  none.
- **DEC-G1a** — the formatting-fallback records carry **no** body. See above.
- **DEC-G2** — with `message_on_error_only = false` the body goes on `message_received` and nowhere
  else. FR-068's two clauses are scoped differently and **only the first is conditional**: "*When*
  error-only message capture is enabled, successful events MUST omit message content". The second —
  "if any destination failed, the original message body needed to reconstruct that post MUST be
  recorded" — binds in both modes, and in the disabled mode the intake record satisfies it, sharing
  the post's `message_id` so it joins to every failure. What FR-068 leaves open is only the
  *placement*. (An earlier draft of this decision claimed FR-068 "constrains only the enabled case",
  which stops the quotation before the second clause's unconditional MUST — contract finding
  CON-004. The behaviour was compliant; the justification was not, and it had been asserted as fact
  in the contract document.)
- **DEC-G3** — the stack is captured by `debug.Stack()` **inside `internal/post`'s deferred
  `recover`**, outside T073's stated file. `recover()` returns the panic *value* and nothing else,
  and the frames are already unwound when the deferred function runs its body — gone entirely by the
  time a `SinkResult` reaches `internal/app`. There is no later point at which a trace can be
  obtained, so FR-071's "traces for panics" is unimplementable without a change at the recovery
  point. The precedent is explicit: `SinkAttempt.NameErr`'s comment records that the panic value used
  to be discarded where it was recovered and that keeping it "needed a change at the point of
  recovery rather than a later task". The accessor is the `post.Traced` **interface**, not an
  exported error type, because the recovered value beside the trace can hold a credential.
- **DEC-G4** — FR-071's ban on manufactured traces is discharged **by construction**: an expected
  operational error does not implement `Traced`, so no code path can invent a stack for one.
- **DEC-G5** — the GUI warns **once per session**. `logging.Logger` latches its degradation and the
  window outlives every post, so re-rendering it would report one failure many times. A degradation
  that *begins* mid-session is still reported, on the first post that could have been affected.
- **DEC-G6** — the warning **replaces** the `Details: <log path>` line rather than joining it.
- **DEC-G7** — `NewRecording` takes `config.LoggingSettings`, whose zero value is the less private
  direction; `TestDefaultsKeepTheMessageBodyOffSuccessfulRecords` pins that the shipped default is
  the safe one.
- **DEC-G8** — **"unexpected errors" yields no `stack`, and that is FR-071 met rather than partly
  delivered.** The requirement qualifies all three of its categories with "where a trace is
  *available* and useful". A plain Go error carries no frames; a panic is the only failure this
  program can obtain real ones for. For an unexpected non-panic error there is nothing to record and
  the same requirement forbids inventing something, so the absent field honours FR-071 from both
  sides. A future error type that captures its own stack need only implement `post.Traced`.
  (Contract finding CON-005: the box had been ticked with this clause neither delivered nor
  discussed.)

## Deviations from the task text — five, and why
`tasks.md` now carries a correction sub-bullet under each, following the T039 precedent at
`tasks.md:139`, because `tasks.md` is the durable artifact and this PR body is not.

1. **T066** is in `internal/app/capture_test.go`, not `internal/logging/logger_test.go`.
2. **T071** is in `internal/app/recorder.go`, not `internal/logging/logger.go`. The rule needs the
   post's outcome and the setting; `internal/logging` receives finished records. T040's own note
   had already assigned the rule to the adapter.
3. **T072** is in `internal/app/recorder.go` and needed no code change at all.
4. **T073** changed `internal/post/service.go` **and** `internal/app/recorder.go`, neither of them
   its stated `internal/logging/logger.go`. See DEC-G3.
5. **T067** is in `internal/app`, not `internal/post/service_test.go`: the posting core imports no
   `internal/logging` and cannot be handed a logger, so "the log could not be opened" is not a
   condition it can be placed in. Three of FR-076's four clauses are asserted there *together* —
   every sink runs, real outcomes reported, exit status tracks the post. The fourth, *exactly one*
   warning, is not observable in a package with no user-visible output; it is carried at the two
   front doors, by `internal/gui/degraded_test.go` across three posts in one session and by the
   pre-existing `internal/cli/cli_test.go` tests.

**T074 did not touch `internal/cli/render.go`**, which its task text names. The CLI half already
worked, through `closeAndWarn`/`warningFor`, with its own tests; only the GUI half was missing.
Adding a second CLI code path to satisfy a file list would be a change with no defect behind it.

## Validation
`make check` (gofmt, vet, full `-race` suite) and a native darwin/arm64 build, re-run after the
CON-001 fix.

`internal/app` is at **100.0% of statements, matching its baseline**, and `internal/logging` at
**99.5%**, unchanged by the new query — the two guards a sink cannot
reach are covered through a new `export_test.go` seam rather than left as branches nobody has
executed. `internal/gui` is at **73.8% against a 74.2% baseline**; the entire difference is one new
statement inside `Run`, which is 0% covered at baseline too because it needs a real Fyne app and a
real settings file.

`GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/post/` is clean, and `internal/app`'s
production code builds clean on 386.

**37 mutants built, 35 killed.** Highlights:

- **Both "manufacture a trace" mutants are killed**, which is FR-071's prohibition tested rather
  than asserted: a `traceFor` falling back to `"goroutine 1 [running]:\n" + err.Error()`, and a
  `panicError.stack` pinned to a plausible constant. The trace assertion names a frame from the
  panicking sink rather than checking the field is non-empty, which is what makes both detectable.
- **The CON-001 regression is killed** — restoring the body to the formatting-fallback records fails
  `TestARescuedPostIsASuccessAndRecordsNoBody` — and the opposite mutant, dropping the body from
  `SinkFinished`, is killed by three tests, so SC-008 stays pinned.
- A capture that fires once is killed by `TestBothFailuresCarryTheBody`, the case where the *second*
  destination is the one a human must re-send to.
- Seven mutants over T074: a warning that never latches, one that never fires, one dropped in
  rendering, one the window never asks for, the stale `Details:` line, and a degradation that flips
  the exit status.
- **Two survivors, neither worked around.** `keepTrace`'s explicit `trace == ""` early return changed
  no behaviour and failed no test — storing `""` when nothing is stored is a no-op, and the
  first-wins check already rejects an empty offer — so it was deleted rather than given a test. The
  same reasoning removed a `traceFor` call from `FormattingFinished` during the CON-001 fix: a
  `FormattingAttempt`'s error comes from an HTTP exchange, never a panic, so that branch was
  unreachable even from a test. The second survivor, a reordering of `degradationWarning.check`, was
  shown to be an **equivalent mutant** rather than a coverage gap: `warned` is only ever set after a
  successful consult, so both orders behave identically on the healthy, degraded and
  healthy-then-degraded sequences.
- **A false kill, caught and corrected.** The first `message_len` mutant "killed" with no test named:
  replacing `utf8.RuneCountInString` with `len` left the `utf8` import unused, so the package failed
  to compile and a non-zero exit was scored as a kill. Rebuilt as a compiling mutant, it is genuinely
  killed. Every later run checks for a build failure before scoring.

One observation worth recording because the tests did not make it: **the record shape changed and the
pre-existing suite stayed green.** Failure records gained a `message` field and nothing noticed,
because no existing test asserted field completeness on a failure record. That is the gap T066 closes,
and why the new tests assert presence *and* absence rather than values alone.

Not claimed: Intel Mac, a non-darwin platform, a 32-bit *run* (vetted and compiled only), and a real
Fyne session — the GUI warning is driven through the window's dispatch seam, not a live window.

## Review status
All three stages have run, and **two fix passes** are applied.

- **Contract: `invalid`**, ten findings. CON-001 was a real defect and is fixed; CON-003 through
  CON-010 were record, contract-text and decision-record corrections, all applied. **CON-002 is not
  fixed** — see the carried prerequisite below.
- **Correctness: `request-changes`**, five findings. All applied.
- **Adversarial: `findings`**, four findings. Three applied; ADV-003 is a product decision and is
  filed rather than fixed.

**The two stages found the same two defects independently** (`COR-002 == ADV-002`,
`COR-001 == ADV-004`), which is the strongest signal in this review.

### The blocker in the GUI warning — my own code, fixed
`internal/gui` called `Logger.Degraded()` after every post, on the Fyne event goroutine. `Degraded()`
calls `Flush()`, which submits a barrier and waits 250 ms; on timeout it calls `failLocked`, whose
own comment reads *"disables this logger permanently"*. So one slow write — a vault on a network
mount, an fsync after a wake from sleep — **discarded every record of every later post in the
session, and froze the window for a quarter second per post.** The code added to satisfy FR-076
could cause the exact outage FR-076 exists to report. Demonstrated as **1 surviving record versus
12** under identical conditions. The CLI was never exposed: it asks once, after `Close`.

Fixed by adding `logging.DegradedSoFar()`, which reads only the two already-settled states —
`openErr` and the writer's latched error — and submits no barrier. What it gives up is synchronicity:
a write that failed but is still queued becomes visible on the next call, or at `Close`. An open
failure, the common case and the one a user can act on, needs no flush at all.

**The fix was completely unguarded, and only a mutant showed it.** Swapping the GUI back to
`Degraded` survived the entire suite, because every test in `internal/gui` supplies `degraded` as a
closure that answers instantly and none can observe a flush barrier.
`TestTheWindowNeverAsksTheFlushingDegradedQuery` now pins the wiring with an AST scan, following
T039's `os.Exit` precedent in `cmd/mp` — an AST scan rather than a grep because `run.go`'s comments
legitimately name `Degraded` while explaining why it is not called.

### The other two
- **ADV-001** — the `Details: <log path>` suppression was keyed on `warning != ""`. The warning is
  spent once per session, so if it was spent on a *successful* post, every later **failed** post got
  the stale invitation back — pointing the user at an unwritten log on the one post they actually
  need to diagnose. My own test only covered the case where the first post is the failing one, which
  is why the suite was green. `check()` now returns `(warning, lost)` and the suppression keys on
  `lost`.
- **COR-001 / ADV-004** — a panic in `Name()` on a post that otherwise succeeds lost its trace,
  while the contract text this batch wrote asserted the terminal record carries it. The two
  requirements are scoped differently and had been conflated: FR-068 is scoped to the post's
  *outcome*, FR-071 to trace *availability*. The trace now goes on the successful terminal record;
  the body still does not, and a mutant that adds both is killed by two tests.

Test-quality findings applied: `fieldOf` now fails loudly on duplicate event names rather than
silently checking the first (COR-003); a dominated, unfailable assertion dropped from T067's test
(COR-004); and the defaults test renamed to say what it asserts, since the old name promised a
record-level property its body never checked (COR-005).

### What the adversarial stage could not break
Worth recording as much as what it could. **No credential leak was constructible through either new
field.** A `debug.Stack()` dump renders frame arguments as hex words and never string contents, and
`describePanic` renders only the panic value's type — a sink whose receiver holds a `config.Secret`,
panicking with a struct carrying a second credential not in `Options.Redact`, leaked neither. The
CON-001 rescue fix was independently re-checked against the **real** telegram sink over HTTP and
holds.

## Carried prerequisite — needs a decision before merge
This PR carries `e0a0c39`, the Batch 10a record reconciliation, which is not on `origin/main`.
`.agents/state.yaml` — written by that very commit — says of it: *"Resolve before either record is
merged to main; they are two independent reconciliations of one event."* The competitor is `d057f45`
on the pushed branch `sgykfjsm/batch-10b-diagnostics`, which records the same 10a merge as
`passed-with-notes` over two cycles where the run manifest shows three cycles and an adversarial
stage that never completed.

Merging this PR as it stands publishes one side of that conflict and buries the other. Either
reconcile the two records first, or split `e0a0c39` out of this branch.

## Scope
No change to the event vocabulary, redaction, rotation, the settings schema, sink behaviour or exit
status. `contracts/log-events.md` is updated: the field table now records which batch delivered each
formerly-owed field rather than claiming any are still owed, the message-capture section says which
records carry the body and which deliberately do not, and the traces section carries DEC-G8.

Deliberately not addressed — three items, which is the whole list:

1. **`internal/app`'s test file does not compile for a 32-bit target**, pre-existing and newly
   discovered by running 10a's cross-vet gate over this batch's packages: `app_test.go:265` uses
   `int(config.MaxTimeoutSeconds)`, which overflows a 32-bit `int`. `app_test.go` is byte-identical
   to `0e3df5e`, so this is not 10b's; 10a solved the same shape in `internal/logging` by scoping
   the 64-bit boundary to its own file.
2. **The GUI cannot report a `Close` failure.** `logger.Close` runs in `Run`'s deferred call, after
   `a.Run()` returns and the window is gone, so there is no surface left — unlike the CLI, which
   closes before it renders. The open failure and every latched write failure are the two this front
   door can report.
3. **The captured body is unbounded** — recorded as open question **DEC-ADV-A** in `state.yaml`, to
   be filed as an issue. The adversarial stage demonstrated it: one 15 MiB message with two failing
   sinks produced 2x amplification on disk and a single record **15x the configured rotation
   threshold**, because rotation is evaluated *before* the write so a threshold cannot bound a
   record. `post.Message.Validate` imposes no length bound and the chat sink applies no client-side
   cap. Not a regression — no record carried the body before this batch — but SC-008 ("re-sendable
   from the log alone") and FR-072/FR-074 (a size control, and never deleting an archive) pull in
   opposite directions, so what the log promises here is a product decision. Three options and their
   costs are recorded.

Issues stay open until this is reviewed and merged. **This PR body carries no closing keyword**, so
closing #67, #68 and #72–#75 after merge is an explicit human action — which is the gap that left
#66 and #69–#71 open for two days after #126, and #60–#65, #115, #118 and #119 open since earlier
batches.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
