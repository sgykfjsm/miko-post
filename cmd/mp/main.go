// Command mp posts one short message to two independent destinations: a
// Telegram chat and an Obsidian daily note.
//
// Running mp with message arguments posts from the command line; running it
// with none opens a small window.
package main

import (
	"fmt"
	"os"

	"github.com/sgykfjsm/miko-post/internal/version"
)

// Scaffolding only. Argument parsing, settings loading, logging, and the
// posting service are wired up in T039; until then this reports that it is
// not yet functional rather than pretending to post.
func main() {
	fmt.Fprintf(os.Stderr, "mp %s (%s): not yet implemented\n", version.Version(), version.Commit())
	os.Exit(1)
}
