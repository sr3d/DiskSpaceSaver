//go:build windows

package platform

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procShellExecuteW              = shell32.NewProc("ShellExecuteW")
	procSHFileOperationW           = shell32.NewProc("SHFileOperationW")
	procSHCreateItemFromParsingNam = shell32.NewProc("SHCreateItemFromParsingName")

	procGetObjectW         = gdi32.NewProc("GetObjectW")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
)

func shellExecute(verb, file, args string) error {
	v, err := windows.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	f, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var a *uint16
	if args != "" {
		if a, err = windows.UTF16PtrFromString(args); err != nil {
			return err
		}
	}
	const swShowNormal = 1
	// ShellExecute reports success as a value above 32; anything at or below
	// is an error code rather than an instance handle.
	r, _, e := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(a)), 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("could not open %s: %w", file, e)
	}
	return nil
}

// SHFILEOPSTRUCTW. The field order reproduces the C layout exactly; Go inserts
// the same padding the compiler does, so this must not be reordered.
type shFileOpStructW struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x0003
	fofAllowUndo      = 0x0040 // the flag that means "Recycle Bin", not "erase"
	fofNoConfirmation = 0x0010
	fofSilent         = 0x0004
	fofNoErrorUI      = 0x0400
	fofNoConfirmMkDir = 0x0200
)

// Trash moves a path to the Recycle Bin, where the user can restore it.
//
// Returns the path it moved to on macOS; Windows does not report one, and the
// Recycle Bin is browsed rather than addressed by path, so the original is
// returned instead of inventing something.
func Trash(path string) (string, error) {
	clean := filepath.Clean(path)
	// pFrom is a double-NUL-terminated list, not a plain string: a single
	// terminator would make the shell read past the end of the buffer.
	from, err := windows.UTF16FromString(clean)
	if err != nil {
		return "", err
	}
	from = append(from, 0)

	op := shFileOpStructW{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofNoConfirmMkDir,
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return "", fmt.Errorf("could not move %s to the Recycle Bin (error %d)", clean, r)
	}
	if op.fAnyOperationsAborted != 0 {
		return "", fmt.Errorf("moving %s to the Recycle Bin was cancelled", clean)
	}
	return clean, nil
}

// SetTaskbarBadge has no implementation yet.
//
// The Windows equivalent is ITaskbarList3::SetOverlayIcon, which needs the
// main window handle. Wails does not expose it, and finding it by window class
// is guesswork this does not need to make — the scan progress is already shown
// in the window.
func SetTaskbarBadge(text string) {}

// SetTaskbarProgress likewise; see SetTaskbarBadge.
func SetTaskbarProgress(fraction float64) {}

// ---------------------------------------------------------------- thumbnails

// IShellItemImageFactory is what Explorer itself uses, so anything Explorer can
// show a thumbnail for — images, PDFs, video, Office documents — is rendered
// here without this file knowing a single file format. It is the direct
// counterpart of QLThumbnailGenerator on macOS.
var iidShellItemImageFactory = windows.GUID{
	Data1: 0xbcc18b79, Data2: 0xba16, Data3: 0x442f,
	Data4: [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b},
}

const (
	siigbfResizeToFit  = 0x00
	siigbfBiggerSizeOk = 0x01
)

// comVTable is the head of any COM object: a pointer to its function table.
type comVTable struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	GetImage       uintptr
}

type shellItemImageFactory struct {
	vtbl *comVTable
}

// Thumbnail renders a preview of a file as a PNG data URI, or "" when the
// system has no representation for it — which is the normal answer for many
// files and not an error.
func Thumbnail(path string, maxPoints int, scale float64) string {
	if maxPoints <= 0 {
		maxPoints = 512
	}
	px := int32(float64(maxPoints) * scale)
	if px <= 0 {
		px = int32(maxPoints)
	}

	// COM is per-thread, and this may be called from any goroutine.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		// Already initialised on this thread in another mode is fine to
		// continue with; anything else and there is no thumbnail to be had.
		if e, ok := err.(windows.Errno); !ok || e != 0x80010106 { // RPC_E_CHANGED_MODE
			return ""
		}
	} else {
		defer windows.CoUninitialize()
	}

	p, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return ""
	}
	var factory *shellItemImageFactory
	r, _, _ := procSHCreateItemFromParsingNam.Call(
		uintptr(unsafe.Pointer(p)), 0,
		uintptr(unsafe.Pointer(&iidShellItemImageFactory)),
		uintptr(unsafe.Pointer(&factory)))
	if r != 0 || factory == nil {
		return ""
	}
	defer syscall.SyscallN(factory.vtbl.Release, uintptr(unsafe.Pointer(factory)))

	// SIZE is two 32-bit values passed by value, which fits one register.
	size := uintptr(uint64(uint32(px))<<32 | uint64(uint32(px)))
	var hbitmap uintptr
	hr, _, _ := syscall.SyscallN(factory.vtbl.GetImage,
		uintptr(unsafe.Pointer(factory)),
		size,
		uintptr(siigbfResizeToFit|siigbfBiggerSizeOk),
		uintptr(unsafe.Pointer(&hbitmap)))
	if hr != 0 || hbitmap == 0 {
		return ""
	}
	defer procDeleteObject.Call(hbitmap)

	img, err := bitmapToImage(hbitmap)
	if err != nil || img == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// BITMAP, as GetObject fills it.
type gdiBitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapToImage copies an HBITMAP's pixels into a Go image.
func bitmapToImage(hbitmap uintptr) (image.Image, error) {
	var bm gdiBitmap
	if r, _, _ := procGetObjectW.Call(hbitmap,
		unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil, fmt.Errorf("could not measure the thumbnail")
	}
	if bm.bmWidth <= 0 || bm.bmHeight <= 0 {
		return nil, fmt.Errorf("thumbnail has no size")
	}

	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return nil, fmt.Errorf("no device context")
	}
	defer procDeleteDC.Call(hdc)

	bi := bitmapInfoHeader{
		Size:  uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width: bm.bmWidth,
		// Negative height asks for top-down rows, matching Go's image layout
		// and saving a flip.
		Height:      -bm.bmHeight,
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
	}
	stride := int(bm.bmWidth) * 4
	raw := make([]byte, stride*int(bm.bmHeight))
	const dibRGBColors = 0
	if r, _, _ := procGetDIBits.Call(hdc, hbitmap, 0, uintptr(bm.bmHeight),
		uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&bi)),
		dibRGBColors); r == 0 {
		return nil, fmt.Errorf("could not read the thumbnail pixels")
	}

	img := image.NewNRGBA(image.Rect(0, 0, int(bm.bmWidth), int(bm.bmHeight)))
	opaque := true
	for i := 0; i < len(raw); i += 4 {
		if raw[i+3] != 0 {
			opaque = false
			break
		}
	}
	for i := 0; i < len(raw); i += 4 {
		b, g, r, a := raw[i], raw[i+1], raw[i+2], raw[i+3]
		// A thumbnail with no alpha channel at all comes back with every
		// alpha byte zero; treating that literally would render it invisible.
		if opaque {
			a = 0xff
		}
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, a
	}
	return img, nil
}
