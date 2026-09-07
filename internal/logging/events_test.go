package logging_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sgykfjsm/miko-post/internal/logging"
)

// contractEvents is the normative minimum set from
// specs/001-dual-sink-quick-post/contracts/log-events.md, transcribed in the
// order that document lists it (FR-067).
//
// Written out as literals rather than referenced through the constants on
// purpose. A test that asserts logging.EventMessageReceived ==
// logging.EventMessageReceived passes no matter what the constant is renamed
// to; the whole value of this test is that the literal strings live somewhere a
// rename cannot follow. Changing one of these strings is changing the log
// contract, and the diff should look like it.
var contractEvents = []string{
	"message_received",

	"obsidian_append_started",
	"obsidian_append_succeeded",
	"obsidian_append_failed",

	"telegram_send_started",
	"telegram_send_succeeded",
	"telegram_send_failed",

	"telegram_markdown_failed",
	"telegram_plaintext_succeeded",
	"telegram_plaintext_failed",

	"request_completed",
	"request_completed_with_error",
}

// TestAllEventsMatchesTheContract pins the registered set against the contract,
// exactly and in order (FR-067).
func TestAllEventsMatchesTheContract(t *testing.T) {
	t.Parallel()

	got := logging.AllEvents()

	if len(got) != len(contractEvents) {
		t.Fatalf("AllEvents has %d entries, the contract lists %d\ngot:  %v\nwant: %v",
			len(got), len(contractEvents), got, contractEvents)
	}

	for i, want := range contractEvents {
		if string(got[i]) != want {
			t.Errorf("AllEvents()[%d] = %q, want %q", i, got[i], want)
		}
	}
}

// TestAllEventsIsWellFormed rejects the defects that a set of string constants
// acquires by copy-paste: a duplicate, an empty name, or a name whose shape
// would not survive being grepped out of a log.
func TestAllEventsIsWellFormed(t *testing.T) {
	t.Parallel()

	seen := make(map[logging.Event]int, len(logging.AllEvents()))

	for i, event := range logging.AllEvents() {
		if event == "" {
			t.Errorf("AllEvents()[%d] is empty", i)

			continue
		}

		if first, duplicate := seen[event]; duplicate {
			// A duplicate is worse than it looks: two lifecycle stages sharing
			// a name make a started event and a succeeded event
			// indistinguishable in the log.
			t.Errorf("AllEvents()[%d] = %q duplicates entry %d", i, event, first)

			continue
		}

		seen[event] = i

		for _, r := range string(event) {
			if (r < 'a' || r > 'z') && r != '_' {
				t.Errorf("AllEvents()[%d] = %q contains %q; event names are lowercase and underscores only",
					i, event, r)

				break
			}
		}
	}
}

// TestAllEventsReturnsAFreshSlice guards the reason AllEvents is a function.
//
// A package-level slice variable would be writable by any importer, and one
// stray assignment would corrupt the vocabulary for the whole process. This
// fails if someone converts it back to a var returning the same backing array.
func TestAllEventsReturnsAFreshSlice(t *testing.T) {
	t.Parallel()

	first := logging.AllEvents()
	if len(first) == 0 {
		t.Fatal("AllEvents is empty")
	}

	original := first[0]
	first[0] = "mutated"

	if second := logging.AllEvents(); second[0] != original {
		t.Errorf("mutating the returned slice changed a later call: got %q, want %q",
			second[0], original)
	}
}

// eventNamePrefix is the naming convention every event constant follows, and
// one of the three ways the scan below recognises a declaration.
//
// Leaning on a name is unusual for a check like this and is deliberate: the
// other two markers — the Event type and an Event(…) conversion — are both
// avoidable by writing `const EventSomething = "something"`, which compiles at
// a real Info() call site because an untyped string constant is assignable to
// Event. The prefix is what closes that. It only works if it is universal, so
// the scan also *requires* it of every declaration it finds, which makes the
// convention self-enforcing rather than aspirational.
//
// The cost is a false positive: a package-level identifier that merely starts
// with "Event" and is not an event name fails this test. That is accepted. The
// package's whole exported vocabulary of Event* names is the event set, and a
// name that reads like one and is not is worth a deliberate rename.
const eventNamePrefix = "Event"

