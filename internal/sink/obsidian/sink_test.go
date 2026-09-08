package obsidian_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
	"github.com/sgykfjsm/miko-post/internal/sink/obsidian"
)

// The clock every test reads unless it needs its own. A fixed instant, so an
// assertion can name the exact bytes the sink is expected to write.
var noon = time.Date(2026, 9, 8, 11, 42, 3, 0, time.Local)

// settingsFor builds enabled settings pointing at dir, with the defaults
// config.Defaults supplies.
func settingsFor(dir string) config.ObsidianSettings {
	return config.ObsidianSettings{
		Enabled:         true,
		DailyNoteDir:    dir,
		FilenameFormat:  "2006-01-02.md",
		TimeFormat:      "15:04",
		CreateIfMissing: true,
	}
}

// fixedClock returns a clock stuck at at.
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// mustMessage builds a Message that has passed validation, as the orchestrator
// guarantees before a sink is reached.
func mustMessage(t *testing.T, text string) post.Message {
	t.Helper()

	message := post.Message{Original: text}
	if err := message.Validate(); err != nil {
		t.Fatalf("post.Message{%q}.Validate: %v", text, err)
	}

	return message
}

// notePath is where the sink will write, given the same clock.
func notePath(dir string, at time.Time) string {
	return filepath.Join(dir, at.Format("2006-01-02.md"))
}

// readNote returns the note's exact bytes.
func readNote(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(raw)
}

// TestAppendNeverAltersExistingContent is T027's central requirement and
// SC-009's whole claim (FR-049, constitution principle VI).
//
// The three cases are the ones T027 names, and the third is the one that
// matters: a note whose last line has no terminator. A read-modify-write
// implementation, or one that tried to "fix" the missing newline, would rewrite
// bytes the user wrote. The assertion is deliberately on the exact prefix — not
// on a line count or a substring — because "never altered" is a claim about
// bytes.
func TestAppendNeverAltersExistingContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		existing string
		// create says whether the note exists at all before the post.
		create bool
	}{
		{name: "a new note", create: false},
		{
			name:     "an existing note ending in a newline",
			existing: "# 2026-09-08\n\n- 09:15 朝の思いつき\n",
			create:   true,
		},
		{
			// The case that catches a read-modify-write, and the one SC-009
			// calls out.
			name:     "an existing note not ending in a newline",
			existing: "# 2026-09-08\n\n- 09:15 朝の思いつき",
			create:   true,
		},
		{
			// A note with content that must not be interpreted: FR-045 forbids
			// searching for or inserting into a named section, so this heading
			// is just bytes.
			name:     "an existing note with headings and sections",
			existing: "# Daily\n\n## Captured\n\n- 09:15 earlier\n\n## Tasks\n\n- [ ] something\n",
			create:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := notePath(dir, noon)

			if test.create {
				if err := os.WriteFile(path, []byte(test.existing), 0o600); err != nil {
					t.Fatalf("seed the note: %v", err)
				}
			}

			sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

			if err := sink.Send(context.Background(), mustMessage(t, "今日も美琴が可愛い♡")); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := readNote(t, path)

			// Every byte that was there is still there, in order, at the
			// front.
			if !strings.HasPrefix(got, test.existing) {
				t.Fatalf("existing content was altered.\n got: %q\nwant prefix: %q", got, test.existing)
			}

			// And exactly one entry was added.
			added := strings.TrimPrefix(got, test.existing)
			if added != "- 11:42 今日も美琴が可愛い♡\n" {
				t.Errorf("appended %q, want %q", added, "- 11:42 今日も美琴が可愛い♡\n")
			}
		})
	}
}

