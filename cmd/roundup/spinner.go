package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

const spinnerSpokes = 8

// spinnerFrames draws one turn of a spoked spinner as template PNGs, where only the alpha channel matters.
func spinnerFrames() [][]byte {
	frames := make([][]byte, spinnerSpokes)
	for f := range frames {
		img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		for y := range 32 {
			for x := range 32 {
				if a := spokeAlpha(float64(x)-15.5, float64(y)-15.5, f); a > 0 {
					img.Set(x, y, color.NRGBA{A: a})
				}
			}
		}
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		frames[f] = b.Bytes()
	}
	return frames
}

// spokeAlpha is the opacity at (dx, dy) from the centre: the spoke at index frame is solid and those behind it fade.
func spokeAlpha(dx, dy float64, frame int) uint8 {
	const inner, outer, halfWidth = 6.0, 13.0, 1.6
	for k := range spinnerSpokes {
		angle := 2 * math.Pi * float64(k) / spinnerSpokes
		ux, uy := math.Sin(angle), -math.Cos(angle)
		along := dx*ux + dy*uy
		across := math.Abs(dx*uy - dy*ux)
		if along < inner || along > outer || across > halfWidth {
			continue
		}
		age := (frame - k + spinnerSpokes) % spinnerSpokes
		return uint8(255 - age*28)
	}
	return 0
}
