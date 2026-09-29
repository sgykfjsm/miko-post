package config

import "os"

// TelegramBotTokenEnv is the only environment variable that overrides a
// settings key in v0.1 (FR-042).
//
// It is exported because it is user-facing: validation names it when the
// credential is missing, and it belongs in the example settings file (T087).
const TelegramBotTokenEnv = "MIKO_POST_TELEGRAM_BOT_TOKEN"

// ResolveCredential applies the environment override for the chat credential
// (FR-042).
//
// The environment wins over the file. The reason the precedence runs this way
// is that the file is the value most likely to be committed, shared, or backed
// up, so the environment has to be able to displace it; the reverse would make
// a stale token in a settings file impossible to override without editing it.
//
// It mutates through a pointer rather than returning a new Settings, so there
// is exactly one Settings value carrying the credential during a load. A
// returning form would leave the caller holding a second copy — one with the
// file's token, one with the environment's — and the discarded one would still
// be reachable for as long as it stayed in scope.
//
// This runs after decoding and before validation. That ordering is what lets
// validation's "required when enabled" check be a single test of the resolved
// value rather than a two-source condition it would have to restate.
//
// Both sources are trimmed of leading and trailing whitespace (DEC-I2, #137).
// An environment variable that is empty, or only whitespace, counts as absent,
// so the file's value survives. That matches how FR-053 and FR-065 treat the
// XDG variables and is the useful reading of `export MIKO_POST_TELEGRAM_BOT_TOKEN=`
// in a shell profile: the user has no token in the environment, not a
// deliberate empty credential.
//
// Trimming replaces an earlier rule that the value was taken exactly as given,
// on the grounds that an altered credential cannot be compared with its source.
// That rule leaked. A token saved with a trailing space passed validation, the
// URL carried it percent-escaped, and the logger's redaction pattern was the
// padded value, so a message body containing the bare token reached the log
// verbatim (Batch 12, ADV-007, reproduced). Whitespace can never be part of a
// Bot API token, so removing it loses nothing and the result still compares
// equal to what the user copied. Interior whitespace is a different mistake and
// is refused by validation rather than repaired here.
//
// Nothing else is trimmed (DEC-J9). A zero-width space, a byte-order mark, a
// smart quote or any other byte outside printable ASCII survives this trim
// wherever it is, and validation refuses it. Between the two, a token either
// trims to printable ASCII or is refused, so whitespace, invisible and non-ASCII
// corruptions can no longer load. Mistakes inside printable ASCII still can: a
// token pasted with its quotes, with a "bot" prefix or with a TOKEN= prefix
// passes both. While it is configured, a bare token in any recorded message
// body is not redacted, because the redaction pattern is the configured value
// and not the bare token: a failed post's captured body, and, when
// logging.message_on_error_only is false, every post's intake record. With the
// chat destination enabled such a token fails at Telegram; with it disabled
// nothing fails, and only the intake case applies.
func ResolveCredential(settings *Settings) {
	settings.Sink.Telegram.BotToken = settings.Sink.Telegram.BotToken.Trimmed()

	token, ok := os.LookupEnv(TelegramBotTokenEnv)
	if !ok {
		return
	}

	fromEnv := NewSecret(token).Trimmed()
	if fromEnv.IsEmpty() {
		return
	}

	settings.Sink.Telegram.BotToken = fromEnv
}
