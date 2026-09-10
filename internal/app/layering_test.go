package app_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// modulePath is the module under test. Read from go.mod rather than written
// down, so a rename cannot leave this file quietly asserting nothing: every
// intra-module edge is recognised by this prefix, and a stale prefix would make
// the graph look empty and every rule below vacuously true.
var modulePath = readModulePath()

// The two layering rules this repository actually depends on.
//
// plan.md's Structure Decision says the package boundaries "are the mechanism by
// which constitution principle II is enforced by the compiler rather than by
// review". That is true of most of them — internal/gui cannot import package
// main, the sinks cannot import a front door without a cycle — but it is not
// true of these two, and both are load-bearing:
//
//   - internal/post importing another internal package compiles perfectly.
//     service.go's own comment cites the invariant as the reason Service takes a
//     time.Duration rather than a config.Settings, and internal/logging imports
//     internal/config, so a single `post -> logging` edge would reintroduce
//     `post -> config` transitively: the exact dependency that design choice
//     refuses, arriving by the back door (decision DEC-D2).
//   - internal/app importing a front door compiles too, right up until the front
//     door imports app back — which it does, because that is what a composition
//     root is for. The cycle is then a build failure in a package nobody was
//     editing, and the obvious local fix is to move wiring back out of app,
//     which is the arrangement DEC-D1 exists to prevent.
//
// A rule that only a reviewer enforces is a convention. This test is the
// enforcement.
var layeringRules = []struct {
	// pkg is the package the rule constrains, as an intra-module import path.
	pkg string

	// forbidden are the intra-module packages pkg must not reach, directly or
	// transitively. Empty means "no intra-module import at all".
	forbidden []string

	// why is printed with a failure, because the next person to hit this is
	// mid-refactor and needs the reason more than the rule.
	why string
}{
	{
		pkg:       "internal/post",
		forbidden: nil,
		why: "internal/post is the posting core and imports no other internal package (DEC-D2). " +
			"service.go cites this as the reason Service takes a time.Duration rather than a " +
			"config.Settings; internal/logging imports internal/config, so post -> logging " +
			"reintroduces post -> config transitively.",
	},
	{
		pkg:       "internal/app",
		forbidden: []string{"internal/cli", "internal/gui"},
		why: "internal/app is the composition root and must not import a front door (DEC-D1). " +
			"The front doors import app, so an edge back is an import cycle whose cheapest " +
			"local fix is to scatter the wiring again.",
	},
}

// TestTheLayeringRulesHold walks the module's own import graph and enforces the
// rules above.
//
// It reads the source rather than shelling out to `go list -deps`, so it cannot
// be defeated by a build-tag combination that happens not to be selected on the
// machine running it: every non-test .go file in the tree contributes its
// imports, whatever its tags say, which makes the rule stricter than the build
// and is the right direction for a rule about what may exist at all.
//
// Test files are excluded. A test may import anything — internal/post's own
// tests are in-package and this very file imports the tree — and a rule that
// caught them would be a rule nobody could keep.
func TestTheLayeringRulesHold(t *testing.T) {
	t.Parallel()

	graph := importGraph(t)

	// A graph this small is a mistake, not a clean bill of health: a wrong
	// module path, a wrong root, or a walk that found nothing all produce a
	// passing suite with every rule vacuous. Four packages is well under the
	// tree's real size and well over what a broken walk returns.
	if len(graph) < 4 {
		t.Fatalf("the import graph has %d packages, which means the walk found nothing; "+
			"module path %q, packages %v", len(graph), modulePath, sortedKeys(graph))
	}

	// Edges that are true today, asserted so that a graph with no edges at all
	// cannot make every rule below vacuously pass. A package count alone does
	// not catch that: a walk that recorded every directory and dropped every
	// import produces the same length and answers "reaches nothing" to
	// everything.
	//
	// The last row is the one that exercises reachable() past its first step,
	// and it has to be checked against the real graph rather than assumed. The
	// row below it used to be labelled transitive and is not: internal/cli
	// imports internal/config directly, at run.go, so it is satisfied at depth
	// one — and with every row satisfied at depth one, a reachable() that
	// dropped the queue extension and returned direct edges only passed this
	// whole test. Only the synthetic graph in TestTheLayeringRulesCanFail
	// caught it, which means the walk over the real tree was asserting less
	// than it looked like.
	//
	// cmd/mp -> internal/sink/telegram is three hops in the real graph
	// (cmd/mp -> internal/cli -> internal/app -> internal/sink/telegram) and is
	// the shape the rules are about rather than an arbitrary long pair: the
	// program's entry point reaches a destination only through a front door and
	// the composition root, so it cannot become a one-hop edge without one of
	// those layers being bypassed — which is a layering change, and would be
	// noticed here.
	mustReach := []struct{ from, to string }{
		{from: "internal/app", to: "internal/config"},
		{from: "internal/app", to: "internal/sink/telegram"},
		{from: "internal/logging", to: "internal/config"},
		{from: "internal/app", to: "internal/post"},
		// Direct, at run.go. Kept because the edge is real and worth pinning,
		// relabelled because it is not the transitive one.
		{from: "internal/cli", to: "internal/config"},
		{from: "cmd/mp", to: "internal/sink/telegram"},
	}

	for _, edge := range mustReach {
		if _, ok := reachable(graph, edge.from)[edge.to]; !ok {
			t.Fatalf("the graph does not have %s -> %s, which is true in the source; "+
				"the walk is dropping edges and every rule below is vacuous",
				edge.from, edge.to)
		}
	}

	for _, rule := range layeringRules {
		t.Run(rule.pkg, func(t *testing.T) {
			t.Parallel()

			if _, ok := graph[rule.pkg]; !ok {
				t.Fatalf("%s is not in the graph, so this rule asserts nothing; found %v",
					rule.pkg, sortedKeys(graph))
			}

			reached := reachable(graph, rule.pkg)

			if rule.forbidden == nil {
				if len(reached) != 0 {
					t.Errorf("%s reaches %v.\n%s", rule.pkg, sortedKeys(reached), rule.why)
				}

				return
			}

			for _, banned := range rule.forbidden {
				if path, ok := reached[banned]; ok {
					t.Errorf("%s reaches %s via %s.\n%s",
						rule.pkg, banned, strings.Join(path, " -> "), rule.why)
				}
			}
		})
	}
}

