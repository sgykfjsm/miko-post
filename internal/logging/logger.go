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
	// Path is the resolved log file path. Use ResolvePath to apply the
	// "empty means the default" rule before filling this in. Ignored when
	// Writer is set.
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
	// A supplied Writer is not closed by Close; whoever supplied it owns it.
	Writer io.Writer

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
func (d *Degradation) Warning() string {
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

	// file is the log file when this package opened it, and nil when the
	// caller supplied a Writer or when opening failed. Only a file this
	// package opened is closed by Close.
	file *os.File

	// openErr is set when the directory or file could not be opened, in which
	// case records are discarded. Reported by Degraded.
	openErr error
}

// Open returns a Logger that is always usable, plus a Degradation when
// diagnostics are not reaching disk (FR-075, FR-076).
//
// There is deliberately no error return. FR-076 requires that a diagnostics
// failure change nothing about the post — every enabled sink still runs, still
// reports its real outcome, and the exit status still reflects only those
// outcomes — and an (*Logger, error) signature invites exactly the caller that
// breaks that: one that treats a non-nil error as fatal, or that keeps a nil
// *Logger and panics on the first record. A logger that discards is a logger,
// so there is no state in which the caller has nothing to log to.
//
// The directory is created first (FR-075). When either the directory or the
// file cannot be opened, the returned Logger writes to nothing and the
// Degradation names the path and the reason for the caller's single warning.
func Open(opts Options) (*Logger, *Degradation) {
	source := opts.Source
	if source != SourceCLI && source != SourceGUI {
		source = sourceUnknown
	}

	logger := &Logger{
		source: source,
		path:   opts.Path,
		build:  buildAttrs(opts),
	}

	var target io.Writer

	switch {
	case opts.Writer != nil:
		// A supplied writer owns its own destination, so no directory is
		// created and no path is recorded as failing.
		target = opts.Writer
	default:
		file, err := openLogFile(opts.Path)
		if err != nil {
			// target stays nil: safeWriter discards, and every method below
			// keeps working.
			logger.openErr = err
		} else {
			logger.file = file
			target = file
		}
	}

	logger.writer = &safeWriter{target: target}
	logger.handler = slog.NewJSONHandler(logger.writer, &slog.HandlerOptions{
		// Info is the floor because the API exposes only Info and Error; see
		// PostLogger.
		Level:       slog.LevelInfo,
		ReplaceAttr: replaceAttr,
	})

	return logger, logger.Degraded()
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

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logFilePerm)
	if err != nil {
		return nil, fmt.Errorf("open the log file: %w", err)
	}

	return file, nil
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

	return &PostLogger{logger: slog.New(l.handler.WithAttrs(attrs))}
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

// Close releases the log file.
//
// A writer supplied through Options.Writer is left alone even when it happens
// to implement io.Closer: this package did not open it and cannot know whether
// the caller is done with it. Closing a discarding logger is a no-op, so a
// deferred Close is correct on whatever Open returned.
func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}

	return l.file.Close()
}

// PostLogger emits records for one post.
//
// It exposes Info and Error and nothing else, so the level field can only ever
// hold the two values contracts/log-events.md admits. A Warn or Debug method
// would put a third value into a field that consumers filter on, and a level
// nobody reads is a record nobody sees.
type PostLogger struct {
	logger *slog.Logger
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
// attributes from the vocabulary above. Only the top level is checked, because
// only the top level is where these keys mean anything — a "level" key inside
// a group is a different field.
//
// The common case allocates nothing: attrs is returned as it came unless a
// collision is actually present.
func reserved(attrs []slog.Attr) []slog.Attr {
	collision := false

	for _, attr := range attrs {
		if isReservedKey(attr.Key) {
			collision = true

			break
		}
	}

	if !collision {
		return attrs
	}

	kept := make([]slog.Attr, 0, len(attrs))

	for _, attr := range attrs {
		if !isReservedKey(attr.Key) {
			kept = append(kept, attr)
		}
	}

	return kept
}

// isReservedKey reports whether key is one this package writes itself.
func isReservedKey(key string) bool {
	switch key {
	case keyTimestamp, keyLevel, keyEvent, keySource, keyMessageID,
		keyAppVersion, keyGitCommit:
		return true
	default:
		return false
	}
}

// replaceAttr renames slog's three built-in keys to the contract's names and
// lowercases the level (FR-066, contracts/log-events.md).
//
// The timestamp keeps its slog.Time value rather than being reformatted here,
// so the handler's own RFC 3339 rendering applies: a numeric zone offset from
// the local zone, with millisecond precision. Whole seconds — as the contract's
// illustrative record shows — would be valid RFC 3339 too, but two sinks in one
// post routinely complete inside the same second, and reading a post's trace in
// order is most of what the log is for.
//
// The groups guard matters: ReplaceAttr is called for attributes inside groups
// as well, and SinkResult.LogValue emits a group carrying its own "reason" and
// "err". Without the guard, a group member named "level" or "msg" would be
// rewritten into the record's own identity keys.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}

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
// that read races the write without this.
type safeWriter struct {
	mu     sync.Mutex
	target io.Writer
	err    error
}

// Write forwards p and always reports it as fully written.
func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.target == nil {
		return len(p), nil
	}

	if _, err := s.target.Write(p); err != nil && s.err == nil {
		s.err = err
	}

	return len(p), nil
}

// firstErr returns the first write failure, or nil.
func (s *safeWriter) firstErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.err
}