// eventDeclScan is what one pass over the package's sources found.
type eventDeclScan struct {
	// found maps each declaring constant's name to the event name it declares.
	found map[string]string

	// problems are declarations the scan refuses to accept, one message each.
	//
	// A problem is a failure and never a skip. A shape this scan cannot read is
	// a shape it cannot check for registration, so passing over it silently
	// would reopen the exact hole the test exists to close — while the test
	// still reported success.
	problems []string
}

func newEventDeclScan() *eventDeclScan {
	return &eventDeclScan{found: make(map[string]string)}
}

func (s *eventDeclScan) problemf(format string, args ...any) {
	s.problems = append(s.problems, fmt.Sprintf(format, args...))
}

// TestEveryDeclaredEventConstantIsRegistered is the half of T022 that a literal
// expected-set test cannot cover.
//
// The test above catches a rename, a typo and a removal, because all three make
// the registered set differ from the transcribed contract. None of them catches
// the opposite mistake: declaring a new Event constant and forgetting to add it
// to AllEvents. That constant would then be emitted into real logs while every
// test still passed and the registered vocabulary claimed it did not exist.
//
// So this reads the declarations out of the package's own source and requires
// the two sets to agree in both directions.
//
// The scan classifies each *declaration*, not the const group it sits in. An
// earlier version gated a whole group on finding a bare `Event` type identifier
// somewhere in it, which let five of the six shapes a real declaration can take
// pass unseen — including `const EventX = Event("x")`, `const EventX = "x"` in
// its own group, and `var EventX Event = "x"`. TestTheScanCatchesEveryDeclarationShape
// pins each of them.
//
// The remit is every const and var declaration in this package's non-test .go
// files, at any nesting depth, recognised by the bare `Event` type identifier,
// an `Event(...)` conversion, or the `Event` name prefix.
//
// Two things sit outside it, both deliberately. A constant with none of the
// three markers — `const somethingElse = "escape"`, used at a call site as
// Info(somethingElse) — would need go/types and the whole package's type
// information; the prefix requirement is the cheaper answer, since a constant
// like that is not an event declaration by any convention this package follows
// and would not survive review as one. And a type alias of Event
// (`type ev = Event`) is matched by neither the type nor the conversion form,
// so such a declaration is caught only if it carries the prefix. Neither shape
// exists here, and both are recorded so the boundary is a statement of what is
// checked rather than an assumption about it.
func TestEveryDeclaredEventConstantIsRegistered(t *testing.T) {
	t.Parallel()

	scan := scanPackageForEventDeclarations(t)

	for _, problem := range scan.problems {
		t.Error(problem)
	}

	if len(scan.found) == 0 {
		// Reaching here means the scan found nothing, which is far more likely
		// to be a broken scan than a package with no events. Failing loudly is
		// the difference between this test and one that silently passes.
		t.Fatal("no Event constants were found in the package source; the scan is broken")
	}

	registered := registeredEvents()

	for _, failure := range unregisteredDeclarations(scan.found, registered) {
		t.Error(failure)
	}

	for _, failure := range undeclaredRegistrations(scan.found, registered) {
		t.Error(failure)
	}
}

