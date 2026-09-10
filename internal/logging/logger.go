package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// Source is the front door a record originated from (FR-066).
//
// Defined type for the same reason as Event: "cli" and "gui" are the only two
// values contracts/log-events.md admits, and a third would be invisible until
// someone filtered a log by source and silently lost records.
type Source string

const (
	SourceCLI Source = "cli"
	SourceGUI Source = "gui"

	// sourceUnknown stands in when Open is handed a Source that is neither of
	// the two above.
	//
	// The alternatives were rejected: panicking is forbidden here (T023 —
	// diagnostics must never take down a post), and refusing to log would let a
	// front-door bug silently disable diagnostics for the whole run, which is
	// the failure FR-076 exists to prevent. A sentinel keeps the records, keeps
	// the enum honest by not pretending the value was "cli", and is greppable.
	//
	// This value appearing in a real log is a bug in the front door that
	// constructed the logger, not a supported source.
	sourceUnknown Source = "unknown"

	// missingMessageID stands in when Logger.Post is handed an empty
	// identifier, for the same reasons.
	//
	// An empty string would emit "message_id":"" and satisfy the letter of the
	// contract's "always present" while breaking its purpose: per-post
	// correlation. Two concurrent posts would both correlate to "", silently
	// interleaving into one apparent post.
	missingMessageID = "unknown"

	// secretMarker replaces a credential found in a record. It matches what
	// config.Secret renders, so a redacted value reads the same however it
	// reached the log.
	secretMarker = "[redacted]"
)

// Record keys this package owns.
//
// Only the keys the logger itself writes are named here. The rest of the field
// vocabulary in contracts/log-events.md — sink, duration_ms, error_type, error,
// http_status, path, message, message_len, message_bytes, stack — is written by
// the tasks that emit those events (T040, T063, T071-T073) and belongs with
// them.
//
// One trap for those tasks, recorded here because this is where someone will
// look: duration_ms must be built with slog.Int64(…, d.Milliseconds()), never
// slog.Duration. slog's JSON handler renders a Duration as its nanosecond
// count, so slog.Duration("duration_ms", d) emits nanoseconds under a key
// promising milliseconds — a factor of a million, in a field FR-066 requires,
// with nothing to make it look wrong.
const (
	keyTimestamp  = "ts"
	keyLevel      = "level"
	keyEvent      = "event"
	keySource     = "source"
	keyMessageID  = "message_id"
	keyAppVersion = "app_version"
	keyGitCommit  = "git_commit"
)

// File and directory permissions for the log.
//
// Tighter than the usual 0o644/0o755 because a log line can carry the message
// body: FR-068 records it whenever any sink failed, so this file accumulates
// the user's own private notes. 0o700 on the directory also matches the XDG
// convention for the state root.
const (
	logDirPerm  os.FileMode = 0o700
	logFilePerm os.FileMode = 0o600
)

// Options configures Open.
type Options struct {
	// Path is the log file path. Empty means the default resolved state path
	// (FR-056, FR-065): Open applies ResolvePath itself, so a caller passing
	// config.LoggingSettings.Path straight through gets the documented rule
	// rather than a silently discarding logger. See Open.
	//
	// When Writer is set nothing here is opened and nothing is resolved, but
	// whatever the caller put here is still recorded as the path a Degradation
	// names — so a rotating writer's write failure can still say which file it
	// was.
	Path string

	// Source is the front door opening the logger.
	Source Source

	// Writer, when non-nil, receives the records instead of a file at Path,
	// and Open touches no part of the filesystem.
	//
	// This is the seam the rotating writer plugs into (T068-T070, FR-072 -
	// FR-074): rotation owns a file's whole lifecycle — deciding before every
	// write whether to rename and reopen — so it cannot be layered onto a
	// *os.File this package opened and holds. The tests use the same seam.
	//
	// Whatever plugs in here inherits two obligations this package discharges
	// for the file it opens itself, because setting Writer means openLogFile
	// never runs:
	//
	//  1. Refuse a non-regular file before opening it. os.OpenFile on a FIFO
	//     blocks inside open(2) until a reader attaches, which stops the post
	//     dead — no degradation, no warning, nothing. See openLogFile.
	//  2. Open for appending and never truncate, so earlier runs survive
	//     (FR-074).
	//
	// Neither is enforceable from here. A writer supplied to this field is
	// trusted with them.
	//
	// A supplied Writer is not closed by Close; whoever supplied it owns it.
	Writer io.Writer

	// Redact are the credentials that must never reach the log, whatever
	// shape a record carries them in (FR-069, FR-043).
	//
	// This exists because the contract requires an `error` field on every
	// failure and its only natural source carries the token. A Telegram
	// transport failure is a *url.Error whose URL field is
	// https://api.telegram.org/bot<TOKEN>/sendMessage, and post.SinkResult
	// deliberately keeps Err exported so the diagnostic logger can reach it —
	// so the value that FR-066 obliges a call site to log is the value that
	// leaks. The redacting types cannot help: an error has no LogValue, and
	// json.Marshal on a *url.Error prints the URL verbatim.
	//
	// Leaving that to call-site discipline was the alternative, and it is the
	// pattern config.Secret's and post.SinkResult's own comments argue against
	// for exactly this class of rule — a rule nobody can be trusted to
	// remember at every site belongs at the one place every record passes
	// through. That place is this package's ReplaceAttr.
	//
	// A config.Secret rather than a string, so the caller does not have to
	// call Reveal to configure redaction; this package calls it once, at
	// construction. An empty or unset Secret is ignored, and no Redact at all
	// means no scanning and no cost.
	//
	// The scrub is exact-substring replacement of a known credential, not a
	// pattern that guesses at what looks secret, so it cannot mangle
	// legitimate diagnostic text. It is defence for the shapes the value types
	// do not cover, not a substitute for them, and not a substitute for
	// T084's end-to-end sentinel gate.
	Redact []config.Secret

	// AppVersion and GitCommit are stamped onto every record when non-empty
	// (FR-066). Empty means the corresponding key is omitted, which is how the
	// include_version and include_git_commit settings are expressed: the caller
	// resolves the setting and passes either the value or "".
	AppVersion string
	GitCommit  string
}

