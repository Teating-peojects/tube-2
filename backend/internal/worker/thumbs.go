package worker

import (
	"image"
	"image/jpeg"
	"os"
)

// inspectJPEG decodes a generated thumbnail: it proves the file is a real JPEG, returns its real
// dimensions and its mean brightness (0..255) from a sparse sample grid.
func inspectJPEG(path string) (w, h int, luma float64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		return 0, 0, 0, err
	}
	b := img.Bounds()
	w, h = b.Dx(), b.Dy()
	if w < 16 || h < 16 {
		return 0, 0, 0, image.ErrFormat
	}
	var sum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y += max(1, h/18) {
		for x := b.Min.X; x < b.Max.X; x += max(1, w/32) {
			r, g, bl, _ := img.At(x, y).RGBA()
			sum += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257 // 16-bit -> 8-bit
			n++
		}
	}
	return w, h, sum / float64(n), nil
}

const darkLuma = 20 // below this a frame is practically black (fade in/out, intro card)

// pickPrimary prefers the default (middle) frame and falls back to the brightest candidate when the
// default is black. Everything stays a candidate the creator can choose from.
func pickPrimary(lumas []float64, def int) int {
	if def < 0 || def >= len(lumas) {
		def = len(lumas) / 2
	}
	if lumas[def] >= darkLuma {
		return def
	}
	best := def
	for i, l := range lumas {
		if l > lumas[best] {
			best = i
		}
	}
	return best
}
