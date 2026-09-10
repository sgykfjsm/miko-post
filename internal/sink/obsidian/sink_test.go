package obsidian_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
		// separated says whether the entry must be preceded by a line feed of
		// its own, which is the case exactly when the note already has content
		// that does not end in one.
		separated bool
	}{
		{name: "a new note", create: false},
		{
			name:     "an existing note ending in a newline",
			existing: "# 2026-09-08\n\n- 09:15 朝の思いつき\n",
			create:   true,
		},
		{
			// The case that catches a read-modify-write, and the one SC-009
			// calls out. It also gets a leading separator (decision DEC-B1),
			// because otherwise the entry continues the user's last line.
			name:      "an existing note not ending in a newline",
			existing:  "# 2026-09-08\n\n- 09:15 朝の思いつき",
			create:    true,
			separated: true,
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

			// And exactly one entry was added, on a line of its own.
			want := "- 11:42 今日も美琴が可愛い♡\n"
			if test.separated {
				want = "\n" + want
			}

			if added := strings.TrimPrefix(got, test.existing); added != want {
				t.Errorf("appended %q, want %q", added, want)
			}

			// However the note ended, the entry is the whole of the final
			// physical line — which is the property DEC-B1 exists for.
			lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
			if final := lines[len(lines)-1]; final != "- 11:42 今日も美琴が可愛い♡" {
				t.Errorf("the last physical line is %q, want the entry alone", final)
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

		// The errno stays wrapped, as it does on the ErrVaultMissing arm.
		// Only that arm asserted it, so this — the far more common of the two
		// — could lose the cause without anything failing, and discarding the
		// cause is what made the original ENOENT misdiagnosis untraceable.
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Send: %v, want the underlying ENOENT to remain wrapped", err)
		}

		// The error names the file, because a failure the user cannot locate
		// is most of a failure they cannot act on.
		if !strings.Contains(err.Error(), path) {
			t.Errorf("Send: %v, want it to name %s", err, path)
		}
	})
}

// reportedTargets collects what a sink reports through post.ReportTarget.
//
// This is the value the orchestrator sees, and it is per post rather than per
// sink: each post installs its own collector on its own context, which is what
// makes "this post's note" a property of the plumbing rather than of timing
// (decision DEC-D3, issue #111).
type reportedTargets struct {
	mu      sync.Mutex
	reports []string
}

func (r *reportedTargets) record(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.reports = append(r.reports, target)
}

func (r *reportedTargets) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.reports)
}

// only returns the single target this post reported, failing the test when the
// sink reported none or more than one.
//
// Both failures matter to the orchestrator rather than only to this test. No
// report means the start event carries no path; two mean the sink contradicted
// itself after a record had already been written, and only the first is kept.
func (r *reportedTargets) only(t *testing.T) string {
	t.Helper()

	reports := r.all()
	if len(reports) != 1 {
		t.Fatalf("the sink reported %d targets (%q), want exactly one", len(reports), reports)
	}

	return reports[0]
}

// reporting returns a context carrying a fresh collector, as the orchestrator's
// per-Send context does.
func reporting(ctx context.Context) (context.Context, *reportedTargets) {
	collected := &reportedTargets{}

	return post.WithTargetReporter(ctx, collected.record), collected
}

// TestTheReportedTargetIsWhatSendActuallyWrote covers post.ReportTarget and the
// trap issue #98 exists to document.
//
// A destination re-resolved after the write would disagree with Send across
// local midnight, and the log would then name a file the post never touched —
// the exact reconstruction trail SC-008 and constitution principle III
// guarantee. The clock advances past midnight between the write and the check,
// which is a claim no test could make by waiting.
func TestTheReportedTargetIsWhatSendActuallyWrote(t *testing.T) {
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

	ctx, reported := reporting(context.Background())

	if err := sink.Send(ctx, mustMessage(t, "深夜の思いつき")); err != nil {
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

	// And the report names it, not the note a second clock reading would give.
	if got := reported.only(t); got != wrote {
		t.Errorf("the sink reported %q, want the note Send wrote (%q); re-resolving gives %q",
			got, wrote, wouldReResolve)
	}

	// The entry's timestamp comes from the same reading as the filename, so
	// the note and the line inside it agree.
	if got := readNote(t, wrote); !strings.HasPrefix(got, "- 23:59 ") {
		t.Errorf("the entry reads %q, want the timestamp from the same instant as the filename", got)
	}
}

// TestTheTargetIsReportedEvenWhenTheWriteFails covers the other half of what
// the orchestrator needs from a reported target.
//
// A failed append still has to be locatable: the log record for
// obsidian_append_failed carries the path, and a user told only "permission
// denied" cannot act on it. This is also why the report happens before the I/O
// rather than after a successful write — moving it later would trade one wrong
// answer for a missing one.
func TestTheTargetIsReportedEvenWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	settings := settingsFor(dir)
	settings.CreateIfMissing = false

	sink := obsidian.NewWithClock(settings, fixedClock(noon))

	ctx, reported := reporting(context.Background())

	if err := sink.Send(ctx, mustMessage(t, "失敗しても場所は分かる")); err == nil {
		t.Fatal("Send succeeded unexpectedly")
	}

	if got, want := reported.only(t), notePath(dir, noon); got != want {
		t.Errorf("the sink reported %q after a failed append, want %q", got, want)
	}
}

