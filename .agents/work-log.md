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

## 2026-09-08 — Batch 5: the posting orchestrator (PR #108, open)

**What was built.** `post.Service` and `post.Outcome`: one ULID per post, every sink started in its
own goroutine under its own deadline derived from `context.Background()`, all results awaited and
aggregated (T025, T026). Pins `oklog/ulid/v2@v2.1.2` as the first importer, which is what T003
defers to each batch. **Phase 2 Foundational is complete** — user story work can begin.

**The decision that shaped it.** FR-015's per-sink bound was only *cooperative*: the orchestrator
handed each sink a deadline and then waited on a `WaitGroup`. The maintainer's call was to enforce
it, because T031's obsidian sink cannot honour a context — `os.OpenFile` and `os.File.Write` take
none — so a vault on a synced or network mount would hold a post open for as long as the mount did,
with its sibling's finished result unreachable. Measured before the fix: 3.0 s on a 100 ms budget,
and forever for a sink that never returned. Delivery now runs on a buffered channel and `run` stops
waiting at timeout plus a 250 ms grace. The grace is not decoration: without it the context deadline
and the backstop fire at the same instant, so which error a cooperative sink's result carried was a
race.

**Three review cycles, three fix passes, and the pattern is the lesson.** Verdict
`passed-with-notes`. The primary defects were all the same shape — a guard I built and then left a
route around:

- `deliver`'s recover was installed one line *after* `sink.Name()`, so a panicking `Name` or a nil
  slice element killed the process along with the sibling's result.
- Having fixed that, `Name` was still resolved *synchronously in `run`*, before the goroutine and
  before the timer existed — so a blocking `Name` reproduced the unbounded hang the backstop had
  just been built to fix, and a `Goexit` in it produced a result naming nothing. Two interface
  methods; I hardened one, twice.
- The backstop timer started before `Name` while the sink's context started after it, so a slow
  `Name` pushed the sink's deadline past the backstop and a cooperative sink was abandoned anyway.

**And the tests were worse than the code.** The cycle-2 correctness review built 36 mutants and
killed 28. Every one of the eight survivors was a test of mine that could not fail for the property
it was written for: the secret-containment assertion checked `Reason` (always one of two constants,
so unfalsifiable) instead of `Err`, where `describePanic`'s output actually lands; `enforcementGrace`
— the single mechanism the whole restructure introduced — had no test at all; an empty
`unknownSinkName` passed; the two user-visible reason phrases could be swapped; the ULID timestamp
could be frozen, defeating R-007's entire rationale; an unbuffered `done` channel parked every
abandoned goroutine permanently; `Duration` could be dropped on two paths; and `deliver`'s
`panicnil` pre-seed could be deleted, because nothing in the suite ever called `panic(nil)`. That
last one now runs as a subprocess, since `GODEBUG` is read at startup.

**Running total worth keeping in view:** nine unfailable assertions across Batches 4 and 5, all
mine, every single one found by mutation and none by coverage — which read 100% throughout. Two of
my own mutants this batch were no-ops I briefly recorded as survivors, and two more failed to
compile. Asserting that the mutation actually applied is not optional.

**Two of my own fixes were also wrong.** The first slow-`Name` test set the name cost above
timeout+grace, where abandonment is the *correct* outcome — the test was wrong, not the code. And
the first reclamation test flaked, comparing against a process-global goroutine baseline that
sibling parallel tests drift by ~28; it now asserts a relative drop.

**Numbers corrected rather than defended.** I documented the abandoned-goroutine cost as ~0.7 KiB.
Measured: ~4.9 KiB — I had counted only the heap and omitted the 4.1 KiB goroutine stack, which is
both the larger term and the resource this design deliberately leaks.

**Follow-ups filed.** #109 (`config.Validate` has no upper bound on `sink_timeout_seconds`;
`18446744074` wraps to a 290 ms deadline, defeating both guards this batch added) and #110 (a
`Name()` panic is discarded, so FR-071 can never record it — and the information is gone before
T073 could).

**Scope held.** `post.Targeter` (#98) waits for T030/T040, because nothing here can type-assert an
interface no sink implements. `Reason`'s fixed set is T056's; this batch adds the two constants
FR-015 forces and pins the invariant that `Reason` is never derived from `Err`. No logging, no
validation, no logger field — an unused field is a claim a batch cannot test. Sub-notes now record
that this batch's suite already carries T052, T054 and T053's reporting half, so US3 is a
verification pass.

## 2026-09-08 — Batch 5 merged; Phase 2 complete

PR #108 squash-merged as `35d24e2`; issues #26–#27 auto-closed. Verified the squashed tree is
byte-identical to the reviewed tip `523e79a` — both resolve to tree `049683e`. `make check` clean on
merged `main`. Still no CI in this repository, so that local gate remains the whole gate.

**Phase 2 Foundational is complete**: posting core contracts, settings, diagnostics, orchestrator.
Twenty-six tasks done. User story work can begin.

**Ready for Batch 6.** Branch `sgykfjsm/batch-6-us1-cli` is cut from merged `main` and carries this
merge record, continuing the pattern: batch N's record ships in batch N+1's PR rather than
accumulating as unpushed commits.

**What Batch 6 inherits.** It is the first end-to-end slice and the first batch to wire a front
door, so five obligations that earlier batches correctly declined all land there at once: #107
(`logging.Open` ignores `ResolvePath`, so a default install writes nothing), #41 (`Options.Redact`,
without which the token scrub is inert), #98 (`Targeter` and the `path` field), #109 (the upper
bound on `sink_timeout_seconds`), and #110 (the discarded `Name` panic). At fourteen tasks — T027
through T040, spanning the obsidian sink, the telegram sink, the CLI and the wiring — it is also the
largest batch attempted so far, and the first where triage should seriously weigh splitting it.