// TestTheTransformationOrderIsNormative covers FR-047's four steps and the one
// ordering that carries a real consequence (SC-010).
//
// Step 1 must precede step 2. Reversed, a \r\n pasted from a Windows editor, a
// chat client or a web page yields two <br> where one was meant — so the same
// text stored from two sources would produce two different lines in the vault.
// Every case below that contains a \r exists to pin that.
func TestTheTransformationOrderIsNormative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "a single line is unchanged apart from the prefix",
			input: "今日も美琴が可愛い♡",
			want:  "- 11:42 今日も美琴が可愛い♡\n",
		},
		{
			name:  "line feeds become one br each",
			input: "一行目\n二行目\n三行目",
			want:  "- 11:42 一行目<br>二行目<br>三行目\n",
		},
		{
			// The ordering trap: one <br>, not two.
			name:  "a CRLF pair becomes one br",
			input: "一行目\r\n二行目",
			want:  "- 11:42 一行目<br>二行目\n",
		},
		{
			name:  "a bare carriage return becomes one br",
			input: "一行目\r二行目",
			want:  "- 11:42 一行目<br>二行目\n",
		},
		{
			name:  "mixed terminators each become exactly one br",
			input: "a\r\nb\rc\nd",
			want:  "- 11:42 a<br>b<br>c<br>d\n",
		},
		{
			// A trailing newline is part of the message and becomes markup, so
			// it cannot add a second physical line.
			name:  "a trailing newline becomes markup, not a second line",
			input: "本文\n",
			want:  "- 11:42 本文<br>\n",
		},
		{
			name:  "a trailing CRLF likewise becomes one br",
			input: "本文\r\n",
			want:  "- 11:42 本文<br>\n",
		},
		{
			name:  "consecutive newlines each become a br",
			input: "上\n\n下",
			want:  "- 11:42 上<br><br>下\n",
		},
		{
			// Markdown metacharacters are the user's text, not ours to escape:
			// FR-011 delivers what was typed, and a note-taking app rendering
			// Markdown is the point of the destination.
			name:  "markdown metacharacters are left alone",
			input: "*bold* #tag [link](url) `code`",
			want:  "- 11:42 *bold* #tag [link](url) `code`\n",
		},
		{
			name:  "an existing br in the message is not doubled",
			input: "文字<br>列",
			want:  "- 11:42 文字<br>列\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

			if err := sink.Send(context.Background(), mustMessage(t, test.input)); err != nil {
				t.Fatalf("Send: %v", err)
			}

			got := readNote(t, notePath(dir, noon))
			if got != test.want {
				t.Errorf("wrote %q, want %q", got, test.want)
			}

			// SC-010 stated directly: one physical line, whatever the input
			// contained.
			if lines := strings.Count(got, "\n"); lines != 1 {
				t.Errorf("the note has %d line feeds, want exactly 1: %q", lines, got)
			}

			// FR-050: LF only. A stray CR would make the note's line endings
			// depend on where the message was pasted from.
			if strings.Contains(got, "\r") {
				t.Errorf("the note contains a carriage return: %q", got)
			}
		})
	}
}

// TestAMissingNoteRespectsCreateIfMissing covers FR-046 in both directions.
func TestAMissingNoteRespectsCreateIfMissing(t *testing.T) {
	t.Parallel()

	t.Run("created when permitted", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		settings := settingsFor(dir)
		settings.CreateIfMissing = true

		sink := obsidian.NewWithClock(settings, fixedClock(noon))

		if err := sink.Send(context.Background(), mustMessage(t, "新しいノート")); err != nil {
			t.Fatalf("Send: %v", err)
		}

		if got := readNote(t, notePath(dir, noon)); got != "- 11:42 新しいノート\n" {
			t.Errorf("wrote %q", got)
		}
	})

	t.Run("this sink fails when creation is forbidden", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := notePath(dir, noon)

		settings := settingsFor(dir)
		settings.CreateIfMissing = false

		sink := obsidian.NewWithClock(settings, fixedClock(noon))

		err := sink.Send(context.Background(), mustMessage(t, "作らないで"))
		if err == nil {
			t.Fatal("Send succeeded for a missing note with create_if_missing false")
		}

		// A distinct error, because the classifier (T056) has to tell "the
		// user asked for this" from a permission problem or a full disk.
		if !errors.Is(err, obsidian.ErrNoteMissing) {
			t.Errorf("Send: %v, want it to wrap ErrNoteMissing", err)
		}

		// And nothing was created.
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("os.Stat(%s) = %v, want the note not to exist", path, statErr)
		}

		// The error names the file, because a failure the user cannot locate
		// is most of a failure they cannot act on.
		if !strings.Contains(err.Error(), path) {
			t.Errorf("Send: %v, want it to name %s", err, path)
		}
	})
}