// Degradation reports that diagnostics are not reaching disk (FR-076).
//
// It carries what the single user-visible warning has to name — the path and
// the reason — and nothing else, because that is all FR-076 permits it to
// change. The sink outcomes and the exit status are computed from the post, not
// from this.
type Degradation struct {
	// Path is the log path that could not be written.
	Path string

	// Err is why. Never nil in a Degradation returned by this package.
	Err error
}

// Warning renders the one warning FR-076 allows, naming the path and the
// reason.
//
// Exactly one, per run, is the requirement — so a front door calls
// Logger.Degraded once when it renders results (T074) rather than warning at
// each failed write, which on a full disk would be once per record.
//
// The empty-path form is a separate sentence rather than the same one with a
// gap in it. Two declared situations reach it — no path could be resolved at
// all, and the Options.Writer seam used without a Path (T068-T070) — and in
// both of them the single-sentence form renders as "could not be written to :
// reason", which reads as a truncated message rather than as the fact that
// there is no path to name.
func (d *Degradation) Warning() string {
	if d.Path == "" {
		return fmt.Sprintf("warning: diagnostics could not be written: %v", d.Err)
	}

	return fmt.Sprintf("warning: diagnostics could not be written to %s: %v", d.Path, d.Err)
}

// ResolvePath applies FR-056's rule that an empty logging.path means the
// resolved default rather than a literal empty path (FR-065).
//
// It lives here rather than in config because config.Load must not touch the
// filesystem on the user's behalf, and because the rule has exactly one
// consumer: the logger. config.LoggingSettings.Path deliberately keeps "" as
// its default so "unset" stays distinguishable from "set to the default value",
// and this is the point of use that distinction was preserved for.
func ResolvePath(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}

	return config.DefaultLogPath()
}

// Logger owns the log destination for a process. It has no logging methods on
// purpose; see Post.
type Logger struct {
	handler slog.Handler
	writer  *safeWriter
	build   []slog.Attr
	source  Source
	path    string

	// openErr is set when the directory or file could not be opened, in which
	// case records are discarded. Reported by Degraded.
	openErr error
}

