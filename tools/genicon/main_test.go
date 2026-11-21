package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// fnOS replaces {0} in app/ui/config's icon path with the pixel size, so the
// UI directory must hold icon_64.png and icon_256.png — icon_1/2/3.png never
// loads and the launcher shows a broken image.
func TestGeneratedIconsMatchTheManifestNames(t *testing.T) {
	want := map[string]int{
		"ICON.PNG":                   64,
		"ICON_256.PNG":               256,
		"app/ui/images/icon_64.png":  64,
		"app/ui/images/icon_256.png": 256,
	}
	got := map[string]int{}
	for _, out := range outputs {
		got[out.path] = out.size
	}
	for path, size := range want {
		if got[path] != size {
			t.Fatalf("outputs lack %s at %dpx: %v", path, size, got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("outputs = %v, want exactly %v", got, want)
	}

	root := filepath.Join("..", "..")
	for path, size := range want {
		f, err := os.Open(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if cfg.Width != size || cfg.Height != size {
			t.Fatalf("%s = %dx%d, want %dx%d", path, cfg.Width, cfg.Height, size, size)
		}
	}
	for _, stale := range []string{"icon_1.png", "icon_2.png", "icon_3.png"} {
		if _, err := os.Stat(filepath.Join(root, "app/ui/images", stale)); err == nil {
			t.Fatalf("stale %s is still on disk", stale)
		}
	}
}

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
