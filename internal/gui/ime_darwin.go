//go:build darwin && !ci

package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>
int mpInstallInputMethod(uintptr_t window);
void mpSetCaretRect(double x, double y, double width, double height);
*/
import "C"

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// preeditSink receives the text being composed ("" when the composition ends)
// and the caret's offset in it, in UTF-16 units, or -1 for its end. It is set
// once, before the window takes input, and called on the main thread.
var preeditSink func(text string, cursor int)

//export mpPreeditChanged
func mpPreeditChanged(text *C.char, cursor C.long) {
	if sink := preeditSink; sink != nil {
		sink(C.GoString(text), int(cursor))
	}
}

// installInputMethod makes macOS open an input method's candidate window beside
// the caret instead of at the window's top-left corner, and reports the text
// being composed, which GLFW otherwise keeps to itself (see ime_darwin.m). The
// returned function stores the caret rectangle in window-content points.
func installInputMethod(w fyne.Window, onPreedit func(text string, cursor int)) (func(pos fyne.Position, size fyne.Size), error) {
	native, ok := w.(driver.NativeWindow)
	if !ok {
		return nil, fmt.Errorf("window does not support native input-method handling")
	}
	var handle C.uintptr_t
	native.RunNative(func(context any) {
		if mac, ok := context.(driver.MacWindowContext); ok {
			handle = C.uintptr_t(mac.NSWindow)
		}
	})
	if handle == 0 {
		return nil, fmt.Errorf("window has no native handle")
	}
	preeditSink = onPreedit
	if C.mpInstallInputMethod(handle) == 0 {
		preeditSink = nil
		return nil, fmt.Errorf("cannot attach to the input-method handling of the window")
	}
	return func(pos fyne.Position, size fyne.Size) {
		C.mpSetCaretRect(C.double(pos.X), C.double(pos.Y), C.double(size.Width), C.double(size.Height))
	}, nil
}
