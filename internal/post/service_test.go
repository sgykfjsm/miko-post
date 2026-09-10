package post

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// generousTimeout is long enough that no test fails because a machine was
// briefly busy. Every test that wants an expiry sets its own short one.
const generousTimeout = 30 * time.Second

// fakeSink is a Sink whose behaviour is supplied per test.
//
// It records what it was given rather than only that it was called, because
// "both sinks ran" is not the whole claim T026 makes: FR-012 also requires
// that both received the identical original message, and a sink that ran with
// the wrong text is a different defect from one that did not run.
type fakeSink struct {
	name string
	send func(ctx context.Context, message Message) error

	mu       sync.Mutex
	calls    int
	received []Message
}

func (f *fakeSink) Name() string { return f.name }

func (f *fakeSink) Send(ctx context.Context, message Message) error {
	f.mu.Lock()
	f.calls++
	f.received = append(f.received, message)
	f.mu.Unlock()

	if f.send == nil {
		return nil
	}

	return f.send(ctx, message)
}

// ran reports how many times this sink was invoked.
func (f *fakeSink) ran() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// sawMessage reports the message this sink was handed on its first call.
func (f *fakeSink) sawMessage(t *testing.T) Message {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.received) == 0 {
		t.Fatalf("sink %s was never called", f.name)
	}

	return f.received[0]
}

// succeeds returns a sink that reports success.
func succeeds(name string) *fakeSink {
	return &fakeSink{name: name}
}

// fails returns a sink that reports err.
func fails(name string, err error) *fakeSink {
	return &fakeSink{name: name, send: func(context.Context, Message) error { return err }}
}

// resultFor finds one sink's result by name.
func resultFor(t *testing.T, outcome Outcome, name string) SinkResult {
	t.Helper()

	for _, result := range outcome.Results {
		if result.Name == name {
			return result
		}
	}

	t.Fatalf("no result for sink %q; got %d results", name, len(outcome.Results))

	return SinkResult{}
}

// mustMessage builds a Message that has passed validation, as Service.Post
// requires.
func mustMessage(t *testing.T, text string) Message {
	t.Helper()

	message := Message{Original: text}
	if err := message.Validate(); err != nil {
		t.Fatalf("Message{%q}.Validate: %v", text, err)
	}

	return message
}

// TestBothSinksRunAndBothResultsAreReported is T026's central requirement
// (FR-013, FR-014).
//
// The constitution asks for more than "the happy path returns success": every
// enabled sink must have run and every result must be present, whichever of
// them failed. A `return` on first error passes an all-succeed test and fails
// every case below.
func TestBothSinksRunAndBothResultsAreReported(t *testing.T) {
	t.Parallel()

	telegramFailure := errors.New("chat not found")
	obsidianFailure := errors.New("permission denied")

	tests := []struct {
		name         string
		telegram     *fakeSink
		obsidian     *fakeSink
		wantTelegram bool
		wantObsidian bool
		wantOverall  bool
	}{
		{
			name:         "both succeed",
			telegram:     succeeds("telegram"),
			obsidian:     succeeds("obsidian"),
			wantTelegram: true,
			wantObsidian: true,
			wantOverall:  true,
		},
		{
			name:         "the first sink fails",
			telegram:     fails("telegram", telegramFailure),
			obsidian:     succeeds("obsidian"),
			wantTelegram: false,
			wantObsidian: true,
			wantOverall:  false,
		},
		{
			// The mirror case matters separately: an implementation that
			// returns on first error passes when the *last* sink is the one
			// that fails.
			name:         "the second sink fails",
			telegram:     succeeds("telegram"),
			obsidian:     fails("obsidian", obsidianFailure),
			wantTelegram: true,
			wantObsidian: false,
			wantOverall:  false,
		},
		{
			name:         "both fail",
			telegram:     fails("telegram", telegramFailure),
			obsidian:     fails("obsidian", obsidianFailure),
			wantTelegram: false,
			wantObsidian: false,
			wantOverall:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := New([]Sink{test.telegram, test.obsidian}, generousTimeout, nil)
			outcome := service.Post(mustMessage(t, "今日も美琴が可愛い♡"))

			// Both sinks ran. Asserted on the sinks themselves, not inferred
			// from the results: a result could in principle be fabricated for
			// a sink that was never invoked, and FR-016's converse — that
			// every sink handed over *is* invoked — is what this checks.
			if got := test.telegram.ran(); got != 1 {
				t.Errorf("telegram ran %d times, want 1", got)
			}

			if got := test.obsidian.ran(); got != 1 {
				t.Errorf("obsidian ran %d times, want 1", got)
			}

			// Both results were reported.
			if len(outcome.Results) != 2 {
				t.Fatalf("got %d results, want 2: %v", len(outcome.Results), outcome.Results)
			}

			if got := resultFor(t, outcome, "telegram").Success; got != test.wantTelegram {
				t.Errorf("telegram Success = %t, want %t", got, test.wantTelegram)
			}

			if got := resultFor(t, outcome, "obsidian").Success; got != test.wantObsidian {
				t.Errorf("obsidian Success = %t, want %t", got, test.wantObsidian)
			}

			if got := outcome.Succeeded(); got != test.wantOverall {
				t.Errorf("Outcome.Succeeded = %t, want %t", got, test.wantOverall)
			}

			// A failure keeps its diagnostic error and gains a display reason;
			// a success has neither (FR-017).
			for _, result := range outcome.Results {
				switch {
				case result.Success && result.Err != nil:
					t.Errorf("%s succeeded but carries an error", result.Name)
				case result.Success && result.Reason != "":
					t.Errorf("%s succeeded but carries reason %q", result.Name, result.Reason)
				case !result.Success && result.Err == nil:
					t.Errorf("%s failed with no diagnostic error", result.Name)
				case !result.Success && result.Reason == "":
					t.Errorf("%s failed with no display reason", result.Name)
				}
			}
		})
	}
}

// TestOneSinkFailingDoesNotCancelItsSibling is the independence guarantee, and
// the reason deliver derives its context from context.Background() and nothing
// else (constitution principle I, FR-013).
//
// The sibling here does not merely run — it *observes its own context* after
// the other sink has already failed. An implementation that derived both
// contexts from one shared cancellable parent would cancel this sink the moment
// its sibling's deliver returned, so the sibling would report ctx.Canceled and
// this test would show it as a failure rather than a success.
func TestOneSinkFailingDoesNotCancelItsSibling(t *testing.T) {
	t.Parallel()

	firstFailed := make(chan struct{})

	failing := &fakeSink{
		name: "telegram",
		send: func(context.Context, Message) error {
			close(firstFailed)

			return errors.New("chat not found")
		},
	}

	watching := &fakeSink{
		name: "obsidian",
		send: func(ctx context.Context, _ Message) error {
			// Bounded for the same reason as the ordering test: an open
			// receive turns a regression into a hung binary.
			select {
			case <-firstFailed:
			case <-ctx.Done():
				return fmt.Errorf("the peer never failed: %w", ctx.Err())
			}

			// The sibling has failed and its deliver has run its deferred
			// cancel. If that cancel reached this context, it is already
			// closed or closes imminently; the wait is bounded so a
			// regression fails rather than hanging.
			select {
			case <-ctx.Done():
				return fmt.Errorf("this sink was cancelled by its sibling's failure: %w", ctx.Err())
			case <-time.After(200 * time.Millisecond):
				return nil
			}
		},
	}

	service := New([]Sink{failing, watching}, generousTimeout, nil)
	outcome := service.Post(mustMessage(t, "independent"))

	sibling := resultFor(t, outcome, "obsidian")
	if !sibling.Success {
		t.Fatalf("the sibling failed after its peer failed: reason=%q err=%v", sibling.Reason, sibling.Err)
	}

	if got := resultFor(t, outcome, "telegram").Success; got {
		t.Error("the failing sink was reported as a success")
	}
}