// TestTheLayeringRulesCanFail is the mutant this file would otherwise need one
// of, run in-process.
//
// Every assertion above is of the form "X does not happen", and the fixture is
// the repository itself — which, when the rules hold, cannot be made to produce
// X. So the graph walker and the reachability search would both be untested by
// a green run: a reachable() that returned nothing, or an importGraph that
// dropped every edge, would pass every rule in the table for the wrong reason.
// This drives the same two functions over a synthetic graph that does violate
// both shapes.
func TestTheLayeringRulesCanFail(t *testing.T) {
	t.Parallel()

	// post -> logging -> config is DEC-D2's exact transitive violation, and
	// app -> cli is DEC-D1's direct one.
	graph := map[string][]string{
		"internal/post":    {"internal/logging"},
		"internal/logging": {"internal/config"},
		"internal/config":  nil,
		"internal/app":     {"internal/cli"},
		"internal/cli":     nil,
	}

	reached := reachable(graph, "internal/post")

	if _, ok := reached["internal/config"]; !ok {
		t.Errorf("the transitive edge post -> logging -> config was not found; "+
			"reachable() reported %v", sortedKeys(reached))
	}

	if got := reached["internal/config"]; len(got) != 3 {
		t.Errorf("the reported path is %v, want three hops so a failure names the route", got)
	}

	if _, ok := reachable(graph, "internal/app")["internal/cli"]; !ok {
		t.Error("the direct edge app -> cli was not found")
	}

	// And the negative, so a reachable() that answered "yes" to everything
	// would fail here rather than passing both halves.
	if _, ok := reachable(graph, "internal/config")["internal/post"]; ok {
		t.Error("reachable() found an edge config -> post that does not exist")
	}
}

// importGraph maps every package in the module to the intra-module packages it
// imports directly. Keys and values are module-relative ("internal/post"), which
// is what the rules are written in and what a failure message should print.
func importGraph(t *testing.T) map[string][]string {
	t.Helper()

	root := moduleRoot(t)
	graph := make(map[string][]string)
	fileSet := token.NewFileSet()

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			// Directories the toolchain itself ignores, plus the ones this
			// repository keeps tooling and fixtures in. Descending into them
			// would add packages that are not part of the module's layering.
			switch entry.Name() {
			case ".git", ".serena", ".specify", ".claude", "bin", "testdata", "vendor":
				return fs.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		relDir, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}

		pkg := filepath.ToSlash(relDir)
		if _, seen := graph[pkg]; !seen {
			// Recorded even when it imports nothing intra-module, so a rule
			// naming a leaf package still finds it in the graph rather than
			// failing as "not present".
			graph[pkg] = nil
		}

		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}

			local, ok := strings.CutPrefix(imported, modulePath+"/")
			if !ok {
				continue
			}

			if !slices.Contains(graph[pkg], local) {
				graph[pkg] = append(graph[pkg], local)
			}
		}

		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk the module source: %v", walkErr)
	}

	return graph
}

// reachable returns every intra-module package reachable from start, mapped to
// one path that reaches it, start included as the first hop.
//
// The path is carried rather than only the set because a transitive violation is
// useless to act on without it: "internal/post reaches internal/config" invites a
// grep of internal/post that finds nothing, while "via internal/post ->
// internal/logging -> internal/config" names the edge to delete.
//
// Breadth-first, so the reported path is a shortest one, and the visited set
// makes it terminate on a cyclic graph — which the Go build would reject, but
// this function reads source and must not hang on a tree that does not compile.
func reachable(graph map[string][]string, start string) map[string][]string {
	found := make(map[string][]string)
	queue := [][]string{{start}}
	visited := map[string]bool{start: true}

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		for _, next := range graph[path[len(path)-1]] {
			if visited[next] {
				continue
			}

			visited[next] = true

			extended := append(append([]string(nil), path...), next)
			found[next] = extended
			queue = append(queue, extended)
		}
	}

	return found
}

// moduleRoot is the directory holding go.mod, found by walking up from this
// package rather than assuming a fixed number of "..".
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}

		dir = parent
	}
}

// readModulePath reads the module line out of go.mod.
//
// It panics rather than taking a *testing.T because it initialises a package
// variable; there is no test to fail yet, and a module path that cannot be read
// makes every rule in this file meaningless.
func readModulePath() string {
	dir, err := os.Getwd()
	if err != nil {
		panic("layering_test: working directory: " + err.Error())
	}

	for {
		raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			for line := range strings.SplitSeq(string(raw), "\n") {
				if path, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return strings.TrimSpace(path)
				}
			}

			panic("layering_test: go.mod has no module line")
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			panic("layering_test: no go.mod above the test's working directory")
		}

		dir = parent
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