// Open returns a Logger that is always usable, even when diagnostics are not
// reaching disk (FR-075, FR-076).
//
// There is deliberately no error return. FR-076 requires that a diagnostics
// failure change nothing about the post — every enabled sink still runs, still
// reports its real outcome, and the exit status still reflects only those
// outcomes — and an (*Logger, error) signature invites exactly the caller that
// breaks that: one that treats a non-nil error as fatal, or that keeps a nil
// *Logger and panics on the first record. A logger that discards is a logger,
// so there is no state in which the caller has nothing to log to.
//
// There is deliberately no *Degradation return either, although there was one.
// It could only ever have been Degraded(), and having both invited the caller
// that emits FR-076's single warning twice: once on what Open handed back, and
// once from the Degraded() call that Warning's own comment asks for at render
// time. Degraded() is the one source of truth, and it is also the only one that
// can report a write that failed after a successful open.
//
// The directory is created first (FR-075). When either the directory or the
// file cannot be opened, the returned Logger writes to nothing and Degraded
// names the path and the reason for the caller's single warning.
//
// An empty Options.Path is resolved here rather than by the caller (issue
// #107). config.LoggingSettings.Path deliberately defaults to "" so that
// "unset" stays distinguishable from "set to the default value", which made the
// natural wiring — Path: settings.Logging.Path — degrade to discarding on every
// default install, reporting sink outcomes normally and writing no diagnostics
// at all. The rule was documented on Options.Path and left to the caller's
// memory, and a comment is the weakest enforcement available: this package's
// other invariants do better, since Logger.Post cannot be called without a
// message_id and Open has no error return so a caller cannot hold a nil
// *Logger. This was the one rule left to discipline, and there were about to be
// two front doors to remember it.
//
// A resolution failure becomes an ordinary Degradation rather than an error
// return. Keeping the no-error signature is the point — see above for why it
// has none — and a machine with no $HOME and no XDG_STATE_HOME is exactly the
// FR-075/FR-076 situation the discarding logger exists for: one warning, and
// the post runs. Degradation.Warning already has a sentence for the case where
// there is no path to name.
func Open(opts Options) *Logger {
	source := opts.Source
	if source != SourceCLI && source != SourceGUI {
		source = sourceUnknown
	}

	logger := &Logger{
		source: source,
		path:   opts.Path,
		build:  buildAttrs(opts),
	}

	var (
		target io.Writer
		// owned is the destination Close is allowed to close, and stays nil
		// for a supplied writer. It is held by safeWriter rather than by
		// Logger so that closing and writing are the same critical section;
		// see safeWriter.close.
		owned io.Closer
	)

	switch {
	case opts.Writer != nil:
		// A supplied writer owns its own destination, so no directory is
		// created, no file is opened, and nothing is resolved: resolving here
		// would invent a path this package is not going to touch and then name
		// it in a degradation about a write that failed somewhere else.
		// logger.path keeps whatever the caller put there so a write failure can
		// still name it — the path is not this package's to open here, which is
		// not the same as being unknown.
		target = opts.Writer
	default:
		path, err := ResolvePath(opts.Path)
		if err != nil {
			// The resolved path is unknown, so logger.path stays as the caller
			// gave it — empty — and Warning renders its no-path sentence.
			logger.openErr = fmt.Errorf("resolve the default log path: %w", err)

			break
		}

		logger.path = path

		file, err := openLogFile(path)
		if err != nil {
			// target stays nil: safeWriter discards, and every method below
			// keeps working.
			logger.openErr = err
		} else {
			target = file
			owned = file
		}
	}

	logger.writer = &safeWriter{target: target, owned: owned}
	logger.handler = slog.NewJSONHandler(logger.writer, &slog.HandlerOptions{
		// Info is the floor because the API exposes only Info and Error; see
		// PostLogger.
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttrRedacting(revealed(opts.Redact)),
	})

	return logger
}

// openLogFile creates the log directory when missing (FR-075) and opens the
// active log for appending.
//
// O_APPEND and never O_TRUNC: an existing log is a record of earlier runs, and
// FR-074 forbids this version from deleting, expiring or compressing anything.
// Truncating on open would delete all of it on the next post, which is the same
// data loss by a different route. O_APPEND also makes concurrent appends from
// two processes — a CLI post and an open GUI window — interleave whole writes
// rather than overwrite each other at a shared offset, which is what keeps
// FR-064's one-object-per-line guarantee true across processes.
func openLogFile(path string) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("no log path was resolved")
	}

	if err := os.MkdirAll(filepath.Dir(path), logDirPerm); err != nil {
		return nil, fmt.Errorf("create the log directory: %w", err)
	}

	// Anything that cannot be appended to is refused before it is opened.
	//
	// This is not defensive tidiness: os.OpenFile on a FIFO blocks inside
	// open(2) until a reader attaches, so a named pipe at the log path made
	// Open never return — no degradation, no warning, no records, and the post
	// never ran at all. That is worse than the failure FR-076 was written for,
	// because FR-076's whole promise is that diagnostics never block a post.
	// Refusing here routes the same situation through the ordinary openErr
	// path: one warning, and the post proceeds.
	//
	// Stat and not Lstat, deliberately. A symlink pointing at a regular file
	// is a legitimate way to place the log on another volume, and Lstat would
	// reject it. Following the link is safe on the same basis as the rest of
	// this path: writing here already requires write access to a directory
	// this package created 0o700 and the user chose.
	//
	// A TOCTOU window remains between this Stat and the OpenFile below —
	// nothing stops the path becoming a FIFO in between. It is accepted for
	// that same reason: exploiting it needs write access to that directory,
	// and closing it properly would mean O_NONBLOCK, which is not portable and
	// whose semantics on a regular file differ per platform. Stat closes the
	// realistic trigger, which is a pipe someone left there or a path typed
	// into logging.path by mistake.
	//
	// A Stat error is deliberately not turned into a degradation. fs.ErrNotExist
	// is the ordinary create path, and for any other error — a permission
	// problem on the directory, an I/O failure — the OpenFile below is about to
	// produce the real one, which is better than a second-hand version of it
	// invented here.
	if info, statErr := os.Stat(path); statErr == nil && !usableAsLog(info.Mode()) {
		return nil, fmt.Errorf("the log path is %s, which cannot be appended to", fileKind(info.Mode()))
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open the log file: %w", err)
	}

	return file, nil
}

