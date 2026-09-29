## Summary
The diagnostic log now rotates losslessly. The active file's creation time and size are
captured when it is opened, both of FR-072's conditions are evaluated before every write,
and rotation renames the active log with a local-time `YYYYMMDDhhmmss` suffix before
opening a fresh one. Nothing is ever deleted, expired, compressed or overwritten: a
colliding rotated name takes `-1`, `-2`, … and the name is claimed with `O_CREATE|O_EXCL`
so two racing rotations cannot be handed the same one.

## Batch
Batch 10a / US5: T065, T068–T070, issues #66, #69–#71. Based on merged Batch 9 (#125).
10b (T066–T067, T071–T074: message capture, traces, the degradation warning) stays out.

## Decisions
- **DEC-F1** — rotation is built into `logging.Open` rather than supplied through
  `Options.Writer`, which batch 4 had anticipated. The writer owns the file's whole
  lifecycle either way, but inside the package it reuses `openLogFile`, so the
  non-regular-file refusal, `O_APPEND`, FR-075's directory creation and FR-076's
  degradation reporting are discharged once for every handle — the first and every one
  after a rotation — instead of being re-implemented by each front door. `Options.Writer`
  keeps its meaning and now documents that a supplied writer is never rotated.

  **DEC-F1 supersedes the premise of the Batch 4 acceptance note on issue #69** (review
  finding CON-006), which added three boxes to T068 because `Options.Writer` "bypasses two
  protections". It does — which is why rotation does not go through it. All three boxes are
  discharged, by construction rather than by three new guards:

  1. *A FIFO at the log path must not block the rotating writer's open.*
     `(*rotatingWriter).open` calls `openLogFile`, for the first handle and after every
     rotation; `openLogFile` refuses a directory, a FIFO and a socket before opening
     (`internal/logging/logger.go`, `usableAsLog` / `fileKind`).
     `TestOpenDoesNotBlockOnANonRegularFile` (`logger_unix_test.go`) is the evidence: it
     builds a real FIFO with `syscall.Mkfifo` and requires `Open` to return within a
     deadline, degrade, and let the post run. `usableAsLog`'s refusal of
     `os.ModeNamedPipe` is pinned by mode in `TestUsableAsLogAllowsWhatCanBeAppendedTo`.
     `TestOpenRotatingRefusesANonRegularPath` is the in-package check that `openRotating`
     inherits that refusal rather than reimplementing the open; it uses a directory,
     because what it pins is which code path is taken and a directory is the one
     non-regular file every platform can build in a test.
  2. *Rotation must open with `O_APPEND` and never `O_TRUNC`.* The same one call site is the
     only place this package opens the active log:
     `os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm)`.
     `TestRotateSizeCountsWhatWasAlreadyOnDisk` fails if a reopen truncates.
  3. *If the writer buffers, records must reach disk before the process can exit.*
     It does not buffer. `rotatingWriter.file` is a bare `*os.File`, and `bufio` appears
     nowhere under `internal/`, so the `os.Exit` durability property Batch 4 measured is
     unchanged.

  The note also predates this decision on the state side: `.agents/state.yaml`'s ADV-D-002
  entry is annotated as superseded in part — its decision stands, only its
  "`Options.Writer` is where rotation's rename/stat/reopen logic will live" clause does not.
  Issue #69 itself is left as written; the record is corrected here and in `state.yaml`.
- **DEC-F2** — the rotated name is claimed by creating it `O_CREATE|O_EXCL`, not by
  asking whether it exists and then renaming onto it. Stat-then-rename is a
  check-then-act that silently overwrites a rotated log when it loses the race, which is
  what FR-074 and constitution principle VI forbid.
