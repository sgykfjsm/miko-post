# Acceptance matrix: design §13 → tests

**Purpose**: Maps each of the 20 acceptance criteria in `docs/design.md` §13 to the tests that
show it holds (T085). Also records why any criterion, or part of one, is verified by hand rather
than automatically (T086). Together these satisfy SC-014 and the constitution quality gate "Every
acceptance criterion MUST have a corresponding automated test or an explicitly recorded
justification for why it is verified manually".

**Date**: 2026-09-25. **Status of manual rows**: all four are done. M1 was run live on 2026-09-28 and
passed, except `thread_id` topic routing, which is not applicable to the test chat (a DM). M2, M3
and M4 were run on the native window on 2026-09-29 and passed. See the Result log.

**Authority**: Where §13 and the spec disagree, `spec.md` (FR-/SC- ids) and its recorded
decisions govern. The Notes column flags each disagreement, and the last section lists them
together. The defaults in §13 (15 s / 30 s auto-close, 30 s request, 60 s per sink, 10 MiB / 7 days)
match `config.Defaults()` in `internal/config/settings.go` and `contracts/config-schema.md`. None
of them diverges.

**How the tests were confirmed**: Every test named below appears in `go test -list '.*' ./...`,
and every one passed on 2026-09-25 when run by exact name with `MIKO_POST_TELEGRAM_BOT_TOKEN`
unset. The cross-check was repeated mechanically: every `pkg.TestName` cited here was matched
against that list.

Coverage classes: **automated**, meaning in-process or process-level tests with no live
service. **automated (headless)**, meaning Fyne's test driver rather than the native glfw driver.
**partial — manual remainder**, meaning automated as far as the tests reach, with a named part
left to a manual check. **manual**.

## Matrix

