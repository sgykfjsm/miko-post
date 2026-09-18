## Summary
The diagnostic log now captures what a failed post needs to be re-sent by hand, records a real
stack for a panic, and tells a GUI user when diagnostics were lost. Three fields that
`contracts/log-events.md` listed as owed are now delivered: `message` under FR-068's capture rule,
`stack` under FR-071, and — at the front door rather than in a record — FR-076's single warning,
which `internal/gui` never produced.

## Batch
Batch 10b / US5: T066, T067, T071–T074, issues #67, #68, #72–#75. Based on merged main `0e3df5e`
(Batch 10a, PR #126). 10a's rotation work is unchanged.

## What each task did
- **T071** — `message` capture. `internal/app/recorder.go` holds the body from `MessageReceived`
  and attaches it to the records that report a failure.
- **T072** — **no code change; verified.** `message_len` and `message_bytes` were already emitted
  with R-010's semantics. Rather than assert that from reading, three mutants were built — a byte
  count under `message_len`, a rune count under `message_bytes`, and the two swapped — and all
  three are killed by the existing `TestASuccessfulPostWritesTheContractsRecords`. The task is
  complete because the behaviour is right *and* pinned, not because the code looked correct.
- **T073** — `stack`. The trace is captured where the panic is recovered, in `internal/post`, and
  read back through the new `post.Traced` interface.
- **T074** — FR-076's warning for the GUI. `internal/gui` never called `Logger.Degraded()`, so
  diagnostics could fail for a whole session with the user never told (issue #75).
- **T066 / T067** — the tests, in `internal/app/capture_test.go`, `internal/app/degraded_path_test.go`
  and `internal/gui/degraded_test.go`.

## Decisions
- **DEC-G1** — the body goes on **every** record that reports a failure, and on the terminal record
  **only when no failure record could carry it**.

  SC-008 asks for a failed post to be re-sendable "without consulting any other source", and the
  per-sink failure record is the one that already names the destination, the error type and the
  detail — so that is where the body belongs, and a reader grepping `telegram_send_failed` gets
  everything on one line. The terminal fallback exists for one shape: a sink whose `Name` panics
  resolves to a sentinel the event vocabulary does not cover (issue #110), so `SinkStarted` returns
  early, no lifecycle record is emitted, and the terminal record is the post's only record. Without
  the fallback FR-068 is unmet for exactly that post. Applied unconditionally instead, a two-sink
  post that lost both destinations would carry the user's private text three times. Hence a
  *claim* — `claimBody` — rather than a plain read.
- **DEC-G2** — with `message_on_error_only = false` the body goes on `message_received` and on
  nothing else. FR-068 constrains only the enabled case ("*when* error-only capture is enabled,
  successful events MUST omit…"), so the disabled case is a choice. The intake event happens once
  per post; putting the body on every record would repeat it once per destination for no
  reconstruction benefit.
- **DEC-G3** — the stack is captured by `debug.Stack()` **inside `internal/post`'s deferred
  `recover`**, which is outside T073's stated file (`internal/logging/logger.go`).

  This is a genuine prerequisite, not scope creep: `recover()` returns the panic *value* and
  nothing else, and the frames are already unwound by the time the deferred function runs its body
  — entirely gone by the time a `SinkResult` reaches `internal/app`. There is no later point at
  which a trace can be obtained, so FR-071's "traces for panics" is unimplementable without a
  change at the recovery point. The precedent is explicit: `SinkAttempt.NameErr`'s own comment
  records that the panic *value* used to be discarded where it was recovered and that keeping it
  "needed a change at the point of recovery rather than a later task". The trace is the same
  problem one field further on. `panicError`'s comment also anticipated this — "No accessor for it
  yet: T073 can add one when it has a use" — and this is that use.

  The accessor is the `post.Traced` **interface**, not an exported error type, because the only
  thing the recording layer needs is the trace: the recovered value beside it can be a struct
  holding a credential, which is why `describePanic` exists. Capture is unconditional, since
  `internal/post` reads no settings and one `debug.Stack()` on a panic costs nothing; whether the
  trace is *recorded* is `stack_trace`'s business, decided in `internal/app`.
- **DEC-G4** — FR-071's "MUST NOT get an artificially manufactured trace" is discharged **by
  construction**. An expected operational error does not implement `Traced`, so no code path can
  produce a plausible-looking stack for one; the absence of a trace in the record is the absence of
  a trace in the error. The alternative — a list of error types that do and do not deserve a trace
  — would have to be kept in step with `internal/post`'s classifier forever. Two mutants attack
  this directly (below).
- **DEC-G5** — the GUI warns **once per session**, not once per post. `logging.Logger` latches its
  degradation, so `Degraded()` keeps answering for the rest of the session while the window
  outlives every post in it. Rendering that answer on each post would put the warning on the second
  post's result, and the third's — one failure reported many times, which is not what FR-076's
  "exactly one warning" or SC-013's "told exactly once" mean. The accepted consequence is stated
  and tested: a degradation that *begins* mid-session is still reported, on the first post that
  could have been affected by it.
- **DEC-G6** — the warning **replaces** the `Details: <log path>` line on a failed post rather than
  sitting beside it. Pointing a user at a log for details when the log is the thing that failed is
  an instruction to read a file that does not have them. `internal/cli/render.go` already applies
  this reasoning to its own pairing; this makes the two front doors agree.
- **DEC-G7** — `NewRecording` now takes `config.LoggingSettings`. Its zero value means both settings
  off, and for `MessageOnErrorOnly` that is the *less* private direction (body recorded always).
  That is safe only because `config.Defaults()` sets it true and both front doors build from loaded
  settings, which always pass through `Defaults`. That assumption is not left to a reader:
  `TestDefaultsKeepTheMessageBodyOffSuccessfulRecords` asserts it. The field mirrors the contract
  key's name and direction rather than being inverted for a safer zero value, because a field that
  disagrees with the key it implements is worse for a reader than a documented zero value.

## Deviations from the task text, and why
- **T067 is in `internal/app`, not `internal/post/service_test.go`.** The posting core does not
  import `internal/logging` and cannot be handed a logger, so "the log could not be opened" is not
  a condition it can be placed in. `internal/app` is the first layer that wires the two together
  and therefore the first layer where all four of FR-076's clauses are observable at once.
  `internal/post`'s existing tests already cover the part that *is* its own — that a recorder
  cannot affect delivery.
- **T074 did not touch `internal/cli/render.go`.** The CLI front door already emitted the warning
  correctly through `closeAndWarn`/`warningFor`, with its own tests. The task named both files; only
  the GUI half was missing, and adding a second code path to the CLI to satisfy a file list would
  have been a change with no defect behind it.

## Validation
`make check` (gofmt, vet, full `-race` suite) and a native darwin/arm64 build.

`internal/app` is at **100.0% of statements, matching its baseline** — the two guards that a sink
cannot reach are covered through a new `export_test.go` seam rather than left as branches nobody has
executed. `internal/gui` is at **73.4% against a 74.2% baseline**; the entire difference is one new
statement inside `Run`, which is 0% covered at baseline too because it needs a real Fyne app and a
real settings file. Every statement added to `result.go` and `window.go` is covered.

`GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/post/` is clean, and `internal/app`'s
production code builds clean on 386.

**28 mutants built, 27 killed.** The one that survived was not worked around — it proved a guard
unnecessary and the guard was deleted; see below. Highlights:

- **Both "manufacture a trace" mutants are killed**, which is FR-071's prohibition tested rather
  than asserted: a `traceFor` that falls back to `"goroutine 1 [running]:\n" + err.Error()` for any
  error, and a `panicError.stack` pinned to a plausible constant `"goroutine 1 [running]:\nmain.main()"`.
  The trace assertion names a frame from the panicking sink rather than checking the field is
  non-empty, which is what makes both detectable.
- A capture that fires once — a flag consumed by the first failure — is killed by
  `TestBothFailuresCarryTheBody`, the case where the *second* destination is the one a human must
  re-send to.
- Removing the terminal fallback is killed; removing the claim that suppresses it is killed
  separately.
- Seven mutants over T074: a warning that never latches, one that never fires, one dropped in
  rendering, one the window never asks for, the stale `Details:` line, and a degradation that flips
  the exit status.
- **One survivor, one deletion.** `keepTrace` had an explicit `trace == ""` early return in front of
  its first-wins check. Removing it changed no behaviour and failed no test, because storing `""`
  when nothing is stored is a no-op and the first-wins check already rejects an empty offer once a
  real trace is held. It was deleted rather than given a test: an unfailable guard in front of a
  working one is weight a reader must account for and a branch no test can justify. Both remaining
  `keepTrace` mutants — last-wins, and a no-op — are killed.
- **A false kill, caught and corrected.** The first `message_len` mutant "killed" with no test
  named: replacing `utf8.RuneCountInString` with `len` left the `utf8` import unused, so the package
  failed to compile and the run was scored as a kill. Rebuilt as a compiling mutant
  (`len(...)+0*utf8.RuneCountInString(...)`), it is genuinely killed by
  `TestASuccessfulPostWritesTheContractsRecords`. The other two T072 mutants compiled and were
  genuine.

One observation worth recording because the tests did not make it: **the record shape changed and
the pre-existing suite stayed green.** Failure records gained a `message` field and nothing noticed,
because no existing test asserted field completeness on a failure record. That is the gap T066 now
closes, and it is why the new tests assert presence *and* absence rather than values alone.

Not claimed: Intel Mac, a non-darwin platform, a 32-bit *run* (vetted and compiled only), and a
real Fyne session — the GUI warning is driven through the window's dispatch seam, not a live window.

## Scope
No change to the event vocabulary, redaction, rotation, the settings schema, sink behaviour or exit
status. `contracts/log-events.md` is updated: the "what is still owed" table now shows `message` and
`stack` as delivered, and the message-capture and traces sections describe which record carries what
and how the no-manufactured-trace rule is kept. No key, default or validation rule changes.

Deliberately not addressed here — two items, which is the whole list:

1. **`internal/app`'s test file does not compile for a 32-bit target**, pre-existing and newly
   discovered by running 10a's cross-vet gate over this batch's packages:
   `app_test.go:265` uses `int(config.MaxTimeoutSeconds)`, a constant that overflows a 32-bit `int`.
   `app_test.go` is byte-identical to `0e3df5e`, so this is not 10b's, and 10a solved the same shape
   in `internal/logging` by scoping the 64-bit boundary to its own file. Fixing it here would mean
   editing an unrelated test file for a defect this batch did not introduce.
2. **The GUI cannot report a `Close` failure.** `logger.Close` runs in `Run`'s deferred call, after
   `a.Run()` returns and the window is gone, so there is no surface left to show it on — unlike the
   CLI, which closes before it renders. `Degraded()` covers the open failure and every failed write,
   which are the two conditions this front door can still report. Whether a close failure deserves
   a surface of its own in the GUI is a product question.

Both are disclosed rather than silently carried. Issues stay open until this is reviewed and merged;
this PR body carries no closing keyword, so the post-merge closure of #67, #68 and #72–#75 is an
explicit action for whoever merges it — which is the gap that left #66 and #69–#71 open for two days
after #126.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