// barrier releases every arrival only once all of them have arrived.
type barrier struct {
	mu       sync.Mutex
	arrived  int
	expected int
	released chan struct{}
}

func newBarrier(expected int) *barrier {
	return &barrier{expected: expected, released: make(chan struct{})}
}

// wait blocks until every expected participant has arrived, or until ctx ends.
func (b *barrier) wait(ctx context.Context) error {
	b.mu.Lock()

	b.arrived++
	if b.arrived == b.expected {
		close(b.released)
	}

	b.mu.Unlock()

	select {
	case <-b.released:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestSinksActuallyOverlap proves concurrency rather than assuming it
// (FR-013).
//
// Every other test in this file passes against a Service that runs its sinks
// one after another — the results are the same, only slower. This one cannot:
// each sink blocks inside Send until *both* have arrived, so a sequential
// implementation leaves the first sink waiting for a sibling that has not been
// started, its own deadline expires, and both results come back as timeouts.
//
// The short timeout is what turns that regression into a failed assertion
// instead of a hung test.
func TestSinksActuallyOverlap(t *testing.T) {
	t.Parallel()

	both := newBarrier(2)

	rendezvous := func(name string) *fakeSink {
		return &fakeSink{
			name: name,
			send: func(ctx context.Context, _ Message) error {
				return both.wait(ctx)
			},
		}
	}

	telegram, obsidian := rendezvous("telegram"), rendezvous("obsidian")

	service := New([]Sink{telegram, obsidian}, 5*time.Second, nil)
	outcome := service.Post(mustMessage(t, "concurrent"))

	for _, result := range outcome.Results {
		if !result.Success {
			t.Errorf("%s did not succeed, so the sinks did not overlap: reason=%q err=%v",
				result.Name, result.Reason, result.Err)
		}
	}
}

// TestABlockingSinkYieldsATimeoutAndDoesNotStallItsSibling covers FR-015 and
// the "when one blocks" case T026 names.
func TestABlockingSinkYieldsATimeoutAndDoesNotStallItsSibling(t *testing.T) {
	t.Parallel()

	const timeout = 150 * time.Millisecond

	// Blocks until its own context expires, which is what a hung network call
	// looks like from here.
	blocking := &fakeSink{
		name: "telegram",
		send: func(ctx context.Context, _ Message) error {
			<-ctx.Done()

			return ctx.Err()
		},
	}

	quick := succeeds("obsidian")

	service := New([]Sink{blocking, quick}, timeout, nil)

	started := time.Now()
	outcome := service.Post(mustMessage(t, "one sink hangs"))
	elapsed := time.Since(started)

	// The blocked sink fails, and says why in words a front door may print.
	blocked := resultFor(t, outcome, "telegram")
	if blocked.Success {
		t.Error("the blocked sink was reported as a success")
	}

	if blocked.Reason != reasonTimedOut {
		t.Errorf("blocked sink reason = %q, want %q", blocked.Reason, reasonTimedOut)
	}

	if !errors.Is(blocked.Err, context.DeadlineExceeded) {
		t.Errorf("blocked sink Err = %v, want it to wrap context.DeadlineExceeded", blocked.Err)
	}

	// Its sibling is unaffected and still reported.
	if sibling := resultFor(t, outcome, "obsidian"); !sibling.Success {
		t.Errorf("the sibling of a blocked sink failed: reason=%q err=%v", sibling.Reason, sibling.Err)
	}

	if quick.ran() != 1 {
		t.Errorf("the sibling ran %d times, want 1", quick.ran())
	}

	// The post waits for the blocked sink but not for it twice: a sequential
	// implementation would spend one timeout on each sink. Generous, because
	// the assertion is about the shape and not the schedule.
	if elapsed >= 2*timeout {
		t.Errorf("the post took %s, more than two timeouts; the sinks were not concurrent", elapsed)
	}
}

// TestAPanickingSinkBecomesAFailureAndItsSiblingStillReports covers the route
// that principle I cannot be defended from by context discipline alone.
//
// An unrecovered panic in a sink's goroutine ends the process: the sibling's
// result is lost, nothing is written, and the exit status describes a post that
// never finished. FR-071 expects panics to be recorded, so they are anticipated
// rather than impossible.
func TestAPanickingSinkBecomesAFailureAndItsSiblingStillReports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "a string", value: "sink exploded", want: "sink exploded"},
		{name: "an error", value: errors.New("nil map write"), want: "nil map write"},
		// A struct is the case that must not be rendered field by field: a
		// panicking sink's value can hold a credential, and %v walks exported
		// fields. Only the type is reported.
		{
			// The remaining shape recover() can hand back: a value whose only
			// rendering is fmt.Stringer.
			name:  "a Stringer",
			value: panicStringer{},
			want:  "a stringer's own words",
		},
		{
			name:  "a struct carrying a secret",
			value: struct{ Token string }{Token: "SENTINEL-TOKEN"},
			want:  "a value of type",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			panicking := &fakeSink{
				name: "telegram",
				send: func(context.Context, Message) error {
					panic(test.value)
				},
			}

			sibling := succeeds("obsidian")

			service := New([]Sink{panicking, sibling}, generousTimeout, nil)
			outcome := service.Post(mustMessage(t, "one sink panics"))

			if len(outcome.Results) != 2 {
				t.Fatalf("got %d results, want 2", len(outcome.Results))
			}

			failed := resultFor(t, outcome, "telegram")
			if failed.Success {
				t.Error("the panicking sink was reported as a success")
			}

			if failed.Err == nil {
				t.Fatal("the panicking sink carries no diagnostic error")
			}

			if !strings.Contains(failed.Err.Error(), test.want) {
				t.Errorf("Err = %q, want it to mention %q", failed.Err.Error(), test.want)
			}

			// The display half stays a constant from this package. A panic
			// value is entirely outside our control, so routing it into Reason
			// would put arbitrary text — a token, a struct dump — on the
			// user's terminal (FR-017, FR-029).
			if failed.Reason != reasonFailed {
				t.Errorf("Reason = %q, want the fixed-set constant %q", failed.Reason, reasonFailed)
			}

			if strings.Contains(failed.Reason, "SENTINEL-TOKEN") {
				t.Errorf("the panic value reached the display reason: %q", failed.Reason)
			}

			// And, more to the point, absent from the field that can actually
			// carry it.
			//
			// Asserting only on Reason proves nothing: Reason is always one of
			// two package constants, so no implementation could put a token
			// there. Err is where describePanic's output lands, and Err is what
			// T040 and T073 write verbatim to the diagnostic log. Appending
			// "%v" to describePanic's default arm is a two-line edit that
			// leaks the struct's fields into exactly this string.
			if strings.Contains(failed.Err.Error(), "SENTINEL-TOKEN") {
				t.Errorf("the panic value's contents reached the diagnostic error: %q",
					failed.Err.Error())
			}

			// And the sibling still ran and still reported.
			if got := resultFor(t, outcome, "obsidian"); !got.Success {
				t.Errorf("the sibling of a panicking sink failed: %v", got.Err)
			}

			if sibling.ran() != 1 {
				t.Errorf("the sibling ran %d times, want 1", sibling.ran())
			}
		})
	}
}