| # | Criterion (design §13) | Spec reqs | Coverage | Tests | Notes |
|---|---|---|---|---|---|
| 1 | `mp` opens a Fyne GUI using the resolved default XDG configuration. | FR-002, FR-005, FR-053, FR-030 | partial — manual remainder (M2) | `internal/cli/cli_test.go`: cli.TestParseDispatchesToTheWindowOnlyWithNoMessage. `internal/gui/errorwindow_test.go`: gui.TestTheWindowReadsOnlyTheDefaultSettings, gui.TestAStartupFailureWithNoPathOpensNoWindow. `cmd/mp/main_test.go`: main.TestTheWindowFrontDoorTakesNoSettingsPath. `internal/config/paths_test.go`: config.TestDefaultPaths | The tests cover dispatch to the window, the settings path the window resolves, and the absence of any override parameter. A native window appearing is observable only by hand (Scenario 7). **Diverges**: see D1. |
| 2 | `mp hello world` joins arguments with spaces and posts to every enabled sink. | FR-003, FR-004, FR-012, FR-013, FR-016 | partial — manual remainder (M1) | `internal/cli/cli_test.go`: cli.TestParseJoinsMessageArgumentsWithOneASCIISpace, cli.TestTheCommandLinePostsWhatWasTypedAndRejectsWhatItMustNotSend. `cmd/mp/main_test.go`: main.TestTheBinaryExitsZeroWhenEveryDestinationSucceeded (the note ends `hello world\n`). `internal/app/app_test.go`: app.TestOnlyEnabledSinksAreBuilt. `internal/post/service_test.go`: post.TestBothSinksRunAndBothResultsAreReported, post.TestEverySinkReceivesTheIdenticalOriginalMessage | Automated for Obsidian and for a local HTTP stand-in for Telegram. Delivery to the real Bot API is manual (Scenario 1). FR-004 pins exactly one ASCII space. |
| 3 | `mp -c PATH hello` uses `PATH`; `mp -c PATH` does not open the GUI and exits `1`. | FR-005, FR-006 | automated | `cmd/mp/main_test.go`: main.TestTheBinaryExitsZeroWhenEveryDestinationSucceeded (posts into the vault named only by the `-c` file), main.TestTheBinaryRefusesAConfigWithoutAMessage (exit 1, required text, no post, and the process exits, so no window is waiting). `internal/cli/cli_test.go`: cli.TestParseDispatchesToTheWindowOnlyWithNoMessage/a_message_with_the_config_flag, cli.TestParseRejectsWhatItCannotUnderstand, cli.TestFR006PrintsTheRequiredText | FR-006 adds required stderr text. The CLI contract adds that `-c ""` is refused rather than read as no override. Both refine §13 without contradicting it (D2). |
| 4 | `mp --help` displays the resolved default configuration path. | FR-007 | automated | `internal/cli/help_test.go`: cli.TestHelpPrintsTheResolvedDefaultPath. `cmd/mp/main_test.go`: main.TestTheBinaryPrintsHelp (`-h` and `--help`, exit 0, expanded path) | — |
| 5 | Telegram receives `chat_id` and the optional `message_thread_id` correctly. | FR-031, FR-032 | partial — manual remainder (M1) | `internal/sink/telegram/sink_test.go`: telegram.TestSendPostsTheConfiguredFieldsToTelegram (both subtests), telegram.TestSendMessageFormOmitsTheThreadKeyRatherThanEmptyingIt, telegram.TestWireNamesMatchTheBotAPI. `internal/config/validate_test.go`: config.TestValidateThreadID | The tests assert the form fields sent to a local server. Whether the real Bot API routes to the topic is manual (Scenario 1 with `thread_id` set). A-006: absence, not 0, means post to the chat directly. |
| 6 | Only a MarkdownV2 parse error triggers one plain-text fallback; fallback success is overall success. | FR-033 – FR-039, FR-061 | partial — manual remainder (M1) | `internal/sink/telegram/rescue_test.go`: telegram.TestFormattingPredicate. `internal/sink/telegram/fallback_test.go`: telegram.TestRescuePreservesTextAndBothOutcomes. `internal/sink/telegram/rescue_integration_test.go`: telegram.TestRescueHTTPServiceJSONLOutcome. `internal/sink/telegram/sink_test.go`: telegram.TestSendMakesExactlyOneAttempt, telegram.TestAContradictoryReplyIsNotAFormattingRejection, telegram.TestSendAlwaysUsesMarkdownV2WhateverParseModeSays. `internal/post/result_test.go`: post.TestAllSucceededTreatsRescuedDeliveryAsSuccess. `internal/app/capture_test.go`: app.TestARescuedPostIsASuccessAndRecordsNoBody | The predicate is tested against recorded reply shapes. Whether the live service still answers `can't parse entities` for reserved characters is manual (Scenario 4). **Diverges**: see D3. |
| 7 | Multiline input becomes one physical Obsidian line with `<br>` separators and a formatted local-time prefix. | FR-047, FR-048, FR-051, SC-010 | automated | `internal/sink/obsidian/sink_test.go`: obsidian.TestTheTransformationOrderIsNormative, obsidian.TestEntryRendersTheTimeThroughTheConfiguredFormat, obsidian.TestTheMessageBodyIsWrittenByteForByte | FR-047 fixes the order: CR/CRLF → LF, LF → `<br>`, then the prefix `- ` + time + space. This refines §13 without contradicting it. |
| 8 | A missing Daily Note is created when configured, while existing content is preserved during append. | FR-046, FR-049, SC-009 | automated | `internal/sink/obsidian/sink_test.go`: obsidian.TestAMissingNoteRespectsCreateIfMissing, obsidian.TestAppendNeverAltersExistingContent, obsidian.TestASeparatorIsAddedOnlyWhenTheNoteNeedsOne, obsidian.TestSendReportsAnUnopenableNote | Also covers FR-046's other arm: with `create_if_missing = false` the note sink fails. |
| 9 | Telegram and Obsidian run independently; failure of either never suppresses the other. | FR-013, FR-014, FR-015, SC-002, SC-011 | automated | `internal/sink/telegram/independence_test.go`: telegram.TestRealSinksRemainIndependent (all five subtests, real sinks against a local server). `internal/post/service_test.go`: post.TestOneSinkFailingDoesNotCancelItsSibling, post.TestSinksActuallyOverlap, post.TestABlockingSinkYieldsATimeoutAndDoesNotStallItsSibling, post.TestAPanickingSinkBecomesAFailureAndItsSiblingStillReports. `cmd/mp/main_test.go`: main.TestTheBinaryReportsPartialFailure | Run `internal/post` under `-race` too (quickstart). Scenario 3 repeats this live, as a supplement rather than a requirement. |
| 10 | Partial and total failures show every failed sink and a short cause in both interfaces. | FR-017, FR-029, FR-062, FR-063, SC-003 | automated (headless) | CLI: `internal/cli/cli_test.go`: cli.TestRenderNamesEveryDestination, cli.TestRenderNeverPrintsTheDiagnosticError, cli.TestASinkFailureExitsOneAndNamesTheLog. `cmd/mp/main_test.go`: main.TestTheBinaryReportsPartialFailure. GUI: `internal/gui/result_test.go`: gui.TestWindowReportsBothCoreOutcomes (all four note/chat combinations) | The CLI half is fully automated. The GUI half runs on the Fyne test driver. FR-063 adds the log path. FR-076 replaces that line with the single diagnostics warning when the log could not be written. |
| 11 | CLI exits `0` only when all enabled sinks succeed; otherwise it exits `1`. | FR-059, FR-060, FR-061, SC-004 | automated | `cmd/mp/main_test.go`: main.TestTheBinaryExitsZeroWhenEveryDestinationSucceeded, main.TestTheBinaryReportsPartialFailure, main.TestTheBinaryRejectsAMessageItMustNotSend. `internal/cli/cli_test.go`: cli.TestASinkFailureExitsOneAndNamesTheLog. `internal/cli/startup_test.go`: cli.TestBothDestinationsSucceedingExitsZero. `internal/post/result_test.go`: post.TestAllSucceeded, post.TestAllSucceededTreatsRescuedDeliveryAsSuccess | **Diverges**: see D4. |
| 12 | GUI Send is disabled during posting and closes after 15 seconds on success or 30 seconds on failure by default. | FR-024, FR-026, FR-027, FR-028, SC-012 | partial — manual remainder (M2) | `internal/gui/window_test.go`: gui.TestButtonsAndShortcutShareSubmissionAndRejectDuplicates (Send disabled, no duplicate), gui.TestResultDeadlineAndStatus (built from `config.Defaults().GUI`; asserts 15 s / 30 s and status 0 / 1; the `…/interaction` subtests cancel), gui.TestConfiguredZeroDeadlineAndCancellation, gui.TestDismissalWaitsForEverySinkAndPreservesFinalStatus | The timers and the activity counter are injected. Neither real elapsed time, process termination, nor the native activity observer (`activity_darwin.m`) runs in these tests. **Diverges**: see D5. |
| 13 | JSONL logs correlate events for one post, omit successful message bodies, include the body on failure, and contain no secrets. | FR-064, FR-066, FR-068, FR-069, FR-043, SC-006, SC-007, SC-008 | automated | `internal/logging/logger_test.go`: logging.TestEveryLineIsAnIndependentlyValidRecord, logging.TestAMessageBodyCannotSplitARecord, logging.TestConcurrentPostsProduceWellFormedRecords, logging.TestTheConfiguredCredentialNeverReachesTheLog. `internal/app/recorder_test.go`: app.TestASuccessfulPostWritesTheContractsRecords (shared `message_id`), app.TestFormattingEventsAreCorrelatedAndOrdered, app.TestTheBotTokenNeverReachesARecordThroughSinkResultErr. `internal/app/capture_test.go`: app.TestAFullySuccessfulPostRecordsNoMessageBody, app.TestAFailedPostRecordsTheBodyOnTheRecordThatNamesTheFailure, app.TestBothFailuresCarryTheBody, app.TestAFailedRescueRecordsTheBody, app.TestMessageOnErrorOnlyOffRecordsTheBodyOnIntakeAndNowhereElse, app.TestACapturedBodyContainingTheBotTokenIsRedacted. `internal/post/secret_leak_test.go`: post.TestSecretLeakGate (T084) | The secret-leak gate is the end-to-end witness for "contain no secrets". It drives `cli.Run` through the real logger and both real sinks with a sentinel token, over eight cases: success, transport failure, a token with trailing whitespace whose transport fails (#137), timeout, an echoed credential, the formatting rescue, both destinations failing, and a message containing the credential. The trailing-whitespace case is the only one that pins `withoutRequestURL` end to end. It asserts the sentinel on neither user stream, in no file written, and in no window result string. It replaces `http.DefaultTransport` in-process, so nothing is dialled. **Diverges**: see D6. |
| 14 | Logs rotate at 10 MiB or seven days, whichever occurs first, with no automatic deletion. | FR-072, FR-074 | automated | `internal/logging/rotate_test.go`: logging.TestRotateSizeTriggerFiresAtTheThresholdAndNotBefore, logging.TestRotateAgeTriggerFiresIndependentlyOfSize, logging.TestRotateEvaluatesBothConditionsBeforeEveryWrite, logging.TestRotateNeverDeletesOrOverwritesARotatedFile, logging.TestOpenRotatesARealLogFile, logging.TestOpenRotatesAnAgedLogFile, logging.TestRotateThresholdConversion. `internal/logging/birthtime_darwin_test.go`: logging.TestCreationTimeUsesTheBirthTimeAndNotTheModificationTime. `internal/app/app_test.go`: app.TestOpenLoggerHonoursTheRotationThresholds | The literal defaults 10 and 7 are pinned by `internal/config/example_test.go`: config.TestExampleSettingsAreTheDefaults, which requires the example file's written values to equal `config.Defaults()`. **Diverges**: see D7. |
| 15 | ASCII-space-only, full-width-space-only, tab-only, and line-break-only messages are rejected before any sink runs. | FR-009, FR-010, FR-011 | automated (headless) | `internal/post/message_test.go`: post.TestMessageValidateRejectsWhitespaceOnly (each form, plus mixed), post.TestMessageValidateAcceptsAndLeavesTextUntrimmed. `internal/cli/cli_test.go`: cli.TestTheCommandLinePostsWhatWasTypedAndRejectsWhatItMustNotSend, cli.TestARejectedMessageOpensNoLog. `cmd/mp/main_test.go`: main.TestTheBinaryRejectsAMessageItMustNotSend. GUI: `internal/gui/window_test.go`: gui.TestInvalidMessageNeverReachesService | The CLI half is fully automated. The GUI half runs on the Fyne test driver. **Diverges**: see D8. |
| 16 | `Esc`, `Enter`, `Cmd+Enter`, and `Cmd+Q` perform cancel, newline, send, and quit respectively. | FR-022, FR-023, A-003 | partial — manual remainder (M3) | `internal/gui/entry_test.go`: gui.TestCommandsWhileEntryHasFocus (all four keys on the focused entry). `internal/gui/window_test.go`: gui.TestCancelBeforeSendingDoesNotContactAnySink, gui.TestEscapeWithButtonFocused, gui.TestButtonsAndShortcutShareSubmissionAndRejectDuplicates (Cmd+Enter and Send share one path) | Every test calls `TypedKey` / `TypedShortcut` on a widget the test itself chose. None proves the glfw driver routes the key there. **Diverges**: see D9. |
| 17 | Telegram HTTP requests default to a 30-second timeout; each sink invocation defaults to a separate 60-second overall timeout. | FR-015, FR-040 | automated | `internal/sink/telegram/sink_test.go`: telegram.TestNewAppliesTheConfiguredRequestTimeoutToItsClient (30 s default; 0 and negative become 30 s), telegram.TestSendIsBoundedByItsOwnRequestTimeout, telegram.TestSendHonoursTheOrchestratorsPerSinkDeadline. `internal/sink/telegram/fallback_test.go`: telegram.TestRescueRetainsPerRequestTimeout. `internal/sink/telegram/rescue_deadline_test.go`: telegram.TestRescueUsesRemainingOverallDeadline. `internal/post/service_test.go`: post.TestEachSinkGetsItsOwnDeadline, post.TestNewReplacesATimeoutThatCannotBoundAnything (floors to 60 s). `internal/app/app_test.go`: app.TestSinkTimeoutConvertsSecondsToADuration | The literal defaults 30 and 60 are pinned by config.TestExampleSettingsAreTheDefaults, as in row 14. The package-level floors are pinned separately. **Diverges**: see D10. |
| 18 | No general HTTP retry occurs in v0.1; MarkdownV2 plain-text fallback continues to work as specified. | FR-019, FR-038, FR-041 | automated | `internal/sink/telegram/sink_test.go`: telegram.TestSendMakesExactlyOneAttempt, telegram.TestSendFailsClosedOnEveryUntrustworthyResponse, telegram.TestSendDoesNotFollowARedirect. `internal/sink/telegram/rescue_test.go`: telegram.TestFormattingPredicate (401 / 403 / 429 / 5xx do not trigger a retry). `internal/sink/telegram/independence_test.go`: telegram.TestRealSinksRemainIndependent (exactly one request per post). `internal/sink/telegram/fallback_test.go`: telegram.TestRescuePreservesTextAndBothOutcomes | — |
| 19 | Rotated logs use a local timestamp suffix such as `app.jsonl.20260827114203`. | FR-073, A-009 | automated | `internal/logging/rotate_test.go`: logging.TestRotateSuffixIsTheLocalTimestampFormat (exact `app.jsonl.20260827114203` from a `time.Local` clock), logging.TestRotateStepsOverAFileItDidNotCreate, logging.TestClaimNameReservesTheNameAtomically | **Diverges**: see D11. |
| 20 | An all-sinks-disabled configuration fails at startup with an actionable correction message. | FR-018, FR-030, FR-058 | partial — manual remainder (M4) | `internal/cli/startup_test.go`: cli.TestNoDestinationEnabledIsAStartupError, cli.TestPartiallyValidSettingsStartNoDestination. `internal/config/validate_test.go`: config.TestRequireDestination. `internal/post/service_test.go`: post.TestPostWithNoSinksReportsNothingDelivered. GUI: `internal/gui/errorwindow_test.go`: gui.TestTheStartupErrorWindowShowsTheMessageAndPathAndNoField, gui.TestAStartupFailureShowsTheErrorWindowAndExitsOne, gui.TestEveryDismissalClosesTheStartupErrorWindow (Quit, the close box, Esc two ways, Cmd+Q). `internal/app/settings_test.go`: app.TestLoadSettingsReturnsThePathOnEveryFailure | The CLI half is fully automated. The error window is automated only headless. Native dismissal is owed to T091 / #92. **Diverges**: see D12. |

No criterion is without automated coverage. Criteria 1, 2, 5, 6, 12, 16 and 20 each leave a named
part to a manual check.

## Manual verification justifications

Each item gives: why automation is not possible or not sufficient, what the automated tests do
cover, and the manual procedure. Results are recorded by T091 in
[validation/t091-quickstart.md](validation/t091-quickstart.md), and summarised in the table at the
end of this section.

### M1: live Telegram delivery (criteria 2, 5, 6)

- **Why manual**: The constitution requires sinks to be testable without contacting live
  services. Every Telegram test therefore runs against `httptest` servers or an in-process
  `RoundTripper`. Three things are observable only against the real Bot API: that a post arrives,
  that `message_thread_id` lands in the intended forum topic, and that the service still answers
  reserved MarkdownV2 characters with the `400 … can't parse entities` text the rescue predicate
  matches. A test that dialled `api.telegram.org` would break that rule and would need a real
  token in the environment.
- **What is automated**: The exact form fields and wire names (`chat_id`, `message_thread_id`
  present or absent, `parse_mode`, verbatim `text`). The rescue predicate against recorded reply
  shapes. Exactly one rescue, with both attempts retained. No retry for any other failure.
  Independence of the real HTTP sink from the note sink.
- **Procedure**: Quickstart Scenario 1, both sinks with and without `thread_id`. Scenario 3, the
  Telegram base unreachable or the token bad. Scenario 4, reserved characters, then the
  bad-token negative case.
- **Rider, not a §13 criterion**: #92's 2026-09-10 rider, tied to DEC-D4 and so to criterion 15's
  divergence D8. One live `sendMessage` against a throwaway bot with `text=a%FFb`, a
  percent-encoded invalid UTF-8 byte. If Telegram answers `400 Strings must be encoded in UTF-8`,
  DEC-D4's premise holds and nothing changes. If it accepts the byte, the premise fails and #104
  should be reopened (Option C). It is item 7 of the owed list in
  [validation/t091-quickstart.md](validation/t091-quickstart.md). Run 2026-09-28: HTTP 400, so the
  premise holds.

### M2: native window launch and auto-close (criteria 1, 12)

- **Why manual**: The window tests run on Fyne's headless driver with an injected timer, an
  injected dispatcher and an injected activity counter. Three things are not exercised: that
  `mp` with no arguments puts a real window on screen, that the process terminates on its own
  after the real 15 s or 30 s, and that the native activity observer (`activity_darwin.m`) turns
  a keypress, click or focus regain into a cancellation. The native path cannot be driven from
  `go test` without a display and Accessibility permission. Batch 11 also showed that headless
  keyboard tests can pass while the real driver behaves differently.
- **What is automated**: Send disabled for the whole submission. The 15 s and 30 s delays taken
  from `config.Defaults().GUI`. Status 0 or 1 carried to close. Interaction cancelling the
  pending close. A zero delay closing immediately. Dismissal waiting for every sink.
- **Prior evidence**: `validation/t049-native-focus.md` (Batch 7, 2026-09-11, 45 s delays)
  recorded native focus-regain cancellation and exit codes 0 and 1. It predates later GUI
  changes, including the #122 background and Batch 11, and was made with non-default delays. It
  is supporting evidence, not this matrix's result.
- **Procedure**: Quickstart Scenario 7, default delays, success and failure, idle and with a
  keypress during the countdown, recording each process exit status. Scenario 8, last step:
  after `mp -c ./other.toml "hi"`, a bare `mp` uses the default path.

### M3: native keyboard routing (criterion 16)

- **Why manual**: The headless tests deliver each key straight to the widget the test chose.
  Under glfw the driver hands a key to the focused widget before the canvas. That is exactly how
  Batch 11 found Esc dead in the startup-error window: the focused `widget.Button` swallowed it
  while a headless test that called the canvas handler passed (`.agents/work-log.md`, "2026-09-24 — Batch 11 … review cycle 0").
  `Cmd+Q` may also be claimed by the macOS application menu before Fyne sees it. No
  automated test observes the real routing.
- **What is automated**: All four keys on the focused entry. Esc with the Send button focused.
  Cmd+Enter and Send sharing one submission path.
- **Procedure**: Quickstart Scenario 7: Enter, Cmd+Enter, Esc before sending with focus on the
  field and then on a button, and Cmd+Q.

### M4: native dismissal of the startup-error window (criterion 20)

- **Why manual**: As for M3. The error window's Esc path is the one Batch 11 fixed after a
  headless false pass. Native dismissal was attempted in Batch 11 but not observed, because
  osascript was blocked on Accessibility permission. The tasks.md T082 note records it as owed
  to T091 / #92.
- **What is automated**: The CLI refusal, before any post, with an actionable message and exit
  1. The window's content: message, resolved path, no field. Every dismissal route on the
  headless driver. Exit 1 after the event loop ends. No window when no path resolves.
- **Procedure**: Quickstart Scenario 8, last paragraph. Set every `enabled = false`, launch `mp`,
  and dismiss the window by each of Quit, the close box, Esc and Cmd+Q, recording the exit
  status each time.

### Result log

| Item | Criteria | Quickstart | Result | Recorded by |
|---|---|---|---|---|
| M1 | 2, 5, 6 | 1, 3, 4 | done 2026-09-28. `thread_id` topic routing (criterion 5) is not applicable: the test chat is a DM, not a forum with topics; the wire field is covered by the automated tests in row 5 | T091 |
| M2 | 1, 12 | 7, 8 | done 2026-09-29: the window opened on the default settings, Send was disabled in flight, the 15 s and 30 s idle auto-closes exited 0 and 1, and a keypress cancelled the countdown | T091 |
| M3 | 16 | 7 | done 2026-09-29: Enter, Cmd+Enter, Esc (field focused, and nothing focused) and Cmd+Q on the native window. A focused button is not reachable by user input in the posting window (case 4c), so that headless case is defensive only | T091 |
| M4 | 20 | 8 | done 2026-09-29: the startup-error window showed the message and path with no field; Quit, the close box, Esc and Cmd+Q each exited 1 | T091 / #92 |

The parts of Scenarios 1, 2, 5, 6, 8 and 9 that need neither the live Bot API nor a person at the
window were run on 2026-09-25 and passed. They are the automatable remainder, not these manual
items, and none of M1–M4 is discharged by them.

M1's live checks were run on 2026-09-28 against the maintainer's test bot and passed: Scenario 1,
Scenario 3 with one and with both destinations broken, and Scenario 4 with its bad-token negative
case (see *Live run, 2026-09-28* in [validation/t091-quickstart.md](validation/t091-quickstart.md)).
Scenario 1 with `thread_id` set was not run: it needs a forum chat with topics, and the test chat
is a DM with the bot. M2, M3 and M4 were run by the maintainer on the native window on 2026-09-29
and passed (see *Window checks, 2026-09-29* in the same file).

The #92 rider was run on the same day. A raw `sendMessage` with `text=a%FFb` got HTTP 400,
`Bad Request: strings must be encoded in UTF-8`, so DEC-D4's premise holds and #104 stays
closed.

## Divergences between design §13 and the spec

The spec governs in every case (A-001 makes `docs/design.md` normative only where the spec is
silent). None of the §13 defaults diverges from the spec or the code.

- **D1 (criterion 1)**: The spec narrows "`mp` opens a Fyne GUI". Invalid settings or no enabled
  destination open the minimal startup-error window instead of the posting window (FR-030). When
  no settings path can be resolved at all (`HOME` and `XDG_CONFIG_HOME` both unset), no window
  opens: the error goes to stderr and the exit status is 1 (decision DEC-H2, gui-interface
  contract).
- **D2 (criterion 3)**: This refines §13 without contradicting it. FR-006 fixes the stderr text
  `--config is only available when posting from CLI`. The CLI contract refuses an empty `-c ""`
  rather than treating it as no override.
- **D3 (criterion 6)**: FR-034 makes `parse_mode` and `fallback_to_plain_text` accepted and
  validated but inert in v0.1. The first attempt is always MarkdownV2, and the single rescue runs
  even with `fallback_to_plain_text = false`. The design's settings sample (§9) implies both keys
  steer delivery.
- **D4 (criterion 11)**: §13 frames exit status by sink outcome only. FR-060 also makes input,
  settings and startup errors exit 1. FR-027 applies the same rule to the window's exit.
- **D5 (criterion 12)**: The default delays match. The spec adds that any interaction during the
  countdown cancels the close, and the result then stays until dismissed (FR-028, SC-012). So
  "closes after 15 / 30 seconds" holds only for an idle window. Auto-close terminates the process
  (FR-027). The schema allows `0`, meaning close immediately.
- **D6 (criterion 13)**: FR-068 makes body omission on success conditional on
  `message_on_error_only`, which defaults to true. With it false, the body is recorded on the
  intake record (app.TestMessageOnErrorOnlyOffRecordsTheBodyOnIntakeAndNowhereElse). §13 states
  the default behaviour as unconditional.
- **D7 (criterion 14)**: FR-072 measures age from the active file's creation time. Per decision
  #128 (config-schema), the age trigger is effective only where the platform records a birth
  time: darwin on APFS or HFS+. On other platforms and volumes a CLI run effectively never fires
  it. Per decision #132, `rotate_size_mib` bounds the file between records, so one record larger
  than the threshold lands whole. FR-074 also forbids compression.
- **D8 (criterion 15)**: FR-009a (decision DEC-D4, #104) adds a second, distinct rejection for
  input that is not valid UTF-8, with its own correction prompt. §13 lists whitespace forms only.
  The same tests cover the second reason (post.TestMessageValidateRejectsInvalidUTF8, and the
  UTF-8 subtest of gui.TestInvalidMessageNeverReachesService).
- **D9 (criterion 16)**: A-003 scopes the keyboard contract to macOS. "Quit" is not immediate
  while a post is in flight: Cmd+Q and Esc both close through the same path, which waits for
  every sink before exiting (spec edge case, gui.TestDismissalWaitsForEverySinkAndPreservesFinalStatus).
- **D10 (criterion 17)**: The defaults match. FR-040 adds that both Telegram attempts together
  stay inside the per-sink overall limit. Validation bounds both keys to `> 0` and
  `≤ 9223372036` (#109).
- **D11 (criterion 19)**: A same-second collision appends `-1`, `-2`, … to the suffix. This is
  claimed atomically with `O_EXCL` (A-009 left it to planning; `internal/logging/rotate.go`
  `claimName`).
- **D12 (criterion 20)**: FR-030 adds the windowed-path behaviour: a minimal error window shows
  the message and the resolved settings path, offers no message field, and exits 1 on dismissal.
  §13 does not say which front door reports the failure.