**One process note worth carrying forward.** Three batches in a row now, the most valuable review
output has been mutation rather than reading: Batch 4's staged review found a FIFO that hung the
whole application, and Batch 5's found nine assertions of mine that could not fail. In both cases
statement coverage was 100% and indicated nothing. For Batch 6 the equivalent risk is different in
kind — real I/O against a real filesystem and a real HTTP surface, rather than pure logic — so the
fakes will need the same scrutiny the assertions did.

## Batch 6a — the Obsidian daily-note sink (PR #112, squashed as `69e17e3`, 2026-09-09)

Merged `passed-with-notes` after four review cycles and four fix passes. The squashed tree is
byte-identical to the reviewed tip `59b876b` (both tree `604f510d`). Closed #28, #31, #32, #33;
advanced #98; filed #111.

**Three silent-loss defects were caught that would otherwise have shipped**, all the same shape — a
post reported as delivered whose text is not in the note. A FIFO at the note path, found
independently by the correctness and adversarial stages from different directions: switching
`O_WRONLY` to `O_RDWR` for the separator rule removed an accidental guard, because `O_RDWR` opens a
pipe instantly where `O_WRONLY` blocked and failed loudly. `unterminated()` returning an
uninitialised byte on the `io.EOF` path, whose branch no test could reach because the fake derived
its size from its own content. And a descriptor leaked on the non-regular refusal, which would cost
a long-lived GUI session one fd per post until `EMFILE`.

**Statement coverage read 100% through all four cycles and flagged none of them.** That is the third
consecutive batch. The lesson has now changed shape: it is no longer "write mutants", it is "make
the assertion itself the thing you mutate". The two moves that actually worked here were neutering a
test's *error* assertions so only its data assertion could speak — which is what proved the FIFO
test's "nothing was stored" check was real rather than decorative — and asserting through the OS
instead of a proxy, since a leaked `O_RDWR` descriptor is still a writer on a pipe and makes a
non-blocking read answer `EAGAIN` rather than `(0, nil)`.

**A bookkeeping failure worth not repeating.** This batch's decisions were coined as `DEC-A1` and
`DEC-A2` in conversation and cited in nine places across five files, but neither was ever written to
`state.yaml` — and `DEC-A1` was already Batch 5's. The most consequential behaviour in the batch had
no resolvable authority at all. Renumbering them then introduced a smaller version of the same
error, because the citation list was built from `grep` output without reading the sentence at each
hit, sweeping a line that legitimately meant the timeout decision. Decisions now get written down
when they are coined, namespaced per batch, and a renumbering is a reading task rather than a
search-and-replace.

**Three residuals are accepted and documented rather than closed** (DEC-B2-RESIDUAL): a hardlink at
the note path, a vault reached through a symlinked parent directory, and — the one adversarial found
— an external writer replacing the note by `rename` between the open and the write, which is how
Obsidian itself and every sync client saves. A successful append means the bytes reached the inode
that was opened, not necessarily the file now at that path. Detection needs a post-write
`Nlink == 0` check behind a second build-tag pair that cannot distinguish a sync client from a
deliberate delete.

**One thing did not get corrected before merge.** PR #112's body still describes the first
implementation — `O_APPEND|O_WRONLY`, "never a read-back" — which is the inverse of what shipped.
The merge was authorised without the body rewrite, so the accurate account went into the squash
commit message and `contracts/obsidian-sink.md` instead. A corrected draft exists if the description
is ever worth fixing retroactively.

**What 6b inherits.** An HTTP surface rather than a filesystem one, so the fake-fidelity problem
that hid the `io.EOF` branch here recurs in a different form: a stub HTTP server that cannot produce
the failure being asserted is the same defect as a fake whose `Stat` and `ReadAt` cannot disagree.
#41 (`Options.Redact`, without which the token scrub is inert) becomes directly relevant the moment
a bot token is in play.

## Batch 6b — the Telegram chat sink (PR #116, squashed as `13705e0`, 2026-09-09)

Merged `passed-with-notes` after three review cycles and three fix passes. The squashed tree is
byte-identical to the reviewed tip `01d258f` (both tree `dbbaf29`). Closed #29, #34, #35, #36;
advanced #98; filed #117 and #118 and commented on #115.