// panicStringer is a panic value that renders only through fmt.Stringer.
type panicStringer struct{}

func (panicStringer) String() string { return "a stringer's own words" }

// TestEverySinkReceivesTheIdenticalOriginalMessage covers FR-012.
//
// The untrimmed text is the point: T006 keeps the original on purpose, and a
// sink that received a trimmed or re-wrapped copy would silently change what
// the user posted. The transformation an obsidian sink applies is its own
// business (T031) and must not travel back into the message.
func TestEverySinkReceivesTheIdenticalOriginalMessage(t *testing.T) {
	t.Parallel()

	const text = "  今日も美琴が可愛い♡\n二行目  "

	telegram, obsidian := succeeds("telegram"), succeeds("obsidian")

	service := New([]Sink{telegram, obsidian}, generousTimeout, nil)
	outcome := service.Post(mustMessage(t, text))

	for _, sink := range []*fakeSink{telegram, obsidian} {
		if got := sink.sawMessage(t); got.Original != text {
			t.Errorf("%s received %q, want the original %q", sink.name, got.Original, text)
		}
	}

	if outcome.Message.Original != text {
		t.Errorf("Outcome.Message = %q, want the original %q", outcome.Message.Original, text)
	}
}

// TestResultsFollowTheSinkOrderNotTheCompletionOrder pins the ordering the
// front doors render.
//
// The sinks finish in the opposite order to the one they were given in, so an
// implementation that appended from each goroutine returns them reversed. That
// would also make the order vary with the network, which is worse than either
// fixed order.
func TestResultsFollowTheSinkOrderNotTheCompletionOrder(t *testing.T) {
	t.Parallel()

	firstMayFinish := make(chan struct{})

	// Bounded on the sink's own context, not an open receive. Unguarded, a
	// sequential regression left this sink waiting for a sibling that had not
	// been started and the whole binary died on the test timeout, burying which
	// property actually broke under a goroutine dump.
	slow := &fakeSink{
		name: "telegram",
		send: func(ctx context.Context, _ Message) error {
			select {
			case <-firstMayFinish:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}

	fast := &fakeSink{
		name: "obsidian",
		send: func(context.Context, Message) error {
			close(firstMayFinish)

			return nil
		},
	}

	service := New([]Sink{slow, fast}, generousTimeout, nil)
	outcome := service.Post(mustMessage(t, "ordered"))

	if len(outcome.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(outcome.Results))
	}

	if outcome.Results[0].Name != "telegram" || outcome.Results[1].Name != "obsidian" {
		t.Errorf("results are ordered %q, %q; want the order the sinks were given",
			outcome.Results[0].Name, outcome.Results[1].Name)
	}
}

// TestEachResultRecordsHowLongItsSinkTook covers the Duration field that
// FR-066's duration_ms is built from.
func TestEachResultRecordsHowLongItsSinkTook(t *testing.T) {
	t.Parallel()

	const spent = 40 * time.Millisecond

	slow := &fakeSink{
		name: "telegram",
		send: func(context.Context, Message) error {
			time.Sleep(spent)

			return nil
		},
	}

	service := New([]Sink{slow}, generousTimeout, nil)
	outcome := service.Post(mustMessage(t, "timed"))

	if got := resultFor(t, outcome, "telegram").Duration; got < spent {
		t.Errorf("Duration = %s, want at least %s", got, spent)
	}
}

// TestPostWithNoSinksReportsNothingDelivered covers the fail-closed path.
//
// FR-018 makes an all-disabled configuration a startup error at the front door,
// so this is unreachable in a wired application. It is asserted anyway because
// the alternative — a vacuous success — would exit 0 for a post that reached no
// destination, which is the single outcome the exit status exists to prevent.
func TestPostWithNoSinksReportsNothingDelivered(t *testing.T) {
	t.Parallel()

	outcome := New(nil, generousTimeout, nil).Post(mustMessage(t, "nowhere to go"))

	if len(outcome.Results) != 0 {
		t.Errorf("got %d results, want none", len(outcome.Results))
	}

	if outcome.Succeeded() {
		t.Error("a post with no sinks reported success")
	}
}

// TestEveryPostGetsItsOwnIdentifier covers the per-post correlation identifier
// (FR-066, R-007).
func TestEveryPostGetsItsOwnIdentifier(t *testing.T) {
	t.Parallel()

	service := New([]Sink{succeeds("telegram")}, generousTimeout, nil)

	before := time.Now()
	first := service.Post(mustMessage(t, "one"))
	second := service.Post(mustMessage(t, "two"))
	after := time.Now()

	if first.ID == second.ID {
		t.Errorf("two posts share the identifier %s; records cannot be correlated to one post", first.ID)
	}

	if first.ID == (ulid.ULID{}) {
		t.Error("the identifier is the zero ULID, which is the generation-failure fallback")
	}

	// Timestamps are non-decreasing, which is what a reader can actually rely
	// on to order two posts.
	//
	// Strict total ordering is deliberately *not* asserted, and the reason is
	// worth recording: an earlier version of this test required
	// second.Compare(first) > 0 and flaked. ulid's monotonic reader is
	// process-wide and re-randomises its entropy whenever the millisecond it
	// is handed differs from the one it last saw. A draw from another test at
	// T+1 that takes the shared lock first therefore resets that stored
	// millisecond, and a draw of ours at T then re-randomises instead of
	// incrementing — same timestamp as the first identifier, fresh random
	// entropy, and a coin flip on the comparison. Nothing in FR-066 or R-007
	// asks for total ordering; correlation needs distinctness, which is
	// asserted above.
	if second.ID.Time() < first.ID.Time() {
		t.Errorf("the second identifier's timestamp %d precedes the first's %d",
			second.ID.Time(), first.ID.Time())
	}

	// And the timestamp is the submission's, not a constant.
	//
	// The comparison above holds for a zero timestamp, a frozen one, and any
	// other constant, so on its own it pins nothing — R-007 chose ULID
	// precisely so that sorting grepped log lines reconstructs a post's order,
	// and a frozen timestamp defeats that entirely. Bracketing against the
	// wall clock either side of the two posts is what makes it falsifiable.
	for _, entry := range []struct {
		label string
		id    ulid.ULID
	}{{"first", first.ID}, {"second", second.ID}} {
		stamp := ulid.Time(entry.id.Time())
		if stamp.Before(before.Add(-time.Second)) || stamp.After(after.Add(time.Second)) {
			t.Errorf("the %s identifier's timestamp %s is outside the submission window [%s, %s]",
				entry.label, stamp, before, after)
		}
	}

	// It must survive as the 26-character string a log record carries.
	if parsed, err := ulid.ParseStrict(first.ID.String()); err != nil {
		t.Errorf("the identifier does not round-trip through its string form: %v", err)
	} else if parsed != first.ID {
		t.Errorf("round-tripped to %s, want %s", parsed, first.ID)
	}
}

// TestAFailureToGenerateAnIdentifierStillPosts covers the fallback branch, and
// is the reason the generator is a field.
//
// ulid.Make panics on monotonic overflow, which is why it is not used. Having
// declined the panic, the remaining question is what a post does when it cannot
// be named — and the answer has to be "it still posts", or a diagnostics
// concern would have decided whether the user's message was delivered.
func TestAFailureToGenerateAnIdentifierStillPosts(t *testing.T) {
	t.Parallel()

	sink := succeeds("telegram")

	service := New([]Sink{sink}, generousTimeout, nil)
	// A *populated* identifier alongside the error, which is what the real
	// generator does: ulid.New applies SetTime before it reads entropy, so an
	// entropy failure returns a half-built, non-zero ULID together with the
	// error. Discarding that partial value is the behaviour under test, and a
	// fake returning the zero ULID could not test it — the assertion below
	// would hold whether identifier honoured the error or ignored it.
	partial := ulid.Make()

	service.newID = func() (ulid.ULID, error) {
		return partial, ulid.ErrMonotonicOverflow
	}

	outcome := service.Post(mustMessage(t, "unnamed but delivered"))

	if sink.ran() != 1 {
		t.Errorf("the sink ran %d times, want 1; the post was abandoned", sink.ran())
	}

	if !outcome.Succeeded() {
		t.Error("the post failed because its identifier could not be generated")
	}

	if outcome.ID != (ulid.ULID{}) {
		t.Errorf("ID = %s, want the zero ULID fallback", outcome.ID)
	}

	// The fallback is still a parseable ULID string, so a log record carrying
	// it is greppable rather than malformed.
	if got := outcome.ID.String(); len(got) != 26 {
		t.Errorf("the fallback renders as %q (%d chars), want a 26-character ULID", got, len(got))
	}
}

// TestConcurrentPostsOnOneServiceDoNotInterfere covers the shape a GUI window
// produces: FR-028 cancels the auto-close on interaction, so a window can
// still be open — and its Service still live — when the next post begins.
//
// Run under -race, this is what would catch shared mutable state on Service.
func TestConcurrentPostsOnOneServiceDoNotInterfere(t *testing.T) {
	t.Parallel()

	const posts = 16

	// Stateless sinks, shared across every concurrent post, so any
	// interference has to come from the Service.
	service := New([]Sink{
		&fakeSink{name: "telegram"},
		&fakeSink{name: "obsidian"},
	}, generousTimeout, nil)

	var (
		running sync.WaitGroup
		mu      sync.Mutex
		ids     = make(map[ulid.ULID]int, posts)
	)

	// Built on the test goroutine: mustMessage can call t.Fatalf, and FailNow
	// must not be reached from a goroutine other than the one running the
	// test — it would fire runtime.Goexit on the worker, leave running.Wait
	// blocked, and report the failure as a timeout rather than as itself.
	messages := make([]Message, posts)
	for i := range messages {
		messages[i] = mustMessage(t, fmt.Sprintf("post %02d", i))
	}

	for i := range posts {
		running.Add(1)

		go func(i int) {
			defer running.Done()

			outcome := service.Post(messages[i])

			if len(outcome.Results) != 2 || !outcome.Succeeded() {
				t.Errorf("post %02d: %d results, succeeded=%t", i, len(outcome.Results), outcome.Succeeded())

				return
			}

			mu.Lock()
			ids[outcome.ID]++
			mu.Unlock()
		}(i)
	}

	running.Wait()

	if len(ids) != posts {
		t.Errorf("%d concurrent posts produced %d distinct identifiers", posts, len(ids))
	}
}

// TestReasonForNeverEchoesTheError guards the split FR-017 and FR-029 exist
// for, at the one function that chooses the display half.
//
// A classifier that fell back to err.Error() would pass every other test in
// this file while printing a bot token on the user's terminal the first time a
// Telegram request failed at the transport layer.
func TestReasonForNeverEchoesTheError(t *testing.T) {
	t.Parallel()

	const secret = "1234567890:SENTINEL-BOT-TOKEN"

	fixedSet := map[string]bool{reasonTimedOut: true, reasonFailed: true}

	// The timeout phrase is contract text, not an internal label:
	// contracts/cli-interface.md shows "Telegram: failed — request timed out"
	// and contracts/log-events.md shows "error":"request timed out". Every
	// other assertion in this file compares against the constants, so swapping
	// the two constants' values would invert what users see with a green suite.
	if reasonTimedOut != "request timed out" {
		t.Errorf("reasonTimedOut = %q, but the contracts specify %q",
			reasonTimedOut, "request timed out")
	}

	errs := []error{
		errors.New("Post \"https://api.telegram.org/bot" + secret + "/sendMessage\": i/o timeout"),
		fmt.Errorf("wrapped: %w", errors.New(secret)),
		context.DeadlineExceeded,
		fmt.Errorf("deadline: %w", context.DeadlineExceeded),
		context.Canceled,
		errors.New(""),
	}

	for _, err := range errs {
		reason := reasonFor(err)

		if !fixedSet[reason] {
			t.Errorf("reasonFor(%v) = %q, which is outside the fixed set", err, reason)
		}

		if strings.Contains(reason, secret) {
			t.Errorf("reasonFor leaked the credential: %q", reason)
		}
	}

	// And the one classification this batch does make.
	if got := reasonFor(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)); got != reasonTimedOut {
		t.Errorf("a wrapped deadline classified as %q, want %q", got, reasonTimedOut)
	}
}

