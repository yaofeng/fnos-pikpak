package main

import (
	"image"
	"testing"
)

func TestLoadOfficialSource(t *testing.T) {
	src, err := loadSource("../../assets/icon-source.png")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	if b := src.Bounds(); b.Dx() != b.Dy() || b.Dx() < 64 {
		t.Fatalf("source = %dx%d, want a square at least 64px", b.Dx(), b.Dy())
	}
}

func TestRenderSizes(t *testing.T) {
	src, err := loadSource("../../assets/icon-source.png")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	for _, size := range []int{64, 128, 256} {
		img := render(src, size)
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("render(%d) = %dx%d", size, b.Dx(), b.Dy())
		}
		if !hasOpaquePixel(img) {
			t.Fatalf("render(%d) is fully transparent", size)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	src, err := loadSource("../../assets/icon-source.png")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	a, b := render(src, 64), render(src, 64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if a.At(x, y) != b.At(x, y) {
				t.Fatalf("pixel (%d,%d) differs between renders", x, y)
			}
		}
	}
}

func hasOpaquePixel(img *image.NRGBA) bool {
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				return true
			}
		}
	}
	return false
}
