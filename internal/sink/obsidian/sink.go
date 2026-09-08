package obsidian

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
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
// sink refuses to follow (decision DEC-A2).
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

// Sink appends one line to the current day's note.
//
// Writes are strictly additive: the file is opened for appending and never
// truncated, never rewritten, never reordered (FR-049, constitution principle
// VI). That is the whole safety story for a tool pointed at a directory of the
// user's own writing.
//
// One byte is read, and only one: whether the note's last byte is a line feed,
// so an entry can start its own physical line (decision DEC-A1, see
// unterminated). An earlier version of this comment said no code path reads the
// note at all, which was true then and is not now. The guarantee that carries
// the safety is that no code path *writes* anywhere but the end.
type Sink struct {
	settings config.ObsidianSettings

	// now is the clock, replaceable by tests. The zero value is not useful, so
	// New installs time.Now; see export_test.go for how a test substitutes it.
	now func() time.Time

	// mu guards target.
	//
	// Target is read by the orchestrator after Send returns while other posts
	// may still be running: a GUI window outlives its submission (FR-028), so
	// one Sink serves more than one post over its life.
	mu     sync.Mutex
	target string
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

// Target reports the note this sink most recently resolved and wrote to
// (post.Targeter, issue #98).
//
// It returns what Send actually used rather than re-deriving it, which is the
// trap #98 exists to document: a Target that called time.Now itself would
// disagree with Send across local midnight, and the log would then name a file
// the post did not touch.
//
// One caveat this design has, recorded rather than hidden, and stated more
// carefully than it was at first. A Sink is constructed once and serves every
// post, so two overlapping posts both write this field. It holds whichever post
// *started* last — setTarget runs at the top of Send, before any I/O — so the
// stale window is not a moment after Send returns: it opens the instant a later
// post enters Send and lasts for the whole of that post's open and write, which
// on a stalled mount is unbounded. Across local midnight that means a completed
// post can read a path naming a note it never touched, which is the SC-008
// reconstruction failure this interface exists to prevent.
//
// Nothing here can close it: post.Sink is deliberately two methods (issue #98's
// own acceptance requires that), so Send has no way to hand its path back per
// call. This is a shape problem, not a synchronisation one — the mutex prevents
// a data race and cannot make the value per-post. Recorded as issue #111 for
// T040, which is where the value is consumed and where a per-post carrier could
// be introduced.
func (s *Sink) Target() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.target
}

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
// has already passed, and spares the vault a note created for a post that was
// never going to be reported.
func (s *Sink) Send(ctx context.Context, message post.Message) error {
	at := s.now()

	path := DailyNotePath(s.settings.DailyNoteDir, s.settings.FilenameFormat, at)
	s.setTarget(path)

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

	// An entry has to start its own physical line (decision DEC-A1).
	//
	// Written verbatim, an entry appended to a note whose last byte is not a
	// line feed continues the user's last line: "- 09:15 an earlier thought-
	// 11:42 a new thought". Their own sentence then looks edited, and the new
	// capture is not a bullet. Notes saved by editors that omit a trailing
	// newline are ordinary, and after a torn write — a killed process, ENOSPC,
	// a revoked mount — the note is *guaranteed* to end mid-entry, so every
	// later post would chain onto that fragment for as long as the file stood.
	//
	// A separator failure is not fatal to the post. Appending the entry with a
	// glued line is worse than appending it with a possibly-redundant blank
	// one, and both are far better than not appending it at all, so a read
	// error here is ignored deliberately rather than returned.
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
// A race with another appender is possible and accepted: between this read and
// our write, another process may append and terminate its own line, leaving our
// separator redundant. The cost is one blank line. The alternative would be
// locking a file in the user's vault, which is a far larger promise than an
// append-only note writer should make.
func unterminated(note noteHandle) bool {
	info, err := note.Stat()
	if err != nil {
		return false
	}

	size := info.Size()
	if size == 0 {
		// A new or empty note: the entry is the first line, so it needs no
		// separator ahead of it.
		return false
	}

	last := make([]byte, 1)

	if _, err := note.ReadAt(last, size-1); err != nil && !errors.Is(err, io.EOF) {
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
	// note's last byte is a line feed (decision DEC-A1). The read is one byte
	// at an explicit offset and changes nothing; O_APPEND still forces every
	// write to the end regardless of where a read left off.
	flags := os.O_APPEND | os.O_RDWR | noFollowFlag
	if s.settings.CreateIfMissing {
		flags |= os.O_CREATE
	}

	note, err := os.OpenFile(path, flags, notePerm)
	if err == nil {
		return note, nil
	}

	return nil, s.diagnose(path, err)
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

// setTarget records the note this post resolved.
func (s *Sink) setTarget(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.target = path
}