**Five defects were caught that would otherwise have shipped**, four in the two worst outcome
classes this sink has. The `http.Client` had no `CheckRedirect`, so a single 302 converted POST to
GET, dropped the message, and reported the final host's `{"ok":true}` as success — while handing
that host the bot token in the `Referer` header, because Go strips userinfo from referers but keeps
the path. The credential net redacted the *rendering* and not the *value*: `*APIError` still carried
the token in its exported `Description`, one `errors.As` away, and `errors.As` is precisely what
`contracts/log-events.md` tells T040 to call for `http_status` — the batch shipped the trap and the
contract instructed the next batch to spring it. `%#v` leaked on the contradiction path alone,
because `Error()` suppresses `Description` when a cause is present and the net decided by reading
`Error()`. And the read cap did not fail closed as its comment claimed: a body sized to exactly the
cap truncates on a *complete* JSON document and parses as success.

**One root cause, named by the reviewers rather than inferred:** assertions written against
`httptest` doubles that could not produce the failure they claimed to rule out. A token-free reply
body. A non-redirecting server. A missing negative direction. A padding size that happened to
truncate mid-string. Statement coverage read 98.8% throughout and indicated none of it — the fourth
consecutive batch.

**The most transferable thing learned here is about doubles, not about HTTP.** Batch 6a's lesson was
"mutate the assertion, not just the code". 6b sharpens it: the assertion can be perfectly written
and still prove nothing, because the *fixture* cannot reach the state being denied. Every one of
these five was a correct assertion pointed at an input that could not fail it. The check that works
is to ask, of each negative assertion, what input would make it fail — and then to actually build
that input.

**Two moments of the process working as designed.** The adversarial reviewer upgraded a clearance
from reading to executing: correctness had cleared HTTP/1.1 transport replay by reading
`isReplayable`, and adversarial built a raw listener that kills a cached keep-alive connection — the
precise condition triggering Go's idempotent replay — and measured one wire request for one `Send`.
And the third fixer caught a regression in its own pass: fixing the value-level leak removed the
only fixture exercising the net's `%#v` arm, so that mutant would have silently stopped being
killed. It noticed, and closed it with a dedicated fixture guarded so the fixture cannot decay into
testing the other arm.

**The bookkeeping failure recurred for the third time.** 6a's headline defect was decisions coined
without being recorded. 6b coined four more — form encoding, credential scrubbing, the timeout
clamp, the redirect refusal — and again none was written down until review demanded it, and `DEC-C4`
was still missing from `state.yaml` after the merge because I said I would add it and did not. The
lesson has not stuck by being written in a work log. What would actually stop it: the implementer
brief must require the `state.yaml` entry as a deliverable, in the same sentence that asks for the
decision.

**What 6c inherits.** The front door, and with it every obligation earlier batches deferred: #107,
#41/T040, #98 boxes 1–3, #110, #111. Its recorded blocker is a decision, not code — `build.go`
cannot live in `internal/post`. T040 must reach `*telegram.APIError` via `errors.As` for
`http_status`, and must exclude `errContradictoryStatus`, which now also covers refused redirects
whose body parses.

## Batch 6c-1 — the US1 front door (branch `sgykfjsm/batch-6c-us1-front-door`, 2026-09-10)