// TestTheScanCatchesEveryDeclarationShape is the test for the test.
//
// Every source below compiles, and every one of them declares something that
// PostLogger.Info would accept as an event. They cannot live in the real
// package — the point is that they are unregistered — so they are parsed from
// string sources and put through the same collector the package scan uses. A
// fixture that stopped being caught would mean the scan had regressed, which is
// invisible from the package itself once the package is clean.
//
// Only the declared-to-registered direction is asserted, plus the problems.
// Running the reverse direction over a one-declaration fixture would report all
// twelve real events as missing and pass for any collector at all, including
// one that found nothing.
func TestTheScanCatchesEveryDeclarationShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		// want is a fragment that at least one failure message for this shape
		// must mention, so a shape cannot be "caught" by an unrelated
		// complaint.
		//
		// At least one rather than all of them: a fixture declaring two names
		// is correctly reported twice, once per declaration, and only one of
		// those messages names any particular one. Requiring it of every
		// message failed the two multi-declaration shapes below for being
		// caught twice, which is the opposite of the defect this guards.
		want string
	}{
		{
			name: "Event-typed via a conversion",
			src:  `const EventEscapeA = Event("escape_a")`,
			want: `"escape_a"`,
		},
		{
			name: "untyped, in its own group",
			src:  `const EventEscapeB = "escape_b"`,
			want: `"escape_b"`,
		},
		{
			name: "untyped, in a shared group",
			src: `const (
				EventEscapeC = "escape_c"
				EventEscapeD = "escape_d"
			)`,
			want: `"escape_c"`,
		},
		{
			// Resolved rather than merely reported: the old scan rejected this
			// as "not a plain string literal", which is a complaint about the
			// syntax and not about the name being unregistered. A long name
			// wrapped by gofmt takes exactly this shape.
			name: "typed, value concatenated",
			src:  `const EventEscapeE Event = "escape" + "_e"`,
			want: `"escape_e"`,
		},
		{
			name: "declared as a var",
			src:  `var EventEscapeF Event = "escape_f"`,
			want: "var",
		},
		{
			name: "Event-typed without the name prefix",
			src:  `const MessageReceived Event = "message_received"`,
			want: eventNamePrefix,
		},
		{
			name: "no value of its own",
			src: `const (
				EventEscapeG Event = "escape_g"
				EventEscapeH
			)`,
			want: "EventEscapeH",
		},
		{
			// Two of the three markers, and it used to be skipped in silence
			// because the scan never descended into a function body.
			name: "inside a function body",
			src: `func emit() {
				const EventEscapeL Event = "escape_l"
				_ = EventEscapeL
			}`,
			want: "EventEscapeL",
		},
		{
			name: "an iota form",
			src: `const (
				EventEscapeI Event = iota
			)`,
			want: "EventEscapeI",
		},
		{
			name: "a value this scan cannot resolve",
			src:  `const EventEscapeJ = Event(strings.Repeat("x", 3))`,
			want: "EventEscapeJ",
		},
		{
			name: "a non-string literal",
			src:  `const EventEscapeK Event = 7`,
			want: "EventEscapeK",
		},
	}

	registered := registeredEvents()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			scan := scanSource(t, test.src)

			failures := append(scan.problems, unregisteredDeclarations(scan.found, registered)...)

			if len(failures) == 0 {
				t.Fatalf("the scan accepted an unregistered event declaration:\n%s", test.src)
			}

			named := false

			for _, failure := range failures {
				if strings.Contains(failure, test.want) {
					named = true

					break
				}
			}

			if !named {
				t.Errorf("no failure mentions %q, so the shape was caught for the wrong reason;\nsource:\n%s\nfailures:\n%s",
					test.want, test.src, strings.Join(failures, "\n"))
			}
		})
	}
}

// TestTheScanAcceptsARegisteredDeclaration is the fixture test's control.
//
// Without it, a collector that reported every declaration as a problem would
// pass every case above. This is the shape the real package uses, with a name
// AllEvents registers, and it must produce nothing at all.
func TestTheScanAcceptsARegisteredDeclaration(t *testing.T) {
	t.Parallel()

	scan := scanSource(t, `const (
		// A doc comment and a blank line, as the real declaration has.
		EventMessageReceived Event = "message_received"
	)`)

	for _, problem := range scan.problems {
		t.Errorf("the scan rejected the shape the package actually uses: %s", problem)
	}

	for _, failure := range unregisteredDeclarations(scan.found, registeredEvents()) {
		t.Error(failure)
	}

	if got := scan.found["EventMessageReceived"]; got != "message_received" {
		t.Errorf("the scan read EventMessageReceived as %q, want %q", got, "message_received")
	}
}

// TestTheScanIgnoresNonEventDeclarations keeps the three markers from turning
// into "every package-level constant".
//
// The package is full of unrelated declarations — the record keys, the file
// modes, the source values — and a scan that claimed them would fail on its
// own package and be deleted rather than fixed.
func TestTheScanIgnoresNonEventDeclarations(t *testing.T) {
	t.Parallel()

	scan := scanSource(t, `const (
		keyEvent = "event"
		sourceUnknown Source = "unknown"
		logFilePerm os.FileMode = 0o600
	)

	var replaceAttr = func() {}`)

	for _, problem := range scan.problems {
		t.Errorf("the scan claimed a declaration that is not an event: %s", problem)
	}

	if len(scan.found) != 0 {
		t.Errorf("the scan found %v, want nothing", scan.found)
	}
}

