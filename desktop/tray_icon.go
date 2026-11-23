package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// renderTrayIconPNG generates a crisp 32x32 PNG icon in memory ("color" or "mono").
func renderTrayIconPNG(mono bool) []byte {
	const sz = 32
	img := image.NewNRGBA(image.Rect(0, 0, sz, sz))

	bgCol := color.NRGBA{R: 29, G: 125, B: 250, A: 255} // #1d7dfa
	fgCol := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if mono {
		bgCol = color.NRGBA{R: 48, G: 52, B: 60, A: 235}
	}

	// Draw rounded 28x28 badge centered in 32x32
	for y := 2; y < 30; y++ {
		for x := 2; x < 30; x++ {
			dx := 0
			if x < 7 {
				dx = 7 - x
			} else if x > 24 {
				dx = x - 24
			}
			dy := 0
			if y < 7 {
				dy = 7 - y
			} else if y > 24 {
				dy = y - 24
			}
			if dx*dx+dy*dy <= 25 {
				img.SetNRGBA(x, y, bgCol)
			}
		}
	}

	// Draw ">" chevron (x: 8..15, y: 10..22)
	for i := 0; i <= 5; i++ {
		for t := 0; t < 3; t++ {
			img.SetNRGBA(8+i+t, 10+i, fgCol)
			img.SetNRGBA(8+i+t, 20-i, fgCol)
		}
	}

	// Draw "_" underscore (x: 17..24, y: 20..22)
	for y := 20; y <= 22; y++ {
		for x := 17; x <= 24; x++ {
			img.SetNRGBA(x, y, fgCol)
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
