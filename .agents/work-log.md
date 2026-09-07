# Work log — miko-post

Append-only. Newest entries at the bottom.

---

## 2026-09-03 — Batch 2: posting core contracts

**Objective.** Execute one `run-batch-cycle` iteration: triage the remaining issues, implement the
single best next batch, review it, and stop.

**Changes made.**

- Triaged 89 open issues. Phase 2 (Foundational, 21 tasks) is too large for one PR and decomposes
  into four independent concerns: posting core contracts, settings, logging, orchestrator. The
  first three are mutually independent; the orchestrator depends on all three.
- Selected and implemented **Batch 2 — posting core contracts** (T006-T010, issues #7-#11):
  `Message` + `Validate`, `SinkResult` + `AllSucceeded`, the two-method `Sink` interface, and
  table-driven tests.
- Applied two authorized review fix passes to `internal/post/result.go` and `result_test.go`.
- Committed as `447cf53`; pushed; opened PR #97, merged to `main` as `3214bbd`.
- Filed issue #98 recording the note-event ownership decision.

**Evidence.**

- `make check` (gofmt + `go vet ./...` + `go test -race ./...`) clean; `go build ./...` clean.
- 10 top-level tests, 86 subtests in `internal/post`.
- Bot-token leak surface: verb sweep across 52 letters x 5 flag sets went from **205/260 leaking**
  to **0/260**; slog JSON and Text handlers, `json.Marshal` and all `fmt` surfaces clean.
- Staged review across 3 cycles (contract / correctness / adversarial each cycle), 2 fix passes.
  Terminal verdict `passed-with-notes`: 0 blockers, 0 should-fix.
- Full review state under `~/.agents/review-runs/sgykfjsm__miko-post/20260902T040356Z-79b5bd57/`.

**Decisions.**

1. `AllSucceeded(nil)` is `false`, not vacuously `true` — fail-closed on the exit status.
2. `SinkResult` gained five render guards after review found a real leak: the Telegram sink puts
   the bot token in the request path, so `net/http` returns a `*url.Error` carrying it, and `%v`
   plus `json.Marshal` printed it verbatim. Chose redaction guards over unexporting `Err`.
3. Note events keep orchestrator ownership; the sink exposes its target (issue #98).
4. `Message` gets no rune/byte accessors — follows T006/T072 over `data-model.md`.

**Blockers and open questions.**

- Six non-blocking review follow-ups were accepted but **not filed as issues**; they exist only in
  the review run state named above. Highest-value: `xml.Marshal` still reaches `Err`; the
  method-enumeration test skips argument-taking methods and logs the skip invisibly under
  `make check`; the `%p` residual relies on an unenforced "every error is a pointer" invariant that
  belongs in T056's acceptance notes.
- `data-model.md` is stale in two places (the `SinkResult` entity omits the render guards; the
  empty-slice decision is recorded only in a Go doc comment). Both for spec-reconciler at close.
- Issue #94 (`git_commit` is `unknown` on tagged installs) remains an open maintainer decision,
  needed before `v0.1.0` is tagged.

**Next best action.** Run `run-batch-cycle` for Batch 3 — Settings (T011-T020, issues #12-#21).

---

## 2026-09-04 — Batch 3: the settings foundation

**Objective.** Complete the Settings concern of Phase 2 (T011-T020, issues #12-#21) and pin the
go-toml third of the re-scoped T003.

**Changes made.** `internal/config/` gained `secret.go`, `paths.go`, `settings.go`, `load.go`,
`validate.go`, `credential.go` with their tests, plus five TOML fixtures under `testdata/config/`.
`github.com/pelletier/go-toml/v2@v2.4.3` is pinned as a direct requirement with `go.sum` populated.
PR #100, two commits: `72d9642` (the batch) and `3bc2f32` (three review fix passes).

**Evidence.** `go build`, `go vet`, `gofmt -l`, `go test`, `go test -race -count=15` all clean;
`internal/config` statement coverage 99.0%, the two uncovered statements identified by file:line.
Suite green under seven ambient `TZ` values and under a crafted transitioning TZif. Review verdict
**passed-with-notes** after three fix cycles; run state under
`~/.agents/review-runs/sgykfjsm__miko-post/20260903T103000Z-1047541d/`.

**Decisions.**

1. `Secret` holds its value behind a `*string` and implements five render guards, not the four
   `data-model.md` prescribes. Verified: `fmt` reaches unexported fields by reflection, so `%d` on a
   string-held Secret printed the token, and `%p`/`%w` bypass `Formatter` entirely — the pointer is
   what closes those two.
2. `Load` never renders the settings document. go-toml's `DecodeError.String()` echoes context lines
   from the enclosing table header, which reproduces `bot_token` for any defect in
   `[sink.telegram]`; only position and key path are used.
3. The two Obsidian format keys are validated by **what they render**, and Go's `MST` element is
   rejected. Three attempts were needed and the first two were wrong in instructive ways: checking
   only whether a string is a layout misses `"../x.md"`; checking a fixed UTC probe misses FR-051's
   local rendering; checking the local rendering too misses that **a `Location` is not a zone** —
   it selects among many by instant (`America/New_York` → EST/EDT), and a hostile TZif's POSIX
   footer makes the transition schedule the attacker's, so any fixed number of samples is one short.
   Refusing the element removes the variable instead of sampling it.
4. Detection compares one instant formatted under two zone names rather than testing for the
   substring: Go's scanner gives the `M` in `"03:04PMST.md"` to the `PM` element, so
   `strings.Contains(v, "MST")` would reject a safe layout.
5. FR-018's all-sinks-disabled check stays out of `config.Load`; `data-model.md` and `plan.md` were
   amended to say so (T081 CLI, T082 GUI). Fixing `data-model.md` in cycle 1 created a contradiction
   with `plan.md` that cycle 2 caught — spec amendments need a consistency sweep, not a local edit.

**Blockers and open questions.**

- Six non-blocking follow-ups from this review are **planned but not filed**: `elide`'s bound is
  declared generally but applied to two keys only (measured 3x file-to-message amplification at four
  sibling sites); `elide` truncates head-only, so a long-prefix traversal is reported with the
  offending substring cut away; the safe-alphabet enumeration backing AC-9 omits `Z` and `AM`/`PM`;
  `elide`'s multibyte branch is untested; `thread_id > 0` is enforced but absent from the schema
  table; the `rendered == ".."` clause is unpinned though proven verdict-neutral.
- Carried from Batch 2 and still open: issue #94 (`git_commit` is `unknown` on tagged installs) needs
  a maintainer decision before `v0.1.0`; issue #98's `Target()` decision must be honored when
  T025/T030/T040 are written; `data-model.md` remains stale on the `SinkResult` entity.

**Next best action.** Run `run-batch-cycle` for Batch 4 — Logging foundation (T021-T024,
issues #22-#25).

---

## 2026-09-07 — Batch 3 merged

**Objective.** Land PR #100 and sync the worktree.

**Changes made.** None to the code. PR #100 squash-merged to `main` as `6d84ae9`; the branch
`sgykfjsm/batch-3-settings` was deleted on the remote.

**Evidence.** `git diff a6a734e origin/main` is empty — the squashed commit is byte-identical to the
reviewed branch tip. Issues #12-#21 all auto-closed by the merge; #4 (T003) correctly remains open
with the ULID and Fyne pins outstanding. On merged `main`: `go build`, `go vet`,
`go test -race -count=1` all clean, `internal/config` coverage 99.0%.

**Decisions.** None.

**Blockers and open questions.** Unchanged: the six Batch 3 follow-ups remain planned but unfiled
(see `review_followups_planned_not_filed` in state.yaml), as do the six from Batch 2. Issues #94,
#95, #96 and #98 are still open and still correctly open.

**Next best action.** Run `run-batch-cycle` for Batch 4 — Logging foundation (T021-T024,
issues #22-#25).

---

## 2026-09-07 — Filed the outstanding review follow-ups

**Objective.** Clear the backlog of review findings that existed only in local run state.

**Changes made.** No code. Filed #101-#105 and commented on #57 and #4, consolidating twelve
findings from the Batch 2 and Batch 3 reviews into five issues and two comments.

**Evidence.** Both review run states remained readable
(`20260902T040356Z-79b5bd57`, `20260903T103000Z-1047541d`), as did the Batch 2 memory note, so no
finding was reconstructed from recollection.

**Decisions.** Consolidated rather than filed one-per-finding — the tracker already carries 75 open
issues, and three of the twelve were the same root cause seen by different reviewers. One finding
(the `rendered == ".."` clause) was deliberately **not** filed: it was proven verdict-neutral over
27,479 accepted layouts, so an issue would imply latent risk that does not exist.

**Blockers and open questions.** None new. #104 is a decision needed before T035.

**Next best action.** Run `run-batch-cycle` for Batch 4 — Logging foundation (T021-T024,
issues #22-#25).

## 2026-09-07 — Batch 4: the logging foundation (PR #106, open)

**What was built.** `internal/logging`: the twelve stable event names from
`contracts/log-events.md` as constants of a defined `Event` type plus `AllEvents()` (T021, T022),
and the `slog` JSON-handler logger (T023, T024). T021–T024 marked complete; issues #22–#25 to close
on merge. No new dependency — `slog` is stdlib, and the ULID pin stays with Batch 5.

Two shapes carried the design. First, `Logger` has no logging methods: `Logger.Post(messageID)`
returns the only type that can emit a record, because `message_id` is contractually on every record
and an attribute callers are merely asked to remember is one they eventually forget. The event name
travels in slog's message slot for the same reason — slog always emits a message, so the field
cannot go missing. Second, `Open` returns neither an error nor a `*Degradation`; a discarding logger
is still a logger, so there is no state in which the caller has nothing to log to, and `Degraded()`
is the single source of FR-076's one warning.

**What the review caught, and what it says about the first attempt.** Verdict
`passed-with-notes` after one fix cycle. Five should-fix findings, and the pattern across them is
worth recording: three were holes in guards whose own doc comments claimed they were closed, and
three were tests that could not fail.

- An empty-key group defeated both protections at once. `slog.Group("", …)` is slog's documented
  inlining idiom; the filter inspected only the group attr's key (`""`, not reserved) and, because
  slog skips `openGroup` for an empty key, `ReplaceAttr` saw `len(groups) == 0` for each inlined
  member. One record carried `event` three times, with the forged value winning in every mainstream
  decoder.
- The filter enumerated slog's *output* key names while the renamer worked from its *input* names.
  Only `level` was in both sets, so an attribute keyed `msg` — one character from the contract's own
  `message` field, and the habitual Go spelling — passed the filter and was then renamed *into*
  `event`.
- `os.OpenFile` on a FIFO blocks inside `open(2)` until a reader attaches, so a named pipe at the
  log path made `Open` never return: no degradation, no warning, no records, and no post. That
  falsified the batch's own "never blocking a post" and was worse than the case FR-076 was written
  for.
- `Close` was not idempotent, and the test asserting idempotence ran only with a supplied writer,
  where the nil-handle guard made `return nil` unconditional — an assertion that could not fail for
  any implementation that had the guard at all.
- The `go/ast` registration scan gated whole const groups on finding a bare `Event` type
  identifier, so five of six declaration shapes escaped, including `const EventX = Event("x")`.
  This defeated precisely the half of T022 that a literal expected-set test cannot cover.

**The lesson about coverage.** Statement coverage was 100% for the first attempt and for the fixed
version, and it masked four defects both times. Every finding this cycle was established by
introducing the defect and watching a test fail, not by reading. Two findings were themselves
mutants that survived: the empty-key `ReplaceAttr` group guard was untested until a test was written
for it, and `TestANamedGroupIsNotTraversed` passed through the filter's allocation-free early return
so the loop it constrains never ran — a change that silently deleted a caller's entire named group
left the whole suite green. Both are now pinned.

**Blockers and open questions.** None blocking. Three decisions are recorded in
`state.yaml` under `batch_4_review_notes.open_notes` and want a maintainer answer: whether
`logging.path = /dev/null` should keep warning on every run now that a non-regular path is refused
(there is no other way to disable diagnostics); whether the emission path should `recover()` so a
panicking `Options.Writer` cannot take down a post, naturally decided with T068–T070; and where the
bot-token scrub belongs — a chokepoint in `internal/logging`, or every call site plus T084's gate.
The last is the substantive one: the contract requires an `error` field on failures, and its only
natural source is `SinkResult.Err`, which for a Telegram transport failure is a `*url.Error`
carrying the token. It leaked through six ordinary attribute spellings and is unreachable only
because nothing imports the package yet.

One note for Batch 10: the non-regular-file guard lives in `openLogFile`, which the `Options.Writer`
seam bypasses entirely, so T068's rotating writer must repeat it or the hang returns. Recorded in
`Options.Writer`'s doc comment where that work will see it.

**Follow-ups filed.** #107 for the `ResolvePath` gap, plus acceptance notes on #69, #41 and #105.
Three findings were deliberately not filed and the reasons are recorded in `state.yaml` under
`batch_4_review_notes.followups_filed.not_filed` — the short-write repair claim was narrowed in
code rather than tracked, the TOCTOU window's reviewer-stated required outcome was explicitly none,
and the work-log's dangling key reference is history rather than current state.

## 2026-09-07 — Batch 4 merged

PR #106 squash-merged as `8b7bed6`; issues #22–#25 auto-closed. Verified the squashed tree is
byte-identical to the reviewed branch tip `acd7d7e` — both resolve to tree `f7c45ff` — so what
landed is what was reviewed. `make check` clean on merged `main`. No CI is configured in this
repository, so that local gate is the whole gate.

The PR carried seven commits: the implementation, the review fix pass, the three escalated
decisions, the follow-up record, and the three `.agents` bookkeeping commits that had no PR of their
own. Squashing collapses them, which is the established convention here — the individual commits
stay visible on the PR page.

**Ready for Batch 5.** Branch `sgykfjsm/batch-5-orchestrator` is cut from the merged `main` and
carries this merge record, so it will ship with Batch 5's PR exactly as Batch 3's record shipped
with Batch 4's. That is the pattern worth keeping: the merge record for batch N lands in batch N+1's
PR rather than accumulating as unpushed commits on a stale branch, which is how twelve review
follow-ups went unfiled and needed a catch-up commit.

Three things Batch 5 inherits, all recorded in `state.yaml` under `time_sensitive`:

- **#107** — `logging.Open` does not apply `ResolvePath`, so a `Logger` built from settings with
  `logging.path` unset (the default) silently writes nothing. T025 is the first task to construct a
  `Logger`, so it owns this unless the front-door tasks take it.
- **#98** — the settled `Targeter` decision. `Target()` must return what `Send` actually resolved
  and wrote, not re-resolve on call; re-resolving reintroduces the local-midnight mismatch the
  decision exists to avoid.
- **#4 / T003** — `oklog/ulid/v2@v2.1.2` is pinned by the batch that first imports it, which is
  this one. Fyne remains outstanding until the GUI batch.