// fileKind names what is sitting at the log path, for the one warning FR-076
// allows.
//
// The mode string is the fallback rather than the answer: "the log path is
// p---------" tells a user nothing, and the point of naming it is that "the log
// path is a named pipe" is immediately actionable.
func fileKind(mode os.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&os.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&os.ModeSocket != 0:
		return "a socket"
	default:
		return fmt.Sprintf("of an unsupported type (mode %s)", mode)
	}
}

// usableAsLog reports whether something already at the log path can be appended
// to without stopping the post.
//
// A regular file is the intended case. A device node is allowed as well, which
// is the whole reason this is a predicate rather than !mode.IsRegular():
// logging.path has no companion "disabled" setting, so pointing it at /dev/null
// is how a user turns diagnostics off, and refusing it produced an
// unsilenceable warning on every post — for a path that opens instantly and
// discards exactly as the user asked. That is a worse answer than the problem.
//
// What stays refused is what cannot work. A FIFO blocks in open(2) until a
// reader attaches and would stop the post dead. A socket cannot be opened this
// way at all. A directory fails, and saying so by name beats relaying EISDIR.
// Rotation (T068-T070) renames the active file, which none of the three could
// ever satisfy either.
func usableAsLog(mode os.FileMode) bool {
	return mode.IsRegular() || mode&os.ModeDevice != 0
}

// revealed extracts the credential strings to scrub, dropping the empty ones.
//
// This is the single Reveal call this package makes, and it happens once at
// construction rather than per record: the values are needed as plain strings
// to be searched for, and doing it here keeps that conversion in one place
// instead of on the emission path.
func revealed(secrets []config.Secret) []string {
	if len(secrets) == 0 {
		return nil
	}

	values := make([]string, 0, len(secrets))

	for _, secret := range secrets {
		if secret.IsEmpty() {
			continue
		}

		// A pattern shorter than the bound is skipped rather than used, and the
		// choice is deliberate in both directions.
		//
		// redactAll is an unanchored substring replacement, so a two-character
		// secret rewrites every field it appears inside: event names, sink names
		// and message_id, the identifier the whole log is correlated by. Measured
		// with "a" as the token, one record came back as
		// "mess[redacted]ge_received" with a mangled message_id — the record is
		// destroyed rather than redacted.
		//
		// Skipping means such a value is not replaced. That is the safer half of
		// the trade rather than a hole: config.Validate rejects a token this short
		// at load time, so anything reaching here below the bound was built
		// without passing validation, and a string that short cannot be a working
		// Bot API credential in the first place. The alternative — honouring it —
		// trades a non-credential for every diagnostic record on the run.
		//
		// The threshold is config's because config is the lower package and owns
		// the user-facing rule; sharing the constant is what keeps the guard and
		// the validation from drifting apart.
		if secret.Len() < config.MinBotTokenLength {
			continue
		}

		values = append(values, secret.Reveal())
	}

	return values
}

// buildAttrs returns the build-identity attributes for every record, omitting
// each one that was passed empty (FR-066).
func buildAttrs(opts Options) []slog.Attr {
	attrs := make([]slog.Attr, 0, 2)

	if opts.AppVersion != "" {
		attrs = append(attrs, slog.String(keyAppVersion, opts.AppVersion))
	}

	if opts.GitCommit != "" {
		attrs = append(attrs, slog.String(keyGitCommit, opts.GitCommit))
	}

	return attrs
}

