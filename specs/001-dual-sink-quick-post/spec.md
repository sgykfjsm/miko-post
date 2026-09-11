# Feature Specification: miko-post v0.1 — Dual-Sink Quick Post

**Feature Branch**: `sgykfjsm/feature-definition-design-doc`

**Created**: 2026-09-01

**Status**: Draft

**Input**: User description: "Read the approved MVP design document at `docs/design.md` and produce the v0.1 feature specification for `miko-post` — a low-friction capture tool that sends one short message to two independent destinations (Telegram and an Obsidian Daily Note) from either a CLI or a minimal GUI, with structured diagnostics."

## Clarifications

### Session 2026-09-01

- Q: For the chat destination, are the formatting-mode and formatting-fallback settings honored at runtime, and does the application escape reserved formatting characters before the first attempt? → A: Fixed behavior, no escaping. The first attempt always uses the rich-formatting mode and sends the message verbatim; exactly one unformatted rescue always follows a formatting-parse rejection. Both settings keys are accepted and validated for forward compatibility but do not alter v0.1 behavior. Ordinary punctuation therefore makes the rescue path common, and that is normal operation rather than a defect.
- Q: For log rotation, what timestamp does "log age" measure from, and when is the rotation condition evaluated? → A: Age is measured from the active log file's creation time, captured when the file is opened. Both the size and the age conditions are evaluated before each write.
- Q: What process exit status does the windowed path produce, and what does auto-close do? → A: Auto-close terminates the process, and the exit status follows the same rule as the command line (`0` only when every enabled destination succeeded). Any interaction with the window cancels the auto-close timer so a failure report can be read past its delay.
- Q: When settings are invalid or every destination is disabled and the user launched the windowed path, how is the failure surfaced? → A: A minimal error window shows the actionable message and the resolved settings path, with no message field. Dismissing it exits with failure.
- Q: If the diagnostic log cannot be written, what happens to the post? → A: The log directory is created if missing; if logging still cannot proceed, destinations run normally and results are reported as usual, with a single warning naming the log path and the reason added to the user-visible output. Exit status reflects destination outcomes only.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Capture a thought from the terminal (Priority: P1)

Someone is already in a terminal and has a thought they want recorded. They type the command
name followed by the message and press Enter. The message is delivered to their chat and appended
to today's note, and they get a one-glance report of what happened. They never leave the terminal
and never open another application.

**Why this priority**: This is the smallest complete slice that delivers the product's core value —
one message, two destinations, minimal friction. Everything else in the feature is either a second
front door onto this same path or a way to see what it did.

**Independent Test**: Run the command with a message and a configuration that enables both
destinations. Verify the message arrives in the chat, appears as a new line in today's note, and
that the command reports both outcomes and exits successfully.

**Acceptance Scenarios**:

1. **Given** a valid configuration with both destinations enabled, **When** the user runs the
   command with a single quoted message, **Then** the message is delivered to both destinations,
   both outcomes are reported, and the command exits with success.
2. **Given** the same configuration, **When** the user supplies several bare words as separate
   arguments, **Then** those words are joined with one ASCII space each and the joined text is
   what both destinations receive.
3. **Given** a valid configuration, **When** the user supplies a message that is only whitespace
   (ASCII spaces, full-width spaces, tabs, or line breaks in any combination), **Then** no
   destination is contacted at all, the user is told the message is empty and asked to correct it,
   and the command exits with failure.
4. **Given** a valid message, **When** the message has leading or trailing whitespace, **Then**
   both destinations receive the message exactly as the user typed it, including that whitespace.

---

### User Story 2 - Capture a thought without a terminal (Priority: P1)

Someone is not in a terminal. They launch the application with no arguments and a small window
appears with the text field already focused. They type — including multiple lines — and send with
a single keystroke. The window shows what happened and then closes itself, so no cleanup is
required.

**Why this priority**: The design commits to two equally supported front doors. Without this,
the tool is only usable by someone who already has a shell open, which is exactly the friction
the product exists to remove.

**Independent Test**: Launch with no arguments, type a multi-line message, send with the keyboard
shortcut, and verify both destinations received it, that the result panel names both outcomes, and
that the application exits on its own afterwards with a status matching the outcomes.

**Acceptance Scenarios**:

1. **Given** the application is launched with no arguments, **When** the window appears, **Then**
   the message field already has keyboard focus and no click is needed before typing.
2. **Given** text has been typed, **When** the user presses the send keystroke (`Cmd+Enter`) or
   activates the Send control, **Then** the same validation and submission path runs for both,
   sending is disabled for the duration so the message cannot be submitted twice, and a compact
   per-destination result is shown when every enabled destination has finished.