// registeredEvents is AllEvents as a set of names.
func registeredEvents() map[string]bool {
	registered := make(map[string]bool, len(logging.AllEvents()))
	for _, event := range logging.AllEvents() {
		registered[string(event)] = true
	}

	return registered
}

// unregisteredDeclarations reports each declared event name that AllEvents does
// not contain: the new constant somebody forgot to register.
func unregisteredDeclarations(found map[string]string, registered map[string]bool) []string {
	failures := make([]string, 0)

	for _, name := range sortedKeys(found) {
		if !registered[found[name]] {
			failures = append(failures,
				fmt.Sprintf("constant %s = %q is declared but missing from AllEvents", name, found[name]))
		}
	}

	return failures
}

// undeclaredRegistrations reports each registered event name that no constant
// declares: a literal that drifted, or a constant that was removed.
func undeclaredRegistrations(found map[string]string, registered map[string]bool) []string {
	declared := make(map[string]bool, len(found))
	for _, value := range found {
		declared[value] = true
	}

	failures := make([]string, 0)

	for _, value := range sortedSet(registered) {
		if !declared[value] {
			failures = append(failures,
				fmt.Sprintf("AllEvents contains %q, which no Event constant declares", value))
		}
	}

	return failures
}

// scanPackageForEventDeclarations parses the package's non-test sources and
// collects every event declaration in them.
//
// The package directory is the test's working directory, so "." is the package
// under test. Files are listed and parsed individually rather than through
// parser.ParseDir, which returns the deprecated ast.Package.
//
// Every .go file is read regardless of its build constraints. That is the
// conservative direction: a constant behind a build tag is still a constant
// somebody has to register, and a scan that honoured tags would stop seeing it
// on the platform where nobody is looking.
func scanPackageForEventDeclarations(t *testing.T) *eventDeclScan {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	scan := newEventDeclScan()
	fset := token.NewFileSet()
	parsed := 0

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		parsed++

		collectEventDeclarations(name, file, scan)
	}

	if parsed == 0 {
		t.Fatal("no non-test Go files were found to scan")
	}

	return scan
}

// scanSource runs the same collector over a fixture source.
//
// The fixture is wrapped in a package clause rather than written out with one,
// so each case above reads as the declaration it is about and nothing else.
func scanSource(t *testing.T, src string) *eventDeclScan {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "fixture.go", "package logging\n\n"+src+"\n", 0)
	if err != nil {
		t.Fatalf("parse the fixture: %v\nsource:\n%s", err, src)
	}

	scan := newEventDeclScan()
	collectEventDeclarations("fixture.go", file, scan)

	return scan
}

// collectEventDeclarations adds every event declaration in file to scan.
//
// var groups are walked as well as const groups. A var is never accepted — see
// readEventSpec — but it has to be *seen* to be rejected, and `var EventX Event
// = "x"` compiles at a log call site exactly like the const does.
func collectEventDeclarations(filename string, file *ast.File, scan *eventDeclScan) {
	// ast.Inspect rather than a loop over file.Decls, so a declaration inside
	// a function body is reached too.
	//
	// The loop was the whole scan once, and it made the boundary narrower than
	// the comments claimed: a function-local `const EventLocal Event = "…"`
	// carries two of the three markers below and was still skipped in silence,
	// because file.Decls holds only the top-level declarations and never
	// descends into a FuncDecl. Walking the whole tree costs nothing here and
	// removes a shape that had to be described rather than checked.
	ast.Inspect(file, func(node ast.Node) bool {
		genDecl, ok := node.(*ast.GenDecl)
		if !ok || (genDecl.Tok != token.CONST && genDecl.Tok != token.VAR) {
			return true
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || !declaresEvent(valueSpec) {
				continue
			}

			scan.readEventSpec(filename, genDecl.Tok, valueSpec)
		}

		return true
	})
}

// declaresEvent reports whether a spec is an event declaration, by any of the
// three markers a real one carries.
//
// Any one is enough, deliberately. Requiring the type would miss the
// conversion form and the untyped form; requiring the prefix would miss a
// mistyped name that the Event type still admits. Over-matching here is safe,
// because readEventSpec's job is to say precisely what is wrong with whatever
// it was handed.
func declaresEvent(spec *ast.ValueSpec) bool {
	if isEventType(spec.Type) {
		return true
	}

	for _, value := range spec.Values {
		if _, ok := eventConversion(value); ok {
			return true
		}
	}

	for _, ident := range spec.Names {
		if strings.HasPrefix(ident.Name, eventNamePrefix) {
			return true
		}
	}

	return false
}

