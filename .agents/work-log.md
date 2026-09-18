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

## 2026-09-11 — Batch 8 draft PR published

Created implementation commit 2b78af1 and pushed the branch with upstream tracking.
Opened draft PR #124: https://github.com/sgykfjsm/miko-post/pull/124.
Existing history was preserved. All seven completed issues have individual closing
references; no merge or issue closure was performed. The pre-existing manifest
timestamp edit remains local and uncommitted. Publication bookkeeping is recorded
in a separate documentation commit.

## 2026-09-11 — Batch 9 reviewed; issue #122 separately in progress

Verified Batch 8 merge #124 at 48fdd6a. Implemented T059–T064 formatting rescue.
Three-stage review cycle 0 found two validation gaps (COR-001/COR-002); one fix pass
added deterministic request-deadline equality, a rejected reset-context mutant,
second-request timeout and real HTTP→Service→JSONL outcome coverage. Fresh cycle 1
contract/correctness/adversarial stages all passed. Full check/native build and
uncached focused race tests passed. T059–T064 checked complete; issues remain open.

Issue #122 explicitly requested separately. Implemented optional bounded JPEG/PNG
background selection in an isolated source snapshot; tests/build and native visual
checks passed. Its independent review continues. No publication. Next: finish #122.

## 2026-09-11 — Issue #122 accepted; cycle complete

Applied ten #122 files byte-identically from the isolated review snapshot. Initial
contract/adversarial stages passed; correctness required meaningful image-limit tests.
One correction pass added valid boundary/over-limit images and recovery. Four applied
mutations fail; fresh contract/correctness/adversarial stages all passed. Combined
Batch 9 + #122 make check and native build passed. All findings are resolved.

Reconciled accepted contracts/spec/design and user/developer guides; preserved the
original #122 Batch 7 deferral and prior status snapshot. Batches 10–12 and unrelated
documentation debts remain. No issues closed or remote publication performed.
Next: publish Batch 9 with #122 and reconciliation kept separately reviewable.

Workflow notes retained for traceability: collaboration thread capacity was exhausted,
so later stages used fresh read-only ephemeral Codex sessions. The first #122 overlay
mutation keys used /tmp while Go resolved /private/tmp; those runs did not apply the
mutants and were discarded. Canonical-path overlays subsequently killed all four.
The generated review diff omitted new-file mode headers and forward git apply rejected
it without changes. The ten files were then copied only after verifying each original
against HEAD (or absence), and byte equality with the immutable review snapshot was
confirmed. Valid git-generated publication patches now reverse-check successfully.

### 2026-09-11 — Authorized commit and PR publication
- User invoked commit-and-pr. Retained the existing non-default branch and excluded the pre-existing Claude manifest edit.
- Committed and pushed Batch 9 as 1cac8b2 and #122 as 5fd39bd; created draft PR #125: https://github.com/sgykfjsm/miko-post/pull/125.
- Publication uses one PR with separate feature and documentation commits, following the skill's existing-branch policy. Earlier separate-PR preparation remains useful scope history.
- Preserved history; no issue closed. Existing combined make check/native build and both fresh three-stage review passes remain the implementation validation; publication adds diff whitespace and state consistency checks.
- Next: obtain acceptance and merge #125 before Batch 10.

### 2026-09-16 — Merge Batch 9 and prepare Batch 10
- User accepted PR #125 and authorized merge. Verified clean/mergeable status and exact reviewed head 7bb43d1; no remote CI checks were listed.
- Squash merged #125 as 17073829398aa540b6a8ea56a0155b0e30880dfc, fetched main, and verified identical trees. No implementation retesting was required.
- Created sgykfjsm/batch-10-diagnostics from merged main, preserving the unrelated Claude manifest edit; removed main upstream tracking to avoid accidental push targeting main.
- Triaged ten US5 tasks/issues into 10a rotation and 10b post diagnostics. Prepared .agents/batch-10-plan.md; no code or task completion changes.
- Issues remain open; no issue closure was performed. Next: implement Batch 10a, then three-stage review.