// Post returns a logger bound to one post's correlation identifier.
//
// This is the only way to obtain something that can emit a record, and it is
// why Logger itself has no Info or Error. contracts/log-events.md marks
// message_id present on *every* record, and an attribute that callers are
// merely asked to remember is one they will eventually forget — producing a
// record that parses, looks complete, and cannot be correlated to the post it
// describes. Requiring the identifier to construct the logging type makes the
// omission impossible to express.
//
// The identifier is the ULID the orchestrator generates (T025, R-007). This
// package does not generate it: the orchestrator owns the post's identity, and
// a logger minting its own would give the same post different identifiers in
// two front doors.
//
// Attribute order in the emitted object is ts, level, event, source,
// message_id, then the build identity, then the caller's own attributes. JSON
// objects are unordered, so no consumer may depend on this; it is chosen so the
// always-present preamble reads first when a human tails the file.
func (l *Logger) Post(messageID string) *PostLogger {
	if messageID == "" {
		messageID = missingMessageID
	}

	attrs := make([]slog.Attr, 0, 2+len(l.build))
	attrs = append(attrs,
		slog.String(keySource, string(l.source)),
		slog.String(keyMessageID, messageID),
	)
	attrs = append(attrs, l.build...)

	return &PostLogger{logger: slog.New(l.handler.WithAttrs(attrs)), writer: l.writer}
}

// Degraded reports that diagnostics are not reaching disk, or nil when they
// are (FR-076).
//
// It covers both ways that happens through one value, because the front door
// has one warning to spend and should not care which it was: the directory or
// file could not be opened, or a write failed after a successful open — a full
// disk, a revoked mount, a removed file. The second is only visible after the
// fact, which is why this is a method to be consulted when results are
// rendered rather than something Open could have returned alone.
//
// Safe to call while sinks are still logging concurrently.
func (l *Logger) Degraded() *Degradation {
	if l.openErr != nil {
		return &Degradation{Path: l.path, Err: l.openErr}
	}

	if err := l.writer.firstErr(); err != nil {
		return &Degradation{Path: l.path, Err: err}
	}

	return nil
}

// Path reports the log file this Logger resolved, which is what FR-063 requires
// user-facing failure output to name.
//
// It is the resolved path and not Options.Path, so a front door prints the file
// a reader can actually open rather than the empty string that means "the
// default". That is the whole reason this accessor exists rather than the front
// door keeping its own copy of what it passed in: after issue #107 the caller no
// longer performs the resolution, so the caller no longer knows the answer.
//
// Empty only when no path could be resolved at all, in which case Degraded is
// non-nil and its warning carries the explanation instead. A front door must
// therefore treat an empty result as "there is no log line to print", not as a
// path.
func (l *Logger) Path() string { return l.path }

// Close releases the log file.
//
// A writer supplied through Options.Writer is left alone even when it happens
// to implement io.Closer: this package did not open it and cannot know whether
// the caller is done with it. Closing a discarding logger is a no-op, so a
// deferred Close is correct on whatever Open returned.
//
// Idempotent, because a deferred Close plus an explicit one is the ordinary
// shape and the second call must not become an error a front door has to
// special-case. The handle is released and dropped inside safeWriter's own
// lock, which also means a straggler write racing Close is discarded rather
// than latching a "file already closed" degradation — spending FR-076's one
// warning on the shutdown itself would be worse than losing the record.
func (l *Logger) Close() error {
	return l.writer.close()
}

// PostLogger emits records for one post.
//
// It exposes Info and Error and nothing else, so the level field can only ever
// hold the two values contracts/log-events.md admits. A Warn or Debug method
// would put a third value into a field that consumers filter on, and a level
// nobody reads is a record nobody sees.
type PostLogger struct {
	logger *slog.Logger

	// writer is the same safeWriter the handler writes through, held so that a
	// recovered panic can be latched where a write error would be. Both reach
	// the caller through Degraded(), so a front door has one thing to consult.
	writer *safeWriter
}

// Info records a successful or informational event.
func (p *PostLogger) Info(event Event, attrs ...slog.Attr) {
	p.log(slog.LevelInfo, event, attrs)
}

// Error records a failure.
//
// Every failure in a post is recorded, not just the first (FR-070): this method
// has no state and no short-circuit, so two sinks failing concurrently produce
// two records.
func (p *PostLogger) Error(event Event, attrs ...slog.Attr) {
	p.log(slog.LevelError, event, attrs)
}

