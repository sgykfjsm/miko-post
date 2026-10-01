package gui

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// messageEntry preserves Entry's editing and IME support while handling the
// commands that the focused entry would otherwise consume (research R-003).
type messageEntry struct {
	widget.Entry
	submit, cancel func()
	shiftHeld      func() bool

	// caretMoved, once set, receives the caret's rectangle in window-content
	// points so the platform can place an input method's candidate window
	// beside it.
	caretMoved func(pos fyne.Position, size fyne.Size)

	// composing is true while an input method holds uncommitted text. The
	// driver delivers the keys that steer the conversion (Backspace, arrows,
	// Return, Esc) to the entry as well as to the input method, so while this
	// is set the entry must not act on them. Atomic because the platform sets
	// it from inside the same key event that then reads it.
	composing atomic.Bool
}

func newMessageEntry(submit, cancel func()) *messageEntry {
	e := &messageEntry{submit: submit, cancel: cancel, shiftHeld: shiftHeld}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	e.OnCursorChanged = e.reportCaret
	return e
}

// reportCaret hands the caret's line to caretMoved. The vertical position is
// clamped to the visible entry because Entry.CursorPosition ignores scrolling.
func (e *messageEntry) reportCaret() {
	if e.caretMoved == nil {
		return
	}
	e.caretMoved(e.caretRect())
}

// caretRect is the caret's line in window-content points.
func (e *messageEntry) caretRect() (fyne.Position, fyne.Size) {
	origin := fyne.CurrentApp().Driver().AbsolutePositionForObject(e)
	line := fyne.MeasureText("あ", e.Theme().Size(theme.SizeNameText), fyne.TextStyle{}).Height
	at := e.CursorPosition()
	y := min(max(at.Y, 0), max(e.Size().Height-line, 0))
	return origin.Add(fyne.NewPos(at.X, y)), fyne.NewSize(1, line)
}

// Resize and Move keep the reported caret right when the layout changes.
func (e *messageEntry) Resize(size fyne.Size) {
	e.Entry.Resize(size)
	e.reportCaret()
}

func (e *messageEntry) Move(pos fyne.Position) {
	e.Entry.Move(pos)
	e.reportCaret()
}

func (e *messageEntry) FocusGained() {
	e.Entry.FocusGained()
	e.reportCaret()
}

func (e *messageEntry) TypedShortcut(s fyne.Shortcut) {
	if command, ok := s.(*desktop.CustomShortcut); ok && command.Modifier == fyne.KeyModifierSuper {
		switch command.KeyName {
		case fyne.KeyReturn:
			e.submit()
			return
		case fyne.KeyQ:
			e.cancel()
			return
		}
	}
	e.Entry.TypedShortcut(s)
}

func (e *messageEntry) TypedKey(k *fyne.KeyEvent) {
	if e.composing.Load() {
		return
	}
	if k.Name == fyne.KeyEscape {
		e.cancel()
		return
	}
	// Only Shift+Return inserts a line break. The macOS driver hands the Return
	// that confirms an IME conversion to the entry as an ordinary key press, so
	// a plain Return would add a stray line break before the committed text.
	// Fyne cannot say whether a composition is active, hence the blanket rule.
	if (k.Name == fyne.KeyReturn || k.Name == fyne.KeyEnter) && !e.shiftHeld() {
		return
	}
	e.Entry.TypedKey(k)
}

func shiftHeld() bool {
	d, ok := fyne.CurrentApp().Driver().(desktop.Driver)
	return ok && d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0
}

// commandButton makes Esc dismiss the window even when a button holds keyboard
// focus, rather than disappearing into Button.TypedKey.
//
// That state is reachable in the startup-error window, which focuses its Quit
// button itself. In the posting window it is not reachable through user input
// (Batch 12, T091 window checks): the multi-line entry consumes Tab, and
// Fyne's Button.Tapped calls Focus(nil), so a click leaves nothing focused.
// There it is a defensive path, kept so that a layout change which does let a
// button take focus cannot bring back the dead Esc Batch 11 found.
type commandButton struct {
	widget.Button
	cancel func()
}

func newCommandButton(label string, tapped, cancel func()) *commandButton {
	b := &commandButton{cancel: cancel}
	b.Text, b.OnTapped = label, tapped
	b.ExtendBaseWidget(b)
	return b
}

func (b *commandButton) TypedKey(k *fyne.KeyEvent) {
	if k.Name == fyne.KeyEscape {
		b.cancel()
		return
	}
	b.Button.TypedKey(k)
}
