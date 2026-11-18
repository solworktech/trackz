//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

static NSString *pathForProcess(NSString *process) {
	// A running application whose executable matches.
	for (NSRunningApplication *a in [[NSWorkspace sharedWorkspace] runningApplications]) {
		if ([[a.executableURL lastPathComponent] isEqualToString:process] && a.bundleURL != nil)
			return a.bundleURL.path;
	}
	return nil;
}

void *trackz_app_icon_png(const char *process, const char *app, int size, int *len) {
	@autoreleasepool {
		NSString *p = [NSString stringWithUTF8String:process];
		NSString *a = [NSString stringWithUTF8String:app];
		NSString *path = nil;
		for (NSRunningApplication *ra in [[NSWorkspace sharedWorkspace] runningApplications]) {
			if (a.length > 0 && [ra.localizedName isEqualToString:a] && ra.bundleURL != nil) {
				path = ra.bundleURL.path;
				break;
			}
		}
		if (path == nil && p.length > 0)
			path = pathForProcess(p);
		if (path == nil && a.length > 0) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
			path = [[NSWorkspace sharedWorkspace] fullPathForApplication:a];
#pragma clang diagnostic pop
		}
		if (path == nil)
			return NULL;
		NSImage *icon = [[NSWorkspace sharedWorkspace] iconForFile:path];
		if (icon == nil)
			return NULL;
		NSBitmapImageRep *rep = [[NSBitmapImageRep alloc]
			initWithBitmapDataPlanes:NULL pixelsWide:size pixelsHigh:size bitsPerSample:8
			samplesPerPixel:4 hasAlpha:YES isPlanar:NO colorSpaceName:NSDeviceRGBColorSpace
			bytesPerRow:0 bitsPerPixel:0];
		[NSGraphicsContext saveGraphicsState];
		[NSGraphicsContext setCurrentContext:[NSGraphicsContext graphicsContextWithBitmapImageRep:rep]];
		[icon drawInRect:NSMakeRect(0, 0, size, size) fromRect:NSZeroRect
		       operation:NSCompositingOperationCopy fraction:1.0];
		[NSGraphicsContext restoreGraphicsState];
		NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
		if (png == nil)
			return NULL;
		void *out = malloc(png.length);
		memcpy(out, png.bytes, png.length);
		*len = (int)png.length;
		return out;
	}
}