// TestEachSinkGetsItsOwnDeadline covers two narrow claims about FR-015's
// per-sink budget, and it is worth being exact about which.
//
// It does verify that every sink is handed a context that *has* a deadline, and
// that the budget is granted per sink rather than consumed across them: both
// sinks here spend three quarters of the timeout, so an implementation that ran
// them in sequence against one shared deadline would leave the second with a
// quarter and expire it.
//
// It does not distinguish a shared deadline from per-sink deadlines when the
// sinks genuinely overlap, and it cannot: two contexts created within
// microseconds of each other expire at effectively the same instant, so the
// two implementations are behaviourally identical here. The independence that
// actually matters — that no sink is ever cancelled because another failed — is
// carried by TestOneSinkFailingDoesNotCancelItsSibling, and the overlap itself
// by TestSinksActuallyOverlap. This test is the sequential-shared-budget
// detector, not the independence proof.
func TestEachSinkGetsItsOwnDeadline(t *testing.T) {
	t.Parallel()

	const timeout = 300 * time.Millisecond

	deadlines := make(chan time.Time, 2)

	record := func(name string, spend time.Duration) *fakeSink {
		return &fakeSink{
			name: name,
			send: func(ctx context.Context, _ Message) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("this sink was given no deadline")
				}

				deadlines <- deadline

				time.Sleep(spend)

				return ctx.Err()
			},
		}
	}

	service := New([]Sink{
		record("telegram", timeout*3/4),
		record("obsidian", timeout*3/4),
	}, timeout, nil)

	outcome := service.Post(mustMessage(t, "budgets"))

	// Both spent three quarters of the timeout, so a budget consumed across
	// sinks rather than granted to each would have expired the second one.
	for _, result := range outcome.Results {
		if !result.Success {
			t.Errorf("%s failed, so the timeout was shared rather than per-sink: reason=%q err=%v",
				result.Name, result.Reason, result.Err)
		}
	}

	close(deadlines)

	var seen []time.Time
	for deadline := range deadlines {
		seen = append(seen, deadline)
	}

	if len(seen) != 2 {
		t.Fatalf("recorded %d deadlines, want 2", len(seen))
	}

	// Each was computed when its own sink started, so they are close but need
	// not be equal. The gap check only rules out one sink being handed a budget
	// that had already been spent elsewhere; it deliberately does not claim to
	// tell a shared deadline from two independent ones.
	if gap := seen[0].Sub(seen[1]); gap > timeout/2 || gap < -timeout/2 {
		t.Errorf("the two deadlines are %s apart, which is more than half the budget", gap)
	}
}

