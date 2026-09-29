package gui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	posting "github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
)

// Run opens the window and returns the process exit status (FR-002, FR-030).
//
// It deliberately accepts no settings-path argument: a CLI override cannot leak
// into the window constructor, because there is no parameter for it to arrive
// through (FR-005, constitution principle V, T078). The window always loads the
// default resolved settings; see windowSettings.
//
// A settings failure — unreadable, invalid, or no destination enabled — opens
// the startup-error window instead of the posting window, and the process exits
// 1 once it is dismissed (FR-030, T082). Nothing that could post has been built
// at that point (FR-058). The same text also goes to errOut, which costs nothing
// and is the only place it can be read when mp was started from a terminal and
// the window is dismissed without being read.
func Run(errOut io.Writer) int {
	return runWith(errOut, os.UserHomeDir, func() fyne.App { return app.NewWithID(appID) })
}

// runWith is Run with the two things a test cannot let it do for real: look up
// the home directory, and construct the Fyne application.
//
// Without a home directory, neither window opens (DEC-J1, #136). Fyne derives
// its preferences and storage directories from the home directory alone, and
// with none it creates Library/ and fyne/ relative to the working directory,
// whatever XDG_CONFIG_HOME says. So the check comes before the settings are
// loaded, and covers the posting window and the startup-error window alike:
// the reason is printed and the process exits 1 before the application exists.
// This extends DEC-H2, which covered only the case where no settings path could
// be resolved either.
//
// A home directory that is not an absolute path is refused the same way
// (DEC-J7). os.UserHomeDir returns $HOME without checking it, so HOME=tmp
// "resolves", and Fyne would then write Library/ under ./tmp: the same failure
// through another door. The message does not print the value, which can be
// arbitrarily long and says nothing the user cannot see with `echo $HOME`.
//
// The line in Run that supplies os.UserHomeDir stays untested, like the one
// that supplies the real constructor (see startupFailure): a test calling Run
// would have to be able to construct a real Fyne application, which needs a
// display. It is covered instead by the native probe recorded for DEC-J1,
// which launches the real binary without HOME from an empty directory and
// checks that it exits 1 and the directory stays empty.
func runWith(errOut io.Writer, homeDir func() (string, error), newApp func() fyne.App) int {
	home, err := homeDir()
	if err != nil {
		fmt.Fprintln(errOut, "mp: cannot open the window without a home directory ("+err.Error()+
			"); the window framework would write its storage into the current directory. "+
			"Set HOME and try again, or post from the command line")

		return 1
	}

	if !filepath.IsAbs(home) {
		fmt.Fprintln(errOut, "mp: cannot open the window: the home directory is not an absolute path, "+
			"so the window framework would write its storage relative to the current directory. "+
			"Set HOME to an absolute path and try again, or post from the command line")

		return 1
	}

	path, settings, err := windowSettings()
	if err != nil {
		return startupFailure(errOut, newApp, err, path)
	}
	a := newApp()
	logger := posting.OpenLogger(settings, logging.SourceGUI)
	defer logger.Close()
	service := posting.NewService(settings, posting.NewRecording(logger, settings.Logging))
	// FR-076's warning for the GUI front door.
	//
	// DegradedSoFar, never Degraded: this is consulted after every post for the
	// life of the window, and Degraded submits a flush barrier whose timeout
	// permanently disables the logger — so probing for a diagnostics failure
	// would itself be able to cause one, and would block the Fyne event
	// goroutine for up to 250ms each time. See logging.DegradedSoFar.
	//
	// logger.Close runs in the deferred call above, after a.Run returns and the
	// window is gone, so a close failure has no surface left to appear on here
	// — unlike the CLI, which closes before it renders. The open failure and
	// every latched write failure are the two this front door can report.
	w := newWindow(a.NewWindow("miko-post"), settings.GUI, service.Post, logger.Path(),
		&degradationWarning{degraded: logger.DegradedSoFar})
	w.native.Show()
	activity, release, err := observeActivity(w.native)
	if err != nil {
		fmt.Fprintln(errOut, "mp: "+err.Error())
		w.native.Close()
		return 1
	}
	w.activity, w.release = activity, release
	a.Run()
	return w.status
}

// appID is the Fyne application identifier, shared by both windows so they are
// one application to the platform.
const appID = "io.github.sgykfjsm.miko-post"

// windowSettings loads the settings the window posts with: always the default
// resolved path, never an override (FR-005). A function rather than an inline
// call so a test can assert which file a window launched after a CLI override
// actually reads (T076).
func windowSettings() (string, config.Settings, error) {
	return posting.LoadSettings("")
}

// startupFailure is Run's settings-failure branch: the error on errOut, the
// startup-error window until it is dismissed, then exit status 1 (FR-030,
// FR-058, T082, T083).
//
// Split out of Run and handed the application's constructor, so that the
// branch's obligations — report, show the error window rather than the posting
// one, return failure — are asserted by a test driving Fyne's headless app
// instead of resting on a reading of the code. What stays untested is the one
// line in Run that supplies the real constructor, which needs a display.
//
// With no resolved path — $HOME unset and XDG_CONFIG_HOME unset too — there is
// no window, only the stderr line and exit 1, and the application is never
// constructed. Constructing it is not free: Fyne derives its preferences and
// storage directories from the home directory, and with none it creates them
// relative to the working directory, so a launch that has already decided it
// cannot run would leave a Library/ and a fyne/ tree wherever it was started.
// FR-030's window exists to show the resolved path; without one it has nothing
// to show that stderr does not, and not writing outside the application's own
// locations outranks it. A constructor rather than an application is taken for
// exactly this reason: an application passed in would already have written.
//
// Reached through Run, that branch is now pre-empted: no resolvable path means
// HOME is unset, and runWith's home-directory check (DEC-J1) refuses before the
// settings are loaded. It stays as this function's own guarantee, which its
// tests assert directly.
func startupFailure(errOut io.Writer, newApp func() fyne.App, err error, path string) int {
	fmt.Fprintln(errOut, "mp: "+err.Error())

	if path == "" {
		return 1
	}

	a := newApp()
	newErrorWindow(a.NewWindow("miko-post"), err.Error(), path).native.Show()
	a.Run()

	return 1
}