### 2026-09-16 — Implement Batch 10a and apply the review correction pass
- Implemented Batch 10a lossless log rotation (T065, T068–T070 / #66, #69–#71) on `sgykfjsm/batch-10-diagnostics-2` from merged main `1707382`, and opened PR #126 at `f3ab572`. The branch `sgykfjsm/batch-10-diagnostics` named by the preparation above was never created; the `-2` branch is the real one.
- New: `internal/logging/rotate.go`, `birthtime_darwin.go`, `birthtime_other.go`, two `Options` fields and the `Open` call site in `internal/logging/logger.go`, and the threshold wiring in `internal/app/app.go`. T065/T068–T070 are marked `[x]` in `tasks.md`; the four issues stay open until #126 merges.
- Decisions DEC-F1 (rotation inside `logging.Open`, not through `Options.Writer`), DEC-F2 (`O_CREATE|O_EXCL` name claim) and DEC-F3 (clamp at the conversion) are recorded in `state.yaml` under `in_progress` 10a with `recorded_in:` pointers. They had existed only in the PR body, which is the recurring "an ID that reads as authoritative and resolves to nothing" problem.
- DEC-F1 supersedes the premise of the Batch 4 acceptance note on issue #69: its three boxes are discharged by construction. `state.yaml`'s ADV-D-002 entry is annotated as superseded in part — the decision stands, only its `Options.Writer` clause is stale. The GitHub issue is left as written.
- Review correction pass (nine authorized findings). One behaviour fix: `open` now dates an *empty* file from the process clock, so a rotation always clears its own trigger — a filesystem creation time far behind the process clock previously made every write rotate. Seven new or reworked guards: a descriptor-count guard on the stat-failure branch, literal-hour anchoring of the day conversion and age boundary, Close-racing-a-write coverage at both the writer and the `Logger` layer, close-failure propagation through `Logger.Close` and `Degraded`, and 32-bit compilability of the package's tests.
- Validation: `make check`, native darwin/arm64 `make build`, `internal/logging` at 99.5% statement coverage, `GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/logging/` clean, and 9 correction-pass mutants built and killed on top of the batch's original 28. (This line first said 8; the second pass below rebuilt all nine, confirmed each is killed, and reconciled the figure — the PR body and `state.yaml` had said nine all along.)
- Corrected in this pass because they contradicted head, not because they were wrong when written: `project-status.md`'s "No next-batch code is implemented", `state.yaml`'s `prepared-not-implemented` and `next_best_action`, and the branch name in `batch-10-plan.md`.
- Not addressed and to be filed separately: two `mp` processes sharing one log path, a symlinked `logging.path`, and `safeWriter`'s truncated-line repair re-entering the rotation predicate. All three are lossless. Next: complete the review of #126 and merge it, then Batch 10b.

### 2026-09-16 — Second review correction pass on Batch 10a
- Eight authorized findings, all fixed in the worktree; nothing committed, pushed or published. The batch is unchanged: T065, T068–T070 / #66, #69–#71.
- COR-006: `w.createdAt = creationTime(info)` had no failing witness — the zero time, `time.Unix(0, 0)` and `info.ModTime()` all survived the suite, the last silently reverting A-011's whole purpose. Every age assertion used either an empty file (whose `createdAt` the first pass's fix overwrites, making the line dead for it) or a genuinely old file asserted to rotate, which any earlier value also satisfies. Added `TestReopeningANonEmptyLogDatesItFromTheFileAndNotTheClock` (portable; pins that `open` dates a pre-existing non-empty log neither earlier nor later than the file does) and `TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime` (darwin-only; the only build where `creationTime` and `ModTime` are different functions, so the only place the third substitution is observable — said so in the portable test). All three mutants now killed, plus a fourth (`createdAt` pinned to `now()`).
- COR-003, second attempt: `TestLoggerCloseRacingAStragglingRecordNeverWritesToAClosedHandle` emitted through `PostAsync`, and `recordQueue`'s single worker drains the queue before calling `closeWriter`, so write and close could never overlap — deleting `s.target = nil` from `safeWriter.close` left it green. Switched to the synchronous `Post`, which emits on the caller's goroutine. Mutant killed 12/12 under `-race`; unmutated green 12/12.
- COR-007 / ADV-005: `TestRotatingWriterCloseRacingAWriteIsWrittenOrRefused`'s closer waited on a channel closed only by the first *accepted* write, so any unexpected `Write` error parked it forever and the recorded `t.Errorf` became a `go test` timeout panic. The closer is now released on every exit path (`defer announced()`); with a forced write error the test FAILs in 0.00s naming the error. The sibling writer-layer test is otherwise untouched.
- ADV-006 (partial, comments only): `birthtime_other.go` and `birthtime_darwin.go` claimed the `ModTime` fallback's worst case was "a log that rotates later than a reader expects". True inside one process; `mp` is a short-lived CLI writing through `O_APPEND`, so on every non-darwin build and on darwin volumes with no birth time each run measures the age from the previous run's last post and the age trigger never fires at all. Both comments now say that. No behaviour change, no statx, no sidecar timestamp — the product question is filed as DEC-ADV-006.
- CON-005: the first pass's one production behaviour change is now **DEC-F4** in `state.yaml` under `in_progress` 10a, with A-011 as the authority, a `recorded_in:` pointer to `rotate.go`'s `open`, and the accepted consequence stated — a pre-existing, genuinely old, *empty* log no longer rotates on its first write. The PR body's bullet states the same consequence.
- CON-003: `safeWriter`'s truncated-line repair re-entering the rotation predicate is now listed in the PR body and `state.yaml`'s `not_fixed_here` alongside the two concurrency items, so all three records agree on three.
- CON-006: reverted the first pass's in-place edit to `batch-10-plan.md`'s "Immediate next action", restoring `sgykfjsm/batch-10-diagnostics`. The appended note now carries the correction, which is what both it and `state.yaml`'s `branch_note` had claimed. The bullet above describing that in-place edit is left as written — it is what the first pass did.
- CON-007: `TestOpenRotatingRefusesANonRegularPath`'s doc comment described a FIFO blocking inside `open(2)` while its body creates a directory. Comment corrected; the PR body's issue-#69 box 1 now cites `TestOpenDoesNotBlockOnANonRegularFile`, which builds a real FIFO against a deadline, and keeps the directory test as the in-package refusal check.
- CON-008: mutant count reconciled to 9 for the first pass (all nine rebuilt and re-verified killed here) and 6 for this one, 15 in total, in all three records. `state.yaml`'s `committed`/`pushed` for 10a corrected to false with a `publication_note` and `committed_through: f3ab572`, so a coordinator reading `state.yaml` alone cannot conclude the reviewed tree is on GitHub.
- Not fixed, by instruction: ADV-001, ADV-002 and ADV-006's behavioural remedy (product decisions), the `safeWriter` repair edge case (disclosure only), and the stale published PR description (coordinator's pre-merge action).
- Validation: `make check`, native `make build`, `GOOS=linux GOARCH=386 go vet ./internal/logging/` clean, `internal/logging` at 99.5% statement coverage. Next: commit and push both correction passes, refresh #126's description, finish the review and merge.