// nameOnlySink panics from Name() and never gets as far as Send.
type nameOnlySink struct {
	value   any
	sendErr error
}

func (s *nameOnlySink) Name() string { panic(s.value) }

func (s *nameOnlySink) Send(context.Context, Message) error { return s.sendErr }

// TestAPanicFromNameIsContainedLikeAnyOther covers the half of the interface
// the guard used to miss (constitution principle I, FR-014).
//
// `Sink` has two methods and both are sink code. deliver's recover was
// installed after `sink.Name()` had already been called, so a panicking Name
// escaped it, escaped the delivering goroutine, and killed the process —
// taking the sibling's finished result with it. That is the exact coupling the
// recover exists to prevent, reached through the other method.
//
// A nil element in the sinks slice is the same defect wearing different
// clothes, and it is the shape T036 can produce: `make([]post.Sink, 2)` plus
// index assignment, or appending the result of a constructor that failed.
func TestAPanicFromNameIsContainedLikeAnyOther(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// wantDelivered is what Send does, which is what the result must
		// report regardless of how badly Name behaved.
		wantDelivered bool
		sinks         func() []Sink
	}{
		{
			name:          "Name panics but Send delivers",
			wantDelivered: true,
			sinks: func() []Sink {
				return []Sink{&nameOnlySink{value: "name exploded"}, succeeds("obsidian")}
			},
		},
		{
			name:          "Name panics and Send fails",
			wantDelivered: false,
			sinks: func() []Sink {
				return []Sink{
					&nameOnlySink{value: "name exploded", sendErr: errors.New("chat not found")},
					succeeds("obsidian"),
				}
			},
		},
		{
			// A nil interface element: Name and Send both fault on it. This is
			// the shape T036 can produce with make([]post.Sink, 2) plus index
			// assignment, or by appending a failed constructor's result.
			name:          "a nil element in the slice",
			wantDelivered: false,
			sinks: func() []Sink {
				return []Sink{nil, succeeds("obsidian")}
			},
		},
		{
			// A typed nil whose methods dereference the receiver.
			name:          "a typed nil sink",
			wantDelivered: false,
			sinks: func() []Sink {
				return []Sink{(*nilReceiverSink)(nil), succeeds("obsidian")}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			sinks := test.sinks()

			outcome := New(sinks, generousTimeout, nil).Post(mustMessage(t, "one sink is broken"))

			// Reaching this line at all is most of the assertion: before the
			// fix the process died here.
			if len(outcome.Results) != 2 {
				t.Fatalf("got %d results, want 2", len(outcome.Results))
			}

			broken := outcome.Results[0]

			// Whatever else is true, the result is attributable. A sink whose
			// own name could not be read is reported under the sentinel, never
			// as an empty string that would give the front door a blank line
			// and the log a record naming no destination.
			if broken.Name != unknownSinkName {
				t.Errorf("Name = %q, want the sentinel %q", broken.Name, unknownSinkName)
			}

			// Independent of the constant's value. Every other assertion here
			// compares against unknownSinkName itself, so they all hold even
			// if it were "" — the one value the property forbids.
			if broken.Name == "" {
				t.Error("the result names no sink at all; the front door would render a blank line")
			}

			// Success follows what Send actually did, not what Name did.
			//
			// That is deliberate and it is the interesting half of this test.
			// A sink whose Name panics but whose Send delivers has delivered:
			// reporting failure would invite the user to send again, and
			// FR-019 rules out any automatic resend precisely because a
			// duplicate chat message is worse than a confusing label. So the
			// unnameable sink keeps its real outcome.
			if broken.Success != test.wantDelivered {
				t.Errorf("Success = %t, want %t (it should follow Send, not Name)",
					broken.Success, test.wantDelivered)
			}

			if !broken.Success {
				if broken.Reason != reasonFailed {
					t.Errorf("Reason = %q, want the fixed-set constant %q", broken.Reason, reasonFailed)
				}

				if broken.Err == nil {
					t.Error("the failing result carries no diagnostic error")
				}
			}

			// And the sibling is untouched, which is the whole point.
			if sibling := outcome.Results[1]; !sibling.Success {
				t.Errorf("the sibling of a broken sink failed: reason=%q err=%v", sibling.Reason, sibling.Err)
			}
		})
	}
}

// nilReceiverSink has methods that dereference a nil receiver.
type nilReceiverSink struct {
	label string
}

func (s *nilReceiverSink) Name() string { return s.label }

func (s *nilReceiverSink) Send(context.Context, Message) error { return errors.New(s.label) }

// uncooperativeSink ignores its context entirely, the way a blocking syscall
// does.
type uncooperativeSink struct {
	name     string
	blockFor time.Duration
	release  chan struct{}
}

func (s *uncooperativeSink) Name() string { return s.name }

func (s *uncooperativeSink) Send(context.Context, Message) error {
	if s.release != nil {
		<-s.release

		return nil
	}

	time.Sleep(s.blockFor)

	return nil
}

// TestASinkThatIgnoresItsContextIsAbandoned covers the orchestrator-side bound
// (FR-015, constitution principle I).
//
// The per-sink context is only an offer: `Send` has to consult it. The obsidian
// sink (T031) cannot — os.OpenFile and os.File.Write take no context — so a
// vault on a synced or network-backed path blocks for as long as the mount
// does. Without a backstop such a sink held the post open with no upper bound
// and its sibling's finished result stayed unreachable the whole time.
//
// Both shapes are covered: one that eventually returns long after its deadline,
// and one that never returns at all. The second is why the assertion is a
// deadline on a channel rather than a plain call — a regression here hangs.
func TestASinkThatIgnoresItsContextIsAbandoned(t *testing.T) {
	t.Parallel()

	const timeout = 100 * time.Millisecond

	tests := []struct {
		name  string
		sink  *uncooperativeSink
		clean func(*uncooperativeSink)
	}{
		{
			name: "returns long after its deadline",
			sink: &uncooperativeSink{name: "obsidian", blockFor: 5 * time.Second},
		},
		{
			name:  "never returns",
			sink:  &uncooperativeSink{name: "obsidian", release: make(chan struct{})},
			clean: func(s *uncooperativeSink) { close(s.release) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.clean != nil {
				// Let the abandoned goroutine exit when the test ends, so the
				// leak this design accepts does not outlive the run.
				t.Cleanup(func() { test.clean(test.sink) })
			}

			service := New([]Sink{test.sink, succeeds("telegram")}, timeout, nil)

			message := mustMessage(t, "the vault is on a dead mount")
			finished := make(chan Outcome, 1)

			go func() {
				finished <- service.Post(message)
			}()

			var outcome Outcome

			// Generous but finite: the point is that Post returns at all, on a
			// bound the orchestrator owns rather than one the sink grants.
			select {
			case outcome = <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("Post did not return; the uncooperative sink is still holding the post open")
			}

			blocked := resultFor(t, outcome, "obsidian")
			if blocked.Success {
				t.Error("the abandoned sink was reported as a success")
			}

			if blocked.Reason != reasonTimedOut {
				t.Errorf("Reason = %q, want %q", blocked.Reason, reasonTimedOut)
			}

			// One classification for both routes to a timeout, whether the
			// sink noticed its deadline or had to be abandoned.
			if !errors.Is(blocked.Err, context.DeadlineExceeded) {
				t.Errorf("Err = %v, want it to wrap context.DeadlineExceeded", blocked.Err)
			}

			// And it reports the time actually spent waiting. The synthesised
			// abandonment result is built by hand rather than by deliver's
			// deferred assignment, so its Duration is the one that can be
			// dropped without any other assertion noticing.
			if blocked.Duration < timeout {
				t.Errorf("Duration = %s, want at least the %s waited before abandoning",
					blocked.Duration, timeout)
			}

			// The sibling's result is reported rather than held hostage.
			if sibling := resultFor(t, outcome, "telegram"); !sibling.Success {
				t.Errorf("the sibling was not reported: reason=%q err=%v", sibling.Reason, sibling.Err)
			}
		})
	}
}

