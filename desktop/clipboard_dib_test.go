package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestDIBToPNG32BitZeroAlphaAndBGRA(t *testing.T) {
	// Build a 2x2 32-bit bottom-up BITMAPINFOHEADER with all-zero alpha (simulating GDI PrintScreen)
	dib := make([]byte, 40+2*2*4)
	binary.LittleEndian.PutUint32(dib[0:4], 40)  // biSize
	binary.LittleEndian.PutUint32(dib[4:8], 2)   // width = 2
	binary.LittleEndian.PutUint32(dib[8:12], 2)  // height = 2 (bottom-up)
	binary.LittleEndian.PutUint16(dib[12:14], 1) // planes = 1
	binary.LittleEndian.PutUint16(dib[14:16], 32)

	// Bottom row (y=1 in final image), pixel (0,1) = pure Red (BGR0 = 0x00, 0x00, 0xFF, 0x00)
	dib[40+0] = 0x00
	dib[40+1] = 0x00
	dib[40+2] = 0xFF
	dib[40+3] = 0x00

	// Top row (y=0 in final image), pixel (0,0) = pure Blue (BGR0 = 0xFF, 0x00, 0x00, 0x00)
	dib[40+8+0] = 0xFF
	dib[40+8+1] = 0x00
	dib[40+8+2] = 0x00
	dib[40+8+3] = 0x00

	pngBytes, err := dibToPNG(dib)
	if err != nil {
		t.Fatalf("dibToPNG failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode failed: %v", err)
	}
	r0, g0, b0, a0 := img.At(0, 0).RGBA()
	if b0 != 0xffff || r0 != 0 || g0 != 0 || a0 != 0xffff {
		t.Fatalf("expected opaque blue at top-left (0,0), got r=%x g=%x b=%x a=%x", r0, g0, b0, a0)
	}
	r1, g1, b1, a1 := img.At(0, 1).RGBA()
	if r1 != 0xffff || g1 != 0 || b1 != 0 || a1 != 0xffff {
		t.Fatalf("expected opaque red at bottom-left (0,1), got r=%x g=%x b=%x a=%x", r1, g1, b1, a1)
	}

	// Test trimPNGToIEND removes trailing zero padding
	padded := append(append([]byte{}, pngBytes...), 0x00, 0x00, 0x00, 0x00)
	trimmed := trimPNGToIEND(padded)
	if !bytes.Equal(trimmed, pngBytes) {
		t.Fatalf("trimPNGToIEND mismatch: got %d bytes, want %d bytes", len(trimmed), len(pngBytes))
	}
}