3. **Given** the user is typing, **When** they press `Enter`, **Then** a line break is inserted and
   nothing is sent.
4. **Given** the window is open, **When** the user presses `Esc` or activates Cancel, **Then** the
   window closes without contacting any destination.
5. **Given** a completed submission where every enabled destination succeeded, **When** the result
   is displayed, **Then** the application closes and terminates on its own after the configured
   success delay (15 seconds by default), exiting with success.
6. **Given** a completed submission where at least one enabled destination failed, **When** the
   result is displayed, **Then** every failed destination and a short reason for each is shown, and
   the application closes and terminates after the configured failure delay (30 seconds by
   default), exiting with failure.
7. **Given** a result is displayed and its auto-close delay is counting down, **When** the user
   interacts with the window at all (any keypress, click, or focus), **Then** the auto-close timer
   is cancelled and the result remains on screen until the user dismisses it themselves.
8. **Given** the window is open, **When** the user presses `Cmd+Q`, **Then** the application quits.

---

### User Story 3 - Never lose a message to a single broken destination (Priority: P1)

One destination is broken — the network is down, a token is wrong, or a directory is not writable.
The user still wants the other destination to receive the message, and wants to be told plainly
which half failed so they can decide whether to re-send.

**Why this priority**: Two destinations only add value if they fail independently. If one outage
can suppress the other, the product has no advantage over a single destination and the user's
thought is silently lost.

**Independent Test**: Make exactly one destination fail (e.g. point it at an unreachable endpoint
or an unwritable path), send a message, and verify the healthy destination still received it, that
the report names the failed destination with a short reason, and that the run exits with failure.

**Acceptance Scenarios**:

1. **Given** both destinations are enabled and one will fail, **When** a message is posted, **Then**
   the other destination still receives the message in full and its success is reported alongside
   the failure.
2. **Given** both destinations are enabled and both will fail, **When** a message is posted,
   **Then** both failures are reported, each with its own short reason, and neither failure is
   hidden behind the other.
3. **Given** any outcome other than "every enabled destination succeeded", **When** the run
   finishes from either front door, **Then** the process exits with failure.
4. **Given** a destination that hangs, **When** its configured overall time limit elapses (60
   seconds by default), **Then** it is reported as a timeout failure and every other destination
   still runs to completion and reports its own real outcome.
5. **Given** any run that produced at least one failure, **When** the result is displayed, **Then**
   the location of the detailed diagnostic log is included in the output.

---

### User Story 4 - Deliver reliably despite message formatting (Priority: P2)

The user writes naturally — punctuation, emoji, Japanese text — without thinking about markup
rules. The chat destination should still accept the message rather than rejecting it over
formatting, and the user should not have to know that a rescue attempt happened.

**Why this priority**: Without this, ordinary punctuation can cause a total delivery failure for
the chat destination. It ranks below the core paths because the message is still preserved in the
note destination when it fails, but it directly determines how often the tool appears broken.

**Independent Test**: Send a message whose formatting the chat service rejects, and verify the
message is nonetheless delivered as unformatted text, that the run is reported as an overall
success, and that both the initial rejection and the rescue are recorded in the diagnostic log.

**Acceptance Scenarios**:

1. **Given** any message, **When** the first delivery attempt is made, **Then** the message is sent
   verbatim under the rich-formatting mode with no escaping, transformation, or normalization of
   characters the formatting mode reserves.
2. **Given** the chat destination rejects a message specifically because it could not parse the
   message's formatting, **When** the rescue path runs, **Then** the message is re-sent exactly
   once as unformatted text with no formatting mode applied.
3. **Given** the unformatted re-send succeeds, **When** results are reported, **Then** the chat
   destination counts as an overall success and does not cause a failure exit code.
4. **Given** the unformatted re-send also fails, **When** results are reported, **Then** the chat
   destination is reported as failed and both attempts are preserved in the diagnostic log.
5. **Given** the chat destination fails for any reason other than a formatting-parse rejection
   (network error, timeout, authorization failure, unknown chat, service error), **When** that
   failure occurs, **Then** no second attempt of any kind is made.
6. **Given** settings that specify a formatting mode or that disable the formatting fallback,
   **When** a message is posted in this version, **Then** delivery behaves identically to the
   defaults: the settings are accepted and validated but do not change what is sent or whether the
   rescue runs.

---

### User Story 5 - Reconstruct what happened after the fact (Priority: P2)