// TestTargetReportsWhatSendActuallyWrote covers post.Targeter and the trap
// issue #98 exists to document.
//
// A Target that re-resolved on call would disagree with Send across local
// midnight, and the log would then name a file the post never touched — the
// exact reconstruction trail SC-008 and constitution principle III guarantee.
// The clock advances past midnight between the write and the read, which is a
// claim no test could make by waiting.
func TestTargetReportsWhatSendActuallyWrote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// 23:59:59 local, then 00:00:01 the next day.
	beforeMidnight := time.Date(2026, 9, 8, 23, 59, 59, 0, time.Local)
	afterMidnight := beforeMidnight.Add(2 * time.Second)

	reading := 0
	clock := func() time.Time {
		reading++
		if reading == 1 {
			return beforeMidnight
		}

		return afterMidnight
	}

	sink := obsidian.NewWithClock(settingsFor(dir), clock)

	if err := sink.Send(context.Background(), mustMessage(t, "深夜の思いつき")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	wrote := notePath(dir, beforeMidnight)
	wouldReResolve := notePath(dir, afterMidnight)

	if wrote == wouldReResolve {
		t.Fatalf("the two instants resolve to the same note (%s); the test cannot detect re-resolution", wrote)
	}

	// The file that actually received the line.
	if got := readNote(t, wrote); got == "" {
		t.Fatalf("nothing was written to %s", wrote)
	}

	if _, err := os.Stat(wouldReResolve); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists; the post was written to the wrong note", wouldReResolve)
	}

	// And Target names it, not the note a second clock reading would give.
	if got := sink.Target(); got != wrote {
		t.Errorf("Target() = %q, want the note Send wrote (%q); re-resolving gives %q",
			got, wrote, wouldReResolve)
	}

	// The entry's timestamp comes from the same reading as the filename, so
	// the note and the line inside it agree.
	if got := readNote(t, wrote); !strings.HasPrefix(got, "- 23:59 ") {
		t.Errorf("the entry reads %q, want the timestamp from the same instant as the filename", got)
	}
}

// TestTargetIsRecordedEvenWhenTheWriteFails covers the other half of what the
// orchestrator needs from Targeter.
//
// A failed append still has to be locatable: the log record for
// obsidian_append_failed carries the path, and a user told only "permission
// denied" cannot act on it.
func TestTargetIsRecordedEvenWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	settings := settingsFor(dir)
	settings.CreateIfMissing = false

	sink := obsidian.NewWithClock(settings, fixedClock(noon))

	if err := sink.Send(context.Background(), mustMessage(t, "失敗しても場所は分かる")); err == nil {
		t.Fatal("Send succeeded unexpectedly")
	}

	if got, want := sink.Target(), notePath(dir, noon); got != want {
		t.Errorf("Target() = %q after a failed append, want %q", got, want)
	}
}

// TestSendReportsAnAlreadyExpiredContext covers the honest extent of this
// sink's cancellation support (FR-015).
//
// os.OpenFile and os.File.Write take no context and cannot be interrupted,
// which is exactly why the orchestrator enforces its own bound rather than
// trusting this sink (decision DEC-A1). What the sink can do is decline to
// start work whose deadline has already passed, and that is asserted here — the
// note must not be created.
func TestSendReportsAnAlreadyExpiredContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

	err := sink.Send(ctx, mustMessage(t, "もう遅い"))
	if err == nil {
		t.Fatal("Send succeeded with an already-cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send: %v, want it to wrap context.Canceled", err)
	}

	if _, statErr := os.Stat(notePath(dir, noon)); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("the note was created for a post whose deadline had already passed")
	}

	// The target is still recorded, so a cancelled post is locatable too.
	if got, want := sink.Target(), notePath(dir, noon); got != want {
		t.Errorf("Target() = %q, want %q", got, want)
	}
}

