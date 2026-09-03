package post

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func succeeded(name string) SinkResult {
	return SinkResult{Name: name, Success: true, Duration: 10 * time.Millisecond}
}

func failed(name, reason string) SinkResult {
	return SinkResult{
		Name:     name,
		Success:  false,
		Reason:   reason,
		Err:      errors.New("detailed diagnostic for " + name),
		Duration: 10 * time.Millisecond,
	}
}

func TestAllSucceeded(t *testing.T) {
	tests := []struct {
		name    string
		results []SinkResult
		want    bool
	}{
		{
			name:    "both sinks succeeded",
			results: []SinkResult{succeeded("telegram"), succeeded("obsidian")},
			want:    true,
		},
		{
			name:    "one enabled sink succeeded",
			results: []SinkResult{succeeded("obsidian")},
			want:    true,
		},
		{
			// Partial failure is the case FR-062 exists for: the run fails even
			// though a destination did receive the message.
			name:    "first sink failed, second succeeded",
			results: []SinkResult{failed("telegram", "chat not found"), succeeded("obsidian")},
			want:    false,
		},
		{
			// Ordering must not matter; a failure late in the slice is not
			// swallowed by an early success.
			name:    "first sink succeeded, second failed",
			results: []SinkResult{succeeded("telegram"), failed("obsidian", "permission denied")},
			want:    false,
		},
		{
			name:    "both sinks failed",
			results: []SinkResult{failed("telegram", "request timed out"), failed("obsidian", "permission denied")},
			want:    false,
		},
		{
			name:    "one enabled sink failed",
			results: []SinkResult{failed("telegram", "request timed out")},
			want:    false,
		},
		{
			// Fail closed rather than vacuously true: no destination received
			// the message, so the post did not succeed. FR-018 should prevent
			// this reaching the orchestrator at all.
			name:    "no results",
			results: nil,
			want:    false,
		},
		{
			name:    "empty non-nil results",
			results: []SinkResult{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllSucceeded(tt.results); got != tt.want {
				t.Errorf("AllSucceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A rescued chat delivery is an ordinary success as far as aggregation is
// concerned: it must not drag the exit status to 1 (FR-061). The rescue itself
// is visible only in the log, so the result carries no failure marking.
func TestAllSucceededTreatsRescuedDeliveryAsSuccess(t *testing.T) {
	rescued := SinkResult{Name: "telegram", Success: true, Duration: 20 * time.Millisecond}

	if !AllSucceeded([]SinkResult{rescued, succeeded("obsidian")}) {
		t.Error("AllSucceeded() = false, want true for a rescued delivery")
	}
}

// tokenSentinel stands in for a Telegram bot token. It is deliberately not a
// substring of anything else a render can legitimately emit, so finding it
// anywhere in the output means it came out of Err.
const tokenSentinel = "123456:AAHfSENTINELTOKENxyz"

// leakyResult is the exact shape a real Telegram timeout produces: the token
// lives in the request path, so net/http returns a *url.Error whose exported
// URL field carries it. Building the fixture this way rather than from a plain
// errors.New keeps the test honest about how the leak actually arrives — a
// guard that only handled flat error strings would still pass a hand-written
// fixture while failing in production.
func leakyResult(underlying error) SinkResult {
	return SinkResult{
		Name:    "telegram",
		Success: false,
		Reason:  "request timed out",
		Err: &url.Error{
			Op:  "Post",
			URL: "https://api.telegram.org/bot" + tokenSentinel + "/sendMessage",
			Err: underlying,
		},
		Duration: time.Minute,
	}
}

// No default render of a SinkResult may emit Err's content, because Err can
// carry the bot token and every one of these surfaces is reachable by accident
// from a front door, a log line, or a debugging print (FR-017, FR-043, FR-069,
// contracts/telegram-sink.md).
//
// The predecessor of this test asserted only that two literals it had just
// written differed, which no production change could falsify. This one renders
// the value through each surface and fails if the sentinel appears, so removing
// any guard on SinkResult — or routing Reason from Err — breaks it.
func TestSinkResultRendersNeverLeakDiagnosticErrorContent(t *testing.T) {
	result := leakyResult(context.DeadlineExceeded)

	single, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(SinkResult) error = %v", err)
	}

	// Front doors and log records carry the whole slice, so the slice form is a
	// distinct surface: a guard that only a single value reached would still
	// leak from the shape the application actually renders.
	slice, err := json.Marshal([]SinkResult{result})
	if err != nil {
		t.Fatalf("json.Marshal([]SinkResult) error = %v", err)
	}

	tests := []struct {
		name   string
		render string
	}{
		{name: "%v", render: fmt.Sprintf("%v", result)},
		{name: "%s", render: fmt.Sprintf("%s", result)},
		{name: "%+v", render: fmt.Sprintf("%+v", result)},
		{name: "%#v", render: fmt.Sprintf("%#v", result)},
		{name: "%v of a slice", render: fmt.Sprintf("%v", []SinkResult{result})},
		{name: "%+v of a pointer", render: fmt.Sprintf("%+v", &result)},
		// A verb no Stringer covers. fmt answers a wrong verb by dumping the
		// exported fields with method dispatch turned off, so before Format
		// existed this one render reached Err directly; the whole-alphabet
		// sweep below is the general form of this case.
		{name: "%t", render: fmt.Sprintf("%t", result)},
		{name: "json.Marshal", render: string(single)},
		{name: "json.Marshal of a slice", render: string(slice)},
		// slog is the surface that matters most, because its destination is the
		// rotating file on disk: a leak here is written down and kept. Both
		// handlers are checked because they format an attribute by different
		// routes, and both the single value and the slice are checked because
		// slog resolves a LogValuer only at the top level — inside a slice the
		// handler falls back to json.Marshal or %+v instead.
		{name: "slog JSON handler", render: slogLine(t, slogJSONHandler, result)},
		{name: "slog text handler", render: slogLine(t, slogTextHandler, result)},
		{name: "slog JSON handler of a pointer", render: slogLine(t, slogJSONHandler, &result)},
		{name: "slog JSON handler of a slice", render: slogLine(t, slogJSONHandler, []SinkResult{result})},
		{name: "slog text handler of a slice", render: slogLine(t, slogTextHandler, []SinkResult{result})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.render, tokenSentinel) {
				t.Errorf("render leaked the token sentinel: %s", tt.render)
			}

			if !strings.Contains(tt.render, errRedactedMarker) {
				t.Errorf("render omits the redaction marker %q, so a reader cannot tell a diagnostic exists: %s", errRedactedMarker, tt.render)
			}
		})
	}
}

// The named renders above only cover the surfaces that exist today. The rule
// FR-017 states is broader: nothing callable on a SinkResult may hand out Err's
// content. The way that rule actually gets broken is someone adding a
// convenience accessor — a DisplayReason() that falls back to r.Err.Error() when
// Reason is empty is the obvious one, and it would sail past every test written
// against a fixed list of verbs because nothing in that list calls it.
//
// So the method set is enumerated instead of listed. Every exported method that
// takes no arguments is invoked and everything it returns is scanned, and the
// five guards are additionally required to still be present, so the test fails
// both when an accessor is added and when a guard is deleted.
//
// The enumeration runs over several result shapes rather than one, because an
// accessor's leak can sit behind a condition. A single classified-failure
// fixture leaves the empty-Reason branch of that obvious DisplayReason()
// unexecuted, so the accessor the comment above names as the motivating case
// survives the test written to catch it.
func TestSinkResultExposesNoAccessorThatEmitsTheDiagnosticError(t *testing.T) {
	shapes := []struct {
		name    string
		success bool
		reason  string
	}{
		{
			name:    "classified failure",
			success: false,
			reason:  "request timed out",
		},
		{
			// Reason is documented empty on success and is filled in by the
			// classifier, so a failure that has not reached the classifier yet
			// carries an empty Reason and a live Err. This is the shape that
			// executes an accessor's empty-Reason fallback.
			name:    "failure whose reason is not classified yet",
			success: false,
			reason:  "",
		},
		{
			// Nothing constrains Err on a success, and a rescued delivery is
			// the real case, so a success can carry a diagnostic too.
			name:    "success still carrying a diagnostic",
			success: true,
			reason:  "",
		},
	}

	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			result := leakyResult(context.DeadlineExceeded)
			result.Success = shape.success
			result.Reason = shape.reason

			found := renderableMethods(t, result)

			for _, guard := range []string{"Format", "String", "GoString", "MarshalJSON", "LogValue"} {
				if !slices.Contains(found.names, guard) {
					t.Errorf("SinkResult no longer exposes %s, so a default Go render reaches Err again", guard)
				}
			}

			// Not a failure: a method taking arguments has no safe value to
			// pass, and Format is the expected entry. Reported rather than
			// dropped, so a new uninvokable method is visible to whoever reads
			// the output instead of vanishing from the enumeration.
			if len(found.skipped) > 0 {
				t.Logf("not invoked, takes arguments: %v", found.skipped)
			}

			for _, tt := range found.renders {
				t.Run(tt.name, func(t *testing.T) {
					if strings.Contains(tt.render, tokenSentinel) {
						t.Errorf("%s handed out the diagnostic error's content: %s", tt.method, tt.render)
					}
				})
			}
		})
	}
}

