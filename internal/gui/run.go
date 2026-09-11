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
	service := posting.NewService(settings, posting.NewRecording(logger))
	w := newWindow(a.NewWindow("miko-post"), settings.GUI, service.Post, logger.Path())
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