// TestConcurrentAppendsToOneNoteAreWhole covers the shape a GUI window
// produces: FR-028 cancels the auto-close on interaction, so a window can post
// again while an earlier post is still in flight.
//
// Run under -race. O_APPEND is what makes each line land whole rather than at a
// shared offset; a writer that seeked, buffered, or read-modify-wrote would
// interleave or lose lines here.
func TestConcurrentAppendsToOneNoteAreWhole(t *testing.T) {
	t.Parallel()

	const posts = 24

	dir := t.TempDir()
	sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

	var running sync.WaitGroup

	for i := range posts {
		running.Add(1)

		go func(i int) {
			defer running.Done()

			if err := sink.Send(context.Background(), mustMessage(t, fmt.Sprintf("post-%02d", i))); err != nil {
				t.Errorf("post %02d: %v", i, err)
			}
		}(i)
	}

	running.Wait()

	got := readNote(t, notePath(dir, noon))

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != posts {
		t.Fatalf("the note has %d lines, want %d:\n%s", len(lines), posts, got)
	}

	// Every line is whole and distinct: a torn write shows up as a line that
	// does not match the shape, and a lost one as a missing index.
	seen := make(map[string]bool, posts)

	for _, line := range lines {
		if !strings.HasPrefix(line, "- 11:42 post-") {
			t.Errorf("line %q is not a whole entry", line)

			continue
		}

		if seen[line] {
			t.Errorf("line %q appears more than once", line)
		}

		seen[line] = true
	}

	if len(seen) != posts {
		t.Errorf("%d distinct entries, want %d", len(seen), posts)
	}
}

// TestTheSinkSatisfiesTheOrchestratorsInterfaces is the compile-time claim,
// asserted rather than assumed.
//
// post.Sink is what the orchestrator holds; post.Targeter is the optional
// interface it type-asserts to add the path field to this sink's events
// (issue #98). A refactor that changed either method's signature would break
// the wiring in T036/T040 rather than here, where the cause is obvious.
func TestTheSinkSatisfiesTheOrchestratorsInterfaces(t *testing.T) {
	t.Parallel()

	sink := obsidian.New(settingsFor(t.TempDir()))

	var asSink post.Sink = sink
	if asSink.Name() != "obsidian" {
		t.Errorf("Name() = %q, want %q", asSink.Name(), "obsidian")
	}

	// The type assertion the orchestrator performs, exercised here so that
	// dropping Target() fails in this package.
	targeter, ok := asSink.(post.Targeter)
	if !ok {
		t.Fatal("the obsidian sink does not satisfy post.Targeter; its events would carry no path")
	}

	// Before any post there is nothing to report, and reporting an invented
	// path would be worse than reporting none.
	if got := targeter.Target(); got != "" {
		t.Errorf("Target() = %q before any post, want empty", got)
	}
}

// TestDailyNotePathUsesTheInstantItIsGiven covers T030 directly.
func TestDailyNotePathUsesTheInstantItIsGiven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		dir    string
		format string
		at     time.Time
		want   string
	}{
		{
			name:   "the default format",
			dir:    "/vault/daily",
			format: "2006-01-02.md",
			at:     noon,
			want:   filepath.Join("/vault/daily", "2026-09-08.md"),
		},
		{
			name:   "a nested format places the note in a subdirectory",
			dir:    "/vault",
			format: "2006/01/2006-01-02.md",
			at:     noon,
			want:   filepath.Join("/vault", "2026", "09", "2026-09-08.md"),
		},
		{
			name:   "a trailing separator on the directory is cleaned",
			dir:    "/vault/daily/",
			format: "2006-01-02.md",
			at:     noon,
			want:   filepath.Join("/vault/daily", "2026-09-08.md"),
		},
		{
			// One second either side of midnight resolves to different notes,
			// which is the property issue #98 turns on.
			name:   "just before local midnight",
			dir:    "/vault",
			format: "2006-01-02.md",
			at:     time.Date(2026, 9, 8, 23, 59, 59, 0, time.Local),
			want:   filepath.Join("/vault", "2026-09-08.md"),
		},
		{
			name:   "just after local midnight",
			dir:    "/vault",
			format: "2006-01-02.md",
			at:     time.Date(2026, 9, 9, 0, 0, 1, 0, time.Local),
			want:   filepath.Join("/vault", "2026-09-09.md"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := obsidian.DailyNotePath(test.dir, test.format, test.at); got != test.want {
				t.Errorf("DailyNotePath(%q, %q, %s) = %q, want %q",
					test.dir, test.format, test.at, got, test.want)
			}
		})
	}
}