// observingContext runs a hook whenever its Err is consulted, which is how a
// test looks at the sink from inside Send without adding a seam for it.
//
// Send checks ctx.Err() at exactly one point before it opens anything, so the
// first observation is taken between the target report and the open. The second
// comes from appendEntry, with the note already open.
type observingContext struct {
	context.Context

	observe func()
}

func (c observingContext) Err() error {
	c.observe()

	return c.Context.Err()
}

// TestTheTargetIsReportedBeforeTheNoteIsOpened pins the ordering sink.go
// documents on ReportsTarget, and which obsidian_append_started depends on for
// its path field.
//
// Reporting at the end of Send passes every other test here, because they all
// check after Send has returned. The difference is only visible from inside, and
// for a started event it is the whole difference: the orchestrator holds that
// event until the report arrives, and the open it precedes is unbounded on a
// stalled mount — so a report deferred past the open means no started record at
// all for as long as that lasts.
func TestTheTargetIsReportedBeforeTheNoteIsOpened(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := notePath(dir, noon)
	sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

	var (
		observations int
		targetsSeen  []string
		noteExisted  bool
	)

	base, reported := reporting(context.Background())

	ctx := observingContext{
		Context: base,
		observe: func() {
			observations++

			if observations > 1 {
				return
			}

			targetsSeen = reported.all()
			_, statErr := os.Stat(path)
			noteExisted = statErr == nil
		},
	}

	if err := sink.Send(ctx, mustMessage(t, "場所は先に決まる")); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if observations == 0 {
		t.Fatal("Send never consulted the context; the observation point this test depends on is gone")
	}

	if len(targetsSeen) != 1 || targetsSeen[0] != path {
		t.Errorf("the sink had reported %q at the first point inside Send, want exactly [%q]",
			targetsSeen, path)
	}

	// The same observation confirms the point is the one claimed: no I/O has
	// happened yet, so "before the open" is not being read off a note this
	// post had already created.
	if noteExisted {
		t.Error("the note existed at the first check inside Send; that point is after the open, not before it")
	}
}

