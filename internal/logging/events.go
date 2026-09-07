package logging

// Event is a stable diagnostic event name (FR-067, R-005).
//
// It is a defined type rather than a bare string because the event name is the
// correlation vocabulary: after-the-fact analysis of a JSONL log greps for
// these exact names, so a name invented at one call site is not a compile
// error, it is a query that silently returns nothing months later. PostLogger's
// Info and Error take an Event, so emitting an unregistered name requires
// writing logging.Event("…") and making the bypass visible in the diff.
//
// Untyped string constants are assignable to Event, so a constant declared
// without the explicit type would still compile at a log call site while
// escaping the type. events_test.go rejects that shape rather than skipping it.
type Event string

// The stable event names (FR-067, contracts/log-events.md).
//
// contracts/log-events.md declares this set both normative and *minimum*: a
// later version may add names. Adding one is a deliberate act that must also
// extend AllEvents and the expected set in events_test.go — the test fails on a
// constant that exists here but is not registered, which is the whole point of
// having three places agree. Renaming or removing a name is a breaking change
// to every saved log query anyone has written.
//
// The formatting-fallback path is three names rather than one name with an
// outcome attribute because FR-039 wants the rescue observable as a distinct
// *sequence*: EventTelegramMarkdownFailed followed by either
// EventTelegramPlaintextSucceeded or EventTelegramPlaintextFailed. An attribute
// would make "did the rescue run at all" a filter over one event's field
// instead of a grep for one name, and the sequence is what distinguishes a
// rescued post from one that never needed rescuing.
//
// EventTelegramSendFailed and EventTelegramPlaintextFailed are both failures
// but not interchangeable: the first is the MarkdownV2 attempt (FR-035), the
// second the unformatted retry (FR-062). A post that logs only the first was
// never rescued; one that logs both was rescued and still failed.
const (
	// The post entered the system, before any sink ran.
	EventMessageReceived Event = "message_received"

	// The daily-note append, one name per lifecycle stage. These carry the
	// resolved note path, which the orchestrator obtains from the sink rather
	// than re-deriving (issue #98).
	EventObsidianAppendStarted   Event = "obsidian_append_started"
	EventObsidianAppendSucceeded Event = "obsidian_append_succeeded"
	EventObsidianAppendFailed    Event = "obsidian_append_failed"

	// The MarkdownV2 chat attempt (FR-035). A rescued post's send is reported
	// as failed here and succeeded by EventTelegramPlaintextSucceeded.
	EventTelegramSendStarted   Event = "telegram_send_started"
	EventTelegramSendSucceeded Event = "telegram_send_succeeded"
	EventTelegramSendFailed    Event = "telegram_send_failed"

	// The formatting-fallback sequence (FR-039).
	EventTelegramMarkdownFailed     Event = "telegram_markdown_failed"
	EventTelegramPlaintextSucceeded Event = "telegram_plaintext_succeeded"
	EventTelegramPlaintextFailed    Event = "telegram_plaintext_failed"

	// The whole post's terminal record. Which of the two is emitted tracks
	// AllSucceeded, and therefore the exit status (FR-059, FR-060).
	EventRequestCompleted          Event = "request_completed"
	EventRequestCompletedWithError Event = "request_completed_with_error"
)

// AllEvents returns every event name this version defines, in the order
// contracts/log-events.md lists them.
//
// A function returning a fresh slice rather than a package-level slice
// variable: a var would be writable by any importer, and one stray
// AllEvents[0] = "…" would corrupt a vocabulary that the whole process and
// every log consumer share. The allocation is irrelevant — this is called by
// tests and by whatever validates a name, not on a posting path.
//
// The order is contractual only in that it matches the document, which makes
// the two readable side by side. Nothing depends on the ordering; membership is
// what matters.
func AllEvents() []Event {
	return []Event{
		EventMessageReceived,

		EventObsidianAppendStarted,
		EventObsidianAppendSucceeded,
		EventObsidianAppendFailed,

		EventTelegramSendStarted,
		EventTelegramSendSucceeded,
		EventTelegramSendFailed,

		EventTelegramMarkdownFailed,
		EventTelegramPlaintextSucceeded,
		EventTelegramPlaintextFailed,

		EventRequestCompleted,
		EventRequestCompletedWithError,
	}
}