type methodRender struct {
	name   string
	method string
	render string
}

// methodSet is what the enumeration found: every exported method name, the text
// produced by those that could be invoked, and those that could not.
type methodSet struct {
	names   []string
	renders []methodRender
	skipped []string
}

// renderableMethods invokes every exported, argument-free method of a SinkResult
// on both the value and the pointer, and returns what each produced.
//
// Unexported helpers are invisible to reflection's method set, which is correct
// here — they are not reachable from outside the package.
func renderableMethods(t *testing.T, result SinkResult) methodSet {
	t.Helper()

	var found methodSet

	for _, subject := range []reflect.Value{reflect.ValueOf(result), reflect.ValueOf(&result)} {
		subjectType := subject.Type()

		for i := range subjectType.NumMethod() {
			name := subjectType.Method(i).Name
			qualified := subjectType.String() + "." + name

			found.names = append(found.names, name)

			render, ok := invokeForRender(t, subject.Method(i))
			if !ok {
				found.skipped = append(found.skipped, qualified)

				continue
			}

			found.renders = append(found.renders, methodRender{
				name:   qualified,
				method: name,
				render: render,
			})
		}
	}

	return found
}

// invokeForRender calls a nullary method and reduces everything it returns to
// text.
//
// Every returned value is rendered twice, through %+v and through json.Marshal,
// rather than matched against a list of return shapes known to carry text. A
// whitelist is the wrong instrument for this test: the point of enumerating the
// method set is to catch the accessor nobody anticipated, and a slog.Value, a
// map[string]any and a bare error all carry Err's content straight past a
// string-or-bytes filter — and, worse, past it in silence.
//
// A byte slice is turned back into its text first, because %+v prints one as
// decimal numbers and json.Marshal base64-encodes it, either of which would let
// a token through the substring scan. That test is by element kind, so a named
// type whose underlying type is []byte is covered; an exact []byte type
// comparison would miss it.
//
// Only a method taking arguments is skipped, because there is no safe value to
// pass. The caller reports those.
func invokeForRender(t *testing.T, method reflect.Value) (string, bool) {
	t.Helper()

	if method.Type().NumIn() != 0 {
		return "", false
	}

	var rendered strings.Builder

	for _, result := range method.Call(nil) {
		if result.Kind() == reflect.Slice && result.Type().Elem().Kind() == reflect.Uint8 {
			fmt.Fprintf(&rendered, "%s\n", result.Bytes())

			continue
		}

		value := result.Interface()

		fmt.Fprintf(&rendered, "%+v\n", value)

		// A value json cannot encode — a func or a channel field — is covered
		// by the %+v above; only the encodable ones add anything here.
		if encoded, err := json.Marshal(value); err == nil {
			fmt.Fprintf(&rendered, "%s\n", encoded)
		}
	}

	return rendered.String(), true
}

