//go:build darwin

package gio

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

static void trackz_set_dock_icon(const void *data, int len) {
	NSData *png = [NSData dataWithBytes:data length:len];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSImage *img = [[NSImage alloc] initWithData:png];
		if (img != nil)
			[NSApp setApplicationIconImage:img];
	});
}
*/
import "C"

import (
	"github.com/solworktech/trackz/internal/assets"
)

// When run outside an .app bundle, macOS shows a generic Dock icon; set ours.
func init() {
	png := assets.IconPNG(256)
	if len(png) == 0 {
		return
	}
	p := C.CBytes(png)
	C.trackz_set_dock_icon(p, C.int(len(png)))
	// The block copies the bytes into an NSData before returning.
	C.free(p)
}