// readEventSpec records one event declaration, or says why it cannot be.
func (s *eventDeclScan) readEventSpec(filename string, tok token.Token, spec *ast.ValueSpec) {
	names := identNames(spec.Names)

	if tok != token.CONST {
		// Not a shape to be read around. AllEvents is a function returning a
		// fresh slice precisely so no importer can rewrite the vocabulary, and
		// a package-level var undoes that with one assignment.
		s.problemf("%s: %v declares an event name as a var; any importer could then reassign it and "+
			"change the vocabulary for the whole process — declare it as `const %s Event = …`",
			filename, names, names[0])

		return
	}

	if len(spec.Names) != len(spec.Values) {
		// A spec with no value of its own repeats the previous one, or is an
		// iota form. Neither is a name this scan can read, and neither belongs
		// in a vocabulary whose whole purpose is being greppable out of a log.
		s.problemf("%s: event constants %v have %d names and %d values; give each one its own "+
			"string literal", filename, names, len(spec.Names), len(spec.Values))

		return
	}

	for i, ident := range spec.Names {
		if !strings.HasPrefix(ident.Name, eventNamePrefix) {
			s.problemf("%s: %s declares an event name without the %q name prefix; the scan that keeps "+
				"AllEvents honest finds declarations by that prefix, so a name without it is a name no "+
				"test can check", filename, ident.Name, eventNamePrefix)

			continue
		}

		value, err := stringConstant(spec.Values[i])
		if err != nil {
			s.problemf("%s: constant %s %v", filename, ident.Name, err)

			continue
		}

		if previous, duplicate := s.found[ident.Name]; duplicate {
			s.problemf("%s: constant %s is declared twice (%q and %q)",
				filename, ident.Name, previous, value)

			continue
		}

		s.found[ident.Name] = value
	}
}

// stringConstant folds the value shapes an event name may legitimately take
// into the string it denotes.
//
// Three are resolved rather than reported, because all three compile at a real
// Info() call site and all three are event declarations:
//
//	Event = "x"             a bare literal
//	Event("x")              a conversion, which is how an untyped constant gets
//	                        the type without a type clause
//	Event = "x" + "_y"      a concatenation, which is the shape a long name
//	                        takes once gofmt has wrapped the line
//
// Anything else is refused rather than resolved. Going further would mean
// evaluating Go constant expressions here — go/constant over a full type-check
// of the package — and a name assembled out of something this file cannot see
// is a name the log contract cannot pin anyway. Refusing is also not a silent
// skip: the caller turns it into a failure naming the constant.
func stringConstant(expr ast.Expr) (string, error) {
	if inner, ok := eventConversion(expr); ok {
		return stringConstant(inner)
	}

	switch value := expr.(type) {
	case *ast.ParenExpr:
		return stringConstant(value.X)
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", fmt.Errorf("is a %s literal, not a string", strings.ToLower(value.Kind.String()))
		}

		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", fmt.Errorf("has an unreadable literal %s: %w", value.Value, err)
		}

		return text, nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", fmt.Errorf("applies %s to its value; an event name is a string constant", value.Op)
		}

		left, err := stringConstant(value.X)
		if err != nil {
			return "", err
		}

		right, err := stringConstant(value.Y)
		if err != nil {
			return "", err
		}

		return left + right, nil
	default:
		return "", fmt.Errorf("has a value of AST type %T, which this scan cannot resolve to a "+
			"string constant", expr)
	}
}

// eventConversion unwraps an Event(x) conversion, which is how an event name
// gets the type without a type clause.
func eventConversion(expr ast.Expr) (ast.Expr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isEventType(call.Fun) {
		return nil, false
	}

	return call.Args[0], true
}

// isEventType reports whether an expression is the bare identifier Event.
func isEventType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)

	return ok && ident.Name == "Event"
}

// identNames renders identifiers for an error message.
func identNames(idents []*ast.Ident) []string {
	names := make([]string, 0, len(idents))
	for _, ident := range idents {
		names = append(names, ident.Name)
	}

	return names
}

// sortedKeys and sortedSet keep failure messages in a stable order, so a diff
// of two failing runs is about the failures and not about map iteration.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func sortedSet(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
