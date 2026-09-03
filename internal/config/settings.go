package config

// Settings is the validated configuration for one run.
//
// The shape mirrors contracts/config-schema.md exactly, including the
// namespaced [sink.telegram] and [sink.obsidian] tables that FR-054 makes
// normative. The nesting is not cosmetic: with strict decoding it is what makes
// a top-level [telegram] table a load error rather than a silent no-op, because
// there is no field for the decoder to match it against.
//
// Every field is exported so go-toml can set it, which means a Settings can be
// printed. The one field that must not be printed is Sink.Telegram.BotToken,
// and it is a Secret precisely so that printing a whole Settings is safe; see
// that type. Nothing here should acquire a second unguarded copy of the token.
//
// A Settings only ever leaves this package fully validated. Load returns the
// zero value alongside any error rather than a partially populated struct, so
// FR-058's "no destination started with partially valid settings" is a property
// of the return convention and not of caller discipline.
type Settings struct {
	Sink    SinkSettings    `toml:"sink"`
	Posting PostingSettings `toml:"posting"`
	GUI     GUISettings     `toml:"gui"`
	Logging LoggingSettings `toml:"logging"`
}

// SinkSettings groups the per-destination tables under the namespaced [sink]
// prefix (FR-054).
type SinkSettings struct {
	Telegram TelegramSettings `toml:"telegram"`
	Obsidian ObsidianSettings `toml:"obsidian"`
}

// TelegramSettings configures the chat destination.
type TelegramSettings struct {
	// Enabled defaults to false. A disabled sink is never constructed and
	// never counts as a failure (FR-016).
	Enabled bool `toml:"enabled"`

	// BotToken is the credential. Required when the sink is enabled and
	// MIKO_POST_TELEGRAM_BOT_TOKEN supplies nothing (FR-042).
	BotToken Secret `toml:"bot_token"`

	// ChatID identifies the destination chat. Required when enabled.
	ChatID string `toml:"chat_id"`

	// ThreadID selects a forum topic within the chat.
	//
	// A pointer because absence is meaningful and distinct from any value
	// (FR-032, A-006): nil means "post to the chat directly" and the request
	// builder omits message_thread_id entirely. An int with a sentinel would
	// make `thread_id = 0` and an absent key indistinguishable, and 0 is not a
	// valid topic — which is why an explicit 0 is a validation error rather
	// than a silently malformed request.
	ThreadID *int64 `toml:"thread_id"`

	// ParseMode is accepted and validated but inert in v0.1 (FR-034). See
	// validateTelegram for what "validated" means for an inert key.
	ParseMode string `toml:"parse_mode"`

	// FallbackToPlainText is accepted and validated but inert in v0.1
	// (FR-034). The single unformatted rescue is fixed application behaviour.
	FallbackToPlainText bool `toml:"fallback_to_plain_text"`

	// HTTPTimeoutSeconds bounds the single delivery attempt (FR-040). It is
	// distinct from Posting.SinkTimeoutSeconds, which bounds the sink overall.
	HTTPTimeoutSeconds int `toml:"http_timeout_seconds"`
}

// ObsidianSettings configures the daily-note destination.
type ObsidianSettings struct {
	// Enabled defaults to false (FR-016).
	Enabled bool `toml:"enabled"`

	// DailyNoteDir is the vault directory holding daily notes. Required and
	// absolute when enabled: the process's working directory is whatever the
	// shell or the window launcher happened to have, so a relative vault path
	// would write somewhere different depending on the front door used.
	DailyNoteDir string `toml:"daily_note_dir"`

	// FilenameFormat renders the current local date into today's note filename
	// through a Go time layout (FR-044, FR-051).
	FilenameFormat string `toml:"filename_format"`

	// TimeFormat renders the current local time into the entry prefix
	// (FR-047, FR-051).
	TimeFormat string `toml:"time_format"`

	// CreateIfMissing governs whether today's note is created when absent
	// (FR-046). When false, a missing note fails this sink and nothing else.
	CreateIfMissing bool `toml:"create_if_missing"`
}

