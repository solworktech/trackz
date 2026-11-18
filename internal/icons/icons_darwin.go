//go:build darwin && cgo

package icons

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=10.13
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>

// Returns PNG data (malloc'd, caller frees) of the icon of the application
// named app (or, failing that, the bundle whose executable is process).
void *trackz_app_icon_png(const char *process, const char *app, int size, int *len);
*/
import "C"

import (
	"bytes"
	"image"
	"image/png"
	"unsafe"
)

func find(process, app string, size int) image.Image {
	cp, ca := C.CString(process), C.CString(app)
	defer C.free(unsafe.Pointer(cp))
	defer C.free(unsafe.Pointer(ca))
	var n C.int
	data := C.trackz_app_icon_png(cp, ca, C.int(size), &n)
	if data == nil {
		return nil
	}
	defer C.free(data)
	img, err := png.Decode(bytes.NewReader(C.GoBytes(data, n)))
	if err != nil {
		return nil
	}
	return img
}