// TestAWriteOnlyNoteIsRefused records what O_RDWR costs, so that it stays a
// decision rather than becoming an accident (decision DEC-B1).
//
// O_WRONLY appended to a 0200 note without complaint; O_RDWR cannot open one
// at all. A note left write-only by the user, a sync client or a restrictive
// ACL therefore fails every post until its mode changes.
//
// Accepted over the alternative, which was to fall back to O_WRONLY when the
// read is refused. That would give this sink a second write path that cannot
// read its own note, so the separator rule would hold on some notes and not
// others with nothing in the output to say which. This failure is loud, names
// the file, and one chmod fixes it.
func TestAWriteOnlyNoteIsRefused(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission bits this test relies on")
	}

	const existing = "# mine\n- 09:15 earlier\n"

	dir := t.TempDir()
	path := notePath(dir, noon)

	if err := os.WriteFile(path, []byte(existing), 0o200); err != nil {
		t.Fatalf("create the write-only note: %v", err)
	}

	err := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon)).
		Send(context.Background(), mustMessage(t, "書き込み専用"))
	if err == nil {
		t.Fatal("Send succeeded on a write-only note; the trade recorded here has changed and the contract needs amending")
	}

	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("Send: %v, want it to wrap os.ErrPermission", err)
	}

	// Never the configured refusal: the note is right there.
	if errors.Is(err, obsidian.ErrNoteMissing) {
		t.Errorf("Send: %v, want a permission failure rather than the configured refusal", err)
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("Send: %v, want it to name %s", err, path)
	}

	// The note is untouched, which is what makes this an honest refusal rather
	// than a partial write.
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		t.Fatalf("chmod to read the note back: %v", chmodErr)
	}

	if got := readNote(t, path); got != existing {
		t.Errorf("the note reads %q, want it unchanged at %q", got, existing)
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

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

	ctx, reported := reporting(cancelled)

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

	// The target is still reported, so a cancelled post is locatable too.
	if got, want := reported.only(t), notePath(dir, noon); got != want {
		t.Errorf("the sink reported %q, want %q", got, want)
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

	// Built here, not in the goroutines: mustMessage can call t.Fatalf, and
	// FailNow must not be reached from a goroutine other than the one running
	// the test. It happens not to deadlock today only because the deferred
	// Done is registered first; moving that line, or adding a fixture that
	// fails validation, would turn a fixture mistake into a hung test.
	messages := make([]post.Message, posts)
	for i := range messages {
		messages[i] = mustMessage(t, fmt.Sprintf("post-%02d", i))
	}

	var running sync.WaitGroup

	for i := range posts {
		running.Add(1)

		go func(i int) {
			defer running.Done()

			if err := sink.Send(context.Background(), messages[i]); err != nil {
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
// post.Sink is what the orchestrator holds; post.TargetReporting is the optional
// interface it type-asserts to decide whether to hold this sink's start event
// for a reported path (issue #98). A refactor that changed either would break
// the wiring in T036/T040 rather than here, where the cause is obvious.
func TestTheSinkSatisfiesTheOrchestratorsInterfaces(t *testing.T) {
	t.Parallel()

	sink := obsidian.New(settingsFor(t.TempDir()))

	var asSink post.Sink = sink
	if asSink.Name() != "obsidian" {
		t.Errorf("Name() = %q, want %q", asSink.Name(), "obsidian")
	}

	// The type assertion the orchestrator performs, exercised here so that
	// dropping ReportsTarget fails in this package.
	//
	// Losing it is a silent failure at the far end rather than a compile error:
	// the orchestrator would stop holding this sink's start event, emit it
	// before Send with no path, and issue #98's requirement that all three
	// obsidian_append_* records carry one would quietly stop holding.
	if _, ok := asSink.(post.TargetReporting); !ok {
		t.Fatal("the obsidian sink does not satisfy post.TargetReporting; " +
			"its start event would carry no path")
	}

	// And nothing is reported without a post to report it for. A sink that
	// reported at construction would put a path on a record belonging to no
	// submission.
	_, reported := reporting(context.Background())

	if got := reported.all(); len(got) != 0 {
		t.Errorf("the sink reported %q before any post, want nothing", got)
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

	// content is what the note already holds, so the separator decision can be
	// driven. statErr and readErr force unterminated's failure paths.
	content string
	statErr error
	readErr error

	// statSize, when non-zero, is the size Stat reports in place of
	// len(content) — the shape a note that shrank between the Stat and the
	// ReadAt presents. A size derived from content can never disagree with
	// what ReadAt can deliver, and that fidelity gap is why the (0, io.EOF)
	// path went unexercised while it was wrong.
	statSize int64

	// eofWithLastByte makes ReadAt report io.EOF alongside the byte it did
	// deliver. io.ReaderAt expressly permits that for a read reaching the end
	// of the file, and it is the case unterminated's EOF tolerance exists for;
	// without a fake that does it, the tolerance is unpinned.
	eofWithLastByte bool

	// wrote records what AppendEntry actually handed the note, so a test can
	// assert on the separator.
	wrote string
}

func (f *failingNote) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}

	if f.statSize != 0 {
		return sizeOnly(f.statSize), nil
	}

	return sizeOnly(len(f.content)), nil
}

func (f *failingNote) ReadAt(p []byte, off int64) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}

	if off < 0 || off >= int64(len(f.content)) {
		return 0, io.EOF
	}

	p[0] = f.content[off]

	if f.eofWithLastByte && off == int64(len(f.content))-1 {
		return 1, io.EOF
	}

	return 1, nil
}

// sizeOnly is an os.FileInfo that answers only Size, which is all unterminated
// asks of it.
type sizeOnly int

func (s sizeOnly) Name() string       { return "note" }
func (s sizeOnly) Size() int64        { return int64(s) }
func (s sizeOnly) Mode() os.FileMode  { return 0o600 }
func (s sizeOnly) ModTime() time.Time { return noon }
func (s sizeOnly) IsDir() bool        { return false }
func (s sizeOnly) Sys() any           { return nil }

func (f *failingNote) WriteString(s string) (int, error) {
	f.wrote += s

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

// TestADescriptorThatCannotBeStattedIsRefusedWithItsCause covers the other arm
// of the non-regular refusal (decision DEC-B2).
//
// The FIFO in sink_unix_test.go reaches the arm where fstat succeeds and
// answers "not a regular file". This one is the arm where fstat itself fails,
// which a descriptor os.OpenFile has just returned does not do on any platform
// this ships to — it takes a revoked network mount (ESTALE) or failing media
// (EIO), both of which arrive on notes that are perfectly ordinary regular
// files.
//
// Two properties, and the second is why this is not just a message test. The
// errno has to survive, or the user is sent looking for a named pipe that is
// not there while the real fault is the mount. And the descriptor has to be
// released: FR-028 keeps one process posting for the life of a GUI window, so a
// vault failing this check on every post would leak one per post until EMFILE
// takes down every sink in the process.
func TestADescriptorThatCannotBeStattedIsRefusedWithItsCause(t *testing.T) {
	t.Parallel()

	revoked := errors.New("stale NFS file handle")
	note := &failingNote{path: "/vault/note.md", statErr: revoked}

	err := obsidian.RefuseUnlessRegular(note.Name(), note)
	if err == nil {
		t.Fatal("a descriptor whose fstat failed was accepted as a note")
	}

	if !errors.Is(err, obsidian.ErrNoteNotRegular) {
		t.Errorf("refusal: %v, want it to wrap ErrNoteNotRegular", err)
	}

	if !errors.Is(err, revoked) {
		t.Errorf("refusal: %v, want it to wrap the fstat failure %v", err, revoked)
	}

	if !strings.Contains(err.Error(), note.Name()) {
		t.Errorf("refusal: %v, want it to name %s", err, note.Name())
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
		name     string
		wantText string
		build    func(t *testing.T, root string) string
	}{
		{
			// The vault directory does not exist. This sink appends to a note
			// in a vault the user set up; creating missing directories is not
			// its job, and doing so silently would scatter directories on a
			// typo in daily_note_dir.
			name:     "the vault directory does not exist",
			wantText: "daily-note directory does not exist",
			build: func(t *testing.T, root string) string {
				t.Helper()

				return filepath.Join(root, "no-such-vault")
			},
		},
		{
			name:     "a directory sits where the note belongs",
			wantText: "opening",
			build: func(t *testing.T, root string) string {
				t.Helper()

				if err := os.Mkdir(filepath.Join(root, noon.Format("2006-01-02.md")), 0o700); err != nil {
					t.Fatalf("create the directory in the note's place: %v", err)
				}

				return root
			},
		},
		{
			name:     "the note is not writable",
			wantText: "opening",
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

			// Never the configured-refusal case: that error means the note is
			// absent inside a vault that exists, and none of these are that.
			if errors.Is(err, obsidian.ErrNoteMissing) {
				t.Errorf("Send: %v, want a real failure rather than ErrNoteMissing", err)
			}

			if !strings.Contains(err.Error(), test.wantText) {
				t.Errorf("Send: %v, want it to mention %q", err, test.wantText)
			}

			// And it names the path, so the user can act on it.
			if want := notePath(dir, noon); !strings.Contains(err.Error(), want) {
				t.Errorf("Send: %v, want it to name %s", err, want)
			}
		})
	}
}

// TestAMissingVaultIsNotReportedAsAConfiguredRefusal covers the misdiagnosis
// both review stages found independently (FR-046).
//
// os.OpenFile returns the same ENOENT whether today's note is absent or the
// directory holding it is, and the two want opposite answers: one is the
// configuration behaving as the user asked, the other is a vault that is not
// there. Reporting the second as the first tells the user to flip
// `create_if_missing`, which produces a different failure — this sink does not
// create directories — so they get two dead ends instead of a cause.
//
// `config.Validate` requires `daily_note_dir` to be absolute but cannot require
// it to exist: an external drive or a synced folder may legitimately be absent
// when settings load and present when a post is made.
func TestAMissingVaultIsNotReportedAsAConfiguredRefusal(t *testing.T) {
	t.Parallel()

	for _, createIfMissing := range []bool{true, false} {
		t.Run(fmt.Sprintf("create_if_missing=%t", createIfMissing), func(t *testing.T) {
			t.Parallel()

			absent := filepath.Join(t.TempDir(), "unmounted-vault")

			settings := settingsFor(absent)
			settings.CreateIfMissing = createIfMissing

			err := obsidian.NewWithClock(settings, fixedClock(noon)).
				Send(context.Background(), mustMessage(t, "the drive is not mounted"))
			if err == nil {
				t.Fatal("Send succeeded with a non-existent vault directory")
			}

			if !errors.Is(err, obsidian.ErrVaultMissing) {
				t.Errorf("Send: %v, want it to wrap ErrVaultMissing", err)
			}

			// The whole point: never the benign one, whichever way
			// create_if_missing is set.
			if errors.Is(err, obsidian.ErrNoteMissing) {
				t.Errorf("Send: %v, reported as the configured refusal; the vault is what is missing", err)
			}

			// The cause stays inspectable. Discarding it is what made the
			// original misdiagnosis untraceable.
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("Send: %v, want the underlying ENOENT to remain wrapped", err)
			}

			if !strings.Contains(err.Error(), absent) {
				t.Errorf("Send: %v, want it to name the missing directory %s", err, absent)
			}
		})
	}
}

// TestOnlyAnAbsentNoteBecomesErrNoteMissing is the other half of the guard, and
// closes the mutation the correctness review found surviving.
//
// A guard of `if !CreateIfMissing` alone — dropping the ENOENT test — would
// report a permission problem, a full disk or an EISDIR as the configured
// refusal. Every existing unopenable-note case ran with create_if_missing true,
// so nothing exercised the combination that matters.
func TestOnlyAnAbsentNoteBecomesErrNoteMissing(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root bypasses the permission bits this test relies on")
	}

	dir := t.TempDir()
	path := notePath(dir, noon)

	// A note that exists, inside a vault that exists, that cannot be opened
	// for writing. Nothing here is absent, so ErrNoteMissing would be a lie.
	if err := os.WriteFile(path, []byte("# existing\n"), 0o400); err != nil {
		t.Fatalf("create the read-only note: %v", err)
	}

	settings := settingsFor(dir)
	settings.CreateIfMissing = false

	err := obsidian.NewWithClock(settings, fixedClock(noon)).
		Send(context.Background(), mustMessage(t, "読めるけど書けない"))
	if err == nil {
		t.Fatal("Send succeeded on a read-only note")
	}

	if errors.Is(err, obsidian.ErrNoteMissing) {
		t.Errorf("Send: %v, want a permission failure rather than the configured refusal", err)
	}

	if errors.Is(err, obsidian.ErrVaultMissing) {
		t.Errorf("Send: %v, the vault exists", err)
	}

	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("Send: %v, want it to wrap os.ErrPermission", err)
	}
}

