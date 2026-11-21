package main

import "testing"

func TestRenderSizeAndShape(t *testing.T) {
	img := render(64)

	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("size = %dx%d, want 64x64", b.Dx(), b.Dy())
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("corner alpha = %d, want 0", a)
	}
	r, g, b, a := img.At(32, 32).RGBA()
	if a == 0 || r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Fatalf("center = %04x %04x %04x %04x, want opaque white", r, g, b, a)
	}
}