// goexitSink ends its goroutine without returning and without panicking.
type goexitSink struct{}

func (goexitSink) Name() string { return "telegram" }

func (goexitSink) Send(context.Context, Message) error {
	runtime.Goexit()

	return nil
}

// TestAnAbnormalExitStillYieldsAWellFormedFailure covers the exits that run
// deferred functions with nothing to recover.
//
// runtime.Goexit does that, and so does panic(nil) under panicnil=1. The named
// result would otherwise come back as its zero value: a failure naming no sink,
// with no Reason for the front door (FR-017) and no Err for the log (FR-070).
// Fail-closed either way, but a result nobody can act on.
func TestAnAbnormalExitStillYieldsAWellFormedFailure(t *testing.T) {
	t.Parallel()

	outcome := New([]Sink{goexitSink{}, succeeds("obsidian")}, generousTimeout, nil).
		Post(mustMessage(t, "one sink Goexits"))

	if len(outcome.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(outcome.Results))
	}

	abnormal := resultFor(t, outcome, "telegram")

	if abnormal.Success {
		t.Error("an abnormally exiting sink was reported as a success")
	}

	if abnormal.Reason != reasonFailed {
		t.Errorf("Reason = %q, want %q", abnormal.Reason, reasonFailed)
	}

	if !errors.Is(abnormal.Err, errAbnormalExit) {
		t.Errorf("Err = %v, want it to wrap errAbnormalExit", abnormal.Err)
	}

	// A Goexit discards deliver's return value, so the Duration its deferred
	// assignment computed is thrown away — the seed has to carry one, or a log
	// record reports duration_ms 0 for work that took real time.
	if abnormal.Duration <= 0 {
		t.Errorf("Duration = %s, want the time actually spent", abnormal.Duration)
	}

	if sibling := resultFor(t, outcome, "obsidian"); !sibling.Success {
		t.Errorf("the sibling failed: %v", sibling.Err)
	}
}

// unrenderablePanic is a typed nil whose Error dereferences its receiver.
type unrenderablePanic struct {
	message string
}

func (e *unrenderablePanic) Error() string { return "sink: " + e.message }

// TestAPanicValueThatCannotRenderDoesNotPanicAgain covers the diagnostic path
// (FR-071).
//
// The recovered value is rendered by calling a method on it — and that method
// belongs to the code that was already panicking. A typed nil pointer to a
// sink-defined error type is the ordinary shape. Unguarded, the render faulted
// at the deliberate Err.Error() access FR-017 reserves for the log, which is
// where T040 and T073 will make it — after deliver had already preserved every
// result.
func TestAPanicValueThatCannotRenderDoesNotPanicAgain(t *testing.T) {
	t.Parallel()

	hostile := &fakeSink{
		name: "telegram",
		send: func(context.Context, Message) error {
			var unrenderable *unrenderablePanic

			panic(unrenderable)
		},
	}

	outcome := New([]Sink{hostile, succeeds("obsidian")}, generousTimeout, nil).
		Post(mustMessage(t, "the panic value cannot render itself"))

	failed := resultFor(t, outcome, "telegram")
	if failed.Err == nil {
		t.Fatal("no diagnostic error was recorded")
	}

	// This is the call that used to fault, and it is the one the diagnostic
	// logger makes.
	description := failed.Err.Error()

	if !strings.Contains(description, "whose own rendering panicked") {
		t.Errorf("Err.Error() = %q, want it to report the failed rendering", description)
	}

	if failed.Reason != reasonFailed {
		t.Errorf("Reason = %q, want %q", failed.Reason, reasonFailed)
	}

	if sibling := resultFor(t, outcome, "obsidian"); !sibling.Success {
		t.Errorf("the sibling failed: %v", sibling.Err)
	}
}

// TestNewReplacesATimeoutThatCannotBoundAnything covers the floor on the
// duration (FR-015).
//
// config.Validate requires posting.sink_timeout_seconds > 0, so a wired
// application cannot reach this — but the seconds-to-Duration conversion is the
// caller's, and it overflows int64 silently: 10000000000 passes validation and
// converts to roughly -2346317h. Passed through, that expired every context
// before its sink was entered and reported "request timed out" for a post
// nothing had attempted.
func TestNewReplacesATimeoutThatCannotBoundAnything(t *testing.T) {
	t.Parallel()

	for _, timeout := range []time.Duration{0, -time.Second, -2346317 * time.Hour} {
		t.Run(timeout.String(), func(t *testing.T) {
			t.Parallel()

			// The sink must consult its context, or this subtest cannot
			// fail for timeout == 0: an already-expired context is invisible
			// to a sink that never looks at it, and 0 plus the enforcement
			// grace keeps run's backstop from firing either. With the floor
			// removed and a context-honouring sink, a zero timeout hands
			// Send a context that is already Done.
			sink := &fakeSink{
				name: "telegram",
				send: func(ctx context.Context, _ Message) error { return ctx.Err() },
			}

			outcome := New([]Sink{sink}, timeout, nil).Post(mustMessage(t, "misconfigured"))

			result := resultFor(t, outcome, "telegram")
			if !result.Success {
				t.Errorf("the sink reported %q for a timeout of %s; it was never given a chance",
					result.Reason, timeout)
			}

			if sink.ran() != 1 {
				t.Errorf("the sink ran %d times, want 1", sink.ran())
			}
		})
	}
}

// obstinateNameSink is a sink whose Name() misbehaves. Send is never reached
// in the blocking and Goexit cases.
type obstinateNameSink struct {
	label   string
	block   chan struct{}
	goexit  bool
	slowFor time.Duration
}

func (s *obstinateNameSink) Name() string {
	switch {
	case s.block != nil:
		<-s.block
	case s.goexit:
		runtime.Goexit()
	case s.slowFor > 0:
		time.Sleep(s.slowFor)
	}

	return s.label
}

func (s *obstinateNameSink) Send(context.Context, Message) error { return nil }