Later — possibly much later, possibly with help from an assistant — the user needs to answer
"did that message actually go out, and if not, what was it?" They open the diagnostic log and find
machine-readable records that tie together everything that happened during one post.

**Why this priority**: The design explicitly makes diagnostics a first-class MVP goal because there
is no retry queue in this version; the log is the only durable record of a failed attempt. It ranks
below the delivery paths because it is a recovery aid rather than the primary flow.

**Independent Test**: Perform one successful post and one failing post, then verify the log file
contains one valid record per line, that all records belonging to a single post share a correlation
identifier, that the successful post's records contain no message body, and that the failed post's
records do contain the original body.

**Acceptance Scenarios**:

1. **Given** any post, **When** it completes, **Then** every line written to the log is an
   independently valid, self-contained record.
2. **Given** a post that involved several destinations running at once, **When** its records are
   examined, **Then** they all carry the same per-post correlation identifier and can be separated
   from a concurrent post's records.
3. **Given** the default privacy setting, **When** a post fully succeeds, **Then** no message body
   appears in any of its records, though a message length may.
4. **Given** any enabled destination failed, **When** its records are examined, **Then** the
   original message body needed to reconstruct that post is present.
5. **Given** any post at all, **When** every record it produced is examined, **Then** no chat
   service credential appears in any of them.
6. **Given** a failure with an available diagnostic trace and trace collection enabled, **When** the
   failure is recorded, **Then** the trace is included; routine expected errors do not get an
   artificially manufactured trace.
7. **Given** the active log has reached its size threshold (10 MiB by default) or its age threshold
   measured from its creation time (7 days by default), whichever comes first, **When** the next
   write is about to occur, **Then** the active log is renamed with a local-time timestamp suffix,
   a new active log begins, and no previously rotated file is deleted.
8. **Given** the diagnostic log cannot be written at all, **When** a post is made, **Then** every
   enabled destination still runs and reports its real outcome, the user-visible output carries one
   warning naming the log path and the reason, and the exit status reflects only the destination
   outcomes.

---

### User Story 6 - Point the tool at the right configuration (Priority: P3)

The user keeps their real settings in a conventional per-user location and expects the tool to find
them without being told. Occasionally they want a single command-line post to use a different
settings file — for a test chat, say — without that choice leaking into the window-based flow.

**Why this priority**: Correct default resolution is required for every other story to work at all,
but the explicit override is a convenience for a narrow case, so the story as a whole sits last.

**Independent Test**: With no override, confirm both front doors resolve the same conventional
path. Then run a command-line post with an explicit settings file and confirm only that post used
it, while a subsequently launched window still used the default.

**Acceptance Scenarios**:

1. **Given** no override is supplied, **When** either front door starts, **Then** the settings file
   is resolved from the user-configuration convention, falling back to the standard home-relative
   location when the convention's environment variable is unset or empty.
2. **Given** an explicit settings file and a message, **When** the command runs, **Then** that file
   is used for this post only.
3. **Given** an explicit settings file and no message, **When** the command runs, **Then** the
   window does **not** open, the user is told the override is only available when posting from the
   command line, and the process exits with failure.
4. **Given** the user asks for help, **When** help is displayed, **Then** it shows the settings path
   actually resolved for the current environment rather than an unexpanded variable expression.
5. **Given** a settings file in which every destination is disabled, **When** the command line is
   used, **Then** no destination is contacted, the user is told to enable at least one, and the
   process exits with failure.
6. **Given** the same settings, **When** the windowed path is launched instead, **Then** a minimal
   error window appears showing the actionable message and the resolved settings path, with no
   message field, and dismissing it exits with failure.
7. **Given** a settings file that cannot be read or fails validation, **When** either front door
   starts, **Then** no destination is started with partially valid settings and the user is told
   what is wrong through the front door they used.

---

### Edge Cases

- **Whitespace-only input in every form**: a message consisting only of ASCII spaces, only
  full-width spaces, only tabs, only line breaks, or any mixture is rejected before any destination
  is contacted. Trimming is used *only* to decide validity — a message that passes is delivered
  exactly as typed, untrimmed.
- **Input that is not valid UTF-8**: on macOS a command-line argument is a byte string, so a
  terminal in a legacy encoding, a paste from a mis-decoded source, or `mp "$(cat somebinary)"` can
  deliver bytes that are not valid UTF-8. Such a message is rejected before any destination is
  contacted, with a correction prompt naming the encoding and distinct from the whitespace one
  (FR-009a). It is *not* delivered to one destination and refused by the other, which is what
  happened before the rule existed. Valid but unusual UTF-8 — emoji, combining marks,
  right-to-left overrides, an encoded U+FFFD the user genuinely typed — is ordinary text and is
  delivered unchanged.
