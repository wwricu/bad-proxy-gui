package main

import (
	"image"
	_ "image/png"
	"os"
	"testing"
)

// The two icons are drawn by build/render-icon.sh from the svgs next to them,
// and nothing else checks that the result is what the two surfaces need. Both
// failures are silent: a menu-bar image that fills its canvas becomes a solid
// block the height of the bar, and a template image drawn in white is masked to
// nothing at all.

func TestAppIconKeepsItsMargin(t *testing.T) {
	ink := darkInk(t, "build/appicon.png")
	if ink.h < ink.canvas*40/100 || ink.h > ink.canvas*60/100 {
		t.Errorf("the letter is %d of a %d canvas: it should hold about half, "+
			"the way every other Dock icon does", ink.h, ink.canvas)
	}
	if !ink.cornersClear {
		t.Error("appicon.png has opaque corners: the rounded tile lost its radius, "+
			"so the Dock will show a square")
	}
}

func TestTrayTemplateIsPaddedAndBlack(t *testing.T) {
	ink := darkInk(t, "build/traytemplate.png")
	// AppKit stretches the whole square to the bar's thickness, so this ratio is
	// the glyph's size in the menu bar.
	if ink.h < ink.canvas*50/100 || ink.h > ink.canvas*70/100 {
		t.Errorf("the glyph is %d of a %d canvas: the bar is %d pt tall, so it would "+
			"draw %d pt of it", ink.h, ink.canvas, 22, ink.h*22/ink.canvas)
	}
	if !ink.cornersClear {
		t.Error("traytemplate.png has opaque corners: a template image is a mask, and "+
			"this would fill the menu bar with a block")
	}
	if ink.hasLight {
		t.Error("traytemplate.png is not flat black: a template image is masked from "+
			"the alpha channel alone, so anything else is lost in the bar")
	}
}

type inkBox struct {
	w, h, canvas int
	cornersClear bool
	hasLight     bool
}

// darkInk measures the black part of a square icon - the glyph, and nothing
// else - and reports whether any other colour is present.
func darkInk(t *testing.T, path string) inkBox {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("%s is %dx%d: both icons are stretched to a square, so they have to be one",
			path, b.Dx(), b.Dy())
	}
	// A rasteriser's faint edge pixels quantise to black - the app icon's own
	// hairline reads as (112,113,115) at half alpha - so the ink is counted only
	// where the paint is near solid and near black.
	const solid, black = 200, 64
	cornerClear := func(x0, y0 int) bool {
		for y := y0; y < y0+6; y++ {
			for x := x0; x < x0+6; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a>>8 >= 32 {
					return false
				}
			}
		}
		return true
	}
	out := inkBox{canvas: b.Dx(), cornersClear: cornerClear(b.Min.X, b.Min.Y) &&
		cornerClear(b.Max.X-6, b.Min.Y) && cornerClear(b.Min.X, b.Max.Y-6) &&
		cornerClear(b.Max.X-6, b.Max.Y-6)}
	minX, minY, maxX, maxY := out.canvas, out.canvas, 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bb, a := img.At(x, y).RGBA()
			if a>>8 < solid {
				continue
			}
			if r>>8 >= black || g>>8 >= black || bb>>8 >= black {
				out.hasLight = true
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	out.w, out.h = maxX-minX+1, maxY-minY+1
	return out
}
