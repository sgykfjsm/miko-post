package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
)

// SinkName is this sink's stable identifier. It appears in results shown to the
// user and in every log record for this destination, so it is contract text
// rather than a debugging label (contracts/log-events.md).
const SinkName = "obsidian"

// notePerm is the mode a created note gets.
//
// 0o600 rather than 0o644: a daily note is the user's private writing, and the
// vault is theirs alone. It applies only when this sink creates the file —
// O_CREATE does not change the mode of a note that already exists, so a vault
// the user set up themselves keeps whatever permissions they chose.
const notePerm os.FileMode = 0o600

// ErrNoteMissing reports that today's note does not exist and settings forbid
// creating it (FR-046).
//
// A distinct error because the orchestrator's classifier (T056) has to tell it
// from a permission problem or a full disk: this one is the configuration
// working as the user asked, and the display reason should say so rather than
// implying a fault.
var ErrNoteMissing = errors.New("today's note does not exist and create_if_missing is false")

// ErrVaultMissing reports that the directory holding today's note does not
// exist.
//
// Distinct from ErrNoteMissing, and the distinction is the whole point. Both
// arrive as ENOENT from os.OpenFile, which cannot say whether the leaf or a
// parent is absent — so an earlier version of this code reported a typo'd or
// unmounted daily_note_dir as "create_if_missing is false", telling the user to
// change a setting that would not help. Flipping that setting produces a second,
// different failure, because this sink does not create directories.
//
// config.Validate requires daily_note_dir to be absolute but does not require it
// to exist, and it cannot: an external drive or a synced folder may legitimately
// be absent when settings are loaded and present when a post is made.
var ErrVaultMissing = errors.New("the daily-note directory does not exist")

// ErrNoteIsSymlink reports that the note path is a symbolic link, which this
// sink refuses to follow (decision DEC-B2).
//
// Following one let anything with write access to the vault redirect the append
// outside it — into an existing file, or, with a dangling link and O_CREATE, into
// a file this sink then created wherever the link pointed. A link to /dev/null
// was worse than either: the post was reported as delivered and nothing was
// stored.
//
// The precondition is write access to the vault, which already permits deleting
// the notes outright, so this is not a privilege boundary. It is a scope one:
// this sink's promise is that it appends to a note inside the vault the user
// configured, and a symlink breaks that promise without any of the user's
// settings saying so.
var ErrNoteIsSymlink = errors.New("the note path is a symbolic link, which is not followed")

// ErrNoteNotRegular reports that the note path is something other than a
// regular file — a FIFO, a socket, a device node (decision DEC-B2).
//
// Its own sentinel rather than a second meaning for ErrNoteIsSymlink: the two
// answer different questions ("is the path a link?" and "is the object a
// file?"), a path can be neither, and the message the user needs differs.
//
// A named pipe is the case that made this necessary, and it reaches DEC-B2's
// harm with no symlink at all. Under the O_WRONLY this sink used to open with,
// a FIFO at the note path blocked inside open(2) and the post failed loudly on
// the orchestrator's timeout. O_RDWR — which the separator rule requires —
// returns immediately, because the process holds the read end itself: the entry
// goes into the pipe buffer, close discards it, and Send returns nil. The user
// is told the post was delivered and their text is gone.
//
// Deliberately stricter than internal/logging's usableAsLog, which permits a
// device node. There, logging.path has no companion "disabled" setting, so
// pointing it at /dev/null is how a user turns diagnostics off. Here there is
// no such reading: this sink is already gated by obsidian.enabled, and "discard
// my notes" is not a thing anyone asks for. So this is a plain IsRegular test
// and must stay one — harmonising the two predicates would reintroduce exactly
// the silent-loss case above.
var ErrNoteNotRegular = errors.New("the note path is not a regular file")