// log is the single emission path.
//
// The event name travels as slog's message rather than as an attribute so that
// the event key cannot be absent: slog always emits a message, and replaceAttr
// renames it. An attribute would be one more thing a call site could forget,
// producing a record with no event — parseable, and useless.
//
// context.Background rather than a plumbed context, deliberately. The only
// contexts in a post are the per-sink timeouts (T055), and they are cancelled
// exactly when a sink fails — the moment its failure most needs recording. A
// handler that honours cancellation would then drop that record, so FR-070's
// "logging must not stop after the first error" would fail precisely on
// timeouts. slog's JSON handler ignores the context; this passes the one that
// stays valid regardless.
func (p *PostLogger) log(level slog.Level, event Event, attrs []slog.Attr) {
	// A panic anywhere below is caught and turned into a degradation.
	//
	// FR-076's first duty is that diagnostics never change what the post does,
	// and a panic is the loudest way to break it: the goroutine unwinds, the
	// sink never reports, and the exit status describes a post that did not
	// finish. Nothing on this path panics today — *os.File.Write does not, and
	// slog contains a panicking LogValuer itself — but Options.Writer is the
	// seam the rotating writer plugs into (T068-T070), and rename/stat/reopen
	// logic is exactly where a nil dereference lives.
	//
	// It is not silent: the panic is latched like a write error, so it comes
	// back through Degraded() and spends FR-076's one warning. That is the
	// difference between this and swallowing the bug — the post survives and
	// the operator is still told the log is not to be trusted.
	defer func() {
		if recovered := recover(); recovered != nil {
			p.writer.latch(fmt.Errorf("the log writer panicked: %v", recovered))
		}
	}()

	p.logger.LogAttrs(context.Background(), level, string(event), reserved(attrs)...)
}

// reserved drops caller attributes that would collide with a key this package
// owns.
//
// slog does not deduplicate keys, so an attribute named "event" would emit a
// second event key in the same object. That object still parses — most decoders
// take the last occurrence — which is what makes it dangerous: every query over
// the log silently reads whichever one the decoder happened to keep. Dropping
// the caller's copy keeps the identity fields trustworthy, and is the direction
// that loses the less important value of the two.
//
// A collision is a bug at the call site; the emitting tasks build their
// attributes from the vocabulary above.
//
// A *named* group is not traversed, because a key inside one is a different
// field: "level" inside slog.Group("upstream", …) is upstream's level, it lands
// in the record as upstream.level, and rewriting or dropping it would silently
// destroy a caller's data. An *empty-key* group is traversed, because slog does
// not open a group for an empty key — its members are emitted at the top level,
// where the record's own keys live, and ReplaceAttr sees len(groups) == 0 for
// each of them. Without recursion, slog.Group("", slog.String("event", …))
// walked straight past a filter that only inspects attr.Key and put a second
// event key in the record. The value is resolved first so the same shape
// reached through a LogValuer — slog.Any("", v) where v.LogValue() returns a
// group, which is exactly what post.SinkResult.LogValue returns — is visible
// here rather than only after the handler expands it.
//
// The recursion is the whole chain rather than one level: nested empty-key
// groups all inline to the top level too.
//
// The common case allocates nothing: attrs is returned as it came unless a
// reserved key or an empty key is actually present. An empty key alone is
// enough to pay for the copy because that is the shape whose contents have to
// be examined, and it does not occur in the attributes the emitting tasks
// build.
func reserved(attrs []slog.Attr) []slog.Attr {
	suspect := false

	for _, attr := range attrs {
		if isReservedKey(attr.Key) || attr.Key == "" {
			suspect = true

			break
		}
	}

	if !suspect {
		return attrs
	}

	kept := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		if isReservedKey(attr.Key) {
			continue
		}

		if attr.Key == "" {
			// Resolved once, and the resolved value is what is kept on both
			// arms below.
			//
			// Keeping the original attr on the non-group arm was a hole in
			// this filter rather than an efficiency: the handler would resolve
			// it a second time, so a LogValuer that answers differently on the
			// second call could return a plain value here — passing the group
			// check — and an empty-key group full of forged identity keys to
			// the handler. That is the whole defect this recursion exists to
			// stop, reached through the one attribute the recursion did not
			// keep hold of. Resolving once and forwarding what we resolved
			// means the handler renders the value this filter actually
			// inspected, so there is no second answer to differ.
			//
			// It also stops a caller's LogValue being invoked twice per
			// record, which for a valuer with a side effect or a cost was
			// wrong on its own.
			value := attr.Value.Resolve()

			if value.Kind() == slog.KindGroup {
				// Re-wrapped with the empty key it came with, so it still
				// inlines exactly where it would have; only its members
				// changed.
				kept = append(kept, slog.Attr{Value: slog.GroupValue(reserved(value.Group())...)})

				continue
			}

			kept = append(kept, slog.Attr{Value: value})

			continue
		}

		kept = append(kept, attr)
	}

	return kept
}