// PostingSettings configures the orchestrator.
type PostingSettings struct {
	// SinkTimeoutSeconds bounds each sink overall. It is applied
	// independently to every sink (FR-015), so it is a per-sink budget and
	// not a budget for the post as a whole.
	SinkTimeoutSeconds int `toml:"sink_timeout_seconds"`
}

// GUISettings configures the window's auto-close behaviour (FR-026).
type GUISettings struct {
	// SuccessCloseSeconds is the delay before the window closes itself after
	// a fully successful post. Zero means close immediately, which is why the
	// bound is >= 0 rather than > 0.
	SuccessCloseSeconds int `toml:"success_close_seconds"`

	// ErrorCloseSeconds is the same delay after any failure.
	ErrorCloseSeconds int `toml:"error_close_seconds"`
}

// LoggingSettings configures the diagnostic log (FR-064 – FR-072).
type LoggingSettings struct {
	// Format is "jsonl" and nothing else in v0.1.
	Format string `toml:"format"`

	// Path overrides the resolved default state path. Empty means "use the
	// default" (FR-056), which is why it is not defaulted to the resolved path
	// here: doing so would freeze the resolution at defaults-construction time
	// and lose the ability to tell "unset" from "deliberately set to the same
	// value". DefaultLogPath resolves it at the point of use (T023).
	Path string `toml:"path"`

	// RotateSizeMiB and RotateAfterDays are the two rotation thresholds, each
	// evaluated before every write (FR-072, T068).
	RotateSizeMiB   int `toml:"rotate_size_mib"`
	RotateAfterDays int `toml:"rotate_after_days"`

	// MessageOnErrorOnly governs whether the message body is captured on
	// success as well as on failure (FR-068).
	MessageOnErrorOnly bool `toml:"message_on_error_only"`

	// StackTrace governs trace collection (FR-071).
	StackTrace bool `toml:"stack_trace"`

	// IncludeVersion and IncludeGitCommit govern the build-identity fields on
	// every record (FR-066).
	IncludeVersion   bool `toml:"include_version"`
	IncludeGitCommit bool `toml:"include_git_commit"`
}

// Defaults returns the settings a completely empty file would produce.
//
// Every default in contracts/config-schema.md is here, and the table is the
// only place they are written down in code. Load decodes *into* this value
// rather than into a zero Settings, which is what makes an absent key keep its
// default while an explicitly written `false` still wins: go-toml only assigns
// fields the document actually contains.
//
// That mechanism is the reason the six booleans defaulting to true work at all.
// A zero-value decode would render them indistinguishable from a user writing
// false, and the failure would be silent — stack traces and message capture
// would simply stop happening. There is a test for exactly this.
//
// Both sinks default to disabled. A file that enables neither is a startup
// error (FR-018), but that check belongs to the front door (T081) rather than
// to validation, because the actionable message it must produce is a front-door
// concern and the same settings are perfectly valid as a document.
func Defaults() Settings {
	return Settings{
		Sink: SinkSettings{
			Telegram: TelegramSettings{
				Enabled:             false,
				ParseMode:           ParseModeMarkdownV2,
				FallbackToPlainText: true,
				HTTPTimeoutSeconds:  30,
			},
			Obsidian: ObsidianSettings{
				Enabled:         false,
				FilenameFormat:  "2006-01-02.md",
				TimeFormat:      "15:04",
				CreateIfMissing: true,
			},
		},
		Posting: PostingSettings{
			SinkTimeoutSeconds: 60,
		},
		GUI: GUISettings{
			SuccessCloseSeconds: 15,
			ErrorCloseSeconds:   30,
		},
		Logging: LoggingSettings{
			Format:             LogFormatJSONL,
			Path:               "",
			RotateSizeMiB:      10,
			RotateAfterDays:    7,
			MessageOnErrorOnly: true,
			StackTrace:         true,
			IncludeVersion:     true,
			IncludeGitCommit:   true,
		},
	}
}