// Sink appends one line to the current day's note.
//
// Writes are strictly additive: the file is opened for appending and never
// truncated, never rewritten, never reordered (FR-049, constitution principle
// VI). That is the whole safety story for a tool pointed at a directory of the
// user's own writing.
//
// One byte is read, and only one: whether the note's last byte is a line feed,
// so an entry can start its own physical line (decision DEC-B1, see
// unterminated). An earlier version of this comment said no code path reads the
// note at all, which was true then and is not now. The guarantee that carries
// the safety is that no code path *writes* anywhere but the end.
type Sink struct {
	settings config.ObsidianSettings

	// now is the clock, replaceable by tests. The zero value is not useful, so
	// New installs time.Now; see export_test.go for how a test substitutes it.
	now func() time.Time
}

// New builds the sink from its settings.
//
// The settings are copied by value, so a later reload cannot change where a
// post in flight is writing.
func New(settings config.ObsidianSettings) *Sink {
	return &Sink{settings: settings, now: time.Now}
}

// Name identifies this sink (post.Sink).
func (s *Sink) Name() string { return SinkName }

// ReportsTarget declares that Send reports the note it resolved through
// post.ReportTarget (post.TargetReporting, issue #98).
//
// The orchestrator holds this sink's start event until that report arrives, so
// obsidian_append_started carries the note path rather than only the two records
// that follow the write. A failure the user cannot locate is most of a failure
// they cannot act on, and the append that fails hardest — a vault on a mount
// that has gone away — fails before there is anything but the path to report.
//
// Never called. The type assertion is the whole content: what the orchestrator
// needs is the declaration, and the value travels per call because a value
// returned from a method here could only ever be the *sink's* last note. One
// Sink serves every post — a GUI window outlives its submission (FR-028) — so a
// field holding the note was read by a completed post while a later one had
// already overwritten it, which across local midnight named a different file
// (issue #111). That is what this method replaces.
func (s *Sink) ReportsTarget() {}

// Send appends the message to today's note (FR-044 – FR-051).
//
// One reading of the clock decides both the note and the timestamp in the
// entry, and that instant is recorded as the target before any I/O, so the
// orchestrator can name the file even for a write that then failed — a failure
// the user cannot locate is most of a failure they cannot act on.
//
// The context is checked before opening and again before writing, and that is
// the honest extent of it: os.OpenFile and os.File.Write take no context and
// cannot be interrupted, so a vault on a stalled network or synced mount blocks
// here for as long as the mount does. That is precisely why the orchestrator
// enforces its own bound rather than trusting this sink to honour a deadline
// (internal/post/service.go, decision DEC-A1). Checking at the two points where
// we do have control still spares the user a pointless write when the deadline
// has already passed.
//
// What it does not always spare them is the file. When the deadline expires
// while os.OpenFile is blocked — the stalled-mount case, and the only one in
// which the second check earns its keep — O_CREATE has already run, so the post
// fails with the context error and leaves a zero-byte note in the vault.
// Cosmetic, and measured rather than reasoned about: an earlier version of this
// comment claimed the note was never created.
func (s *Sink) Send(ctx context.Context, message post.Message) error {
	at := s.now()

	path := DailyNotePath(s.settings.DailyNoteDir, s.settings.FilenameFormat, at)

	// Reported before the context check and before any I/O, so the
	// orchestrator can name the note whatever happens next — including a
	// deadline that had already expired when Send was entered, which is a
	// failure whose record is useless without the path (decision DEC-D3).
	// The value goes to the caller of this Send and to no other post: it
	// travels on ctx rather than on this Sink, which is what makes it
	// per-post rather than per-sink (issue #111).
	post.ReportTarget(ctx, path)

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("appending to %s: %w", path, err)
	}

	note, err := s.open(path)
	if err != nil {
		return err
	}

	return appendEntry(ctx, note, Entry(message.Original, at, s.settings.TimeFormat))
}

