package logging_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
// the two sets to agree in both directions. It deliberately does not skip
// anything it cannot classify: a const declared in an Event group without the
// explicit type is reported as a defect rather than passed over, because an
// untyped string constant is still assignable to Event and would be usable at
// a log call site while escaping this scan.
func TestEveryDeclaredEventConstantIsRegistered(t *testing.T) {
	t.Parallel()

	declared := declaredEventConstants(t)

	if len(declared) == 0 {
		// Reaching here means the scan found nothing, which is far more likely
		// to be a broken scan than a package with no events. Failing loudly is
		// the difference between this test and one that silently passes.
		t.Fatal("no Event constants were found in the package source; the scan is broken")
	}

	registered := make(map[string]bool, len(logging.AllEvents()))
	for _, event := range logging.AllEvents() {
		registered[string(event)] = true
	}

	for name, value := range declared {
		if !registered[value] {
			t.Errorf("constant %s = %q is declared but missing from AllEvents", name, value)
		}
	}

	declaredValues := make(map[string]bool, len(declared))
	for _, value := range declared {
		declaredValues[value] = true
	}

	for value := range registered {
		if !declaredValues[value] {
			t.Errorf("AllEvents contains %q, which no Event constant declares", value)
		}
	}
}

// declaredEventConstants parses the package's non-test sources and returns
// every constant declared with the Event type, as constant name to value.
//
// The package directory is the test's working directory, so "." is the package
// under test. Files are listed and parsed individually rather than through
// parser.ParseDir, which returns the deprecated ast.Package.
func declaredEventConstants(t *testing.T) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	found := make(map[string]string)
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

		collectEventConstants(t, name, file, found)
	}

	if parsed == 0 {
		t.Fatal("no non-test Go files were found to scan")
	}

	return found
}

// collectEventConstants adds every Event-typed constant in file to found.
func collectEventConstants(t *testing.T, filename string, file *ast.File, found map[string]string) {
	t.Helper()

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}

		if !groupDeclaresEvents(genDecl) {
			continue
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			if !isEventType(valueSpec.Type) {
				// Inside a group that declares Event constants, a spec without
				// the explicit type is an untyped string constant: still
				// assignable to Event, still usable at a log call site, and
				// invisible to this scan. Report it instead of skipping.
				for _, ident := range valueSpec.Names {
					t.Errorf("%s: constant %s sits in an Event const group without the explicit Event type; "+
						"declare it as `%s Event = …` or move it to its own group",
						filename, ident.Name, ident.Name)
				}

				continue
			}

			collectSpecValues(t, filename, valueSpec, found)
		}
	}
}

// collectSpecValues records one Event-typed spec's name/value pairs.
func collectSpecValues(t *testing.T, filename string, spec *ast.ValueSpec, found map[string]string) {
	t.Helper()

	if len(spec.Names) != len(spec.Values) {
		// An Event constant with no value of its own repeats the previous
		// spec, or is an iota form. Neither is a shape this scan can read a
		// name out of, and neither belongs in an event vocabulary.
		t.Errorf("%s: Event constants %v have %d names and %d values; give each one its own string literal",
			filename, identNames(spec.Names), len(spec.Names), len(spec.Values))

		return
	}

	for i, ident := range spec.Names {
		literal, ok := spec.Values[i].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			t.Errorf("%s: constant %s is not a plain string literal; the log contract needs a literal name",
				filename, ident.Name)

			continue
		}

		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Errorf("%s: constant %s has an unreadable literal %s: %v",
				filename, ident.Name, literal.Value, err)

			continue
		}

		if previous, duplicate := found[ident.Name]; duplicate {
			t.Errorf("%s: constant %s is declared twice (%q and %q)",
				filename, ident.Name, previous, value)

			continue
		}

		found[ident.Name] = value
	}
}

// isEventType reports whether a spec's type is the bare identifier Event.
func isEventType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)

	return ok && ident.Name == "Event"
}

// groupDeclaresEvents reports whether any spec in a const group carries the
// Event type, which is what makes the group's untyped specs suspicious.
func groupDeclaresEvents(decl *ast.GenDecl) bool {
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if ok && isEventType(valueSpec.Type) {
			return true
		}
	}

	return false
}

// identNames renders identifiers for an error message.
func identNames(idents []*ast.Ident) []string {
	names := make([]string, 0, len(idents))
	for _, ident := range idents {
		names = append(names, ident.Name)
	}

	return names
}