// isReservedKey reports whether key would collide with one of the record's own.
//
// Two name sets belong here, and only "level" is in both. The first is what
// this package writes — the keys above, which are what a consumer greps for.
// The second is what replaceAttr renames *from*: it manufactures ts out of
// slog.TimeKey and event out of slog.MessageKey, so an attribute keyed "time"
// or "msg" is not merely a near miss, it is renamed *into* an identity key
// after passing the filter. "msg" is one character from the contract's own
// message field and is the habitual slog spelling, so a call site will reach
// for it.
//
// Two switch statements rather than one case list, because keyLevel and
// slog.LevelKey are both "level" and a single switch carrying the same constant
// twice does not compile. Both are named anyway: the sets are separate reasons,
// so the next key added to either one has an obvious home, and slog's constants
// are used rather than "time"/"msg" literals so a toolchain that renamed them
// would move this filter with them.
func isReservedKey(key string) bool {
	// What this package writes.
	switch key {
	case keyTimestamp, keyLevel, keyEvent, keySource, keyMessageID,
		keyAppVersion, keyGitCommit:
		return true
	}

	// What replaceAttr renames from.
	switch key {
	case slog.TimeKey, slog.LevelKey, slog.MessageKey:
		return true
	}

	return false
}

// replaceAttr renames slog's three built-in keys to the contract's names and
// lowercases the level (FR-066, contracts/log-events.md).
//
// The timestamp keeps its slog.Time value rather than being reformatted here,
// so the handler's own rendering applies: a numeric offset from the local zone,
// and sub-second precision, which is what matters — two sinks in one post
// routinely complete inside the same second, and reading a post's trace in
// order is most of what the log is for. Whole seconds, as the contract's
// illustrative record shows, would be valid RFC 3339 too but would not order.
//
// The exact shape of the fractional part is the JSON handler's, not this
// package's, and it is worth knowing precisely because it is easy to state
// wrongly: slog's JSON handler formats with time.RFC3339Nano, so the fraction
// carries whatever the clock supplied (microseconds in practice here, not
// milliseconds — only slog's *text* handler truncates to milliseconds), and
// RFC3339Nano strips trailing zeros, so the fraction is variable-width and
// absent altogether on a whole second. Every one of those forms is a valid
// RFC 3339 timestamp, which is what contracts/log-events.md asks for; a
// consumer must parse the field rather than slice it.
//
// The groups guard matters: ReplaceAttr is called for attributes inside groups
// as well, and SinkResult.LogValue emits a group carrying its own "reason" and
// "err". Without the guard, a group member named "level" or "msg" would be
// rewritten into the record's own identity keys.
func replaceAttrRedacting(secrets []string) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		// The rename runs first, and the order is load bearing rather than
		// arbitrary.
		//
		// It used to run last, and that silently broke the level field for
		// every logger with a credential configured — which is every real run,
		// because app.OpenLogger always arms Redact. slog.Level implements
		// fmt.Stringer, so scrub's Stringer arm converted the level attribute
		// to the string "INFO" before renameBuiltin could see it; its type
		// assertion to slog.Level then failed and the lowercasing never
		// happened. contracts/log-events.md admits only "info" and "error", so
		// a consumer filtering level == "error" got nothing from a real log
		// while every test in this package passed — none of them populated
		// Redact and read the level in the same run.
		//
		// Renaming first hands scrub the already-lowercased string, which it
		// scans harmlessly, and leaves the timestamp and event untouched for
		// the same reasons as before.
		if len(groups) == 0 {
			a = renameBuiltin(a)
		}

		return scrub(a, secrets)
	}
}

// scrub removes every configured credential from an attribute's value, and
// flattens an error to its message on the way (FR-069, FR-043).
//
// Applied at every depth, groups included, because a credential nested inside
// a group is no less written down than one at the top level. This is the reason
// scrub runs before the groups guard rather than after it.
//
// Errors are converted to a string rather than searched in place, and that is
// the substantive half of this function. slog renders an unrecognised value by
// handing it to json.Marshal, which for a *url.Error prints its exported URL
// field — the token, verbatim, as structured JSON. There is no way to redact
// inside that shape after the fact, and contracts/log-events.md wants `error`
// to be a "detailed message" anyway, so the message is both the safer and the
// specified form. fmt.Stringer is covered for the same reason.
//
// Nothing happens when no credential is configured, so the ordinary path pays
// one length check.
func scrub(a slog.Attr, secrets []string) slog.Attr {
	if len(secrets) == 0 {
		return a
	}

	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(redactAll(a.Value.String(), secrets))
	case slog.KindAny:
		switch value := a.Value.Any().(type) {
		case error:
			a.Value = slog.StringValue(redactAll(value.Error(), secrets))
		case fmt.Stringer:
			a.Value = slog.StringValue(redactAll(value.String(), secrets))
		}
	default:
		// A number, a bool, a duration or a time cannot carry a credential,
		// and a group's members arrive here individually.
	}

	return a
}

