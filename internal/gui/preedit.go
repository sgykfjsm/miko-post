package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// preeditView draws the text an input method is composing, which the platform
// driver does not hand to Entry until it is confirmed. It is a layer above the
// editor that never touches the message text, so a conversion that is still
// open cannot end up in a post. It draws over any text to the right of the
// caret, which is acceptable for a short note typed at its end.
type preeditView struct {
	layer      *fyne.Container
	background *canvas.Rectangle
	text       *canvas.Text
	underline  *canvas.Line
	caret      *canvas.Line
}

func newPreeditView() *preeditView {
	dark := backgroundTheme{theme.DefaultTheme()}
	p := &preeditView{
		background: canvas.NewRectangle(dark.Color(theme.ColorNameSelection, theme.VariantDark)),
		text:       canvas.NewText("", dark.Color(theme.ColorNameForeground, theme.VariantDark)),
		underline:  canvas.NewLine(dark.Color(theme.ColorNameForeground, theme.VariantDark)),
		caret:      canvas.NewLine(dark.Color(theme.ColorNameForeground, theme.VariantDark)),
	}
	p.underline.StrokeWidth = 1
	p.caret.StrokeWidth = 2
	p.layer = container.NewWithoutLayout(p.background, p.text, p.underline, p.caret)
	p.hide()
	return p
}

func (p *preeditView) hide() {
	p.background.Hide()
	p.text.Hide()
	p.underline.Hide()
	p.caret.Hide()
}

// show draws text at the caret, kept inside a window of the given width, with
// its own caret at cursor (UTF-16 units into text, negative for the end). The
// entry's caret stays where the composition began, under this view, so this one
// stands in for it.
func (p *preeditView) show(text string, cursor int, caret fyne.Position, lineHeight, windowWidth float32) {
	if text == "" {
		p.hide()
		return
	}
	p.text.Text = text
	p.text.TextSize = theme.TextSize()
	size := p.text.MinSize()
	edge := theme.Padding()
	x := min(caret.X, windowWidth-edge-size.Width)
	x = max(x, edge)
	p.background.Move(fyne.NewPos(x, caret.Y))
	p.background.Resize(fyne.NewSize(size.Width, lineHeight))
	p.text.Move(fyne.NewPos(x, caret.Y+(lineHeight-size.Height)/2))
	p.text.Resize(size)
	bottom := caret.Y + lineHeight - 1
	p.underline.Position1 = fyne.NewPos(x, bottom)
	p.underline.Position2 = fyne.NewPos(x+size.Width, bottom)
	before := text[:byteOffset(text, cursor)]
	at := x + fyne.MeasureText(before, p.text.TextSize, p.text.TextStyle).Width
	p.caret.Position1 = fyne.NewPos(at, caret.Y+1)
	p.caret.Position2 = fyne.NewPos(at, caret.Y+lineHeight-1)
	p.caret.Show()
	p.background.Show()
	p.text.Show()
	p.underline.Show()
	p.layer.Refresh()
}

// byteOffset converts an offset in UTF-16 units into a byte offset in s. A
// negative or too-large offset means the end of s, and one that falls inside a
// surrogate pair rounds up to the end of that character.
func byteOffset(s string, utf16Units int) int {
	if utf16Units < 0 {
		return len(s)
	}
	units := 0
	for i, r := range s {
		if units >= utf16Units {
			return i
		}
		units++
		if r >= 0x10000 {
			units++
		}
	}
	return len(s)
}