- **Ordinary punctuation in an ordinary message**: because messages are sent verbatim under the
  rich-formatting mode with no escaping, everyday characters the formatting mode reserves (such as
  `.`, `-`, `!`, `(`) will routinely cause the first attempt to be rejected and the unformatted
  rescue to succeed. This is expected steady-state behavior, not a defect, and the resulting log
  pattern must not be treated as an error condition.
- **Multi-line input reaching a line-oriented destination**: the note destination stores the whole
  message as one physical line, so embedded line breaks must be converted rather than written
  through. Carriage-return and carriage-return/line-feed sequences must normalize to the same
  result as a bare line feed, so the same text produces the same stored line regardless of where it
  was pasted from.
- **Today's note does not exist yet**: it is created when creation is permitted by settings, and not
  created when it is not — in which case the note destination fails while the chat destination is
  unaffected.
- **Today's note already has content**: appending must never rewrite, truncate, or reorder what is
  already there, including when the existing file does not end in a line break.
- **Both destinations fail at once**: both failures are reported and both are logged; neither is
  discarded once the first is known.
- **A destination exceeds its time limit**: it becomes a failure result rather than aborting the
  post; other destinations continue and report real outcomes.
- **The chat destination is configured without a thread**: the message posts to the chat directly
  rather than into a thread.
- **The credential is supplied by environment variable and by settings file**: the environment
  variable wins, and neither value is ever printed.
- **The window is dismissed while a post is in flight**: the post's outcome must still be recorded
  in the diagnostic log even if no one is left to read the on-screen result.
- **The user keeps a failure result on screen**: interacting with the window cancels auto-close, so
  the process may stay alive indefinitely until the user dismisses it; the exit status when it
  finally closes still reflects the post's outcomes.
- **A second post starts while a first is still running**: records from the two posts remain
  separable by correlation identifier, and both notes' appends land without corrupting each other.
- **The log directory does not exist**: it is created. If logging still cannot proceed — the path is
  unwritable, or the volume is full — the post proceeds anyway and the user is warned exactly once
  with the path and reason.
- **A destination succeeded but its diagnostics were lost**: the reported result and exit status
  reflect the real destination outcome, never the logging failure.

## Requirements *(mandatory)*

### Functional Requirements

**Entry points and dispatch**

- **FR-001**: A single distributed executable MUST provide both the command-line and windowed
  entry points.
- **FR-002**: Invoking the command with no message arguments MUST open the window using the
  default resolved settings path.
- **FR-003**: Invoking the command with one or more message arguments MUST post from the command
  line without opening a window.
- **FR-004**: Multiple message arguments MUST be joined with exactly one ASCII space between
  adjacent arguments, and the joined result is the message.
- **FR-005**: The settings-file override MUST apply to command-line posting only and MUST NOT
  change the settings used by the window.
- **FR-006**: Supplying the settings-file override without a message MUST print an error, MUST NOT
  open the window, and MUST exit with failure.
- **FR-007**: Help output MUST include the settings path as resolved for the current environment.
- **FR-008**: This version MUST NOT read message content from standard input.

**Message validation**

- **FR-009**: Before any destination is contacted, the message MUST be validated by trimming
  leading and trailing Unicode whitespace — including ASCII spaces, full-width spaces, tabs, and
  line breaks — and rejecting the message if the trimmed result is empty.
- **FR-009a**: The message MUST also be rejected when it carries bytes that are not a valid UTF-8
  encoding. This is a **second, distinct** rejection reason: it MUST produce its own correction
  prompt naming the encoding, and neither rejection reason may be reported for the other.
- **FR-010**: A rejected message MUST result in no destination being contacted, a correction prompt
  shown to the user, and a failure exit.
- **FR-011**: Trimming MUST be used for validation only; every destination MUST receive the
  original, untrimmed message.