// A guard that redacted everything would pass the leak test and be useless. The
// display half must survive it, and a nil Err must stay distinguishable from a
// suppressed one so a log line still says whether there is a diagnostic to look
// up.
func TestSinkResultRendersKeepTheDisplayHalfReadable(t *testing.T) {
	failure := leakyResult(context.DeadlineExceeded)
	success := succeeded("obsidian")

	tests := []struct {
		name        string
		render      string
		wantStrings []string
	}{
		{
			name:        "failure keeps name and reason",
			render:      fmt.Sprintf("%v", failure),
			wantStrings: []string{"telegram", "request timed out", errRedactedMarker},
		},
		{
			name:        "failure json keeps name and reason",
			render:      mustMarshal(t, failure),
			wantStrings: []string{"telegram", "request timed out", errRedactedMarker},
		},
		{
			name:        "absent error is distinguishable from a redacted one",
			render:      fmt.Sprintf("%v", success),
			wantStrings: []string{"obsidian", errAbsentMarker},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range tt.wantStrings {
				if !strings.Contains(tt.render, want) {
					t.Errorf("render = %s, want it to contain %q", tt.render, want)
				}
			}
		})
	}
}

// Redaction applies to rendering, not to the field. The diagnostic logger reads
// r.Err deliberately, and errors.Is must still reach through the *url.Error
// wrapper to the cause the sink returned — otherwise the guard would have bought
// safety by destroying the diagnostic it is protecting (FR-017).
func TestSinkResultErrRemainsAvailableToTheLogger(t *testing.T) {
	underlying := errors.New("dial tcp: i/o timeout")
	result := leakyResult(underlying)

	if !errors.Is(result.Err, underlying) {
		t.Errorf("errors.Is(result.Err, underlying) = false, want true")
	}

	if !strings.Contains(result.Err.Error(), tokenSentinel) {
		t.Error("result.Err lost the underlying detail the diagnostic log needs")
	}

	if result.Reason == result.Err.Error() {
		t.Error("Reason must not be the error string")
	}
}

