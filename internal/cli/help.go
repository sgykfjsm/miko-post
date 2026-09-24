package cli

import (
	"fmt"
	"io"

	"github.com/sgykfjsm/miko-post/internal/config"
)

// helpFormat is contracts/cli-interface.md's help output, with the one value
// that varies left as a verb (FR-007).
//
// Kept as a single literal so the text can be compared against the contract by
// eye, which is the only review it will ever get: help output has no behaviour
// to test beyond "it says what the contract says".
const helpFormat = `Usage:
  mp [options] [message...]

If no message is specified, the GUI is launched.

Options:
  -c, --config PATH
        Path to the configuration file for CLI posting.
        Default: %s

  -h, --help
        Show this help.
`

// Help prints the help text and returns the exit status for it (FR-007).
//
// The default path is resolved here, for the current environment, and printed
// as resolved — never as a $XDG_CONFIG_HOME expression. That is the whole of
// FR-007's content requirement: the reader wants to know which file the tool
// will read on this machine, and an unexpanded variable answers a question they
// did not ask. It is config.DefaultConfigPath, the same function both front
// doors load from, so the help cannot name a file the program does not read.
//
// Resolution can fail — only when $HOME is unset and XDG_CONFIG_HOME is not
// set either — and help is still printed, with the reason in place of the
// path, and still exits 0. The user asked how to use the command, and that
// question has an answer even when the default location has none; failing the
// help would hide the -c option, which is exactly what that user needs.
func Help(out io.Writer) int {
	fmt.Fprintf(out, helpFormat, defaultPathForHelp())

	return ExitSuccess
}

// defaultPathForHelp is the value Help prints for "Default:".
func defaultPathForHelp() string {
	path, err := config.DefaultConfigPath()
	if err != nil {
		return "could not be resolved (" + err.Error() + ")"
	}

	return path
}