> **The reading of FR-050 that FR-009a rests on** (issue #104, decision DEC-D4, taken 2026-09-09).
>
> FR-050 — "Written content MUST use UTF-8 encoding and line-feed line endings" — and its identical
> sentence in constitution principle VI are read as a requirement on **what this application accepts
> and emits**, not only on the byte-level encoding of the note file. Under that reading a message
> that is not valid UTF-8 cannot be carried, and FR-009a follows.
>
> This is a decision and not a derivation. The spec was silent, and its clauses conflict: FR-011 and
> FR-012 promise every destination the original message, while FR-050 and principle VI require
> UTF-8. It was settled by observing that the permissive reading's central promise is not true —
> **no destination keeps the bytes**. The chat service's documented contract is UTF-8 only and it
> answers `400 Strings must be encoded in UTF-8`, whose text does not match the formatting-rescue
> predicate, so that destination fails closed. FR-064's JSON Lines log cannot hold the bytes at all,
> since JSON is UTF-8 by definition and the handler substitutes U+FFFD silently — so **SC-008 and
> FR-068 are unsatisfiable for exactly these messages**. Obsidian re-serialises a note as UTF-8 at
> the user's next save of it. The behaviour before FR-009a was therefore *note succeeds, chat fails,
> exit 1*: a half-delivered post, which is the outcome the two-destination design exists to prevent.
>
> Refusing at validation does not engage principle VI's data-preservation clause: nothing is opened
> and nothing is written, and that principle's scope is appends to notes and content already there.
> Silently substituting U+FFFD before dispatch was considered and rejected — it is the only option
> that produces a stored artefact the user did not write.
>
> **Accepted cost**: a terminal that reliably produces non-UTF-8 bytes — a legacy Shift_JIS or
> EUC-JP locale is the realistic case — cannot use the command-line front door until its locale is
> fixed. Under the permissive reading that same user gets a half-delivered post every time instead,
> with no recoverable record; the correction prompt names the locale so the fix is actionable.
>
> **Unverified at the time of the decision**: whether the chat service refuses these bytes or
> accepts and substitutes them server-side. The evidence is the Bot API documentation and tdlib's
> source; no live call was made, because the constitution keeps live external services out of the
> test suite. If it accepts them, the failure is cosmetic rather than partial and the decision
> should be revisited. Recorded in issue #104.
>
> Reversible in both directions at any time: the rule is one guard, and notes already on disk are
> unaffected either way. What is not reversible is each individual post made without it.

**Posting orchestration**

- **FR-012**: Every enabled destination MUST receive the identical original message.
- **FR-013**: Enabled destinations MUST be started concurrently and MUST run to completion
  independently of one another.
- **FR-014**: The orchestrator MUST wait for and aggregate every enabled destination's result and
  MUST NOT return upon the first error.
- **FR-015**: Each destination invocation MUST be bounded by its own independently configurable
  overall time limit covering that destination's entire operation, defaulting to 60 seconds; a
  destination that exceeds it MUST yield a failure result while others continue.
- **FR-016**: Disabled destinations MUST NOT be invoked and MUST NOT count as failures.
- **FR-017**: Each destination result MUST distinguish a short, safe, human-readable reason
  intended for display from the detailed diagnostic error intended for the log.
- **FR-018**: Settings in which every destination is disabled MUST be treated as a startup error
  with an actionable message asking the user to enable at least one destination, no post attempt,
  and a failure exit.
- **FR-019**: This version MUST NOT queue or automatically re-send a failed chat message; the note
  entry and the diagnostic log are the record of the attempt.

**Windowed interface**

- **FR-020**: The window MUST contain a multi-line message field, a Send control, a Cancel control,
  and a compact result/error area, with no additional controls.
- **FR-020a** (accepted extension, issue #122): An optional absolute
  `gui.background_image_dir` MUST select one usable top-level PNG/JPEG image at launch,
  keep it fixed for that window, and display it faintly over black without reducing
  editor, control or result readability. Unusable sources MUST fall back to another
  candidate or black. Placement, format and decoding limits are specified in
  [the background contract](../../docs/gui-background.md). This decorative extension
  adds no control or image-posting capability.
- **FR-021**: The message field MUST hold keyboard focus when the window appears.
- **FR-022**: `Esc` MUST cancel and close without posting; `Enter` MUST insert a line break;
  `Cmd+Enter` MUST send; `Cmd+Q` MUST quit the application.
- **FR-023**: Controls and keyboard shortcuts MUST share the same validation and submission paths.
- **FR-024**: The Send control MUST be disabled for the duration of a submission so a post cannot
  be submitted twice.
- **FR-025**: The window MUST wait for every enabled destination to finish, then display a compact
  per-destination result.
- **FR-026**: The window MUST schedule an automatic close after a configurable delay: 15 seconds by
  default when every enabled destination succeeded, 30 seconds by default when at least one failed.
- **FR-027**: Automatic close MUST terminate the process, and the resulting exit status MUST follow
  the same rule as the command line.
- **FR-028**: Any user interaction with the window while an automatic-close delay is pending — a
  keypress, a click, or the window regaining focus — MUST cancel that automatic close, leaving the
  result on screen until the user dismisses it.
- **FR-029**: A failure display MUST name every failed destination with a short human-readable
  reason; detailed errors and traces MUST NOT be shown in the window.
- **FR-030**: When settings are invalid or every destination is disabled and the windowed path was
  launched, a minimal error window MUST display the actionable message and the resolved settings
  path, MUST NOT offer a message field, and MUST exit with failure when dismissed.

**Chat destination**

- **FR-031**: The chat destination MUST send text to the configured chat identifier.
- **FR-032**: When a thread identifier is configured, it MUST be passed as the message thread; when
  absent, the message MUST post to the chat directly.
- **FR-033**: The first delivery attempt MUST always use the rich-formatting mode and MUST send the
  message verbatim: the application MUST NOT escape, transform, or normalize characters that the
  formatting mode reserves.
- **FR-034**: The formatting-mode and formatting-fallback settings keys MUST be accepted and
  validated, but MUST NOT alter delivery behavior in this version; the first-attempt mode and the
  single unformatted rescue are fixed application behavior.
- **FR-035**: Only a rejection caused by the service being unable to parse the message's formatting
  MUST trigger exactly one retry as unformatted text with no formatting mode applied.
- **FR-036**: A successful unformatted retry MUST count as overall success for the chat destination
  and MUST NOT cause a failure exit code, while the original formatting failure is retained in the
  log.
- **FR-037**: If the unformatted retry also fails, the chat destination MUST fail and both attempts
  MUST be retained in the log.
- **FR-038**: The unformatted retry MUST NOT be used as a general retry for failures unrelated to
  formatting parsing.
- **FR-039**: The formatting fallback path MUST be observable through distinct, stable log events
  for the initial formatting failure and for the success or failure of the unformatted attempt.
- **FR-040**: Each chat request MUST be bounded by its own independently configurable request time
  limit, defaulting to 30 seconds; both attempts together remain bounded by the destination's
  overall time limit.
- **FR-041**: This version MUST NOT automatically retry transport errors, timeouts, error status
  codes, or service-reported errors.
- **FR-042**: The chat credential MUST be resolved with the dedicated environment variable taking
  precedence over the settings file, and it is the only setting with an environment-variable
  override in this version.
- **FR-043**: The chat credential MUST NEVER appear in logs, on-screen errors, command-line errors,
  or settings dumps.

**Note destination**

- **FR-044**: The note destination MUST append exactly one physical UTF-8 line to the end of the
  current day's note, resolved as the configured directory joined with the current date rendered
  through the configured filename format.
- **FR-045**: It MUST NOT search for, create, or insert into a named section within the note.
- **FR-046**: When the day's note does not exist and creation is permitted by settings, it MUST be
  created; when creation is not permitted, the destination MUST fail without affecting other
  destinations.
- **FR-047**: The message MUST be transformed in this exact order: normalize carriage-return/line-
  feed pairs and bare carriage returns to line feeds; replace every remaining line feed with the
  literal text `<br>`; prefix the result with `- ` followed by the current local time rendered
  through the configured time format and one space.
- **FR-048**: The transformed entry MUST be written followed by exactly one line-feed character.
- **FR-049**: The file MUST be opened in append mode so existing content is never rewritten,
  truncated, or reordered.
- **FR-050**: Written content MUST use UTF-8 encoding and line-feed line endings.
- **FR-051**: All date and time rendering for this destination MUST use local time.

**Settings**

- **FR-052**: Settings MUST be expressed in TOML.
- **FR-053**: The default settings path MUST resolve from the user-configuration environment
  convention, falling back to the standard home-relative configuration location when that variable
  is unset or empty.
- **FR-054**: The chat and note destination section names MUST be the namespaced forms
  (`[sink.telegram]`, `[sink.obsidian]`); top-level unnamespaced sections MUST NOT be part of this
  version's schema.
- **FR-055**: The settings schema MUST cover, at minimum: per-destination enablement; chat
  credential, chat identifier, optional thread identifier, formatting mode, formatting-fallback
  flag, and request time limit; note directory, filename format, time format, and create-if-missing
  flag; the per-destination overall time limit; the two window auto-close delays; and the log
  format, path override, size and age rotation thresholds, error-only message capture flag, trace
  collection flag, and version/commit inclusion flags.
- **FR-056**: An empty log path setting MUST mean "use the default state-directory path".
- **FR-057**: Application behavior — exit codes, encoding and line endings, destination concurrency,
  the note line transformation, and the formatting-fallback algorithm — MUST NOT be exposed as
  settings.
- **FR-058**: If settings fail to load or validate, no destination MUST be started with partially
  valid settings, and the failure MUST be surfaced through the front door that was used.

**Results and exit status**

- **FR-059**: The process MUST exit `0` only when every enabled destination succeeded.
- **FR-060**: The process MUST exit `1` for input errors, settings errors, startup errors, or any
  destination failure.
- **FR-061**: A chat delivery rescued by the unformatted retry MUST count as success and MUST NOT
  produce exit `1`.
- **FR-062**: Both front doors MUST name every failed destination, give a short reason for each,
  and make partial success visible.
- **FR-063**: User-facing failure output MUST include the path to the detailed diagnostic log.

**Diagnostics**

- **FR-064**: Logs MUST be written as line-delimited records, one independently valid,
  self-contained record per line.
- **FR-065**: The default log path MUST resolve from the user-state environment convention, falling
  back to the standard home-relative state location when that variable is unset or empty; a
  non-empty configured path MUST override it.
- **FR-066**: Records MUST carry, as applicable: a timestamp with time zone; a level and a stable
  event name; the originating front door; a per-post correlation identifier; destination name and
  outcome; duration in milliseconds; error type, detailed error text, and service status code when
  available; the note target path when relevant; and application version and commit when enabled.
- **FR-067**: Stable event names MUST at minimum cover: message received; note append started,
  succeeded, and failed; chat send started, succeeded, and failed; chat formatting rejected; chat
  unformatted attempt succeeded and failed; and request completed with and without error.
- **FR-068**: When error-only message capture is enabled, successful events MUST omit message
  content while still permitting a recorded message length; if any destination failed, the original
  message body needed to reconstruct that post MUST be recorded.
- **FR-069**: Secrets MUST NEVER be recorded.
- **FR-070**: When several destinations fail in one post, every failure MUST be logged; logging
  MUST NOT stop after the first error.
- **FR-071**: Diagnostic traces MUST be recorded for panics, unexpected errors, and failures where
  a trace is available and useful, subject to the trace collection setting; expected operational
  errors MUST NOT get an artificially manufactured trace.
- **FR-072**: The active log MUST rotate when **either** its size reaches the configured size
  threshold (10 MiB by default) **or** its age reaches the configured age threshold (7 days by
  default), where age is measured from the active log file's creation time captured when the file
  is opened. Both conditions MUST be evaluated before each write.
- **FR-073**: Rotation MUST rename the active log by appending a local-time timestamp suffix of the
  form `YYYYMMDDhhmmss` (for example `app.jsonl` → `app.jsonl.20260827114203`), after which a new
  active log begins.
- **FR-074**: This version MUST NOT automatically delete, expire, or compress rotated logs.
- **FR-075**: A missing log directory MUST be created before logging begins.
- **FR-076**: A failure to write diagnostics MUST NOT prevent any destination from running, MUST
  NOT change the reported destination outcomes, and MUST NOT change the exit status; it MUST add
  exactly one warning to the user-visible output naming the log path and the reason.

### Out of Scope for This Version

The following are explicitly excluded and MUST NOT be implemented, even opportunistically:

- Posting images or any non-text content.
- A settings-editing window.
- A retry queue or automatic re-send of failed chat posts.
- Reading the message from standard input.
- Automatic transport-level retries (the formatting fallback is not a transport retry).
- Escaping or rewriting the user's message to satisfy the chat service's formatting rules.
- Runtime selection of the formatting mode or of whether the unformatted rescue runs.
- Per-invocation destination toggles such as flags to skip one destination.
- Automatic retention, expiry, or deletion of rotated logs.
- Any destination type beyond the chat and note destinations.
- Platform-specific application bundles, code signing, notarization, and standalone installers.

### Key Entities

- **Message**: The user's text as originally entered. Has an untrimmed original form (what
  destinations receive) and a trimmed form (used only to decide validity). May contain line breaks,
  full-width characters, and emoji.
- **Post**: One user submission. Carries a correlation identifier, an originating front door, the
  set of enabled destinations, and the aggregated set of destination results. Succeeds only when
  every enabled destination succeeded.
- **Destination (sink)**: A named delivery target with an enabled flag, its own settings, and its
  own overall time limit. Runs concurrently with and independently of every other destination.
- **Destination result**: Per-destination outcome carrying the destination name, success flag, a
  short safe reason for display, and a detailed diagnostic error for the log.
- **Settings**: The validated set of destination, orchestration, window, and diagnostics options
  loaded from one TOML file, plus the environment-supplied chat credential.
- **Diagnostic record**: One self-contained log line describing one event within one post, tied to
  other records by the post's correlation identifier.
- **Daily note**: The append-only per-day file whose path is derived from the configured directory
  and the current date, and to which posts contribute exactly one line each.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A user with working settings can go from "I have a thought" to "it is recorded in
  both destinations" in a single command or a single window interaction, with no intermediate
  prompts, confirmations, or menus.
- **SC-002**: In 100% of runs where exactly one destination is broken, the healthy destination
  still receives the complete message.
- **SC-003**: In 100% of runs with at least one failure, the user-visible output names every failed
  destination, gives a reason for each, and points at the diagnostic log.
- **SC-004**: The reported exit status agrees with the actual per-destination outcomes in 100% of
  runs from either front door: success only when every enabled destination succeeded.
- **SC-005**: A message that ordinary formatting rules would reject is still delivered to the chat
  destination, and the user is not required to know that a rescue occurred.
- **SC-006**: In 100% of runs, no credential appears in any user-visible output or any log record.
- **SC-007**: 100% of log lines parse independently as valid records, and all records from one post
  can be gathered by its correlation identifier alone.
- **SC-008**: For any failed post, the diagnostic log contains enough information — original
  message, destination, error type, and detail — to re-send the message by hand without consulting
  any other source.
- **SC-009**: Appending to a day's note never alters or loses previously present content, across
  repeated posts, missing files, and files not ending in a line break.
- **SC-010**: A multi-line message occupies exactly one physical line in the day's note, with line
  breaks represented by the literal separator and a local-time prefix.
- **SC-011**: A destination that stops responding cannot delay a post beyond its configured overall
  limit, and cannot prevent another destination from reporting its own real outcome.
- **SC-012**: A user reading a failure report in the window can keep it on screen for as long as
  they need; no failure report disappears while the user is interacting with it.
- **SC-013**: In 100% of runs where diagnostics cannot be written, every enabled destination still
  runs, the real outcomes are still reported, and the user is told exactly once that diagnostics
  were lost.
- **SC-014**: All 20 acceptance criteria enumerated in §13 of `docs/design.md` are demonstrably
  satisfied.

## Assumptions

Reasonable defaults adopted where `docs/design.md` was silent. Each is cheap to revise if wrong.

- **A-001**: `docs/design.md` (English) is the normative source. Where `docs/design.ja.md` differs,
  the English document governs; the translation is not a second source of requirements.
- **A-002**: The technology decisions in the design document — implementation language and windowing
  toolkit, TOML settings, line-delimited JSON diagnostics, the XDG path conventions, the chat
  service, and the note application's daily-note layout — are **already-made product constraints**,
  not open choices to be revisited during planning.
- **A-003**: The stated keyboard shortcuts are specified for macOS. Behavior on other platforms is
  not a v0.1 requirement, and the tool is assumed to be used on macOS in this version.
- **A-004**: A single user runs the tool interactively, at low frequency. Concurrent posts are rare
  but must not corrupt the day's note or interleave log records unrecoverably; heavy concurrent
  throughput is not a design target.
- **A-005**: The note directory is a locally mounted path that may be synced by an external service
  (the example path is inside a Dropbox tree). The tool is not responsible for sync conflicts,
  locking against the sync client, or resolving divergent copies.
- **A-006**: The optional thread identifier is a numeric value in settings, and its absence — not a
  sentinel value — means "post to the chat directly".
- **A-007**: The final module path used for distribution is not yet decided (the design document
  leaves it as a placeholder). Choosing it is a planning-time task and does not affect any
  requirement in this specification.
- **A-008**: "Version" and "commit" recorded in diagnostics are stamped into the binary at build
  time; how they are stamped is a planning concern.
- **A-009**: A rotation timestamp suffix is precise to the second. Two rotations within the same
  second are assumed not to occur in normal single-user operation; if planning finds otherwise, a
  collision rule is a planning-level decision, not a change to these requirements.
- **A-010**: Message length recorded on successful posts is a character or byte count; the choice
  of unit is a planning detail with no user-visible requirement attached.
- **A-011**: The file creation time used for log age is available on the target platform. If a
  future platform does not expose it, substituting the oldest available timestamp is a planning
  decision that does not change FR-072's intent.
- **A-012**: The warning emitted when diagnostics cannot be written goes to the same place as other
  user-facing errors for the front door in use; it is not itself a destination failure.