T029, T036, T037, T038, T039 (#30, #37, #38, #39, #40), plus #107, #109, #114 and the #104
decision. `mp "hello world"` now posts to both sinks and exits on the aggregate. Awaiting review.

**Three structural decisions were settled and written down before a line was implemented** — the
first time in this project that happened in that order. `DEC-D1` puts the composition root in a new
`internal/app`, because T036's recorded `internal/post/build.go` cannot compile: the sinks import
`internal/post` to implement `post.Sink`. `cmd/mp` was the obvious alternative and is wrong for a
specific reason — `internal/gui` cannot import `package main`, and #111's cheapest fix puts sink
construction on the GUI's *submit* path. `DEC-D2` keeps `internal/post` importing nothing internal,
which is load-bearing rather than stylistic: `service.go:79-82` cites it as the reason `Service`
takes a `time.Duration` instead of `config.Settings`, and `internal/logging` imports
`internal/config`, so `post → logging` would reintroduce exactly that dependency transitively.
`DEC-D3` replaces `Targeter` with a context-installed target reporter, which is 6c-2's to build.

**The invariant is now enforced by a test rather than by habit.** `internal/app/layering_test.go`
parses the source and fails if `internal/post` acquires an internal import or if `internal/app`
acquires a front door. Every review since Batch 5 has verified that property by hand; now the suite
does.

**`DEC-D4` — invalid UTF-8 is rejected at validation.** The decision (#104) turned on three things
none of which were known when #104 and #113 were filed, and the framing it arrived with was wrong.
It is not "reject versus store verbatim": the log substitutes U+FFFD silently (measured, so SC-008
and FR-068 are unsatisfiable for exactly these messages), Telegram appears to refuse rather than
substitute (tdlib's `check_utf8`, so the sink fails closed and the post half-delivers), and Obsidian
rewrites the note at the user's next save. **No destination keeps the bytes.** The real choice was
between refusing, and accepting a post that reaches one destination of two and cannot be
reconstructed from its own failure log. The accepted cost is recorded honestly: a user on a legacy
Shift_JIS locale loses the CLI until they fix it, and Option C is the humane answer if such a user
appears. Whether Telegram truly refuses is unverified without a live token and is recorded as the
decisive unknown.

**One thing this batch caught that is worth generalising.** A stray `internal/app/2026-09-09.md`
was found in the working tree — an Obsidian daily note, mode 0600, written *into the source
package*. It was residue from a mutation run, not the passing suite: the mutant swaps the sinks'
settings, so the obsidian sink gets an empty `DailyNoteDir` and `filepath.Join("", …)` resolves
relative to the package directory. The mutant proved itself by leaving evidence on disk. Confirmed
by deleting it and re-running the suite clean. **A mutation harness that copies the tree is not
enough when the code under test writes to the filesystem — the mutant can escape the copy through
a relative path.** Worth checking `git status` after every mutation run, not just the exit code.

**What 6c-2 owes.** T040, the `Recorder` seam and the adapter, #41's four riders, #98 boxes 1–3,
#110, and #111 via DEC-D3. `DEC-D2` carries a mandatory obligation: the logging package's
source-scan test cannot see the adapter, so 6c-2 must assert that the set of event names the
adapter can produce equals the orchestrator-reachable subset of `AllEvents()` — otherwise an
unmapped event silently never fires. Two open questions to answer rather than default: whether
`error_type` is emitted from today's two `Reason` constants or omitted until T056, and whether the
`message` field is omitted entirely until T071 with only `message_len` recorded.

## Batch 6c-1 — the US1 front door (PR #120, squashed as `f757707`, 2026-09-10)

Merged `passed-with-notes` after one review cycle and two fix passes. The squashed tree is
byte-identical to the reviewed tip `22ca3da` (both tree `d4f4cb57`). Closed #30, #37, #38, #39, #40,
#107, #109, #114, #117; advanced #104; filed #119.

**The first batch here whose decisions were written down before the code.** DEC-D1 through DEC-D4
were recorded in `state.yaml` with their rationale, rejected alternatives and costs before
implementation started — the concrete fix for the bookkeeping failure that had recurred in three
consecutive batches. It held: the contract reviewer judged each decision against the code rather
than the record, and found none overreaching.

**Two review findings were the same shape, and it is a shape worth naming.** `mp -c <non-regular
path>` never returned — a FIFO hung with no output, no timeout and no exit status, and `/dev/zero`
reached ~1.9 GB RSS in a second. `internal/logging` had refused exactly this for the *log* path since
Batch 4, with a comment recording that a FIFO there "made Open never return". The guard existed; the
counterpart didn't. Separately, the batch made a comment false in `internal/sink/telegram/sink.go` —
a file it never opened — because its own new `MaxTimeoutSeconds` ceiling falsified a sentence saying
validation "bounds it only from below", while the contract it *did* write asserted the correction had
already been made. **Both are the same failure: a fact established in one place and not carried to
the place that already depended on it.** Worth a habit — when a batch changes a fact, grep for who
asserts it.

**Two unfailable assertions were caught during authorship rather than a batch later**, which is new.
One asserted `Contains(err, "a directory")`, which `os.ReadFile`'s own EISDIR text already satisfies,
so it passed with the guard deleted. One keyed off `"bot_token"`, which the required-when-enabled
problem satisfies for a different reason on different input. Both now assert their rule's own clause
— text nothing but the new code writes. The check that found them is cheap and should be routine:
**for each new assertion, ask what text or state would satisfy it without the fix present.**

**A coordinator error worth recording, because a memory did not prevent it.** The PR was opened with
`Closes #30 (T029), #37 (T036), #38 …` on one line, and GitHub registered only the first — the
identical defect found as CON-001 in Batch 6a, which already had a memory written about it. It was
caught because `closingIssuesReferences` is checked after every PR open rather than trusted from the
body; without that, five issues would have stayed open after merge. The verification habit saved it,
the memory did not. The lesson is not "remember harder" — it is that a check at the point of action
beats a note recalled at the point of writing.

**The honest hole, filed as #119.** No test drives two destinations both succeeding, and as written
none can: `telegram.Sink.baseURL` is unexported and `cli.Run` has no seam. The `sinks[:1]` mutant was
killed only by the partial-failure test — by a *failing* Telegram — with `internal/app` and
`internal/cli` both passing it. The issue's acceptance is the mutant, not a test's existence.

**What 6c-2 inherits.** T040, the `Recorder` seam, the adapter, #41's four riders, #98 boxes 1–3,
#110, and #111 via DEC-D3. Four obligations are recorded in `state.yaml`, the load-bearing one being
that `internal/logging`'s event-name test cannot see the adapter, so an unmapped event would silently
never fire unless 6c-2 asserts the producible set against `AllEvents()`.

## Batch 6c-2 — event emission (T040, branch `sgykfjsm/batch-6c2-event-emission`, 2026-09-10)

Implemented and validated; not yet reviewed. `make check` clean, 100.0% statement coverage in
`internal/post` and `internal/app`, 99.5% in `internal/logging` (unchanged). Discharges T040 and
closes #41, #98, #110 and #111. Thirty-two mutants built, one surviving by design.

**The shape is DEC-D2's, and the boundary held.** `post.Recording` and `post.Recorder` are
domain-shaped interfaces declared in `internal/post`; the mapping onto `logging.Event` and the whole
field vocabulary live in `internal/app/recorder.go`. `internal/post` still imports no other internal
package, and `internal/app/layering_test.go` is what says so rather than three PR bodies. The cost
DEC-D2 predicted is real and was paid where it said to pay it: every adapter test drives a whole post
through a real `logging.Logger` and reads the bytes back off disk, decoding one line at a time so
FR-064's self-containment fails rather than being reassembled by a lenient reader.

**`post.Targeter` is gone (DEC-D3), and the replacement needed one decision the decision did not
cover.** `post.ReportTarget(ctx, path)` carries the note per call, so the value never leaves the
goroutine running the post — that is #111 closed by construction rather than by synchronisation, and
the sink lost its mutex and its field along with the getter. What DEC-D3 did not say is how the
orchestrator knows *whether to wait* for a report before emitting a sink's start event, and it has to
know before `Send` is entered. `post.TargetReporting` answers it with one empty method nobody calls.
A marker method is unusual in Go; the alternatives were worse. Making every sink call
`ReportTarget(ctx, "")` removes the marker but edits the merged telegram sink to report nothing and
moves correctness from the type system onto a convention — which is exactly what #115 is filed about.

**Two fields were judgement calls, and both are recorded in three places rather than one.**
`error_type` is emitted now with two values, from the same `errors.Is(err,
context.DeadlineExceeded)` predicate `reasonFor` uses, with a test asserting the two partition
failures identically — so the log and the terminal cannot describe one failure differently, and T056
widens the set rather than redefining it. An absent field would have been a saved query that
silently returns nothing, which is the harm `events.go`'s own comment is written about. `message` is
emitted nowhere at all, and FR-068 is recorded as knowingly unmet until T071 in `tasks.md` and in a
new section of `contracts/log-events.md` — so a reader finds a scheduled gap rather than inferring
an oversight.

**#110 closed without adding vocabulary, and it took three attempts to see why that was the
constraint.** The recovered `Name` panic now survives as `SinkAttempt.NameErr`, wrapped in the same
`panicError` a panicking `Send` produces. It cannot ride on that sink's lifecycle records, and the
reason is structural rather than aesthetic: a panicking `Name` resolves to the `unknown` sentinel,
and the event names are keyed by sink name, so **there are no records for such a sink at all**. That
is the finding worth keeping — the adapter's honest answer for an unmapped sink name was going to be
"emit nothing", which is the silent-drop shape five previous batches were caught by. Both the panic
and the missing vocabulary are now named in the post's terminal record, whose own name and level
still track `AllSucceeded` so the exit status and the log agree.

**One defect outside the batch, found by the first end-to-end read of real records.**
`internal/logging` emitted `level` as `"INFO"`/`"ERROR"` for every logger with `Options.Redact`
populated — which `app.OpenLogger` always does, so every run of the shipped binary since 6c-1.
`replaceAttrRedacting` ran `scrub` before `renameBuiltin`; `slog.Level` implements `fmt.Stringer`, so
the scrub's Stringer arm stringified the level and the rename's type assertion to `slog.Level` then
failed. `contracts/log-events.md` admits only `info` and `error`, and a consumer filtering `level ==
"error"` got nothing. The package's own `TestLevelIsExactlyInfoOrError` passed throughout, because it
configures no credential — **the one configuration no front door produces.** The lesson generalises
past this bug: a test that exercises a component in a shape its callers never use can be green while
the shipped behaviour is wrong, and 100% statement coverage says nothing about it. Fixed by
reordering, pinned by a test that arms `Redact` and asserts both the level *and* that the scrub still
fires, so the fix cannot decay into "the scrub was removed".

**Two unfailable guards were caught by mutation rather than by reading.** Removing `sinkRecord`'s
second-finish guard survived the suite, because `run` calls `finish` once per sink and nothing else
can — so the guard was correct, load-bearing for a change a later task might make, and completely
unexercised. Removing `guard`'s nil-recorder check survived too, because `guarded`'s own recover
absorbs the nil method call: two guards in series, where neither one's test can tell which is doing
the work — the shape `app.go`'s `SinkTimeout` comment already warns about. Both are now pinned by
direct unit tests of the types rather than through a post, which is the difference between a
documented intention and an asserted one. **The check that found them is cheap: build the mutant for
every guard, not for every branch.**

**What 6c-2 leaves for the next batch.** US1 is complete. Batch 7 is US2 (T041–T051, the Fyne GUI)
and needs the `fyne/v2` pin, the last third of #4. #119 is untouched and still open: no test drives
two destinations both succeeding, and `cli.Run` still has no sink seam — this batch changed
`NewService`'s signature in that same function without closing it, deliberately, because a seam is
its own decision.


## 2026-09-11 — Codex takeover

User requested takeover from Claude Code. This checkout is on
`sgykfjsm/batch-6c2-event-emission-2`, with implementation committed as `124abea`.
Orca lists this Codex terminal and an idle setup shell in this checkout; no Claude
terminal is attached here. Other checkouts were not stopped or modified.
`make check` passed (formatting, vet, race-enabled tests). Corrected stale current
state claiming the implementation was uncommitted. Preserved the existing Claude
integration installation timestamp change. No implementation changes made.
Next action: structured review of Batch 6c-2 at `124abea`. Prior coverage and mutation
figures are inherited evidence and were not rerun during takeover.


## 2026-09-11 — Batch 6c-2 staged review

Reviewed PR #121, base f757707 → head 124abea, review-only. Contract valid;
correctness inspected all 22 changed files/hunks; the independent adversarial
stage confirmed the diagnostic-blocking defect. Verdict: request-changes.

Required findings: COR-001 / ADV-001 (synchronous logger blocks delivery and
bypasses timeout completion), CON-001 / COR-002 (missing integrated emitted
message_id/path overlap test), CON-002 (terminal failure field contract ambiguity).
A real logger with a controlled blocked Writer reproduced blocking at four event
stages past 350 ms with a 10 ms sink timeout; the start stall changed the outcome.
No physical stalled mount or live Telegram service was used.

Fresh make check passed on the exact snapshot. Coverage reproduced: post 100%,
app 100%, logging 99.5%. Historical mutations were not rerun. #110's specific
acceptance is supported; #41/#98/#111 remain open pending corrections. No fixes,
commits, pushes, PR updates, or issue mutations. Existing local edits preserved.

Review report and reproduction: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T004422Z-c992f78b
Next action: explicitly authorized correction pass, then full staged rereview.


## 2026-09-11 — Authorized Batch 6c-2 correction pass

User invoked fix-review-findings for COR-001, CON-001 and CON-002. Verified HEAD
124abea and preserved pre-existing state edits and Claude setup timestamp.
Reproduced all four blocked-writer cases before correction.

COR-001 / ADV-001: production recording now uses PostAsync and one ordered,
bounded logger worker. Flush/Close wait at most 250 ms, the queue holds 256 pending
entries, and error-state reads no longer wait behind the writer lock. Timeout or
saturation permanently disables this logger's queue, drops pending records and
warns. An in-flight write can land late; one worker/handle remains until it returns.
Normal errors retain existing recovery. Timestamps are captured at admission;
crash-time queue loss and delayed cleanup are documented in the log contract.

CON-001 / COR-002: integrated Service.Post tests share a real Obsidian sink across
midnight and force B to complete before A appends, then decode JSONL and match each
start/finish path to its returned ID and actual note. Includes a failed B append.
CON-002: clarified sink-only error_type and detailed errors, with DEC-E3's terminal
identity diagnostic exception; tests reject ordinary aggregate failure fields.

Validation: make check passed (fmt/vet/full race suite). Focused liveness, queue
ordering/timestamp, saturation, timeout, late cleanup and close-stall tests passed.
Three temporary mutants were rejected: synchronous production logging, shared latest
target, and aggregate error_type. Mutants never changed the working checkout.

All three dispositions are fixed, pending full staged rereview. No issues closed,
PR changes, commits or pushes. Receipt: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T004422Z-c992f78b/fix-01/receipt.yaml
Next action: full review of the updated uncommitted Batch 6c-2 diff.


## 2026-09-11 — Commit and full Batch 6c-2 rereview

User requested commit, checks, and review. Committed corrections and project-state
updates as 1420095 (`fix: isolate diagnostic writes and verify post correlation`).
Preserved the unrelated Claude integration timestamp. No push.

make check passed after commit. Fresh uncached go test -race -count=1 ./... passed
on the archived exact commit. Coverage: post/app100.0%, logging99.3%. Correctness
review inspected all114hunks/26files and independently reran five relevant packages;
it passed without findings. Contract is valid. The built-in fresh adversarial
launcher hit its thread limit; recorded that abandoned attempt, then ran a fresh
ephemeral read-only Codex CLI reviewer with only the packet/contract/raw validation.
It passed without findings; it verified23non-state file blobs, inspected applicable
risk lanes, and did not independently rerun tests.

Original COR-001/CON-001/CON-002 are resolved. Overall request-changes is now limited
to publication requirement CON-101: PR121 still describes remote124abea and needs
current queue/completion/validation wording before publishing1420095. Prepared a
local body draft. CON-102 (stale T053 evidence annotation) is non-blocking and left
to its owning documentation/batch workflow. No issue closures or PR mutations.

Cycle-1 report: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T004422Z-c992f78b/cycle-01/integrated.yaml
PR draft: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T004422Z-c992f78b/cycle-01/pr-body-draft.md
Next action: authorized publication of reviewed commit and accurate PR body, then
verify remote state. These post-review state updates remain uncommitted.


## 2026-09-11 — Publish review corrections and post-review notes

User authorized committing state notes, pushing, and updating the PR description.
Committed post-review notes as5c30ce5, then fast-forward pushed1420095 and5c30ce5 to
PR121's existing branch sgykfjsm/batch-6c2-event-emission. The local takeover branch
now tracks that remote branch. The unrelated Claude installation timestamp remains
uncommitted.

Updated PR121 from the prepared body, removing draft language and documenting the
reviewed implementation, queue limits, current validation, selected issue acceptance,
and exclusions. Read back the remote head and exact body and verified that changes
since the tested1420095 are project-state documentation only. CON-101 is resolved;
current disposition passed-with-notes. CON-102 remains a non-blocking T053 annotation
follow-up, noted in the PR; no new issue or task closure is needed for publication.
No merge or direct issue closure was performed. This entry records publication and
will accompany the final documentation-only push.

Next action: merge PR121 when authorized.


## 2026-09-11 — Batch 7 implementation and review start

User invoked run-batch-cycle for Batch 7. Verified PR #121 merged as 9287366 and
selected T041–T051/#42–#52 plus the final Fyne pin. No selected issue was safely
closable without implementation. Added the focused Fyne window, shared submission,
results/timers, native macOS activity observer and process dispatch, with headless
race tests. Saturated large timer settings before duration multiplication.

make check and native arm64 build passed; temporary-vault UI checks confirmed
newline/submit, source:gui diagnostics, success/failure auto-close exit 0/1 and
keypress/background-click retention. Focus-only regain could not be independently
established through accessibility Raise; real IME and Intel Mac also unverified.
No live Telegram call was made. Initial test-harness failures were corrected
(Fyne TypeOnCanvas bypasses focus, test Window.Clipboard returns fresh instances,
and the test driver does not tolerate closing the same window twice).

Prepared a local PR description and immutable review packet under
/tmp/mp-batch7-packet. Started review-only contract/correctness/adversarial cycle 0;
coordinator state is /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T022010Z-8e1ba8b0.
No commit, push, remote PR or closure. T003 remains open pending the required go.sum
commit. Preserved the unrelated Claude manifest timestamp. Archived the previous
project snapshot and retained prior decisions in state.yaml.

Next action: finish the staged review and obey its terminal verdict.


## 2026-09-11 — Batch 7 staged review complete: request-changes

Fresh contract, correctness, and adversarial agents completed cycle 0 against the
same verified 14-file worktree target. Contract is valid. Correctness inspected
all changed hunks and reran make check plus uncached GUI/cmd race tests; adversarial
traced native observer lifetime, AppKit shutdown, cancellation, concurrency,
configuration, disclosure and durability, with uncached GUI race validation.
Neither behavioral stage established a code defect. Both remain inconclusive for
the required native focus-only regain acceptance check. CON-001 requires the
checked T049 task to reflect that gap or for the check to be completed.

Verdict: request-changes. Review-only mode, zero fix passes. No source/task fix,
commit, push, remote PR, or issue closure followed the review. #50 remains open
pending focus evidence, #4 pending committed go.sum; other selected issues remain
open through review/publication. No post-review non-code closure was justified.
The unrelated Claude manifest timestamp remains untouched.

Integrated report:
/Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T022010Z-8e1ba8b0/integrated.yaml
The immutable packet and prepared PR description are archived under its input/
subdirectory. .agents/batch-7-cycle.md records triage and the concrete resume step.

Next action: complete native focus-only validation and reconcile T049, then rerun
staged review before publication. Do not start Batch 8 yet.


## 2026-09-11 — T049 native focus-only revalidation passed; cycle1 review started

User requested another T049 verification followed by rereview. Built the unchanged
production binary and a passive test-only AppKit observer via DYLD_INSERT_LIBRARIES.
The observer generates no input/focus changes and returns every event unchanged;
it records a superset of production input plus actual key-window notifications and
isKeyWindow/visibility snapshots. An idle control auto-exited after45.091s, code0.

Cross-app automation reintroduced mouse input into the target and was discarded.
A standard About panel instead produced actual main-window key loss, then AXRaise
with the application active produced actual key gain without additional target
input. Success and failure retained results25.647s and44.603s beyond the45s delay,
then manual Esc returned0 and1. Read-only trace verification passes all three cases.
This is actual window focus regain, not a separately isolated cross-app switch.

Updated only tasks.md's evidence annotation and a new native validation note;
all13 prior production/test/dependency hashes unchanged. Fresh uncached GUI/cmd
race tests and native build passed. Native source needed no fix. Raw evidence,
observer source, verifier and exact binary are in review cycle-01/input/native-validation.
Preserved cycle0 integrated report under cycle-00/integrated.yaml. Started fresh
cycle1 contract review on target fingerprint d1589bed2fd21ce3d048b70676b87a5ab482be282f784e727cd01cb6d8c5cdc5.
No commit, push, remote PR or issue closure. Next: finish staged rereview.


## 2026-09-11 — T049 native proof accepted; Batch 7 rereview passed

Cycle 1 contract, correctness and adversarial reviewers independently accepted the
unchanged production implementation and native proof. All 15 reviewed files / 17
hunks inspected; native raw verifier and fresh GUI/cmd race tests passed. CON-001
resolved. Final integrated verdict: passed, no required findings. Initial failed
review and discarded cross-app trial remain preserved. State and cycle summary
now reflect acceptance; work remains uncommitted and unpublished, issues open.
Next: publish reviewed Batch 7 when authorized.

User separately authorized registering a future GUI background-image feature.
Inspected all 110 existing issues for overlap; none matched. Created #122
(https://github.com/sgykfjsm/miko-post/issues/122): random image from a configured
directory at GUI launch, faint under the dark appearance. Opacity, layout, formats
and fallback details remain design decisions. Implementation deferred, outside
Batch 7. No production/spec implementation change for this new request.

## 2026-09-11 — Batch 7 commit and push

Using the explicitly requested commit-and-pr workflow, committed the reviewed
Batch 7 work as `9c3e47f` (`feat: add GUI quick-post window`) and pushed
`sgykfjsm/batch-7-cycle` to origin. This also satisfies T003's committed
`go.sum` condition, so T003 was checked. No open PR existed for the branch;
draft PR creation remained the next step.

Created draft PR #123 at https://github.com/sgykfjsm/miko-post/pull/123 with the
Batch 7 implementation, validation evidence, T003 completion, and disclosed
out-of-scope limits. No issue was closed.

## 2026-09-11 — Batch 8 implementation and verification

- Confirmed Batch 7 PR #123 merged as 6f40dada; archived its prior status snapshot.
- Triaged #53–#59: T052/T054/T055 and rendering already implemented; T053 logging
  already supplied by Batch 6c-2; T056 needs the classifier. No issue was closed.
- Implemented fixed-vocabulary classification with error identities, preserving the
  original diagnostic and post package boundary; Telegram matches only trusted refusals.
- Added real local HTTP/filesystem integration and core-driven headless GUI matrices;
  strengthened both-failure log classification assertions.
- make check and native arm64 production build passed. Initial test fixture mistakes
  (Outcome ID field and note filename extension) were corrected before full validation.
- Prepared PR text; all changes uncommitted. Three-stage review started in review-only
  mode. DEC-A1 timeout grace retained; later batches and Claude manifest edit excluded.
- Next: complete the three-stage review of the prepared Batch 8 diff.

## 2026-09-11 — Batch 8 review completed: request-changes

- Contract valid; fresh correctness and adversarial stages independently found the same
  should-fix defect: COR-001 / ADV-001, missing/null Telegram ok interpreted as a valid
  refusal for classification. Deduplicated to one required correction.
- Full suite/build stayed green; coordinator and adversarial external overlays reproduced
  the missing/null envelope failure. Explicit false control passes; no reviewed file
  was changed by probes. Raw reports/evidence: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T055009Z-f2faa64e.
- Review-only, zero fix passes. No commits, pushes, PR publication or issue closure.
- Post-review tracking corrected T056 to incomplete; six other selected tasks verified.
  This tasks.md-only metadata delta is recorded separately from the frozen review target.
- Post-review cleanup found no additional closure to publish. No feature closeout or
  guide/task-artifact archival: implementation still needs the required correction.
- Next: fix COR-001 (also ADV-001) and rerun the full three-stage Batch 8 review.

## 2026-09-11 — Authorized Batch 8 correction and fresh rereview

- User explicitly authorized COR-001/ADV-001 fixes, regression tests and all-three-stage
  rereview, leaving changes uncommitted. Interruption audit confirmed no partial fix.
- Single fixer changed response.go and reason_test.go only. A required boolean ok is
  now distinguished from missing/null; invalid envelopes retain generic classification.
  Optional null description is treated like omission; exact chat-not-found text remains
  required for that category. Missing/null error codes remain generic.
- Eight regression failures reproduced before correction; targeted race tests, make check
  and native arm64 build passed afterward. No task-completion or remote edits.
- Fresh collaboration reviewer launch hit the agent-thread limit before starting. Its
  attempt was abandoned and replaced by a fresh read-only Codex CLI session; reviewers
  remain independent and receive only the immutable packet and valid contract.
- Cycle 1 full target fingerprint: ba30b1d0bc6ed7a84852862c379baf78f04e9b23f5dc68c048c465d33c9e3050.
- Next: complete contract, correctness and adversarial rereview of the full diff.

## 2026-09-11 — Batch 8 authorized rereview passed

- One correction pass resolved COR-001/ADV-001. All three fresh stages passed against
  the full 13-file target ba30b1d0bc6ed7a84852862c379baf78f04e9b23f5dc68c048c465d33c9e3050.
- Canonical target/contract/stage fingerprints verified; original diagnostics and HTTP
  status retained, invalid ok remains generic, optional description policy tested.
- make check and native arm64 build passed. Independent race tests passed for core,
  adapter, CLI/GUI and Telegram classification. Reviewer socket restrictions blocked
  fresh HTTP reruns; the fixer's full validation had exercised that matrix successfully.
- Process history retained: collaboration thread-limit failure; rejected redundant
  fingerprint typo; inconclusive correctness setup superseded by a fresh verified run.
- Integrated report: /Users/shige/.agents/review-runs/sgykfjsm__miko-post/20260911T055009Z-f2faa64e/integrated.yaml.
- No task-completion, staging, commit, push, PR or issue mutations. T056 remains unchecked
  as historical tracking; current acceptance is passed. No remaining review findings.
- Next: reconcile preserved T056 and prepared PR tracking before publication.

## 2026-09-11 — Batch 8 publication preparation

User invoked commit-and-pr. Verified the implementation still matches the passed review;
reconciled T056 and all seven closing references, preserving cycle 0 history.
The unrelated pre-existing Claude manifest timestamp remains outside publication.
