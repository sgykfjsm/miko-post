package gui

import (
	"fmt"
	"io"

	"fyne.io/fyne/v2/app"
	posting "github.com/sgykfjsm/miko-post/internal/app"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/logging"
)

// Run deliberately accepts no settings-path argument: a CLI override cannot
// leak into the window constructor. The startup-error window remains T082.
func Run(errOut io.Writer) int {
	path, err := config.DefaultConfigPath()
	if err != nil {
		fmt.Fprintln(errOut, "mp: "+err.Error())
		return 1
	}
	settings, err := config.Load(path)
	if err != nil {
		fmt.Fprintln(errOut, "mp: "+err.Error())
		return 1
	}
	a := app.NewWithID("io.github.sgykfjsm.miko-post")
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