// TestNameIsBoundedLikeSend covers the half of the interface the first fix
// pass left outside the bound it built (FR-015, constitution principle I).
//
// `Sink` has two methods and both are sink code. Hardening Send and leaving
// Name synchronous in run reproduced every failure the backstop had just been
// added to fix, one method over: a Name that blocks held the post open with no
// upper bound and its sibling's finished result unreachable; a Name that
// Goexits produced a result naming nothing, with no Reason for a front door and
// no Err for the log; and a slow Name was excluded from Duration even though
// the code claimed the opposite.
func TestNameIsBoundedLikeSend(t *testing.T) {
	t.Parallel()

	t.Run("a blocking Name does not hang the post", func(t *testing.T) {
		t.Parallel()

		const timeout = 100 * time.Millisecond

		blocked := &obstinateNameSink{label: "obsidian", block: make(chan struct{})}
		t.Cleanup(func() { close(blocked.block) })

		service := New([]Sink{blocked, succeeds("telegram")}, timeout, nil)

		finished := make(chan Outcome, 1)
		message := mustMessage(t, "Name is stuck")

		go func() { finished <- service.Post(message) }()

		var outcome Outcome

		select {
		case outcome = <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("Post did not return; a blocking Name is still outside the bound")
		}

		// Abandoned before it could say what it was called, so it is reported
		// under the sentinel rather than as a blank line.
		abandoned := outcome.Results[0]
		if abandoned.Name != unknownSinkName {
			t.Errorf("Name = %q, want the sentinel %q", abandoned.Name, unknownSinkName)
		}

		if abandoned.Reason != reasonTimedOut {
			t.Errorf("Reason = %q, want %q", abandoned.Reason, reasonTimedOut)
		}

		if sibling := resultFor(t, outcome, "telegram"); !sibling.Success {
			t.Errorf("the sibling was held hostage: reason=%q err=%v", sibling.Reason, sibling.Err)
		}
	})

	t.Run("a Goexit in Name still yields a well-formed failure", func(t *testing.T) {
		t.Parallel()

		outcome := New([]Sink{&obstinateNameSink{label: "obsidian", goexit: true}, succeeds("telegram")},
			generousTimeout, nil).Post(mustMessage(t, "Name Goexits"))

		abnormal := outcome.Results[0]

		if abnormal.Name != unknownSinkName {
			t.Errorf("Name = %q, want the sentinel %q", abnormal.Name, unknownSinkName)
		}

		if abnormal.Reason != reasonFailed {
			t.Errorf("Reason = %q, want %q", abnormal.Reason, reasonFailed)
		}

		if !errors.Is(abnormal.Err, errAbnormalExit) {
			t.Errorf("Err = %v, want it to wrap errAbnormalExit", abnormal.Err)
		}

		if sibling := resultFor(t, outcome, "telegram"); !sibling.Success {
			t.Errorf("the sibling failed: %v", sibling.Err)
		}
	})

	t.Run("a slow Name is counted in Duration", func(t *testing.T) {
		t.Parallel()

		const slow = 120 * time.Millisecond

		outcome := New([]Sink{&obstinateNameSink{label: "obsidian", slowFor: slow}}, generousTimeout, nil).
			Post(mustMessage(t, "Name is slow"))

		result := resultFor(t, outcome, "obsidian")
		if result.Duration < slow {
			t.Errorf("Duration = %s, want at least the %s spent in Name", result.Duration, slow)
		}
	})
}

// TestAnAbandonedSinkKeepsItsNameWhenItHasOne is the other half of the
// attribution rule, and the case the backstop actually exists for.
//
// The motivating shape is a sink whose Name() answers immediately and whose
// Send() hangs on a stalled mount. Reporting that under the sentinel would tell
// the user something timed out without saying what, which is most of what they
// need to know.
func TestAnAbandonedSinkKeepsItsNameWhenItHasOne(t *testing.T) {
	t.Parallel()

	hung := &uncooperativeSink{name: "obsidian", release: make(chan struct{})}
	t.Cleanup(func() { close(hung.release) })

	service := New([]Sink{hung, succeeds("telegram")}, 100*time.Millisecond, nil)

	finished := make(chan Outcome, 1)
	message := mustMessage(t, "Send hangs, Name is fine")

	go func() { finished <- service.Post(message) }()

	select {
	case outcome := <-finished:
		abandoned := resultFor(t, outcome, "obsidian")
		if abandoned.Reason != reasonTimedOut {
			t.Errorf("Reason = %q, want %q", abandoned.Reason, reasonTimedOut)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Post did not return")
	}
}

// TestAHugeTimeoutIsNotAnInstantTimeout covers the positive half of the
// overflow class (FR-015).
//
// The enforcement bound adds a grace period to the configured timeout, and near
// MaxInt64 that addition wraps to a large negative duration. time.NewTimer
// fires a negative duration immediately, so every post reported a timeout — for
// deliveries that in fact ran and landed. New's floor catches only the negative
// half of the same overflow class.
func TestAHugeTimeoutIsNotAnInstantTimeout(t *testing.T) {
	t.Parallel()

	for _, timeout := range []time.Duration{math.MaxInt64, math.MaxInt64 - 1, math.MaxInt64 - enforcementGrace/2} {
		t.Run(fmt.Sprintf("%d", int64(timeout)), func(t *testing.T) {
			t.Parallel()

			sink := succeeds("telegram")

			outcome := New([]Sink{sink}, timeout, nil).Post(mustMessage(t, "effectively unbounded"))

			result := resultFor(t, outcome, "telegram")
			if !result.Success {
				t.Errorf("reported %q for a huge positive timeout; the bound overflowed", result.Reason)
			}

			if sink.ran() != 1 {
				t.Errorf("the sink ran %d times, want 1", sink.ran())
			}
		})
	}
}

// TestNewCopiesTheSinks covers the ownership of the caller's slice.
//
// Without a copy, a caller that keeps and reuses its slice races every post in
// flight — and worse, a mutation landing mid-post could swap in a sink this
// Service was never handed, which is FR-016's "disabled sinks are not invoked"
// quietly becoming conditional on the caller's discipline.
func TestNewCopiesTheSinks(t *testing.T) {
	t.Parallel()

	intended := succeeds("telegram")
	intruder := succeeds("intruder")

	sinks := []Sink{intended}

	service := New(sinks, generousTimeout, nil)

	// The caller reuses its slice after handing it over.
	sinks[0] = intruder

	outcome := service.Post(mustMessage(t, "the caller mutated its slice"))

	if len(outcome.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(outcome.Results))
	}

	if outcome.Results[0].Name != "telegram" {
		t.Errorf("posted to %q; the Service used the caller's mutated slice", outcome.Results[0].Name)
	}

	if intruder.ran() != 0 {
		t.Errorf("the intruder sink ran %d times; it was never given to New", intruder.ran())
	}
}

// TestACooperativeSinkReportsItsOwnTimeoutNotTheBackstops covers
// enforcementGrace, which was the one mechanism this design added with no test
// at all (FR-015).
//
// The grace period exists so the per-sink context stays the primary mechanism
// and the backstop decides only for sinks that cannot be cancelled. Nothing
// asserted that: with the grace set to zero, or to microseconds, every ordinary
// cooperative timeout — a hung chat request, the common case — took the
// abandonment branch instead, discarding the sink's own error in favour of a
// synthesised one and leaking a goroutine where none had leaked before. Both
// results say "request timed out", so only the diagnostic half distinguishes
// them.
func TestACooperativeSinkReportsItsOwnTimeoutNotTheBackstops(t *testing.T) {
	t.Parallel()

	const timeout = 80 * time.Millisecond

	cooperative := &fakeSink{
		name: "telegram",
		send: func(ctx context.Context, _ Message) error {
			<-ctx.Done()

			return ctx.Err()
		},
	}

	outcome := New([]Sink{cooperative}, timeout, nil).Post(mustMessage(t, "the sink honours its deadline"))

	result := resultFor(t, outcome, "telegram")

	if result.Reason != reasonTimedOut {
		t.Fatalf("Reason = %q, want %q", result.Reason, reasonTimedOut)
	}

	// The sink's own error, not the orchestrator's. "abandoned" appears only in
	// the synthesised backstop error, so its absence is what proves the
	// cooperative route was taken.
	if strings.Contains(result.Err.Error(), "abandoned") {
		t.Errorf("Err = %q; the backstop preempted a sink that honoured its own deadline",
			result.Err.Error())
	}

	if !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Errorf("Err = %v, want it to wrap context.DeadlineExceeded", result.Err)
	}
}