// TestTheNotePathRefusalsStopAtTheLeaf covers decision DEC-B2 and the
// boundary DEC-B2-RESIDUAL draws around it: refused at the leaf, accepted
// above it. Both halves are asserted here, so the name says so.
//
// Following a symlink at the note path let anything with write access to the
// vault redirect the append outside it — into an existing file, or, with a
// dangling link and O_CREATE, into a file this sink then created wherever the
// link pointed. A link to /dev/null was worse than either: the post was
// reported as delivered and nothing was stored.
//
// The precondition is write access to the vault, which already permits deleting
// the notes outright, so this is not a privilege boundary. It is a scope one:
// this sink's promise is that it appends inside the vault the user configured.
//
// The last two cases are the other side of that boundary: the configurations
// DEC-B2-RESIDUAL deliberately accepts. They are here rather than in a test of
// their own because the boundary is one decision — refused at the leaf,
// accepted above it — and a tightening that erased the accepted half would
// otherwise pass a suite whose every symlink case expected a refusal.
func TestTheNotePathRefusalsStopAtTheLeaf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		// build prepares the case and returns the path it is about: for a
		// refusal, the thing beyond the link that must be left untouched; for
		// an accepted case, the second name for the note's bytes, which must
		// end up holding the entry.
		build func(t *testing.T, root, note string) string

		// accepted inverts the expectation to "the append succeeds and lands".
		accepted bool
	}{
		{
			name: "pointing at a file outside the vault",
			build: func(t *testing.T, root, note string) string {
				t.Helper()

				victim := filepath.Join(root, "outside.txt")
				if err := os.WriteFile(victim, []byte("PRECIOUS\n"), 0o600); err != nil {
					t.Fatalf("seed the victim: %v", err)
				}

				if err := os.Symlink(victim, note); err != nil {
					t.Fatalf("symlink: %v", err)
				}

				return victim
			},
		},
		{
			// The dangling case is the sharpest: following it would have this
			// sink *create* a file wherever the link pointed.
			name: "dangling, pointing outside the vault",
			build: func(t *testing.T, root, note string) string {
				t.Helper()

				victim := filepath.Join(root, "would-be-created")
				if err := os.Symlink(victim, note); err != nil {
					t.Fatalf("symlink: %v", err)
				}

				return victim
			},
		},
		{
			name: "pointing at /dev/null, which would swallow the post",
			build: func(t *testing.T, _, note string) string {
				t.Helper()

				if err := os.Symlink(os.DevNull, note); err != nil {
					t.Fatalf("symlink: %v", err)
				}

				return os.DevNull
			},
		},
		{
			// Even a link that stays inside the vault is refused: the rule is
			// about the note path being a link, not about where it lands, so
			// there is one behaviour to reason about rather than two.
			name: "pointing at another file inside the vault",
			build: func(t *testing.T, root, note string) string {
				t.Helper()

				sibling := filepath.Join(root, "vault", "other.md")
				if err := os.WriteFile(sibling, []byte("KEEP\n"), 0o600); err != nil {
					t.Fatalf("seed the sibling: %v", err)
				}

				if err := os.Symlink(sibling, note); err != nil {
					t.Fatalf("symlink: %v", err)
				}

				return sibling
			},
		},
		{
			// Accepted (DEC-B2-RESIDUAL). O_NOFOLLOW constrains the final
			// component only, and keeping a vault on another volume behind a
			// symlinked directory is an ordinary way to run one. A refusal
			// that walked the path instead of stat-ing the leaf would break
			// it, which is what this case exists to stop.
			name:     "reached through a symlinked vault directory, which is accepted",
			accepted: true,
			build: func(t *testing.T, root, note string) string {
				t.Helper()

				// The real directory is put elsewhere and the vault the
				// harness made becomes a link to it, so the path the sink
				// resolves is unchanged and every component but the leaf now
				// arrives through a link.
				vault := filepath.Dir(note)
				if err := os.Remove(vault); err != nil {
					t.Fatalf("clear the real vault out of the link's way: %v", err)
				}

				volume := filepath.Join(root, "another-volume")
				if err := os.Mkdir(volume, 0o700); err != nil {
					t.Fatalf("create the directory the vault links to: %v", err)
				}

				if err := os.Symlink(volume, vault); err != nil {
					t.Fatalf("symlink the vault directory: %v", err)
				}

				return filepath.Join(volume, filepath.Base(note))
			},
		},
		{
			// Accepted (DEC-B2-RESIDUAL). A hardlink redirects the append to
			// content outside the vault exactly as a symlink does, and it is a
			// regular file, so neither refusal can see it. Closing it means
			// refusing Nlink > 1, which every snapshot, backup and dedup tool
			// that hardlinks unchanged files would trip — so the note keeps
			// working, and the assertion is deliberately that the *other* name
			// sees the bytes.
			name:     "a hardlinked note, whose second name is accepted too",
			accepted: true,
			build: func(t *testing.T, root, note string) string {
				t.Helper()

				if err := os.WriteFile(note, []byte("# 既存\n"), 0o600); err != nil {
					t.Fatalf("seed the note: %v", err)
				}

				outside := filepath.Join(root, "outside-the-vault.md")
				if err := os.Link(note, outside); err != nil {
					t.Fatalf("hardlink the note out of the vault: %v", err)
				}

				return outside
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()

			vault := filepath.Join(root, "vault")
			if err := os.Mkdir(vault, 0o700); err != nil {
				t.Fatalf("create the vault: %v", err)
			}

			note := notePath(vault, noon)

			// Named for what it is in both halves of the table: the path this
			// case watches. A refusal must leave it exactly as it was; an
			// accepted case must find the entry in it.
			watched := test.build(t, root, note)

			before, _ := os.ReadFile(watched)

			const text = "リンクの先には書かない"

			err := obsidian.NewWithClock(settingsFor(vault), fixedClock(noon)).
				Send(context.Background(), mustMessage(t, text))

			if test.accepted {
				if err != nil {
					t.Fatalf("Send: %v, want the append to succeed", err)
				}

				// Read through the second name, not through the note path.
				// That the append reached the same object by another route is
				// the entire content of what DEC-B2-RESIDUAL accepts, and
				// reading the note path would assert nothing about it.
				want := string(before) + "- 11:42 " + text + "\n"
				if got := readNote(t, watched); got != want {
					t.Errorf("%s holds %q, want %q", watched, got, want)
				}

				return
			}

			if err == nil {
				t.Fatal("Send followed a symlinked note path")
			}

			if !errors.Is(err, obsidian.ErrNoteIsSymlink) {
				t.Errorf("Send: %v, want it to wrap ErrNoteIsSymlink", err)
			}

			if !strings.Contains(err.Error(), note) {
				t.Errorf("Send: %v, want it to name the note path %s", err, note)
			}

			// Nothing beyond the link was touched, and nothing was created.
			if watched == os.DevNull {
				return
			}

			after, readErr := os.ReadFile(watched)
			switch {
			case len(before) == 0 && readErr == nil:
				t.Errorf("%s was created; the append followed a dangling link", watched)
			case readErr == nil && string(after) != string(before):
				t.Errorf("%s changed from %q to %q", watched, before, after)
			}
		})
	}
}