// TestEntryRendersTheTimeThroughTheConfiguredFormat covers T031's prefix half.
func TestEntryRendersTheTimeThroughTheConfiguredFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "the default 24-hour format", format: "15:04", want: "- 11:42 本文\n"},
		{name: "with seconds", format: "15:04:05", want: "- 11:42:03 本文\n"},
		{name: "a 12-hour format", format: "3:04 PM", want: "- 11:42 AM 本文\n"},
		{
			// An empty format renders as an empty string, leaving the prefix
			// and its separating space. Odd but not this function's to reject:
			// config validates the format keys (T017), and inventing a
			// fallback here would hide a settings problem.
			name:   "an empty format",
			format: "",
			want:   "-  本文\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := obsidian.Entry("本文", noon, test.format); got != test.want {
				t.Errorf("Entry(%q, %s, %q) = %q, want %q", "本文", noon, test.format, got, test.want)
			}
		})
	}
}

// failingNote is an open note whose write or close misbehaves.
type failingNote struct {
	path     string
	written  int
	writeErr error
	closeErr error
	closes   int
}

func (f *failingNote) WriteString(s string) (int, error) {
	if f.written > 0 || f.writeErr != nil {
		return f.written, f.writeErr
	}

	return len(s), nil
}

func (f *failingNote) Close() error {
	f.closes++

	return f.closeErr
}

func (f *failingNote) Name() string { return f.path }

// TestAppendEntryReportsIOFailuresAndAlwaysCloses covers the write-and-close
// step's failure paths (FR-048, FR-049).
//
// These are the paths that decide whether a partially written line is reported
// to the user as a success, and none of them can be provoked portably through
// the filesystem — a write that fails after a successful open needs a full disk
// or a revoked mount, and a close that fails needs a filesystem deferring its
// flush. That is what the noteHandle seam is for.
func TestAppendEntryReportsIOFailuresAndAlwaysCloses(t *testing.T) {
	t.Parallel()

	diskFull := errors.New("no space left on device")
	flushFailed := errors.New("input/output error")

	tests := []struct {
		name     string
		note     *failingNote
		ctx      func() (context.Context, context.CancelFunc)
		wantErr  error
		wantText string
	}{
		{
			name:     "the write fails",
			note:     &failingNote{path: "/vault/note.md", writeErr: diskFull},
			wantErr:  diskFull,
			wantText: "appending to /vault/note.md",
		},
		{
			// A short count with no error. io.Writer forbids it and *os.File
			// does not do it, but the consequence here is a partial line in
			// the user's note, so it is treated as a failure in its own right.
			name:     "the write is short but reports no error",
			note:     &failingNote{path: "/vault/note.md", written: 3},
			wantText: "wrote 3 of",
		},
		{
			name:     "the close fails after a good write",
			note:     &failingNote{path: "/vault/note.md", closeErr: flushFailed},
			wantErr:  flushFailed,
			wantText: "closing /vault/note.md after appending",
		},
		{
			// The write failure is the cause; the close failure is usually the
			// same condition seen twice, and reporting it would bury the
			// cause.
			name:     "a write failure wins over a close failure",
			note:     &failingNote{path: "/vault/note.md", writeErr: diskFull, closeErr: flushFailed},
			wantErr:  diskFull,
			wantText: "appending to",
		},
		{
			name: "an expired context declines the write",
			note: &failingNote{path: "/vault/note.md"},
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx, func() {}
			},
			wantErr:  context.Canceled,
			wantText: "appending to /vault/note.md",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			if test.ctx != nil {
				var cancel context.CancelFunc
				ctx, cancel = test.ctx()

				defer cancel()
			}

			err := obsidian.AppendEntry(ctx, test.note, "- 11:42 本文\n")
			if err == nil {
				t.Fatal("AppendEntry succeeded unexpectedly")
			}

			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Errorf("AppendEntry: %v, want it to wrap %v", err, test.wantErr)
			}

			if !strings.Contains(err.Error(), test.wantText) {
				t.Errorf("AppendEntry: %v, want it to mention %q", err, test.wantText)
			}

			// The descriptor is released on every path, including the one that
			// gives up before writing. Leaking it would exhaust the process
			// over a long GUI session against a failing vault.
			if test.note.closes != 1 {
				t.Errorf("the note was closed %d times, want exactly 1", test.note.closes)
			}
		})
	}
}