// TestASlowNameDoesNotCostTheSinkItsOwnDeadline covers the deadline origin.
//
// The backstop timer starts before Name() runs, so if the sink's context began
// when deliver was entered — after Name — a Name costing more than the grace
// pushed the sink's deadline past the backstop and a sink that honoured
// cancellation was abandoned anyway. Both deadlines now come from the same
// origin, so the grace means what it says whatever Name costs.
func TestASlowNameDoesNotCostTheSinkItsOwnDeadline(t *testing.T) {
	t.Parallel()

	// nameCost exceeds the grace, which is the shape that used to cause
	// preemption, but stays well inside the timeout — because Name is part of
	// the sink's operation, so it spends the sink's own budget rather than
	// extending it. A Name costing more than timeout+grace is abandoned, and
	// correctly so: that is the bound working.
	const (
		timeout  = time.Second
		nameCost = enforcementGrace * 2
	)

	slowlyNamed := &deadlineObservingSink{label: "obsidian", nameCost: nameCost}

	outcome := New([]Sink{slowlyNamed}, timeout, nil).Post(mustMessage(t, "Name is slower than the grace"))

	result := resultFor(t, outcome, "obsidian")

	// Attribution survives: the name was resolved, so the report says which
	// sink timed out.
	if result.Name != "obsidian" {
		t.Errorf("Name = %q, want the sink's own name", result.Name)
	}

	if strings.Contains(result.Err.Error(), "abandoned") {
		t.Errorf("Err = %q; a slow Name let the backstop preempt the sink's own deadline",
			result.Err.Error())
	}

	// The sink's deadline is measured from the same origin as the backstop, so
	// the whole operation — Name included — fits inside the configured timeout
	// rather than Name pushing the deadline out past it.
	if result.Duration >= timeout+enforcementGrace {
		t.Errorf("Duration = %s, want less than the enforcement bound %s",
			result.Duration, timeout+enforcementGrace)
	}
}

// deadlineObservingSink spends nameCost in Name and then waits for its own
// context to expire.
type deadlineObservingSink struct {
	label    string
	nameCost time.Duration
}

func (s *deadlineObservingSink) Name() string {
	time.Sleep(s.nameCost)

	return s.label
}

func (s *deadlineObservingSink) Send(ctx context.Context, _ Message) error {
	<-ctx.Done()

	return ctx.Err()
}

// TestAbandonedDeliveriesAreReclaimed covers the buffering that lets an
// abandoned goroutine exit at all.
//
// run's accepted-consequences note promises the leak is bounded to the blocked
// window and that everything is reclaimed once the sink unblocks. Nothing
// observed that: with `done` unbuffered, every abandoned goroutine parks
// forever on a send nobody will receive, and the whole suite stayed green — so
// a temporary leak would have become a permanent one for a long-lived GUI
// session against a dead mount.
//
// The assertion is a *relative drop* rather than a comparison against a
// baseline taken before the posts. An earlier version did the latter and
// flaked: this package's other tests are parallel, so the process-wide
// goroutine count drifts underneath any absolute figure (observed at 28
// sibling goroutines). Requiring the count to fall by at least one per post
// after the sinks unblock proves both halves at once — that many goroutines
// must have existed to be reclaimed — and cannot be perturbed by a sibling
// test starting or finishing work of its own.
func TestAbandonedDeliveriesAreReclaimed(t *testing.T) {
	t.Parallel()

	const posts = 40

	release := make(chan struct{})
	hung := &uncooperativeSink{name: "obsidian", release: release}

	service := New([]Sink{hung}, time.Millisecond, nil)

	for range posts {
		service.Post(mustMessage(t, "the mount is dead"))
	}

	// Every delivery is still resident here, which is the documented cost.
	blocked := runtime.NumGoroutine()

	close(release)

	// And every one exits once the sink unblocks. Polled rather than slept on:
	// the exact moment is the scheduler's, only the outcome is ours.
	want := blocked - posts

	deadline := time.Now().Add(20 * time.Second)
	for runtime.NumGoroutine() > want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if settled := runtime.NumGoroutine(); settled > want {
		t.Errorf("goroutines fell only from %d to %d after the sinks unblocked, want at most %d; "+
			"%d abandoned deliveries were not reclaimed", blocked, settled, want, posts)
	}
}

// panicNilSink panics with an untyped nil.
type panicNilSink struct{}

func (panicNilSink) Name() string { return "telegram" }

func (panicNilSink) Send(context.Context, Message) error {
	//nolint:govet // panic(nil) is the case under test.
	panic(nil)
}

// TestPanicNilIsAWellFormedFailure covers deliver's pre-seed, which existed
// for a case no test reached.
//
// Under Go 1.21+ defaults, panic(nil) becomes a *runtime.PanicNilError, so
// recover returns non-nil and the ordinary handler fires. Under
// GODEBUG=panicnil=1 the old semantics come back: the panic is arrested but
// recover returns nil, so the handler does not fire and deliver returns its
// named result — which is why that result is pre-seeded. Deleting the seed left
// the whole suite green, because nothing in it ever panicked with nil.
//
// GODEBUG is read once at startup, so the panicnil half cannot be exercised in
// this process. It runs as a subprocess of the test binary instead, which is
// the only way to assert a claim about a runtime mode.
func TestPanicNilIsAWellFormedFailure(t *testing.T) {
	if os.Getenv(panicNilSubprocessEnv) == "1" {
		runPanicNilCase(t)

		return
	}

	t.Parallel()

	// The default-semantics half, in this process.
	t.Run("default semantics", func(t *testing.T) {
		t.Parallel()

		runPanicNilCase(t)
	})

	// And the panicnil=1 half, in a subprocess with the mode set.
	t.Run("under GODEBUG=panicnil=1", func(t *testing.T) {
		t.Parallel()

		command := exec.Command(os.Args[0], "-test.run=^TestPanicNilIsAWellFormedFailure$", "-test.v")
		command.Env = append(os.Environ(), panicNilSubprocessEnv+"=1", "GODEBUG=panicnil=1")

		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("the panicnil=1 subprocess failed: %v\n%s", err, output)
		}
	})
}

// panicNilSubprocessEnv marks the re-executed child of the test above.
const panicNilSubprocessEnv = "MIKO_POST_PANICNIL_CASE"

// runPanicNilCase asserts the result shape, whichever semantics are in force.
//
// Both routes have to produce a well-formed failure: the difference is only
// which mechanism produces it, and the point of the test is that neither leaves
// a zero-valued result.
func runPanicNilCase(t *testing.T) {
	t.Helper()

	outcome := New([]Sink{panicNilSink{}, succeeds("obsidian")}, generousTimeout, nil).
		Post(mustMessage(t, "the sink panics with nil"))

	if len(outcome.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(outcome.Results))
	}

	failed := resultFor(t, outcome, "telegram")

	if failed.Success {
		t.Error("a sink that panicked with nil was reported as a success")
	}

	if failed.Name == "" {
		t.Error("the failing result names no sink")
	}

	if failed.Reason != reasonFailed {
		t.Errorf("Reason = %q, want %q", failed.Reason, reasonFailed)
	}

	if failed.Err == nil {
		t.Error("the failing result carries no diagnostic error")
	}

	if failed.Duration <= 0 {
		t.Error("the failing result reports no duration")
	}

	if sibling := resultFor(t, outcome, "obsidian"); !sibling.Success {
		t.Errorf("the sibling failed: %v", sibling.Err)
	}
}
