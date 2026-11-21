// Command genicon writes the fnOS app icons from the official PikPak icon.
//
// The source artwork lives in assets/icon-source.png (the 180x180 official
// touch icon). Run `make genicon` after replacing it; the generated PNGs are
// committed so the build does not depend on this tool.
package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

const sourcePath = "assets/icon-source.png"

type output struct {
	path string
	size int
}

// fnOS substitutes the pixel size for {0} in app/ui/config's icon path, so the
// UI icons are named icon_64.png and icon_256.png.
var outputs = []output{
	{"ICON.PNG", 64},
	{"ICON_256.PNG", 256},
	{"app/ui/images/icon_64.png", 64},
	{"app/ui/images/icon_256.png", 256},
}

func main() {
	src, err := loadSource(sourcePath)
	if err != nil {
		log.Fatalf("load %s: %v", sourcePath, err)
	}
	for _, out := range outputs {
		if err := writePNG(out.path, render(src, out.size)); err != nil {
			log.Fatalf("write %s: %v", out.path, err)
		}
		fmt.Println(out.path)
	}
}

// loadSource decodes the official icon. It must be square: fnOS crops icons to
// a square anyway, and non-square art would be distorted.
func loadSource(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() != b.Dy() {
		return nil, fmt.Errorf("%s is %dx%d, want square", path, b.Dx(), b.Dy())
	}
	return src, nil
}

// render scales the source to size x size with smooth interpolation.
func render(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
