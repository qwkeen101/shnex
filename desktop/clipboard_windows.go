//go:build windows

package main

import (
	"encoding/base64"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32Clip                  = windows.NewLazySystemDLL("user32.dll")
	procOpenClipboard              = modUser32Clip.NewProc("OpenClipboard")
	procCloseClipboard             = modUser32Clip.NewProc("CloseClipboard")
	procIsClipboardFormatAvailable = modUser32Clip.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData           = modUser32Clip.NewProc("GetClipboardData")
	procRegisterClipboardFormatW   = modUser32Clip.NewProc("RegisterClipboardFormatW")

	modKernel32Clip  = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalLock   = modKernel32Clip.NewProc("GlobalLock")
	procGlobalUnlock = modKernel32Clip.NewProc("GlobalUnlock")
	procGlobalSize   = modKernel32Clip.NewProc("GlobalSize")

	modShell32Clip     = windows.NewLazySystemDLL("shell32.dll")
	procDragQueryFileW = modShell32Clip.NewProc("DragQueryFileW")
)

const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDrop       = 15
	cfDIBV5       = 17
)

func registerClipFormat(name string) uint32 {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(ptr)))
	return uint32(r)
}

func isClipFormatAvailable(fmtID uint32) bool {
	if fmtID == 0 {
		return false
	}
	r, _, _ := procIsClipboardFormatAvailable.Call(uintptr(fmtID))
	return r != 0
}

func copyGlobalMemBytes(hMem uintptr) []byte {
	if hMem == 0 {
		return nil
	}
	sz, _, _ := procGlobalSize.Call(hMem)
	if sz == 0 || sz > 128<<20 {
		return nil
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(hMem)

	src := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(sz))
	out := make([]byte, len(src))
	copy(out, src)
	return out
}

func isSupportedImagePath(p string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(p))) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".svg", ".avif":
		return true
	default:
		return false
	}
}

// readSystemClipboard inspects the native Win32 OS clipboard directly so screenshots
// from Win+Shift+S, WeChat, QQ, Snipaste, PrintScreen, Browser "Copy Image", or Explorer
// image file copies work seamlessly without WebView2 clipboard permission limits.
func readSystemClipboard() ClipboardReadResult {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	opened := false
	for attempt := 0; attempt < 6; attempt++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			opened = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !opened {
		return ClipboardReadResult{Kind: "empty"}
	}
	defer procCloseClipboard.Call()

	// 1. Explorer copied file(s) (CF_HDROP)
	if isClipFormatAvailable(cfHDrop) {
		hDrop, _, _ := procGetClipboardData.Call(cfHDrop)
		if hDrop != 0 {
			count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
			if count > 0 {
				var buf [1024]uint16
				n, _, _ := procDragQueryFileW.Call(hDrop, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				if n > 0 {
					filePath := windows.UTF16ToString(buf[:n])
					if isSupportedImagePath(filePath) {
						return ClipboardReadResult{
							Kind:      "image_path",
							LocalPath: filePath,
							FileName:  filepath.Base(filePath),
						}
					}
					return ClipboardReadResult{
						Kind: "text",
						Text: filePath,
					}
				}
			}
		}
	}

	// 2. Plain Unicode text (CF_UNICODETEXT) — when user copied text/code rather than a screenshot
	if isClipFormatAvailable(cfUnicodeText) {
		hTxt, _, _ := procGetClipboardData.Call(cfUnicodeText)
		if hTxt != 0 {
			ptr, _, _ := procGlobalLock.Call(hTxt)
			if ptr != 0 {
				sz, _, _ := procGlobalSize.Call(hTxt)
				maxU16 := int(sz / 2)
				if maxU16 > 0 {
					u16 := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), maxU16)
					txt := windows.UTF16ToString(u16)
					procGlobalUnlock.Call(hTxt)
					if txt != "" {
						return ClipboardReadResult{
							Kind: "text",
							Text: txt,
						}
					}
				} else {
					procGlobalUnlock.Call(hTxt)
				}
			}
		}
	}

	// 3. Registered native PNG / image/png formats (Win+Shift+S, Snipaste, Chrome/Edge Copy Image)
	for _, fmtName := range []string{"PNG", "image/png"} {
		fmtID := registerClipFormat(fmtName)
		if isClipFormatAvailable(fmtID) {
			hData, _, _ := procGetClipboardData.Call(uintptr(fmtID))
			raw := copyGlobalMemBytes(hData)
			raw = trimPNGToIEND(raw)
			if len(raw) > 16 {
				return ClipboardReadResult{
					Kind:       "image",
					DataBase64: base64.StdEncoding.EncodeToString(raw),
					MimeType:   "image/png",
					FileName:   "screenshot.png",
				}
			}
		}
	}

	// 4. Universal Win32 DIB / DIBV5 bitmap (synthesized automatically by Windows for ALL screenshots!)
	for _, dibFmt := range []uint32{cfDIB, cfDIBV5} {
		if isClipFormatAvailable(dibFmt) {
			hDib, _, _ := procGetClipboardData.Call(uintptr(dibFmt))
			dibBytes := copyGlobalMemBytes(hDib)
			if len(dibBytes) >= 40 {
				if pngBytes, err := dibToPNG(dibBytes); err == nil && len(pngBytes) > 0 {
					return ClipboardReadResult{
						Kind:       "image",
						DataBase64: base64.StdEncoding.EncodeToString(pngBytes),
						MimeType:   "image/png",
						FileName:   "screenshot.png",
					}
				}
			}
		}
	}

	return ClipboardReadResult{Kind: "empty"}
}
