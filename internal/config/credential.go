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
// An empty environment variable counts as absent, so the file's value survives.
// That matches how FR-053 and FR-065 treat the XDG variables and is the useful
// reading of `export MIKO_POST_TELEGRAM_BOT_TOKEN=` in a shell profile: the
// user has no token in the environment, not a deliberate empty credential.
//
// The value is otherwise taken exactly as given, with no trimming. A token
// carrying a stray newline from `export TOKEN=$(cat file)` will therefore fail
// at the chat service rather than being silently repaired here. That is the
// deliberate choice: this package must not alter a credential, because a
// credential it has altered is one no one can compare against the source they
// copied it from.
func ResolveCredential(settings *Settings) {
	token, ok := os.LookupEnv(TelegramBotTokenEnv)
	if !ok || token == "" {
		return
	}

	settings.Sink.Telegram.BotToken = NewSecret(token)
}