### 2026-09-16 — Third and final review correction pass on Batch 10a
- Six authorized findings, all fixed in the worktree; nothing committed, pushed or published. The batch is unchanged: T065, T068–T070 / #66, #69–#71. This is the last pass the review loop permits.
- **No production behaviour changed.** Every change is a record, a doc comment or a test. `internal/logging/rotate.go` is byte-identical to the tree this pass started from, and `logger.go` is unmodified against `f3ab572`, as it has been since the batch was opened.
- CON-009: `DEC-ADV-006` was coined by the second pass and never defined — four citations, no record, two of them in shipped source (`birthtime_other.go`, `birthtime_darwin.go`) that will outlive `.agents/`. It is now an `open_questions` entry under `in_progress` 10a in `state.yaml`, beside `DEC-F1..F4` but explicitly recorded as an open question rather than a decision taken: should FR-072's age trigger be made meaningful on builds where creation time falls back to `ModTime` (statx, a Windows `Sys()` path, a sidecar open-time stamp), or is "darwin with a recorded birth time" the accepted support boundary for `rotate_after_days`? A-011 sanctions the fallback, so this is a scope question and not a defect; the three options and their costs are recorded. No remedy implemented.
- CON-009 (b): the deferred lists disagreed again — the PR body and `state.yaml` said three and claimed to be the whole list, `work-log.md` listed four. Both now enumerate four, with DEC-ADV-006's behavioural remedy as item 4, and both say plainly that this list has twice been short. Same shape as CON-003, re-created by the fix that closed CON-003.
- ADV-007: both birth-time comments asserted "ModTime is never earlier than the creation time". False on exactly the platforms `birthtime_other.go` serves — `utimensat` lets a preserved-timestamp copy or a restore from backup leave mtime behind the inode's creation, and the first write then rotates immediately. Both now state the conditional guarantee, name the mechanism, and record that `open`'s `w.size == 0` branch bounds the consequence to one rotation. darwin/APFS cannot reach the case: it clamps the birth time down to a backdated mtime, which is what `birthtime_darwin_test.go` relies on.
- ADV-008: "a user posting more often than `rotate_after_days` never sees the age condition fire" is true of the CLI only. `internal/gui/run.go` opens one `Logger` for the life of the window, so `createdAt` is captured at session start and a session open longer than the threshold does fire the trigger. Both comments now say "per open" rather than "per run" and name the GUI session. It fails safe, but DEC-ADV-006 will be decided against this description.
- ADV-009: `TestLoggerCloseRacingAStragglingRecordNeverWritesToAClosedHandle` could pass having asserted nothing — all eight writers and the closer released by one `close(start)`, so a close that won outright discarded all 160 `Post`s, left `firstErr()` nil and gave the JSON loop an empty file. Given its sibling's treatment: the closer is released by the first record confirmed on disk, released on every exit path so it cannot hang, and a positive `landed == 0` assertion makes a vacuous run a FAIL. Installing a discarding `safeWriter` in `Open` — which the old test passed — now fails it 5/5; deleting `s.target = nil` from `safeWriter.close` is still killed 12/12 under `-race`; unmutated green 10/10.
- COR-009: `TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime` called `t.Fatalf` when the birth/mtime gap was zero, which on a coarse-granularity volume that does record a birth time (FAT32 at 2s, HFS+ at 1s) reports an unsupported environment as a rotation bug. It now skips, saying which two timestamps were equal. Unweakened where it matters: on APFS `w.createdAt = info.ModTime()` still fails it.
- CON-010: `DEC-F4`'s `authority` said the substitution "is narrower than the one A-011 anticipates". It is narrower only in scope; in substance it EXTENDS A-011, whose precondition is a platform that does not expose a creation time and whose sanctioned substitute is the *oldest* available timestamp, where `w.now()` is the newest. Both records now say "extends" and keep the justification — R-006 resolves A-011 with a fallback defended on "not earlier than creation", which `w.now()` on an empty file satisfies unconditionally. `DEC-F4` is also promoted into the PR body's Decisions section, where it belongs as the batch's only production behaviour change.
- Observed, not filed and not this batch's: the GUI front door never calls `Degraded()`, so an FR-076 warning is zero there today. That is unshipped T074, still unchecked in `tasks.md` — existing backlog, not a deferral of Batch 10a, and deliberately kept out of the deferred list.
- Not fixed, by instruction: ADV-001, ADV-002, the `safeWriter` repair edge case and DEC-ADV-006's behavioural remedy (all product decisions, all still in the deferred lists), and the stale published PR description (the coordinator's pre-merge action).
- Validation: `make check`, native darwin/arm64 `make build`, `GOOS=linux GOARCH=386 go vet ./internal/logging/` clean, `internal/logging` at 99.5% statement coverage, 16 correction-pass mutants in total. Next: commit and push all three passes, refresh #126's description, finish the review and merge.