// noteHandle is the part of an open note appendEntry uses.
//
// An interface rather than *os.File so that the failure paths below are
// reachable from a test. Every one of them is an I/O error that cannot be
// provoked portably through the filesystem — a write that fails after a
// successful open needs a full disk or a revoked mount, and a close that fails
// needs a filesystem that defers its flush — and they are the paths that decide
// whether a partially written line is reported as a success. Leaving them
// untested in the one component whose job is not losing the user's writing
// would be the wrong trade.
//
// *os.File satisfies this as it stands; nothing wraps it in production.
type noteHandle interface {
	WriteString(string) (int, error)
	ReadAt([]byte, int64) (int, error)
	Stat() (os.FileInfo, error)
	Close() error
	Name() string
}

// appendEntry writes the entry and closes the note, reporting whichever failed.
//
// The close is not deferred, because its error matters: on some filesystems a
// buffered flush fails at close and nowhere else, so a close error is the last
// chance to learn that the line did not land. It runs before the write error is
// inspected so that a failed write does not also leak the descriptor.
//
// What close does not buy is durability. There is no fsync here, so a successful
// return means the bytes reached the kernel, not the disk, and a power loss can
// still lose the last entry. That is the ordinary trade for a note-append tool —
// FR-049 asks that existing content survive, which it does, not that each entry
// be durable before the command exits — and an earlier version of this comment
// overstated it.
//
// A write error wins over a close error when both occur. The write failure is
// the cause; the close failure is usually the same condition observed a second
// time, and reporting the second would bury the first.
//
// The context is consulted once more here because open can block for an
// unbounded time on a stalled mount, so the deadline may have passed while we
// waited for a descriptor we now hold. Nothing after this point is
// interruptible: os.File.Write takes no context, which is why the orchestrator
// enforces its own bound (internal/post/service.go, decision DEC-A1).
func appendEntry(ctx context.Context, note noteHandle, entry string) error {
	path := note.Name()

	if err := ctx.Err(); err != nil {
		closeNote(note)

		return fmt.Errorf("appending to %s: %w", path, err)
	}

	// An entry has to start its own physical line (decision DEC-B1).
	//
	// Written verbatim, an entry appended to a note whose last byte is not a
	// line feed continues the user's last line: "- 09:15 an earlier thought-
	// 11:42 a new thought". Their own sentence then looks edited, and the new
	// capture is not a bullet. Notes saved by editors that omit a trailing
	// newline are ordinary, and after a torn write — a killed process, ENOSPC,
	// a revoked mount — the note is *guaranteed* to end mid-entry, so every
	// later post would chain onto that fragment for as long as the file stood.
	//
	// A separator failure is not fatal to the post. A spurious blank line is
	// the worse of the two mistakes — it edits a note that was already correct,
	// where a glued line only fails to improve one that was already broken — and
	// both are far better than not appending at all, so a read error here is
	// ignored deliberately rather than returned, and unterminated answers false
	// on every failure so that the mistake it cannot avoid is the recoverable
	// one.
	if unterminated(note) {
		entry = "\n" + entry
	}

	written, writeErr := note.WriteString(entry)

	if writeErr == nil && written != len(entry) {
		// io.Writer forbids a short write without an error and *os.File does
		// not do it, but a short write here is a partial line in the user's
		// note — the one outcome this sink exists to prevent. Reporting it
		// costs one comparison.
		writeErr = fmt.Errorf("wrote %d of %d bytes", written, len(entry))
	}

	closeErr := note.Close()

	switch {
	case writeErr != nil:
		return fmt.Errorf("appending to %s: %w", path, writeErr)
	case closeErr != nil:
		return fmt.Errorf("closing %s after appending: %w", path, closeErr)
	default:
		return nil
	}
}

