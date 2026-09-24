package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// errorWindow is FR-030's minimal startup-error window: the settings could not
// be used, so there is nothing to post with, and this is how the windowed
// front door says so (T082, T083, FR-058).
//
// It shows two things and offers one action. The actionable message, which is
// the settings error's own text — config.Load's and config.RequireDestination's
// errors are written to be read by the user, each naming the key or file at
// fault — and the resolved settings path on a line of its own, because the fix
// is almost always "open this file" and the path is what the user has to find.
// The action is Quit.
//
// The labels are not selectable. A selectable label takes focus, and focus is
// what routes Esc (see the Quit button below), so making the path copyable is a
// change to dismissal as much as to display; the same text is on stderr for a
// user who started mp from a terminal. path is never empty here: startupFailure
// opens no window when the path could not be resolved.
//
// It has no message field, and that is the requirement rather than minimalism:
// a field would invite typing a post that could not be sent, which is the
// worst outcome available to a window whose whole purpose is quick capture.
//
// Every way of dismissing it — Quit, the close box, Esc, Cmd+Q — does the same
// thing, and the process exits 1 whichever is used, since the startup failed
// however the window was closed.
type errorWindow struct {
	native fyne.Window
}

func newErrorWindow(native fyne.Window, message, path string) *errorWindow {
	w := &errorWindow{native: native}

	text := widget.NewLabel(message)
	text.Wrapping = fyne.TextWrapWord

	location := widget.NewLabel("Settings file: " + path)
	location.Wrapping = fyne.TextWrapBreak

	// A commandButton, not a plain widget.Button: Quit has the focus, and the
	// driver delivers a key to the focused widget rather than to the canvas,
	// so Esc reaches the canvas handler below only when nothing is focused. A
	// plain Button handles Space and nothing else, which made Esc dead in the
	// real driver while a test calling the canvas handler directly passed. The
	// posting window's Send and Cancel buttons are commandButtons for the same
	// reason (R-003).
	quit := newCommandButton("Quit", w.close, w.close)
	quit.Importance = widget.HighImportance

	native.SetContent(container.NewBorder(nil, container.NewHBox(quit), nil, nil,
		container.NewVBox(widget.NewLabel("miko-post cannot start"), text, location)))
	native.Resize(fyne.NewSize(440, 220))
	native.SetCloseIntercept(w.close)
	native.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierSuper},
		func(fyne.Shortcut) { w.close() })
	// Esc while nothing has focus, which a click on empty space produces.
	native.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			w.close()
		}
	})
	native.Canvas().Focus(quit)

	return w
}

// close dismisses the window. Closing the only window ends the Fyne event loop,
// so a.Run returns and Run reports the failure status.
func (w *errorWindow) close() { w.native.Close() }