### 2026-09-18 — Reconcile the records to the merged reality and close #66, #69–#71
- Objective: a status check found the `.agents/` records contradicting git. Verified the real state, reconciled all three managed files, and closed the four Batch 10a issues that the merge left open. No code changed; `internal/` is untouched.
- **PR #126 merged two days earlier and nothing wrote it down.** It was squash-merged on 2026-09-16T14:32:40Z as `0e3df5ec524df20e0d1d46b4572e11dbe83e810a`, from head `73d9c575137aab69bcf56d96ecb14362e532be0f`, by `sgykfjsm` — 5 commits, +3565/−54 across 17 files. `main` and this worktree's `HEAD` are both at `0e3df5e`.
- What was stale and why it split: the batch's own last two commits (`9ba5b5d`, `73d9c57`) updated **only** `state.yaml`, bringing its `in_progress` 10a entry up to the published head and recording follow-ups #127–#130. So `state.yaml` already knew the correction passes were committed and the PR re-described, while `project-status.md` and `work-log.md` were never updated past the third correction pass and still said "Both are uncommitted: #126 still shows f3ab572 only". `next_best_action` was stale in the same way. Nothing in any of the three recorded the merge, because the merge happened outside a session that ran this skill.
- `state.yaml`: moved the 10a entry from `in_progress` to `completed` (now 14 entries; `in_progress: []`), with `state: merged`, all five branch commits listed and annotated, `committed_through`, `merged_from_head`, `squashed_as`, `merged_at`, `merged_by`, and a `merge_note` saying the merged tree is the reviewed tree and that the record was written two days late. Rewrote `next_best_action` as exactly one item (Batch 10b), replaced `remaining_batches`' batch-10 row with a 10b row, marked `next_batch_preparation` as history rather than preparation, and set `last_updated: 2026-09-18`. Validated the file parses.
- **The review verdict is recorded as `not-final-at-merge`, not `passed`.** No GitHub review was ever submitted (the staged review ran locally, so `get_reviews` returns empty) and no final verdict was recorded anywhere. Three fix passes were applied — the maximum the loop permits — and the merge followed without a fourth cycle pronouncing. Upgrading that to "passed" would have been the easy write and would have been false; each of the three passes had found a test that could not fail on the property it named, so the loop was still producing findings when it stopped.
- Closed #66, #69, #70 and #71 as `completed`, each with a comment naming PR #126 and `0e3df5e`, the delivering files and the validation. They did not auto-close because the PR body deliberately said the issues stay open until review and merge, so it carried no closing keyword — and `closingIssuesReferences` had been **verified empty before the merge** and recorded in `publication_note`. The check was done, the prediction was right, and nothing acted on it for two days; the missing step was an owner for the post-merge closure, not the verification. Each comment also carries the batch's caveats to where they now live: #69 restates DEC-F1 superseding its own Batch 4 acceptance note and names DEC-F4, #70 points at #128 for the `ModTime`-fallback cost, #71 points at #127 for the shared-log-path limit. Open issues: 59 → 55.
- `project-status.md`: overwritten, with the outgoing copy archived as `.agents/archive/project-status-before-batch-10b.md`, following the existing `before-batch-7/8/9` practice. It carries a closing record-keeping note describing this drift, so the next reader meets it rather than rediscovering it.
- Evidence: `make check` green on `0e3df5e` across all ten packages (`cmd/mp`, `internal/app`, `cli`, `config`, `gui`, `logging`, `post`, `sink/obsidian`, `sink/telegram`, `version`); PR #126 read back as `state: closed, merged: true`; all four issues read back `CLOSED / COMPLETED`; `state.yaml` re-parsed after editing.
- Decisions: record the merge honestly rather than retro-fitting a verdict (above); keep the four issues' closure administrative — with a merge reference, per instruction — rather than reopening the review; leave `tasks.md` alone, since T065/T068–T070 were already `[x]` and correctly so.
- Open questions: none introduced. #127–#130 remain the four filed 10a follow-ups and all four still await a product decision. The `.specify/integrations/claude.manifest.json` edit remains uncommitted and excluded, as it has been through every batch.
- Next best action: implement Batch 10b (T066, T067, T071–T074 / #67, #68, #72–#75) on a fresh branch from merged `main`, using `.agents/batch-10-plan.md`.
- **Correction, same day, before committing:** the verdict claim above was made from `state.yaml` at `0e3df5e` plus an empty GitHub review list, and it needed a source. The review run manifest at `~/.agents/review-runs/sgykfjsm__miko-post/20260916T030000Z-3be7358f` is that source and it strengthens the claim: three cycles ran, not the two recorded anywhere else, and cycle 2's adversarial stage is still `status: running` with no `cycle-02` report directory. The third fix pass applied ADV-007–ADV-009 from that stage, so it delivered findings; nothing reviewed the tree those fixes produced, and that is the tree that merged. All three records now cite the manifest.
- **A competing reconciliation of this same event already existed and I nearly overwrote the question it raises.** Commit `d057f45` on `sgykfjsm/batch-10b-diagnostics` — pushed, never merged to `main`, checked out in the sibling worktree `sgykfjsm-batch-10-diagnostics-2` — was made two minutes after the merge and records the same region as `review_verdict: passed-with-notes`, `review_cycles: 2`, `adversarial: findings-none-blocking`. The manifest contradicts all three. Found only because the natural branch name for 10b was already taken; had it been free, this session would have committed a second divergent record onto `main` and neither would have known about the other. Its `merge_verification` is better than what this session had — tree equality (`0e3df5e` and `73d9c57` both at tree `a26e21c`) rather than matching head shas — and is adopted. Recorded as `competing_record` under `completed` 10a; the two branches are not merged and must be reconciled before either reaches `main`.
- **The closure sweep is wider than Batch 10a.** #60–#65 (T059–T064, delivered in Batch 9 / PR #125) are merged but still open, as are review follow-ups #115, #118 and #119. Same cause as #66/#69–#71: no closing keyword, no owner for the post-merge step. The other session had already flagged this in its `next_best_action`. Not swept in this session — only the four Batch 10a issues were authorized.

### 2026-09-18 — Implement Batch 10b (US5 post diagnostics)
- Objective: one batch delivery loop for 10b — T066, T067, T071–T074 / #67, #68, #72–#75 — on a fresh branch from merged main. Implemented and validated; **not committed, pushed or reviewed** at the time of writing.
- Branch `sgykfjsm/batch-10b-post-diagnostics`, from this worktree's `e0a0c39` (merged main `0e3df5e` plus the 10a record reconciliation). **Not** `sgykfjsm/batch-10b-diagnostics` — that name was already taken by a pushed branch holding a competing reconciliation of the 10a merge (`d057f45`), checked out in a sibling worktree and left untouched. The collision is the only reason that branch was found.
- **T071** — `message` capture in `internal/app/recorder.go`. The body is held from `MessageReceived` and attached to every record that reports a failure, with the terminal record as a fallback **only** when no failure record could carry it (DEC-G1). That fallback exists for one shape: a sink whose `Name` panics resolves to a sentinel the vocabulary does not cover, so no lifecycle record is emitted and the terminal record is the post's only record — FR-068 would otherwise be unmet for exactly that post, while applying it unconditionally would put the user's private text in the log three times for a two-sink post that lost both.
- **T072** — **no code change.** `message_len`/`message_bytes` already carried R-010's semantics. Rather than read the code and declare it done, three mutants were built (byte count under `message_len`, rune count under `message_bytes`, the two swapped); all three are killed by the existing `TestASuccessfulPostWritesTheContractsRecords`. Complete because it is right *and* pinned.
- **T073** — `stack`. `debug.Stack()` inside `internal/post`'s deferred `recover`, exposed through the new `post.Traced` interface. This changed `internal/post/service.go`, outside T073's stated file, and it is unavoidable: `recover()` returns the panic value only and the frames are gone by the time any later layer sees the error. `SinkAttempt.NameErr`'s comment is the precedent — the panic *value* used to be discarded at the recovery point and keeping it "needed a change at the point of recovery rather than a later task". An interface rather than an exported error type, because the recovered value beside the trace can hold a credential.
- **FR-071's no-manufactured-trace rule is discharged by construction**, not by a list of error types: an expected operational error does not implement `Traced`, so no code path can produce a plausible stack for one. Two mutants attack it directly — a `traceFor` falling back to `"goroutine 1 [running]:\n" + err.Error()`, and a `panicError.stack` pinned to a plausible constant — and both are killed, because the assertion names a frame from the panicking sink rather than checking the field is non-empty.
- **T074** — FR-076's warning for the GUI, which never called `Logger.Degraded()` before this batch (issue #75), so diagnostics could fail for a whole session with the user never told. Once per *session*, not per post: the logger latches its degradation and the window outlives every post, so re-rendering it would report one failure many times (DEC-G5). The warning replaces the `Details: <path>` line rather than sitting beside it, because pointing a user at a log for details when the log is what failed is an instruction to read a file that does not have them (DEC-G6) — the reasoning `internal/cli/render.go` already applied to its own pairing.
- Did **not** touch `internal/cli/render.go`, which T074's text names: the CLI already emitted the warning correctly with its own tests, and adding a second CLI code path to satisfy a file list would be a change with no defect behind it. T067 went to `internal/app` rather than `internal/post/service_test.go` for a structural reason — the posting core does not import `internal/logging` and cannot be placed in the condition.
- **Coverage regressed and was fixed rather than explained away.** `internal/app` dropped from 100.0% to 99.1%: `claimBody`'s empty-body branch and `keepTrace`'s branches are unreachable through a sink. Rather than leave guards no test can enter — the shape `internal/logging` has already been bitten by twice — a `CaptureProbe` seam was added to the existing `export_test.go` and both are now covered. Back to 100.0%, matching baseline. `internal/gui` is 73.4% against 74.2%; the whole difference is one new statement inside `Run`, which is 0% covered at baseline too, verified by reading the baseline profile in its own worktree after the first comparison mis-resolved function names against current source.
- **One mutant survived and a guard was deleted.** `keepTrace` had an explicit `trace == ""` early return in front of its first-wins check; removing it failed nothing, because storing `""` when nothing is stored is a no-op and the first-wins check already rejects an empty offer once a real trace is held. Deleted rather than given a test. Both remaining `keepTrace` mutants are killed.
- **A false kill was caught.** The first `message_len` mutant scored as killed with no test named — replacing `utf8.RuneCountInString` with `len` left the `utf8` import unused, so the package failed to compile and a non-zero exit was read as a kill. Rebuilt as a compiling mutant and genuinely killed. Every later mutant run checks for a build failure before scoring. 28 built, 27 killed overall.
- **Observed and worth keeping:** the record shape changed and the pre-existing suite stayed green. Failure records gained a `message` field and nothing noticed, because no existing test asserted field completeness on a failure record. That is the gap T066 closes, and why its tests assert presence *and* absence rather than values alone.
- `contracts/log-events.md` updated: the "what is still owed" table now shows `message` and `stack` as delivered, and the capture and traces sections say which record carries what and how the prohibition is kept. `tasks.md` marks all six 10b tasks `[x]` — 75 of 91 done, 16 open.
- Not fixed, disclosed in the PR body: `internal/app`'s test file does not compile for a 32-bit target (`app_test.go:265`, `int(config.MaxTimeoutSeconds)`) — pre-existing, byte-identical to `0e3df5e`, found only by running 10a's cross-vet gate over this batch's packages; and the GUI cannot report a `Close` failure, because `logger.Close` runs after `a.Run()` returns and the window is gone.
- Validation: `make check`, native darwin/arm64 `make build`, `GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/post/` clean, `internal/app` production code builds clean on 386.
- Next: review Batch 10b through the three stages, then publish, merge, and close #67, #68 and #72–#75 explicitly.

### 2026-09-18 — Batch 10b contract review: invalid, one real defect fixed
- Contract stage returned **`invalid`** with ten findings and two blockers. Correctness and adversarial have **not** run; the skill gates them behind a valid contract, so they are owed against the fixed tree.
- **CON-001 was a real defect in my own code, and I confirmed it with a failing test before fixing it rather than taking the reviewer's word.** `FormattingFinished` attached the body to `telegram_markdown_failed`, which FR-039's plaintext rescue emits *inside a post that then succeeds* — so every rescued post wrote the user's private message body to the log. FR-068's privacy half, breached on the most ordinary path there is: the rescue exists because Telegram rejects ordinary punctuation. The fix narrows the body to `SinkFinished`'s records; SC-008 loses nothing, because a rescue that itself fails returns a `RescueError` and the sink's own `telegram_send_failed` record carries the body. Recorded as **DEC-G1a**, pinned by `TestARescuedPostIsASuccessAndRecordsNoBody` and `TestAFailedRescueRecordsTheBody`, and the regression mutant is killed.
- My own T066 tests did not catch it: they exercised non-rescue sinks only, and the test file's own comment asserted "the rule is that no record carries the body" — which was false in general while the test only checked the easy case. The reviewer found it by reading the contract document against the code, which is exactly what the contract stage is for.
- The CON-001 fix also removed a `traceFor` call from `FormattingFinished`: coverage fell to 99.5% and the branch turned out to be unreachable even from a test, since a `FormattingAttempt`'s error comes from an HTTP exchange and never a panic. Deleted for the same reason as `keepTrace`'s redundant guard. Back to 100.0%.
- **CON-005 was the other substantive one.** T073's acceptance text says "panics **and unexpected errors**", and only `*panicError` implements `Traced`, so the middle clause was closed by checkbox. It is now **DEC-G8**: FR-071 qualifies all three categories with "where a trace is *available* and useful", a plain Go error carries no frames, and the requirement forbids inventing any — so the absent field honours FR-071 from both sides rather than deferring it. `contracts/log-events.md` had asserted a coverage it then silently narrowed; both now agree.
- **CON-004**: DEC-G2 justified itself with "FR-068 constrains only the enabled case", which stops the quotation before the second clause's unconditional MUST. The behaviour was compliant; the justification was not, and it had been written into a durable contract file where a future change could have relied on it to stop recording the body in that mode altogether. Rewritten in both places.
- **CON-003**: I had disclosed three deviations from the task text; there were five. T066's, T071's and T072's stated files were wrong too, and all six boxes were flipped to `[x]` with the file names untouched — so `tasks.md`, the durable artifact, asserted that `internal/logging` implements the capture rule, the counts and the traces. It implements none of them. Correction sub-bullets are now under all six tasks, following the T039 precedent.
- **CON-009**: DEC-G1..G8 were defined only in the PR body. Every prior series is a structured block in `state.yaml`, and `state.yaml` itself records that 10a's PR description was replaced wholesale after review — so eight rationales were one rewrite away from being unreachable. Mirrored into `state.yaml`.
- Also applied: CON-006 (T040's residual for T072 answered rather than dropped — no other record needs the counts, and why), CON-007 (the field table claimed an entry was still owed by T056, which is complete; it now records which batch delivered each formerly-owed field), CON-008 (DEC-G1's repetition claim now matches the shipped bound), CON-010 (T067 asserts three of FR-076's four clauses, not four — a package with no user-visible output cannot observe "exactly one warning").
- **CON-002 is not fixed and needs a human decision.** This branch carries `e0a0c39`, the 10a record reconciliation, which is not on `origin/main` — so the PR would be three commits, and `state.yaml` (written by that commit) says the two competing 10a records must be resolved before either reaches main. Disclosed in the PR body under "Carried prerequisite"; merging as-is would publish one side and bury the other.
- Mutants now 30 built, 29 killed. Validation re-run after the fix: `make check`, native darwin/arm64 build, `internal/app` back to 100.0%.
- Next: correctness and adversarial review against the fixed tree; resolve CON-002 with the user.

### 2026-09-18 — Batch 10b correctness + adversarial review: two blockers in my own code, both fixed
- Both stages ran against the contract-fixed tree and **found the same two defects independently** — `COR-002 == ADV-002` and `COR-001 == ADV-004`. That convergence is the strongest signal in this review. Correctness demonstrated the severe one as **1 surviving record versus 12** under identical conditions.
- **COR-002/ADV-002 was the serious one, and it was mine.** `internal/gui` called `Logger.Degraded()` after every post, on the Fyne event goroutine. `Degraded()` calls `Flush()`, which submits a barrier and waits 250ms; on timeout it calls `failLocked`, whose own comment reads "disables this logger permanently". So one slow write — a vault on a network mount, an fsync after a wake from sleep — discarded every record of every later post in the session, **and froze the window for a quarter second per post**. The code I added to satisfy FR-076 could cause the exact outage FR-076 exists to report. I confirmed the mechanism myself by reading `queue.go` before accepting the finding. The CLI was never exposed: it asks once, after `Close`.
- Fixed by adding `logging.DegradedSoFar()` — reads only the two already-settled states (`openErr`, the writer's latched error) and submits no barrier. What it gives up is synchronicity: a write that failed but is still queued is not visible until the next call or `Close`. An open failure, the common case and the one a user can act on, needs no flush at all.
- **The fix was completely unguarded, and only a mutant showed it.** Swapping the GUI back to `Degraded` survived the entire suite, because every test in `internal/gui` supplies `degraded` as a closure that answers instantly — none can observe a flush barrier. Added `TestTheWindowNeverAsksTheFlushingDegradedQuery`, an AST scan following T039's `os.Exit` precedent in `cmd/mp`; an AST scan rather than a grep because `run.go`'s comments legitimately name `Degraded` while explaining why it is not called. The mutant is now killed.
- **ADV-001**, found by adversarial alone: the `Details: <log path>` suppression was keyed on `warning != ""`. The warning is spent once per session — so if it was spent on a *successful* post, every later **failed** post got the stale invitation back, pointing the user at an unwritten log on the one post they actually need to diagnose. My own test only covered the case where the first post is the failing one, which is why the suite was green. `check()` now returns `(warning, lost)` and the suppression keys on `lost`.
- **COR-001/ADV-004**: a panic in `Name()` on a post that otherwise *succeeds* lost its trace, while the contract text I wrote asserted the terminal record carries it. The two requirements are scoped differently and I had conflated them: FR-068 is scoped to the post's outcome, FR-071 to trace *availability*. The trace now goes on the successful terminal record; the body still does not, and a mutant that adds both is killed by two tests.
- Test-quality findings applied: `fieldOf` now fails loudly on duplicate event names rather than silently checking the first (COR-003); a dominated, unfailable assertion dropped from T067's test (COR-004); `TestDefaultsKeepTheMessageBodyOffSuccessfulRecords` renamed to `TestTheShippedDefaultsRestrictCaptureAndCollectTraces` to say what it actually asserts, since the old name promised a record-level property its body never checked (COR-005).
- **ADV-003 is filed, not fixed** — recorded as open question **DEC-ADV-A**. The captured body is unbounded user text written once per failed destination: one 15 MiB message with two failing sinks produced 2x amplification and a single record 15x the configured rotation threshold, because rotation is evaluated *before* the write so a threshold cannot bound a record. Not a regression — no record carried the body before this batch — but it is a product decision about what the log promises, the same shape as 10a's DEC-ADV-006.
- **What the adversarial stage could not break, which is worth recording as much as what it could:** no credential leak was constructible through either new field. A `debug.Stack()` dump renders frame arguments as hex words and never string contents, and `describePanic` renders only the panic value's type — it tried a sink whose receiver holds a `config.Secret` panicking with a struct carrying a second credential, and neither reached the log. The CON-001 rescue fix was independently re-checked against the *real* telegram sink over HTTP and holds.
- Mutants now **37 built, 35 killed**. Two survivors, neither worked around: `keepTrace`'s redundant guard (deleted), and V5 — a reordering of `check()` — which I proved is an **equivalent mutant** rather than a coverage gap, since `warned` is only ever set after a successful consult so both orders behave identically on every sequence.
- Validation re-run: `make check`, native darwin/arm64 build, `internal/app` 100.0%, `internal/logging` 99.5%, `internal/gui` 73.8% (up from 73.4%, still under the 74.2% baseline for the untestable `Run` statements), `GOOS=linux GOARCH=386|arm go vet` clean.
- Next: CON-002 still needs a human decision (this branch carries the competing 10a record commit), and DEC-ADV-A needs filing as an issue.
- **Swept for dangling citations after the renames, and found one of my own plus one pre-existing.** A rename during COR-005 left `DEC-G7` in both `state.yaml` and the PR body citing a test name that no longer existed — the precise failure this project has recorded twice (an id or name that reads as authoritative and resolves to nothing), re-created by a fix for a different finding. Both corrected. The sweep also found `internal/post/recorder_test.go:798`, a doc comment naming `TestErrorTypeClassifiesAFailureWithNoError` above a function called `TestErrorTypeIsSpecificEvenWithNoError`, pre-existing since Batch 6c-2 (`9287366`); fixed, one line, no behaviour. The work log's own mention of the old name is left as written, because recording a rename means naming what was renamed.
- Worth considering as a durable check rather than a per-batch sweep: every `Test…` name cited in `.agents/`, `specs/` or a Go doc comment should resolve to a defined test function. A dozen lines of Go would make it a test rather than something a reviewer has to notice.

### 2026-09-18 — Split the 10a record commit onto its own PR (CON-002 resolved)
- User decided to put the carried 10a record commit into a new PR of its own, which is CON-002's required outcome. Created `sgykfjsm/batch-10a-records` at `e0a0c39`; because that commit's parent is already merged `main` (`0e3df5e`), the branch is **exactly one commit** and needed no rebase, cherry-pick or history rewriting.
- The PR body is `.agents/batch-10a-records-pr-body.md`, and it leads with the conflict rather than burying it: a side-by-side table of this record against `d057f45`'s — `not-final-at-merge` vs `passed-with-notes`, three cycles vs two, an adversarial stage that never completed vs `findings-none-blocking` — the manifest evidence for each, and an explicit instruction that if the reviewer prefers the other record they should close this PR and merge `d057f45` instead. What must not happen is both being merged, or one merging without the conflict being noticed.
- `d057f45`'s one genuinely better fact is carried across with attribution: it verified **tree equality** (`0e3df5e` and reviewed tip `73d9c57` share tree `a26e21c`) rather than matching head SHAs.
- The records branch is left at exactly `e0a0c39`; the PR-body artifact is committed on the 10b branch with the rest of the `.agents/` preparation, since the description itself is supplied through the API rather than needing to live in the branch.
- **Batch 10b now has a sequencing obligation**: it still contains `e0a0c39` and must be rebased onto the new `main` once the records PR merges, so its own PR carries only its own commits. Recorded in `state.yaml` and in 10b's PR body, because a rebase nobody remembers is how the conflict would quietly come back.
