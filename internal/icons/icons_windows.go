//go:build windows

package icons

import (
	"image"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")

	procExtractIconExW = shell32.NewProc("ExtractIconExW")
	procDestroyIcon    = user32.NewProc("DestroyIcon")
	procGetIconInfo    = user32.NewProc("GetIconInfo")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procGetDIBits      = gdi32.NewProc("GetDIBits")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
	procGetObjectW     = gdi32.NewProc("GetObjectW")
)

// find locates a running process with the given name (trackz stores the
// executable name without ".exe") and extracts its icon. Processes that are
// no longer running get no icon.
func find(process, app string, size int) image.Image {
	path := runningExecutable(process)
	if path == "" {
		return nil
	}
	return extractIcon(path)
}

func runningExecutable(process string) string {
	if process == "" {
		return ""
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(snap) }() // query only
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		exe := windows.UTF16ToString(pe.ExeFile[:])
		if !strings.EqualFold(strings.TrimSuffix(exe, filepath.Ext(exe)), process) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pe.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		err = windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
		_ = windows.CloseHandle(h) // query only
		if err == nil {
			return windows.UTF16ToString(buf[:n])
		}
	}
	return ""
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  windows.Handle
	hbmColor windows.Handle
}

type bitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

func extractIcon(path string) image.Image {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var large windows.Handle
	if n, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&large)), 0, 1); n == 0 || large == 0 {
		return nil
	}
	// The clean-up calls below free GDI objects; their failure leaves
	// nothing to recover.
	defer func() { _, _, _ = procDestroyIcon.Call(uintptr(large)) }()

	var ii iconInfo
	if r, _, _ := procGetIconInfo.Call(uintptr(large), uintptr(unsafe.Pointer(&ii))); r == 0 {
		return nil
	}
	defer func() {
		_, _, _ = procDeleteObject.Call(uintptr(ii.hbmColor))
		_, _, _ = procDeleteObject.Call(uintptr(ii.hbmMask))
	}()
	if ii.hbmColor == 0 {
		return nil // monochrome icon
	}
	var bm bitmap
	if r, _, _ := procGetObjectW.Call(uintptr(ii.hbmColor), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil
	}
	w, h := int(bm.bmWidth), int(bm.bmHeight)
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil
	}
	hdr := bitmapInfoHeader{
		biWidth:    int32(w),
		biHeight:   -int32(h), // top-down
		biPlanes:   1,
		biBitCount: 32,
	}
	hdr.biSize = uint32(unsafe.Sizeof(hdr))
	pix := make([]byte, w*h*4)
	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return nil
	}
	defer func() { _, _, _ = procReleaseDC.Call(0, dc) }()
	if r, _, _ := procGetDIBits.Call(dc, uintptr(ii.hbmColor), 0, uintptr(h),
		uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&hdr)), 0); r == 0 {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hasAlpha := false
	for i := 0; i < len(pix); i += 4 {
		if pix[i+3] != 0 {
			hasAlpha = true
			break
		}
	}
	for i := 0; i < len(pix); i += 4 {
		// BGRA -> RGBA
		img.Pix[i], img.Pix[i+1], img.Pix[i+2] = pix[i+2], pix[i+1], pix[i]
		if hasAlpha {
			img.Pix[i+3] = pix[i+3]
		} else {
			img.Pix[i+3] = 255
		}
	}
	return img
}
