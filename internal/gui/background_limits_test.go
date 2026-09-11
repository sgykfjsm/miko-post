package gui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestBackgroundEncodedSizeLimits(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	// PNG decoding accepts trailing bytes. These files would decode successfully
	// without the size guard, so decode failure cannot masquerade as enforcement.
	data := make([]byte, maxBackgroundBytes+1)
	copy(data, encoded.Bytes())
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exact := filepath.Join(dir, "exact.png")
	if err := os.WriteFile(exact, data[:maxBackgroundBytes], 0600); err != nil {
		t.Fatal(err)
	}
	if readBackground(exact) == nil {
		t.Fatal("exact 8 MiB image rejected")
	}
	over := filepath.Join(dir, "over.png")
	if err := os.WriteFile(over, data, 0600); err != nil {
		t.Fatal(err)
	}
	if readBackground(over) != nil {
		t.Fatal("over 8 MiB image accepted")
	}
	recovery := t.TempDir()
	if err := os.WriteFile(filepath.Join(recovery, "over.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recovery, "usable.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if img := chooseBackground(recovery); img == nil || img.Bounds().Dx() != 1 {
		t.Fatal("oversized candidate prevented recovery")
	}
}

func TestBackgroundPixelLimits(t *testing.T) {
	dir := t.TempDir()
	for _, width := range []int{4000, 4001} {
		path := filepath.Join(dir, "image.png")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		// Gray keeps fixture memory modest while producing a genuinely decodable
		// image on each side of the 16-million-pixel bound.
		if err = png.Encode(f, image.NewGray(image.Rect(0, 0, width, 4000))); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
		img := readBackground(path)
		if width == 4000 {
			if img == nil || img.Bounds().Dx() != 4000 || img.Bounds().Dy() != 4000 {
				t.Fatal("exact pixel boundary rejected")
			}
		} else {
			if img != nil {
				t.Fatal("over pixel boundary accepted")
			}
			var small bytes.Buffer
			if err = png.Encode(&small, image.NewGray(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "usable.png"), small.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if got := chooseBackground(dir); got == nil || got.Bounds().Dx() != 2 {
				t.Fatal("oversized pixels prevented recovery")
			}
		}
	}
}
