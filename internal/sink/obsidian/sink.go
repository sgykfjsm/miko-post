package obsidian

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// Sink appends one line to the current day's note.
//
// Writes are strictly additive: the file is opened for appending and never
// truncated, never read back, never rewritten (FR-049, constitution principle
// VI). That is the whole safety story for a tool pointed at a directory of the
// user's own writing — there is no code path here that can lose an existing
// character, because there is no code path that reads one.
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
// One caveat this design has, recorded rather than hidden. A Sink is
// constructed once and serves every post, so two posts running concurrently
// through the same Sink both write this field and Target returns whichever
// finished last. The orchestrator reads it immediately after that sink's Send
// returns, so the window is small, but it is not zero, and nothing here can
// close it: post.Sink is deliberately two methods (issue #98's own acceptance
// requires that), so Send has no way to hand its path back per call. Recorded
// as issue #111 for T040, which is where the value is consumed and where a
// per-post carrier could be introduced.
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
	Close() error
	Name() string
}

// appendEntry writes the entry and closes the note, reporting whichever failed.
//
// The close is not deferred, because its error matters: on some filesystems a
// buffered flush fails at close and nowhere else, and reporting success for a
// line that never reached the disk is the worst outcome available here. It runs
// before the write error is inspected so that a failed write does not also leak
// the descriptor.
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
	flags := os.O_APPEND | os.O_WRONLY
	if s.settings.CreateIfMissing {
		flags |= os.O_CREATE
	}

	note, err := os.OpenFile(path, flags, notePerm)
	if err == nil {
		return note, nil
	}

	if errors.Is(err, os.ErrNotExist) && !s.settings.CreateIfMissing {
		return nil, fmt.Errorf("%s: %w", path, ErrNoteMissing)
	}

	return nil, fmt.Errorf("opening %s for appending: %w", path, err)
}

// setTarget records the note this post resolved.
func (s *Sink) setTarget(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.target = path
}