// TestASeparatorIsAddedOnlyWhenTheNoteNeedsOne covers decision DEC-B1's
// boundaries directly, including the read failures that must not fail a post.
func TestASeparatorIsAddedOnlyWhenTheNoteNeedsOne(t *testing.T) {
	t.Parallel()

	const entry = "- 11:42 本文\n"

	tests := []struct {
		name string
		note *failingNote
		want string
	}{
		{
			name: "an empty note needs none",
			note: &failingNote{path: "/vault/note.md"},
			want: entry,
		},
		{
			name: "a note ending in a line feed needs none",
			note: &failingNote{path: "/vault/note.md", content: "# head\n- 09:15 earlier\n"},
			want: entry,
		},
		{
			name: "a note not ending in a line feed gets one",
			note: &failingNote{path: "/vault/note.md", content: "# head\n- 09:15 earlier"},
			want: "\n" + entry,
		},
		{
			// The torn-write case: a fragment left by ENOSPC or a killed
			// process. Without the separator every later entry chains onto it
			// for as long as the file stands.
			name: "a note ending mid-entry after a torn write gets one",
			note: &failingNote{path: "/vault/note.md", content: "- 11:00 a post that hit ENOSPC halfw"},
			want: "\n" + entry,
		},
		{
			// A read failure must not fail the post. A possibly-redundant
			// blank line would be worse than a glued one, and both are far
			// better than not appending at all.
			name: "a Stat failure appends without a separator",
			note: &failingNote{path: "/vault/note.md", content: "unterminated", statErr: errors.New("stat failed")},
			want: entry,
		},
		{
			name: "a read failure appends without a separator",
			note: &failingNote{path: "/vault/note.md", content: "unterminated", readErr: errors.New("read failed")},
			want: entry,
		},
		{
			// The note shrank between the Stat and the read, so ReadAt returns
			// (0, io.EOF) and the one-byte buffer is untouched. Answering from
			// it reads the buffer's zero value, which is not '\n', and puts a
			// blank line in the user's note on the strength of a byte nobody
			// read — the opposite of "every failure answers false".
			name: "a note that shrank between the size and the read appends without a separator",
			note: &failingNote{path: "/vault/note.md", content: "abc\n", statSize: 5},
			want: entry,
		},
		{
			// io.EOF reported *with* the byte is the case the EOF tolerance
			// exists for: the read succeeded, so the note's own last byte
			// decides, and this one is unterminated.
			name: "io.EOF alongside the last byte is not a failure and still separates",
			note: &failingNote{path: "/vault/note.md", content: "# head\n- 09:15 earlier", eofWithLastByte: true},
			want: "\n" + entry,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if err := obsidian.AppendEntry(context.Background(), test.note, entry); err != nil {
				t.Fatalf("AppendEntry: %v", err)
			}

			if test.note.wrote != test.want {
				t.Errorf("wrote %q, want %q", test.note.wrote, test.want)
			}
		})
	}
}

