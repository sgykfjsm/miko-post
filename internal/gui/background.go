package gui

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

const maxBackgroundBytes = 8 << 20
const maxBackgroundPixels = 16_000_000

// chooseBackground checks only top-level regular JPEG/PNG files. Randomizing
// candidate order lets a broken image fall through to another usable one.
func chooseBackground(dir string) image.Image {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var candidates []string
	for _, entry := range entries {
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.Type().IsRegular() && (ext == ".png" || ext == ".jpg" || ext == ".jpeg") {
			candidates = append(candidates, filepath.Join(dir, entry.Name()))
		}
	}
	for _, i := range rand.Perm(len(candidates)) {
		if img := readBackground(candidates[i]); img != nil {
			return img
		}
	}
	return nil
}

func readBackground(path string) image.Image {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxBackgroundBytes {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBackgroundBytes+1))
	if err != nil || len(data) > maxBackgroundBytes {
		return nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxBackgroundPixels/cfg.Height {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return img
}

// backgroundTheme keeps the foreground readable even when the OS uses a light
// theme. The input is transparent so the image can appear behind the editor.
type backgroundTheme struct{ fyne.Theme }

func (t backgroundTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameInputBackground {
		return color.Transparent
	}
	if name == theme.ColorNameBackground {
		return color.Black
	}
	return t.Theme.Color(name, theme.VariantDark)
}
func withBackground(content fyne.CanvasObject, dir string) fyne.CanvasObject {
	layers := []fyne.CanvasObject{canvas.NewRectangle(color.Black)}
	if img := chooseBackground(dir); img != nil {
		background := canvas.NewImageFromImage(img)
		background.FillMode = canvas.ImageFillContain
		background.Translucency = 0.88
		layers = append(layers, background)
	}
	layers = append(layers, container.NewThemeOverride(content, backgroundTheme{theme.DefaultTheme()}))
	return container.NewStack(layers...)
}