- **DEC-F3** — `rotate_size_mib` and `rotate_after_days` are clamped where they are
  converted, in `internal/logging`. `config.Validate` bounds both keys from below only, and
  an unclamped conversion wraps negative — which does not disable rotation, it rotates on
  every single write.

  It is DEC-C3's shape but not DEC-C3's situation, and the difference matters. DEC-C3's
  clamp was interim because #114 was open and owned the load-time bound, and that deferral
  was written down where a reader would meet it (`tasks.md:120`,
  `contracts/telegram-sink.md:63`). **No issue owns a load-time upper bound for these two
  keys.** #109 and #114 did own numeric upper bounds, and both are closed and scoped to the
  timeout keys; #101 is about bounding the *size of the error message* validation produces,
  and says nothing about numeric ranges. So this clamp is not interim — it is the only guard
  there is, and this PR records it on both rows of
  `specs/001-dual-sink-quick-post/contracts/config-schema.md` so a reader of the settings
  contract meets it there rather than discovering it in `internal/logging`. Adding a
  user-visible bound in `internal/config` is out of this batch's scope.
- **DEC-F4** — `open` dates a file of **exactly zero bytes** from the process clock rather
  than from the filesystem's creation time. A file that already holds records keeps the
  filesystem's answer, which is FR-072's literal text. This is the batch's **only production
  behaviour change**, and it came from the first review correction pass rather than from the
  original implementation; the Validation section below describes the failure it fixes and
  the consequence it accepts.

  **The authority is A-011, and this extends A-011 rather than narrowing it.** A-011's
  precondition is a platform that "does not expose" a creation time, where darwin here
  exposes one that is merely implausible; and A-011 sanctions "the oldest available
  timestamp", where `w.now()` is the newest. It is narrower only in *scope* — a zero-byte
  file. What makes the extension sound is that it keeps the property A-011's substitution was
  chosen for: `research.md` R-006 resolves A-011 with a `ModTime` fallback whose whole defence
  is that it is not *earlier* than the creation time, so the age is never overstated and the
  log never rotates early. "Not earlier" is this project's established reading of A-011's
  intent, and `w.now()` on an empty file satisfies it unconditionally — where `ModTime` only
  satisfies it absent a preserved-timestamp copy or a restore from backup. So `spec.md` is not
  edited; the decision, its authority and its accepted consequence are recorded in
  `.agents/state.yaml`.

## Validation
`make check` (gofmt, vet, full `-race` suite) and a native darwin/arm64 build.
`internal/logging` is at 99.5% statement coverage against a 99.3% baseline, with the only
uncovered blocks pre-existing. 28 mutants were built across the new guards — both
thresholds and their off-by-ones, each disable-guard, `O_EXCL`, the collision numbering,
the suffix layout and its time zone, both clamps, the placeholder cleanup, the archived
handle's close, the birthtime lookup and both its fallbacks, and each of the four wiring
sites from settings to writer — and all 28 were killed. Three needed a new test first:
a descriptor-leak guard, an age-triggered rotation through `Open`, and the bounded
collision search.

A first review correction pass then added one behaviour fix and nine new or reworked tests,
and nine further mutants were built and killed. (All nine were rebuilt and re-verified as
killed during the second pass below; `work-log.md` had recorded eight, which was the
undercount, not this figure.)

- **A rotation now always clears its own trigger.** The age condition subtracts the process
  clock from whatever the filesystem recorded, and nothing keeps the two together: a log
  directory on a network mount whose server clock is days behind, or a darwin volume
  reporting a non-zero but nonsensical `Birthtimespec`, made a file created a microsecond
  ago already expired, so every write rotated — measured as five writes producing five
  archives, the first of them zero bytes. `open` now dates an *empty* file from the same
  clock the comparison uses; a file that holds records keeps the filesystem's answer, so a
  genuinely old **non-empty** log still rotates on the first write after a restart. The
  accepted consequence is the other half: a pre-existing, genuinely old, **empty** log no
  longer rotates on its first write — it is simply appended to, which loses nothing because
  the rotation it skips would have archived zero bytes. This is **DEC-F4** in the Decisions
  section above and in `.agents/state.yaml`; the authority is **A-011**, extended rather than
  narrowed, and there is no `spec.md` edit.