// TestEachOverlappingPostReportsItsOwnNote is issue #111's acceptance, and the
// reason decision DEC-D3 exists.
//
// The interface this replaced returned the note from a method on the sink, and a
// Sink is constructed once and serves every post — a GUI window outlives its
// submission (FR-028). So the field held whichever post had *started* last: the
// report ran at the top of Send, before the context check and before the open,
// so the stale window opened the instant a later post entered Send and lasted
// for the whole of that post's I/O. On a stalled mount that is unbounded.
//
// This reproduces it exactly rather than approximately. Post A resolves the note
// before local midnight and then blocks where the old defect's window opened —
// inside Send, after the target was known and before the note was opened. Post B
// runs to completion in the meantime with a clock that has crossed midnight, so
// it resolves a *different file*. Under the old shape A's caller then read B's
// note; here A's own collector can only ever hold A's value, because the
// collector never leaves A's context.
//
// Run under -race: the sink now has no shared mutable state at all, which is the
// other half of what removing the field bought.
func TestEachOverlappingPostReportsItsOwnNote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	beforeMidnight := time.Date(2026, 9, 8, 23, 59, 59, 0, time.Local)
	afterMidnight := beforeMidnight.Add(2 * time.Second)

	// One clock shared by both posts, handing out the two instants in the order
	// they are drawn. A guards the ordering below, so A always draws first.
	var (
		clockMu  sync.Mutex
		readings int
	)

	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()

		readings++

		if readings == 1 {
			return beforeMidnight
		}

		return afterMidnight
	}

	sink := obsidian.NewWithClock(settingsFor(dir), clock)

	notes := struct{ a, b string }{
		a: notePath(dir, beforeMidnight),
		b: notePath(dir, afterMidnight),
	}

	if notes.a == notes.b {
		t.Fatalf("both instants resolve to %s; the test cannot tell the two posts apart", notes.a)
	}

	var (
		aReported = make(chan struct{})
		bFinished = make(chan struct{})
		running   sync.WaitGroup
	)

	// Post A: reports, then parks in the window until B has finished.
	base, reportedByA := reporting(context.Background())

	firstCheck := true
	aCtx := observingContext{
		Context: base,
		observe: func() {
			if !firstCheck {
				return
			}

			firstCheck = false

			close(aReported)
			<-bFinished
		},
	}

	running.Add(2)

	go func() {
		defer running.Done()

		if err := sink.Send(aCtx, mustMessage(t, "post A (yesterday)")); err != nil {
			t.Errorf("post A: Send: %v", err)
		}
	}()

	go func() {
		defer running.Done()
		defer close(bFinished)

		// Not started until A has reported, so the clock readings are ordered
		// and B genuinely runs inside A's window rather than before it.
		<-aReported

		ctx, reportedByB := reporting(context.Background())

		if err := sink.Send(ctx, mustMessage(t, "post B (today)")); err != nil {
			t.Errorf("post B: Send: %v", err)

			return
		}

		if got := reportedByB.only(t); got != notes.b {
			t.Errorf("post B reported %q, want its own note %q", got, notes.b)
		}
	}()

	running.Wait()

	// The assertion the old shape failed: A's caller reads A's note, even
	// though B started later, resolved a different file, and finished first.
	if got := reportedByA.only(t); got != notes.a {
		t.Errorf("post A reported %q, want its own note %q (post B's was %q)",
			got, notes.a, notes.b)
	}

	// And each post's line is in its own file, so the reports match what
	// reached disk rather than merely differing from each other.
	if got := readNote(t, notes.a); !strings.Contains(got, "post A (yesterday)") {
		t.Errorf("%s holds %q, want post A's entry", notes.a, got)
	}

	if got := readNote(t, notes.b); !strings.Contains(got, "post B (today)") {
		t.Errorf("%s holds %q, want post B's entry", notes.b, got)
	}
}