// unterminated reports whether the note has content that does not end in a line
// feed.
//
// This is the one read this package performs, and it is worth being exact about
// what it is not: one byte at an explicit offset, which changes no content, moves
// no file offset that a write depends on (O_APPEND puts every write at the end
// regardless), and cannot fail in a way that loses anything. The package's older
// claim that "there is no code path that reads one" is no longer true and has
// been corrected where it appeared; "no code path *rewrites* one" is the
// guarantee that matters and still holds absolutely.
//
// Every failure answers false — do not add a separator — because a spurious
// blank line in the user's note is a worse outcome than an occasional glued one,
// and neither is worth failing a post over.
//
// A race with another appender is possible and accepted, and it costs more than
// it first appeared to. Between this read and our write another process may
// append: if what it appends ends in a line feed, our separator is merely
// redundant and the note gains a blank line. If it does not — a foreign
// unterminated append, on a note whose last byte was a line feed when we looked
// — we add no separator and our entry glues onto its fragment, which is the
// outcome DEC-B1 exists to prevent, reached through the window DEC-B1's own
// read opens. Measured, not deduced.
//
// Two concurrent senders of this sink cannot produce it: every entry it writes
// ends in a line feed, so the byte read here is only ever stale in the harmless
// direction. It takes a foreign appender that leaves a line open. The
// alternative would be locking a file in the user's vault, which is a far
// larger promise than an append-only note writer should make.
func unterminated(note noteHandle) bool {
	info, err := note.Stat()
	if err != nil {
		return false
	}

	size := info.Size()
	if size == 0 {
		// A new or empty note: the entry is the first line, so it needs no
		// separator ahead of it.
		//
		// Redundant for an *os.File, and kept deliberately: ReadAt at the
		// offset -1 this would otherwise compute returns (0, err), so the
		// read != 1 guard below already answers false. It stays because the
		// handle is an interface — a note that answers a negative offset
		// differently would reach the guard with an unread byte — and because
		// "an empty note needs no separator" is the reason, not a side effect
		// of how a read fails. A mutant relaxing this to size < 0 survives the
		// suite for the same reason, which is expected rather than a gap.
		return false
	}

	last := make([]byte, 1)

	// The byte count is checked, not just the error, and that is the whole
	// point of this condition. io.ReaderAt may report io.EOF alongside a byte
	// it did deliver, which is why the EOF tolerance is here at all — but it
	// reports (0, io.EOF) as well, for a note that shrank between the Stat
	// above and this read. Tolerating the error alone left last[0] at its zero
	// value, which is not '\n', so a read that returned nothing answered "this
	// note needs a separator" and put a blank line in the user's note on the
	// strength of a byte nobody read.
	if read, err := note.ReadAt(last, size-1); read != 1 || (err != nil && !errors.Is(err, io.EOF)) {
		return false
	}

	return last[0] != '\n'
}

// closeNote closes without reporting, for the paths that already have an error
// worth returning.
func closeNote(note noteHandle) {
	// Deliberately discarded. The caller is about to return the reason it gave
	// up, and replacing that with a close failure would report a symptom in
	// place of the cause.
	_ = note.Close()
}

// open opens today's note for appending, creating it only when settings permit
// (FR-046, FR-049).
//
// The flags are the contract, not a preference. O_APPEND makes every write land
// at the end even if another process appended since the file was opened, which
// is what lets a CLI post and an open GUI window share a note safely. O_TRUNC
// is absent and must stay absent: it would empty the note on every post, and
// every test that writes one line to a fresh file would still pass.
//
// O_CREATE is added only when create_if_missing is true. When it is false a
// missing note has to fail this sink and nothing else (FR-046), so the error is
// translated into ErrNoteMissing rather than passed through as a bare ENOENT —
// the user asked for this behaviour and the reason they see should reflect
// that.
func (s *Sink) open(path string) (*os.File, error) {
	// Lstat, not Stat: the question is whether the path *itself* is a link,
	// not what it resolves to. A link is refused before any open is attempted
	// so the user gets a reason naming the actual problem; noFollowFlag then
	// closes the window between this check and the open.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNoteIsSymlink)
	}

	// O_RDWR rather than O_WRONLY, because appendEntry has to know whether the
	// note's last byte is a line feed (decision DEC-B1). The read is one byte
	// at an explicit offset and changes nothing; O_APPEND still forces every
	// write to the end regardless of where a read left off.
	flags := os.O_APPEND | os.O_RDWR | noFollowFlag
	if s.settings.CreateIfMissing {
		flags |= os.O_CREATE
	}

	note, err := os.OpenFile(path, flags, notePerm)
	if err != nil {
		return nil, s.diagnose(path, err)
	}

	// What is at the path is decided from the descriptor we hold, not from the
	// path, and after the open rather than before it. An fstat describes the
	// object actually opened, so there is nothing to race; a second path-based
	// check would only add another TOCTOU window beside the one noFollowFlag
	// exists to close, and it would answer about a file we might not be holding.
	if err := refuseUnlessRegular(path, note); err != nil {
		return nil, err
	}

	return note, nil
}

