//go:build darwin && !ci

package gui

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>
void *mpObserve(uintptr_t window);
uint64_t mpActivity(void *observer);
void mpRelease(void *observer);
*/
import "C"

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// Fyne has no public window-wide mouse observer. A macOS local event monitor
// observes without consuming events, and a window notification observes focus.
// No event content crosses into Go: only a monotonically increasing counter.
func observeActivity(w fyne.Window) (func() uint64, func(), error) {
	native, ok := w.(driver.NativeWindow)
	if !ok {
		return nil, nil, fmt.Errorf("window does not support native activity observation")
	}
	var handle C.uintptr_t
	native.RunNative(func(context any) {
		if mac, ok := context.(driver.MacWindowContext); ok {
			handle = C.uintptr_t(mac.NSWindow)
		}
	})
	if handle == 0 {
		return nil, nil, fmt.Errorf("window has no native handle")
	}
	observer := C.mpObserve(handle)
	if observer == nil {
		return nil, nil, fmt.Errorf("cannot observe window interaction")
	}
	return func() uint64 { return uint64(C.mpActivity(observer)) }, func() { C.mpRelease(observer) }, nil
}