// TestACreatedNoteIsPrivateToTheUser covers the file mode, which nothing
// asserted.
//
// The mode is a deliberate privacy decision the code documents, and nothing
// else in the repo constrains it — the spec, the contract and the design
// document are all silent — so a regression to 0644 would silently make the
// user's private notes world-readable on a shared machine.
func TestACreatedNoteIsPrivateToTheUser(t *testing.T) {
	t.Parallel()

	t.Run("a note this sink creates", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		if err := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon)).
			Send(context.Background(), mustMessage(t, "新規作成")); err != nil {
			t.Fatalf("Send: %v", err)
		}

		info, err := os.Stat(notePath(dir, noon))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}

		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("created note mode is %#o, want 0600", perm)
		}
	})

	t.Run("an existing note keeps the mode the user chose", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := notePath(dir, noon)

		// A vault the user set up themselves, group-readable on purpose.
		if err := os.WriteFile(path, []byte("# mine\n"), 0o640); err != nil {
			t.Fatalf("seed the note: %v", err)
		}

		if err := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon)).
			Send(context.Background(), mustMessage(t, "既存")); err != nil {
			t.Fatalf("Send: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}

		if perm := info.Mode().Perm(); perm != 0o640 {
			t.Errorf("existing note mode changed to %#o, want the user's 0640", perm)
		}
	})
}

