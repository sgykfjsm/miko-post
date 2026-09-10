// Package app is the composition root: the one place that turns a validated
// config.Settings into the objects a front door runs with — the enabled sinks,
// the posting service, and the diagnostic logger.
//
// It exists because there is nowhere else the wiring can live (decision DEC-D1).
// tasks.md placed sink construction in internal/post/build.go, and that file
// cannot compile: both sink packages import internal/post in order to implement
// post.Sink, so internal/post constructing them is an import cycle. cmd/mp is
// the obvious alternative and is wrong for its own reason — internal/gui cannot
// import package main, so the GUI would need a second copy of the wiring, and
// issue #111's cheapest fix moves sink construction onto the GUI's submit path
// rather than its constructor. internal/sink, the empty parent package, could
// host sink construction and nothing else, which leaves the wiring in two
// places. One composition root beats two.
//
// # The import rule this package lives under
//
// This package may import anything under internal/. Nothing may import it
// except a front door and cmd/mp, and it must never import a front door
// (internal/cli, internal/gui) — that is the same cycle arriving from the other
// side, and it is the failure mode a composition root is most prone to. It also
// must not become a place that decides anything: sink selection is FR-016 and
// belongs here, but what a sink does, what the orchestrator does, and what a
// front door prints all stay where they are.
//
// The rule is enforced rather than stated. layering_test.go walks the module's
// own import graph from source and fails on a violation, which is what plan.md's
// Structure Decision means by "the package boundaries are the mechanism by which
// constitution principle II is enforced by the compiler rather than by review" —
// a boundary that only the reviewer enforces is a convention, and this project
// has one boundary (internal/post's) whose violation would be silent.
package app
