package gui

import (
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/sgykfjsm/miko-post/internal/config"
	"github.com/sgykfjsm/miko-post/internal/post"
)

type timer interface{ Stop() bool }

// All controller state belongs to Fyne's event goroutine. Workers deliver an
// immutable outcome through dispatch; even an already queued timer is invalidated
// by its generation, so stopping a timer never races a second submission.
type window struct {
	native                fyne.Window
	entry                 *messageEntry
	send                  *commandButton
	result                *widget.Label
	post                  func(post.Message) post.Outcome
	settings              config.GUISettings
	logPath               string
	dispatch              func(func())
	after                 func(time.Duration, func()) timer
	activity              func() uint64
	release               func()
	pending               timer
	generation            uint64
	busy, closing, closed bool
	status                int
}

func newWindow(native fyne.Window, settings config.GUISettings, postMessage func(post.Message) post.Outcome, logPath string) *window {
	w := &window{native: native, settings: settings, post: postMessage, logPath: logPath,
		dispatch: fyne.Do, after: func(d time.Duration, f func()) timer { return time.AfterFunc(d, f) },
		activity: func() uint64 { return 0 }, release: func() {}, status: 1}
	w.entry = newMessageEntry(w.submit, w.close)
	w.entry.SetPlaceHolder("What’s on your mind?")
	w.send = newCommandButton("Send", w.submit, w.close)
	cancel := newCommandButton("Cancel", w.close, w.close)
	w.result = widget.NewLabel("")
	w.result.Wrapping = fyne.TextWrapWord
	native.SetContent(container.NewBorder(nil, container.NewVBox(w.result, container.NewHBox(w.send, cancel)), nil, nil, w.entry))
	native.Resize(fyne.NewSize(440, 260))
	native.SetCloseIntercept(w.close)
	native.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierSuper}, func(fyne.Shortcut) { w.submit() })
	native.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierSuper}, func(fyne.Shortcut) { w.close() })
	native.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
		if k.Name == fyne.KeyEscape {
			w.close()
		}
	})
	native.Canvas().Focus(w.entry)
	return w
}

func (w *window) stopTimer() {
	w.generation++
	if w.pending != nil {
		w.pending.Stop()
		w.pending = nil
	}
}

func (w *window) submit() {
	if w.busy || w.closing || w.closed {
		return
	}
	w.stopTimer()
	message := post.Message{Original: w.entry.Text}
	if err := message.Validate(); err != nil {
		w.status = 1
		w.result.SetText(correction(err))
		return
	}
	w.busy = true
	w.send.Disable()
	w.result.SetText("Sending…")
	go func() {
		outcome := w.post(message)
		w.dispatch(func() { w.finished(outcome) })
	}()
}

func (w *window) finished(outcome post.Outcome) {
	w.busy = false
	w.status = 1
	delay := w.settings.ErrorCloseSeconds
	if outcome.Succeeded() {
		w.status = 0
		delay = w.settings.SuccessCloseSeconds
	}
	if w.closing {
		w.close()
		return
	}
	w.send.Enable()
	w.result.SetText(resultText(outcome, w.logPath))
	generation, activity := w.generation, w.activity()
	w.pending = w.after(closeDelay(delay), func() {
		w.dispatch(func() {
			if w.closed || w.closing || generation != w.generation {
				return
			}
			// Native events count even when a widget consumes them, including clicks
			// on scroll bars, empty space and the title bar. An interaction permanently
			// cancels this result's deadline; it does not restart the countdown.
			if activity != w.activity() {
				w.stopTimer()
				return
			}
			w.close()
		})
	})
}

func (w *window) close() {
	if w.closed {
		return
	}
	w.stopTimer()
	w.closing = true
	if w.busy {
		// Dismiss immediately, but keep the event loop and logger alive until every
		// sink has an outcome. Closing the last native window would quit too early.
		w.native.Hide()
		return
	}
	w.closed = true
	w.release()
	w.native.Close()
}

// Saturate before multiplication: settings admit arbitrarily large nonnegative
// seconds, which must never wrap into an immediate (negative) timer.
func closeDelay(seconds int) time.Duration {
	if uint64(seconds) > uint64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds) * time.Second
}