// refuseUnlessRegular closes the note and reports ErrNoteNotRegular unless the
// descriptor names a regular file (decision DEC-B2).
//
// A Stat failure refuses on the same branch, with the same sentinel: the
// question is whether this is a regular file, a descriptor that cannot answer
// it is not a note we are willing to write into, and a second sentinel for a
// case that needs EBADF or a failing filesystem to reach would be a
// distinction nothing downstream could act on.
//
// What that arm must not do is drop the cause. ESTALE from a revoked network
// mount and EIO from failing media both arrive here on a note that is a
// perfectly ordinary regular file, and answering them with "the note path is
// not a regular file" alone would be the misdiagnosis ErrVaultMissing exists to
// prevent, reached by another route. So the errno is wrapped beside the
// sentinel, the way diagnose does for ENOENT.
//
// A function rather than an inline branch in open, and taking a noteHandle
// rather than *os.File, because neither of the two things this decides — which
// error the user reads, and whether the descriptor is released — is otherwise
// observable: a Stat that fails on a descriptor os.OpenFile just returned needs
// a filesystem no test can arrange portably.
func refuseUnlessRegular(path string, note noteHandle) error {
	// statErr is consulted first and has to stay first: info is nil on that
	// arm, so asking it for a mode would panic.
	if info, statErr := note.Stat(); statErr != nil || !info.Mode().IsRegular() {
		closeNote(note)

		if statErr != nil {
			return fmt.Errorf("%s: %w: %w", path, ErrNoteNotRegular, statErr)
		}

		return fmt.Errorf("%s: %w", path, ErrNoteNotRegular)
	}

	return nil
}

// diagnose turns an open failure into the error that names the actual cause.
//
// ENOENT is the case worth the trouble. os.OpenFile returns it whether today's
// note is absent or the directory holding it is, and those want opposite
// answers: one is the configuration behaving as the user asked, the other is a
// vault that is not there. Nothing downstream can recover the distinction — this
// is the only place that still has the path — so it is established here, by
// asking about the parent directory.
//
// The underlying error is wrapped rather than replaced in every case, so a
// caller that wants the errno still has it. Discarding it was what made the
// original misdiagnosis untraceable.
func (s *Sink) diagnose(path string, err error) error {
	// One default arm, shared. An earlier shape returned the same generic
	// wrapping from two places — once for a non-ENOENT failure and once as a
	// fallthrough for ENOENT-with-an-existing-vault-and-creation-permitted,
	// which O_CREATE makes unreachable. Two returns meant one of them could
	// never be exercised; folding them leaves nothing that no input can reach.
	if errors.Is(err, os.ErrNotExist) {
		dir := filepath.Dir(path)

		if _, statErr := os.Stat(dir); statErr != nil {
			return fmt.Errorf("%s: %w: %w", dir, ErrVaultMissing, err)
		}

		if !s.settings.CreateIfMissing {
			return fmt.Errorf("%s: %w: %w", path, ErrNoteMissing, err)
		}
	}

	return fmt.Errorf("opening %s for appending: %w", path, err)
}