func mustMarshal(t *testing.T, result SinkResult) string {
	t.Helper()

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return string(encoded)
}

// Format is the only guard that has to answer for a verb nobody wrote on
// purpose. fmt consults Stringer for just v, s, q, x and X and GoStringer for
// %#v; every other verb reaches fmt's bad-verb path, which dumps the exported
// fields with method dispatch suppressed and walks into Err. So the property
// worth pinning is not "these verbs are safe" but "every verb is", and the
// alphabet is small enough to simply try.
//
// The redaction marker is required as well as the sentinel forbidden, because
// its presence is what proves Format was consulted rather than fmt having
// fallen back to its own dump.
//
// Three verbs are the documented exception, because fmt answers them itself
// before asking Formatter, so no marker appears in their output. They are still
// checked for the sentinel, since the leak is what matters and their fallback
// output is a field dump.
func TestSinkResultFormatKeepsEveryFmtVerbSafe(t *testing.T) {
	result := leakyResult(context.DeadlineExceeded)

	tests := []struct {
		name  string
		flags string
	}{
		{name: "no flags", flags: ""},
		{name: "plus flag", flags: "+"},
		{name: "sharp flag", flags: "#"},
		{name: "minus flag", flags: "-"},
		{name: "space flag", flags: " "},
	}

	// %T prints the type name without touching the value; %p and %w are
	// rejected by fmt before Formatter is reached, so they fall back to a field
	// dump with method dispatch suppressed. Their output carries no marker,
	// and the sentinel check below is what covers them.
	const answeredByFmtItself = "Tpw"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, verb := range "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" {
				format := "%" + tt.flags + string(verb)
				render := fmt.Sprintf(format, result)

				if strings.Contains(render, tokenSentinel) {
					t.Errorf("%s leaked the token sentinel: %s", format, render)
				}

				if strings.ContainsRune(answeredByFmtItself, verb) {
					continue
				}

				if !strings.Contains(render, errRedactedMarker) {
					t.Errorf("%s omits the redaction marker %q, so fmt rendered the struct itself: %s", format, errRedactedMarker, render)
				}
			}
		})
	}
}

// An Err holding a typed nil is not caught by the nil check, so every render
// reports a diagnostic that does not exist. That is deliberate and documented
// on errMarker: over-redacting cannot disclose anything, and the value is a bug
// at the site that built the result, which the diagnostic logger will hit on
// r.Err.Error() anyway.
//
// Pinned here so that adding a reflection-based nil check is a decision someone
// makes rather than a silent change of behavior. It also pins the stronger
// property that no guard calls Err.Error(): this Err panics if anything does,
// so a guard that reached for the error's text would fail here rather than in
// production, where the error could equally well be one that recurses forever
// or renders 64 MiB.
func TestSinkResultRendersReportATypedNilErrAsPresent(t *testing.T) {
	var missing *url.Error

	result := SinkResult{
		Name:     "telegram",
		Success:  false,
		Reason:   "request timed out",
		Err:      missing,
		Duration: time.Minute,
	}

	tests := []struct {
		name   string
		render string
	}{
		{name: "%v", render: fmt.Sprintf("%v", result)},
		{name: "%#v", render: fmt.Sprintf("%#v", result)},
		{name: "%t", render: fmt.Sprintf("%t", result)},
		{name: "json.Marshal", render: mustMarshal(t, result)},
		{name: "slog JSON handler", render: slogLine(t, slogJSONHandler, result)},
		{name: "slog text handler", render: slogLine(t, slogTextHandler, result)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.render, errRedactedMarker) {
				t.Errorf("render = %s, want the redaction marker %q for a typed-nil Err", tt.render, errRedactedMarker)
			}

			if strings.Contains(tt.render, errAbsentMarker) {
				t.Errorf("render = %s, want a typed-nil Err reported as present, not as %q", tt.render, errAbsentMarker)
			}
		})
	}
}

// slogLine logs one attribute through the given handler and returns the line it
// wrote, so a slog render can be checked like any other string.
func slogLine(t *testing.T, newHandler func(io.Writer) slog.Handler, value any) string {
	t.Helper()

	var line bytes.Buffer

	slog.New(newHandler(&line)).Error("sink send failed", "result", value)

	return line.String()
}

func slogJSONHandler(w io.Writer) slog.Handler {
	return slog.NewJSONHandler(w, nil)
}

func slogTextHandler(w io.Writer) slog.Handler {
	return slog.NewTextHandler(w, nil)
}