- The stat-failure branch of `open` leaked one descriptor per record while the failure
  persisted, and deleting its `Close` left the whole suite green. It now has a
  descriptor-count guard, the sibling of the archived-handle one.
- The day-to-duration conversion and the age boundary are anchored to literal hours.
  Every age assertion had been written as a multiple of `dayDuration`, so setting it to
  23 hours passed the entire suite.
- Close racing a straggling write — the production arrangement, since `recordQueue`'s worker
  closes the writer while a `PostLogger` goroutine may still be emitting — is covered at
  both layers. Removing `Close`'s mutex is now a race-detector failure.
- Close-failure propagation is pinned end to end: a `(*os.File).Close` reporting a deferred
  write error reaches `Logger.Close` and `Degraded`, and a `Write` that failed for three
  reasons at once reports all three. Four surviving error-composition mutants are killed.
- `internal/logging`'s tests compile on the 32-bit platforms the package builds on again
  (`GOOS=linux GOARCH=386|arm|mips|mipsle go vet ./internal/logging/`). The size clamp's
  boundary is inherently 64-bit, so it is scoped to a separate file that says why.

A second correction pass then fixed three tests that could not fail on the property they
named, and corrected two doc comments. Six further mutants, all killed or promptly
reported:

- **The line that dates the active log had no failing witness.** Replacing
  `w.createdAt = creationTime(info)` with the zero time, with `time.Unix(0, 0)`, or with
  `info.ModTime()` left the whole package green. Every age assertion either used an empty
  file — whose `createdAt` the fix above overwrites, so the line is dead for it — or seeded
  a genuinely old file and asserted that it *does* rotate, which an arbitrarily early value
  satisfies too. Nothing tested the too-old direction, and a log dated in 1970 would be
  archived on the first write of every run: one file per post, for a short-lived CLI.
  `TestReopeningANonEmptyLogDatesItFromTheFileAndNotTheClock` now fails if `open` dates a
  pre-existing non-empty log earlier *or* later than the file does, killing the first two
  substitutions and a `createdAt` pinned to `now()`.
  `TestOpenDatesAPreExistingLogFromItsBirthTimeAndNotItsModificationTime` kills the third
  through `open()` on darwin, which is the only build where the two expressions differ; the
  portable test says so where a reader will meet it.
- **The `Logger`-layer close/write race test could not fail.** It emitted through
  `PostAsync`, and `recordQueue` runs a single worker that drains the queue and only then
  closes the writer — so the write and the close were strictly ordered on one goroutine and
  could never overlap. Deleting `s.target = nil` from `safeWriter.close`, the only guard
  stopping a post-close write from reaching the released handle, left it passing. It now
  emits through the synchronous `Post`, which runs on the caller's goroutine: that mutant is
  killed 12 times out of 12 under `-race`, and the unmutated test is green 12 out of 12.
- **A failing write in the writer-layer close/write race test hung instead of failing.** The
  closer goroutine waited on a channel closed only by the first *accepted* write, so a full
  disk, a revoked mount or any unexpected `Write` error parked it forever while the main
  goroutine waited on the close — turning recorded `t.Errorf` calls into a package-wide
  `go test` timeout panic. The closer is now released on every exit path; with a forced
  write error the test FAILs in 0.00s naming the error.
- Two doc comments understated the `ModTime` fallback. It read as "a log that rotates later
  than a reader expects", true within one process. `mp` is a short-lived CLI writing through
  `O_APPEND`, so on any build where `creationTime` resolves to `ModTime` — every non-darwin
  `GOOS`, and darwin on a volume with no birth time — each run measures the log's age from
  the previous run's last post and the age trigger never fires at all. Both comments now say
  that. The behaviour is unchanged and the underlying question is filed as **DEC-ADV-006**,
  which is recorded as an open question — not a decision taken — under batch 10a in
  `.agents/state.yaml`, and is item 4 of the deferred list below. Both comments name the id
  and say where it lives, because an id cited in shipped source and defined nowhere is exactly
  the failure this project already recorded for DEC-F1..F3.
