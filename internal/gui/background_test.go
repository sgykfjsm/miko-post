package gui

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func backgroundFixture(t *testing.T, dir, name string, c color.Color) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, c)
		}
	}
	if filepath.Ext(name) == ".jpg" {
		err = jpeg.Encode(f, img, nil)
	} else {
		err = png.Encode(f, img)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestBackgroundSelectionAndFallback(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{"", filepath.Join(dir, "missing"), dir} {
		if chooseBackground(path) != nil {
			t.Fatalf("expected fallback for %q", path)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.png"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	backgroundFixture(t, nested, "ignored.png", color.White)
	if chooseBackground(dir) != nil {
		t.Fatal("broken or nested image selected")
	}
	red := backgroundFixture(t, dir, "red.PNG", color.RGBA{R: 255, A: 255})
	if img := chooseBackground(dir); img == nil || img.Bounds().Dx() != 32 {
		t.Fatal("usable image not selected after broken candidate")
	}
	backgroundFixture(t, dir, "blue.jpg", color.RGBA{B: 255, A: 255})
	seen := map[bool]bool{}
	for range 64 {
		img := chooseBackground(dir)
		if img == nil {
			t.Fatal("lost usable images")
		}
		r, _, b, _ := img.At(0, 0).RGBA()
		seen[r > b] = true
	}
	if len(seen) != 2 {
		t.Fatal("selection never varied across 64 launches")
	}
	oversized := filepath.Join(dir, "oversize.png")
	f, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxBackgroundBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if readBackground(oversized) != nil || readBackground(dir) != nil {
		t.Fatal("unsafe file accepted")
	}
	links := t.TempDir()
	if err = os.Symlink(red, filepath.Join(links, "link.png")); err != nil {
		t.Fatal(err)
	}
	if chooseBackground(links) != nil {
		t.Fatal("symlink selected")
	}
}
func TestBackgroundLayerPreservesEditorInteraction(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	dir := t.TempDir()
	backgroundFixture(t, dir, "white.png", color.White)
	entry := widget.NewEntry()
	obj := withBackground(entry, dir).(*fyne.Container)
	if len(obj.Objects) != 3 {
		t.Fatalf("layers=%d", len(obj.Objects))
	}
	img, ok := obj.Objects[1].(*canvas.Image)
	if !ok || img.FillMode != canvas.ImageFillContain || img.Translucency != 0.88 {
		t.Fatal("image display policy changed")
	}
	w := a.NewWindow("background")
	defer w.Close()
	w.SetContent(obj)
	w.Resize(fyne.NewSize(440, 260))
	test.Type(entry, "Readable 日本語")
	if entry.Text != "Readable 日本語" {
		t.Fatal("background intercepts editor")
	}
}
