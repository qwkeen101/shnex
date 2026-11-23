//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	outDir := "."
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	_ = os.MkdirAll(outDir, 0o755)

	png512 := renderAppIconPNG(512)
	if err := os.WriteFile(filepath.Join(outDir, "appicon.png"), png512, 0o644); err != nil {
		panic(err)
	}

	sizes := []int{256, 64, 48, 32, 16}
	var pngBlobs [][]byte
	for _, sz := range sizes {
		pngBlobs = append(pngBlobs, renderAppIconPNG(sz))
	}
	icoBytes := buildICO(sizes, pngBlobs)
	if err := os.WriteFile(filepath.Join(outDir, "appicon.ico"), icoBytes, 0o644); err != nil {
		panic(err)
	}
}

func renderAppIconPNG(size int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	fSize := float64(size)
	pad := fSize * 0.06
	radius := fSize * 0.22

	bgTop := color.NRGBA{R: 28, G: 30, B: 38, A: 255}
	bgBot := color.NRGBA{R: 16, G: 17, B: 22, A: 255}
	borderCol := color.NRGBA{R: 245, G: 158, B: 11, A: 220}
	amber := color.NRGBA{R: 251, G: 191, B: 36, A: 255}
	green := color.NRGBA{R: 63, G: 185, B: 80, A: 255}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx := float64(x) + 0.5
			fy := float64(y) + 0.5
			dist := roundedBoxSDF(fx-fSize/2, fy-fSize/2, (fSize/2)-pad, (fSize/2)-pad, radius)
			if dist > 1.0 {
				continue
			}

			t := fy / fSize
			c := lerpColor(bgTop, bgBot, t)

			// Subtle amber border
			if dist > -fSize*0.022 {
				c = borderCol
			}

			// Draw bold terminal prompt chevron ">" on left half
			nx := fx / fSize
			ny := fy / fSize
			thickness := 0.052

			// Upper leg of ">": from (0.26, 0.33) to (0.48, 0.50)
			d1 := segDist(nx, ny, 0.26, 0.33, 0.48, 0.50)
			// Lower leg of ">": from (0.48, 0.50) to (0.26, 0.67)
			d2 := segDist(nx, ny, 0.48, 0.50, 0.26, 0.67)
			if d1 < thickness || d2 < thickness {
				c = amber
			}

			// Underscore cursor "_" on right half: from (0.54, 0.66) to (0.76, 0.66)
			d3 := segDist(nx, ny, 0.54, 0.66, 0.76, 0.66)
			if d3 < thickness*0.92 {
				c = green
			}

			// Anti-aliased outer edge
			if dist > 0 {
				c.A = uint8(float64(c.A) * (1.0 - dist))
			}
			img.SetNRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func roundedBoxSDF(px, py, bx, by, r float64) float64 {
	qx := math.Abs(px) - bx + r
	qy := math.Abs(py) - by + r
	out := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	in := math.Min(math.Max(qx, qy), 0)
	return out + in - r
}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	pax, pay := px-ax, py-ay
	bax, bay := bx-ax, by-ay
	h := (pax*bax + pay*bay) / (bax*bax + bay*bay)
	if h < 0 {
		h = 0
	} else if h > 1 {
		h = 1
	}
	return math.Hypot(pax-bax*h, pay-bay*h)
}

func lerpColor(a, b color.NRGBA, t float64) color.NRGBA {
	return color.NRGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 255,
	}
}

func buildICO(sizes []int, pngs [][]byte) []byte {
	count := len(sizes)
	var buf bytes.Buffer
	// ICONDIR header (6 bytes)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // 1 = ICO
	_ = binary.Write(&buf, binary.LittleEndian, uint16(count))

	offset := uint32(6 + 16*count)
	for i, sz := range sizes {
		w := uint8(sz)
		if sz >= 256 {
			w = 0
		}
		buf.WriteByte(w)
		buf.WriteByte(w)
		buf.WriteByte(0) // palette
		buf.WriteByte(0) // reserved
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))  // color planes
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32)) // bpp
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pngs[i])))
		_ = binary.Write(&buf, binary.LittleEndian, offset)
		offset += uint32(len(pngs[i]))
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	return buf.Bytes()
}