- `TestOpenRotatingRefusesANonRegularPath`'s doc comment described a FIFO blocking inside
  `open(2)`; its body creates a directory. It now describes what it does, and box 1 above
  cites the test that actually builds a FIFO.

A third and final correction pass then defined the id the second pass had coined, corrected
two factual claims in the same two doc comments, and closed one more vacuous test. One
further mutant:

- **The Logger-layer close/write race test could pass having asserted nothing.** All eight
  writers and the closer were released by one `close(start)`, so `Logger.Close` could reach
  `safeWriter.close` first: `target` is cleared, all 160 `Post`s are discarded silently,
  `firstErr()` is nil because no write ever reached a handle, and the JSON-per-line loop
  iterates an empty file. Its sibling documents this exact mode and fixes it with a
  `sync.Once` released by the first accepted write; this one had not been given the same
  treatment. It now is, plus a positive assertion that at least one record landed. Making
  `Open` install a discarding writer — which the old test passed — now fails it 5/5, and the
  guard it exists for is unchanged: deleting `s.target = nil` from `safeWriter.close` is still
  killed 12 out of 12 under `-race`.
- Two claims in `birthtime_other.go` and `birthtime_darwin.go` were wrong rather than
  understated. "ModTime is never earlier than the creation time" is false on exactly the
  platforms `birthtime_other.go` serves — `utimensat` sets mtime freely, so `cp -p`, `rsync
  -a`, `tar -x` or a restore from backup leaves mtime behind the inode's creation and the
  first write rotates immediately. (It is unreachable on darwin/APFS, which clamps the birth
  time *down* to a backdated mtime.) And "never sees the age condition fire on this build" is
  true of the CLI only: `internal/gui/run.go` holds one `Logger` for the whole session, so a
  window open longer than `rotate_after_days` does fire the trigger, measured from session
  start. Both comments now state the real guarantee, say "per open" rather than "per run",
  and note that `open`'s `w.size == 0` branch bounds the backdated case to one rotation.
- The darwin birth-time test's `t.Skip` covered only a volume with no birth time. A volume
  that records one but stamps mtime too coarsely to differ from it (FAT32 at 2s, HFS+ at 1s)
  reached a `t.Fatalf` and reported an unsupported environment as a rotation bug. It now
  skips. On APFS the assertion is unweakened: `w.createdAt = info.ModTime()` still fails it.

Coverage is unchanged at 99.5%. 16 correction-pass mutants in total across the three passes.

Not claimed: Intel Mac, a non-darwin platform, and a real multi-day-old log (age is driven
by a clock seam in unit tests and by backdating the file end to end). A 32-bit platform is
vetted and compiled, not run.

## Scope
No change to the event vocabulary, redaction, the degradation contract, settings schema,
sink behaviour or exit status. `contracts/config-schema.md` gains prose describing the
clamp that already exists; no key, default or validation rule changes. Issues stay open
until this is reviewed and merged.

Deliberately not addressed here, and to be filed separately — four items, which is the
whole list:

1. Two `mp` processes sharing one log path: after a rotation the older process keeps
   appending to the archived file, and each writer's size counter is per-process.
2. A symlinked `logging.path`: rotation renames the link itself.
3. `safeWriter`'s truncated-line repair re-entering the rotation predicate: the recovery
   newline is a second `Write`, so it is measured against both thresholds again and can
   itself rotate, landing in the new active file while the fragment it was meant to
   terminate stays in the archive.
4. The behavioural remedy for **DEC-ADV-006**: on every build where `creationTime` falls back
   to `ModTime`, `rotate_after_days` measures the age of the *open handle* rather than of the
   log. Whether to make it meaningful (statx, a Windows `Sys()` path, a sidecar open-time
   stamp) or to accept "darwin with a recorded birth time" as the support boundary is the
   product question. This batch records the question and its options; it does not answer it.

All four are lossless and delete nothing, so FR-074 holds, and all four need a product
decision about what the log promises. This section has twice been shorter than
`work-log.md`: it listed two items where the work log listed three, and then three where the
work log listed four.