// redactAll replaces every occurrence of every credential with the marker.
//
// The marker matches what config.Secret renders, so a redacted value reads the
// same wherever it came from.
func redactAll(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, secretMarker)
	}

	return text
}

// renameBuiltin maps slog's three built-in keys to the contract's names and
// lowercases the level (FR-066, contracts/log-events.md).
func renameBuiltin(a slog.Attr) slog.Attr {

	switch a.Key {
	case slog.TimeKey:
		a.Key = keyTimestamp
	case slog.LevelKey:
		a.Key = keyLevel

		if level, ok := a.Value.Any().(slog.Level); ok {
			// "INFO" -> "info". Only Info and Error are reachable through
			// PostLogger, so the offset forms slog.Level.String can produce
			// ("INFO+1") cannot occur here.
			a.Value = slog.StringValue(strings.ToLower(level.String()))
		}
	case slog.MessageKey:
		a.Key = keyEvent
	}

	return a
}

// safeWriter forwards records to the log and never reports a failure back to
// slog.
//
// slog's Logger discards whatever error a handler returns, so a failing write
// is already silent — but it is silent in a way that loses the reason. This
// keeps the first one, which is what FR-076's warning needs, and reports
// success upward so no slog internal ever treats the write as worth retrying or
// abandoning. A nil target is the discarding case; every write is accepted and
// dropped.
//
// Only the first error is kept. A full disk fails every subsequent write with
// the same cause, and FR-076 allows exactly one warning, so later errors have
// nothing to add. Writing continues rather than latching off, so a transient
// failure recovers on its own.
//
// The mutex is not redundant with slog's. slog's JSON handler holds its own
// lock across the write, so Write is already serialised against itself — but
// firstErr is called from the front door while sinks are still posting, and
// that read races the write without this. Close is in the same critical
// section for the same reason.
type safeWriter struct {
	mu     sync.Mutex
	target io.Writer
	err    error

	// owned is the destination this package opened and may close, and is nil
	// when the caller supplied the writer. Cleared by close, which is what
	// makes Close idempotent.
	owned io.Closer
}

// Write forwards p and always reports it as fully written.
func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.target == nil {
		return len(p), nil
	}

	n, err := s.target.Write(p)

	// A short count with no error is treated as a failure. io.Writer forbids
	// it, but a wrapper that returns one anyway would otherwise be completely
	// silent — and it is not a partial success: slog hands this method one
	// whole record, trailing newline included, in a single Write, so any
	// n < len(p) ends the line inside the JSON object.
	if err == nil && n < len(p) {
		err = fmt.Errorf("wrote %d of %d bytes", n, len(p))
	}

	if err != nil && s.err == nil {
		s.err = err
	}

	if 0 < n && n < len(p) {
		// Best effort: terminate the truncated line so the *next* record still
		// decodes on its own. Without this the next record is appended onto the
		// fragment and one unreadable line costs two records instead of one,
		// which is what FR-064's per-line guarantee is there to bound.
		//
		// "Best effort" is the whole claim, and the bound holds only when the
		// writer can accept this one byte. A writer that short-writes *every*
		// call returns 0 for a one-byte write too, so the newline never lands
		// and every later record joins the same fragment — N records in one
		// unreadable line rather than two. It is not looped, because a writer
		// that always returns 0 would spin here, and spinning inside a log
		// write is worse than a merged line: FR-076's first duty is not to
		// block the post. Such a writer violates io.Writer either way, and the
		// latched error above still raises the one warning.
		//
		// The result is deliberately discarded. This is a repair, not a record;
		// if it fails, the cause is already the error kept above, and letting it
		// displace that would replace a real reason with a symptom.
		_, _ = s.target.Write([]byte{'\n'})
	}

	return len(p), nil
}

// latch records err as the reason diagnostics are degraded, if nothing has been
// recorded yet.
//
// Separate from Write because a panic recovered on the emission path never
// reached a write, and because the first reason is the one kept: FR-076 allows
// one warning, so a later cause has nothing to add.
func (s *safeWriter) latch(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err == nil {
		s.err = err
	}
}

// firstErr returns the first write failure, or nil.
func (s *safeWriter) firstErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}

// close releases an owned destination once, and does nothing on every later
// call or for a writer this package did not open.
//
// target is cleared along with the handle so a write arriving after Close —
// a straggler goroutine, a deferred emit — is discarded like any other write
// to a logger with no destination, rather than failing against a closed file
// and latching a degradation that describes the shutdown instead of a problem.
func (s *safeWriter) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.owned == nil {
		return nil
	}

	owned := s.owned
	s.owned = nil
	s.target = nil

	return owned.Close()
}