// TestAppendEntrySucceedsAndCloses is the control for the table above.
//
// Without it, an implementation that returned an error unconditionally would
// pass every case there.
func TestAppendEntrySucceedsAndCloses(t *testing.T) {
	t.Parallel()

	note := &failingNote{path: "/vault/note.md"}

	if err := obsidian.AppendEntry(context.Background(), note, "- 11:42 本文\n"); err != nil {
		t.Fatalf("AppendEntry: %v", err)
	}

	if note.closes != 1 {
		t.Errorf("the note was closed %d times, want exactly 1", note.closes)
	}
}

// TestSendReportsAnUnopenableNote covers open's remaining failure path
// (FR-046).
//
// Distinct from the create_if_missing case: here creation is permitted and the
// open still fails, so the error must come through as itself rather than being
// translated into ErrNoteMissing — the classifier (T056) has to tell "the user
// asked for this" from "something is wrong".
func TestSendReportsAnUnopenableNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(t *testing.T, root string) string
	}{
		{
			// The vault directory does not exist. This sink appends to a note
			// in a vault the user set up; creating missing directories is not
			// its job, and doing so silently would scatter directories on a
			// typo in daily_note_dir.
			name: "the vault directory does not exist",
			build: func(t *testing.T, root string) string {
				t.Helper()

				return filepath.Join(root, "no-such-vault")
			},
		},
		{
			name: "a directory sits where the note belongs",
			build: func(t *testing.T, root string) string {
				t.Helper()

				if err := os.Mkdir(filepath.Join(root, noon.Format("2006-01-02.md")), 0o700); err != nil {
					t.Fatalf("create the directory in the note's place: %v", err)
				}

				return root
			},
		},
		{
			name: "the note is not writable",
			build: func(t *testing.T, root string) string {
				t.Helper()

				if os.Geteuid() == 0 {
					t.Skip("root bypasses the permission bits this case relies on")
				}

				path := filepath.Join(root, noon.Format("2006-01-02.md"))
				if err := os.WriteFile(path, []byte("# existing\n"), 0o400); err != nil {
					t.Fatalf("create the read-only note: %v", err)
				}

				return root
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := test.build(t, t.TempDir())

			sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

			err := sink.Send(context.Background(), mustMessage(t, "開けない"))
			if err == nil {
				t.Fatal("Send succeeded for a note it cannot open")
			}

			// Not translated: create_if_missing is true here, so this is not
			// the configured-refusal case.
			if errors.Is(err, obsidian.ErrNoteMissing) {
				t.Errorf("Send: %v, want a real open failure rather than ErrNoteMissing", err)
			}

			if !strings.Contains(err.Error(), "opening") {
				t.Errorf("Send: %v, want it to say the open failed", err)
			}

			// And it names the path, so the user can act on it.
			if want := notePath(dir, noon); !strings.Contains(err.Error(), want) {
				t.Errorf("Send: %v, want it to name %s", err, want)
			}
		})
	}
}
