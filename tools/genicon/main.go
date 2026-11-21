// Command genicon writes the fnOS app icons. It has no third-party deps so it
// can run anywhere the Go toolchain is available.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

var (
	background = color.NRGBA{R: 0x1b, G: 0x2a, B: 0x4a, A: 0xff}
	foreground = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// render draws a rounded square with a play triangle in the middle.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	radius := s * 0.22

	inRoundedSquare := func(x, y float64) bool {
		cx := math.Min(math.Max(x, radius), s-radius)
		cy := math.Min(math.Max(y, radius), s-radius)
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}

	// Triangle points (fractions of size): left top, left bottom, right middle.
	ax, ay := s*0.40, s*0.30
	bx, by := s*0.40, s*0.70
	cx, cy := s*0.72, s*0.50

	inTriangle := func(x, y float64) bool {
		d1 := (x-bx)*(ay-by) - (ax-bx)*(y-by)
		d2 := (x-cx)*(by-cy) - (bx-cx)*(y-cy)
		d3 := (x-ax)*(cy-ay) - (cx-ax)*(y-ay)
		hasNeg := d1 < 0 || d2 < 0 || d3 < 0
		hasPos := d1 > 0 || d2 > 0 || d3 > 0
		return !(hasNeg && hasPos)
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			if !inRoundedSquare(fx, fy) {
				continue
			}
			if inTriangle(fx, fy) {
				img.SetNRGBA(x, y, foreground)
			} else {
				img.SetNRGBA(x, y, background)
			}
		}
	}
	return img
}

func writePNG(path string, size int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, render(size))
}

func main() {
	targets := []struct {
		path string
		size int
	}{
		{"ICON.PNG", 64},
		{"ICON_256.PNG", 256},
		{"app/ui/images/icon_1.png", 64},
		{"app/ui/images/icon_2.png", 128},
		{"app/ui/images/icon_3.png", 256},
	}
	for _, t := range targets {
		if err := writePNG(t.path, t.size); err != nil {
			log.Fatalf("write %s: %v", t.path, err)
		}
	}
}