// TestTheMessageBodyIsWrittenByteForByte is the byte-exactness assertion this
// package lacked (issue #104, "whichever option wins").
//
// Every other test here reads the note through a substring or a prefix check,
// which is the right shape for what those tests are about and cannot see a body
// that was normalised, re-encoded, or run through a formatter on its way to
// disk. FR-011 and FR-012 make non-transformation of the body load-bearing, and
// until now it was pinned only in internal/sink/telegram — so the two sinks
// could have diverged on exactly the property the two-sink design exists to
// guarantee, with nothing failing.
//
// The comparison is against a line this test assembles from the message's own
// bytes, so it fails on any change to them rather than on a re-derivation that
// would change with the code. FR-047's CR and LF handling is a separate rule
// with its own test, so no case here contains a line break; what is asserted is
// that everything else survives.
//
// The invalid-UTF-8 rows are deliberate under decision DEC-D4. Message.Validate
// now refuses those bytes, so no front door can deliver them — but Send takes a
// post.Message and a Message can be constructed without passing a front door, so
// the sink must not start assuming validation ran. If DEC-D4 is ever reversed
// (issue #104 records what would change the decision), these rows are already
// the guard.
func TestTheMessageBodyIsWrittenByteForByte(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "plain ASCII", body: "hello world"},
		{name: "japanese", body: "日本語のメモ"},
		{name: "emoji with a variation selector", body: "\U0001F363️ sushi"},
		{name: "a combining mark", body: "éclair"},
		{name: "an encoded replacement character", body: "already � mangled"},
		{name: "markdown that must not be escaped", body: "*bold* _under_ `code` [x](y)"},
		{name: "a literal br tag the user typed", body: "not a newline: <br>"},
		{name: "an existing bullet prefix", body: "- 09:15 looks like an entry"},
		{name: "leading and trailing spaces", body: "  padded  "},
		{name: "an ideographic space", body: "a　b"},
		{name: "a tab", body: "a\tb"},
		{name: "a NUL", body: "a\x00b"},
		{name: "a lone 0xFF", body: "a\xffb"},
		{name: "shift_jis bytes", body: "\x93\xfa\x96\x7b\x8c\xea"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Not mustMessage: half these bodies are exactly what
			// Message.Validate refuses, and the point of the row is that the
			// sink does not depend on that.
			sink := obsidian.NewWithClock(settingsFor(dir), fixedClock(noon))

			if err := sink.Send(t.Context(), post.Message{Original: tt.body}); err != nil {
				t.Fatalf("Send: %v", err)
			}

			want := "- " + noon.Format("15:04") + " " + tt.body + "\n"

			if got := readNote(t, notePath(dir, noon)); got != want {
				t.Errorf("the note holds\n  %q\nwant\n  %q", got, want)
			}
		})
	}
}
