package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
)

// ClipboardReadResult describes the current content of the native OS clipboard.
type ClipboardReadResult struct {
	Kind       string `json:"kind"`                   // "image", "image_path", "text", "empty"
	Text       string `json:"text,omitempty"`         // populated when Kind == "text"
	LocalPath  string `json:"local_path,omitempty"`   // populated when Kind == "image_path"
	DataBase64 string `json:"data_base64,omitempty"`  // populated when Kind == "image"
	MimeType   string `json:"mime_type,omitempty"`    // e.g. "image/png"
	FileName   string `json:"file_name,omitempty"`    // e.g. "clipboard.png"
}

var pngIENDMarker = []byte{0x00, 0x00, 0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}
var pngMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// trimPNGToIEND strips any GlobalAlloc zero-padding bytes after the PNG IEND chunk.
func trimPNGToIEND(raw []byte) []byte {
	if len(raw) < len(pngMagic)+len(pngIENDMarker) || !bytes.Equal(raw[:8], pngMagic) {
		return raw
	}
	if idx := bytes.Index(raw, pngIENDMarker); idx >= 0 {
		return raw[:idx+len(pngIENDMarker)]
	}
	return raw
}

// dibToPNG converts a Win32 CF_DIB / CF_DIBV5 memory block into a lossless PNG image.
// Handles 32-bpp BGRA, 32-bpp BGR0 (GDI PrintScreen where alpha bytes are all zero),
// 24-bpp BGR, and 8-bpp indexed DIBs in both bottom-up and top-down orientations.
func dibToPNG(dib []byte) ([]byte, error) {
	if len(dib) < 40 {
		return nil, errors.New("dib header too short")
	}
	biSize := binary.LittleEndian.Uint32(dib[0:4])
	if biSize < 40 || int(biSize) > len(dib) {
		return nil, errors.New("invalid dib header size")
	}

	width := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	rawHeight := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])
	clrUsed := binary.LittleEndian.Uint32(dib[32:36])

	topDown := false
	height := rawHeight
	if rawHeight < 0 {
		topDown = true
		height = -rawHeight
	}
	if width <= 0 || height <= 0 || width > 16384 || height > 16384 {
		return nil, errors.New("invalid dib dimensions")
	}

	const (
		biRGB       = 0
		biBitfields = 3
	)
	if compression != biRGB && compression != biBitfields {
		return nil, errors.New("unsupported compressed dib format")
	}

	pixelOffset := int(biSize)
	var rMask, gMask, bMask, aMask uint32
	hasCustomMasks := false

	if compression == biBitfields {
		if biSize == 40 {
			if len(dib) < 52 {
				return nil, errors.New("dib bitfields truncated")
			}
			rMask = binary.LittleEndian.Uint32(dib[40:44])
			gMask = binary.LittleEndian.Uint32(dib[44:48])
			bMask = binary.LittleEndian.Uint32(dib[48:52])
			pixelOffset += 12
			hasCustomMasks = true
		} else if biSize >= 56 {
			rMask = binary.LittleEndian.Uint32(dib[40:44])
			gMask = binary.LittleEndian.Uint32(dib[44:48])
			bMask = binary.LittleEndian.Uint32(dib[48:52])
			aMask = binary.LittleEndian.Uint32(dib[52:56])
			hasCustomMasks = true
		}
	}

	paletteOffset := int(biSize)
	if bitCount <= 8 {
		numColors := int(clrUsed)
		if numColors == 0 {
			numColors = 1 << bitCount
		}
		pixelOffset = paletteOffset + numColors*4
	}

	stride := ((width*int(bitCount) + 31) / 32) * 4
	if pixelOffset+stride*height > len(dib) {
		// Tolerate slight row truncation only if at least 1 row is available, else error
		if pixelOffset >= len(dib) {
			return nil, errors.New("dib pixel buffer out of bounds")
		}
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	switch bitCount {
	case 32:
		hasAnyAlpha := false
		for row := 0; row < height; row++ {
			srcRow := pixelOffset + row*stride
			if srcRow+width*4 > len(dib) {
				break
			}
			dstY := height - 1 - row
			if topDown {
				dstY = row
			}
			for x := 0; x < width; x++ {
				p := srcRow + x*4
				var r, g, b, a uint8
				if hasCustomMasks && (rMask != 0xff0000 || gMask != 0x00ff00 || bMask != 0x0000ff) {
					px := binary.LittleEndian.Uint32(dib[p : p+4])
					r = extractChannel(px, rMask)
					g = extractChannel(px, gMask)
					b = extractChannel(px, bMask)
					if aMask != 0 {
						a = extractChannel(px, aMask)
					} else {
						a = dib[p+3]
					}
				} else {
					b = dib[p+0]
					g = dib[p+1]
					r = dib[p+2]
					a = dib[p+3]
				}
				if a != 0 {
					hasAnyAlpha = true
				}
				img.SetNRGBA(x, dstY, color.NRGBA{R: r, G: g, B: b, A: a})
			}
		}
		// GDI BitBlt / PrintScreen leaves alpha channel all-zero on opaque 32-bpp bitmaps
		if !hasAnyAlpha {
			pix := img.Pix
			for i := 3; i < len(pix); i += 4 {
				pix[i] = 0xff
			}
		}

	case 24:
		for row := 0; row < height; row++ {
			srcRow := pixelOffset + row*stride
			if srcRow+width*3 > len(dib) {
				break
			}
			dstY := height - 1 - row
			if topDown {
				dstY = row
			}
			for x := 0; x < width; x++ {
				p := srcRow + x*3
				img.SetNRGBA(x, dstY, color.NRGBA{
					R: dib[p+2],
					G: dib[p+1],
					B: dib[p+0],
					A: 0xff,
				})
			}
		}

	case 8:
		for row := 0; row < height; row++ {
			srcRow := pixelOffset + row*stride
			if srcRow+width > len(dib) {
				break
			}
			dstY := height - 1 - row
			if topDown {
				dstY = row
			}
			for x := 0; x < width; x++ {
				idx := int(dib[srcRow+x])
				pal := paletteOffset + idx*4
				if pal+3 < pixelOffset && pal+3 < len(dib) {
					img.SetNRGBA(x, dstY, color.NRGBA{
						R: dib[pal+2],
						G: dib[pal+1],
						B: dib[pal+0],
						A: 0xff,
					})
				}
			}
		}

	default:
		return nil, errors.New("unsupported dib bit depth")
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func extractChannel(val, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := uint(0)
	m := mask
	for (m & 1) == 0 {
		m >>= 1
		shift++
	}
	raw := (val & mask) >> shift
	if m == 0xff {
		return uint8(raw)
	}
	return uint8((uint64(raw) * 255) / uint64(m))
}
