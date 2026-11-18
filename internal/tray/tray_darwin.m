//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#include "tray_darwin.h"
#include "_cgo_export.h"

// The status item must be created and changed on the main thread, which
// Gio's run loop owns; everything is dispatched there. Nothing here touches
// the application delegate.

static NSStatusItem *statusItem = nil;
static NSMenu *menu = nil;

@interface TrackzTrayTarget : NSObject
@end

@implementation TrackzTrayTarget
- (void)clicked:(id)sender {
	NSEvent *e = [NSApp currentEvent];
	BOOL secondary = e.type == NSEventTypeRightMouseUp ||
		(e.modifierFlags & NSEventModifierFlagControl) != 0;
	if (secondary) {
		// Show the menu for this click only, so a left click still
		// reaches clicked:.
		statusItem.menu = menu;
		[statusItem.button performClick:nil];
		statusItem.menu = nil;
	} else {
		[NSApp activateIgnoringOtherApps:YES];
		goTrayActivate();
	}
}
- (void)menuItem:(NSMenuItem *)item {
	goTrayMenuItem((int)item.tag);
}
@end

static TrackzTrayTarget *target = nil;

void trackz_tray_create(const void *png, int pngLen, const char *tooltip) {
	NSData *data = [NSData dataWithBytes:png length:pngLen];
	NSString *tip = [NSString stringWithUTF8String:tooltip];
	dispatch_async(dispatch_get_main_queue(), ^{
		target = [[TrackzTrayTarget alloc] init];
		menu = [[NSMenu alloc] init];
		statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
		NSImage *img = [[NSImage alloc] initWithData:data];
		img.size = NSMakeSize(18, 18);
		statusItem.button.image = img;
		statusItem.button.toolTip = tip;
		statusItem.button.target = target;
		statusItem.button.action = @selector(clicked:);
		[statusItem.button sendActionOn:(NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp)];
	});
}

void trackz_tray_menu_reset(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[menu removeAllItems];
	});
}

void trackz_tray_menu_add(int tag, const char *label) {
	NSString *title = label ? [NSString stringWithUTF8String:label] : nil;
	dispatch_async(dispatch_get_main_queue(), ^{
		if (title == nil) {
			[menu addItem:[NSMenuItem separatorItem]];
			return;
		}
		NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:@selector(menuItem:) keyEquivalent:@""];
		item.target = target;
		item.tag = tag;
		[menu addItem:item];
	});
}

void trackz_tray_remove(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (statusItem != nil) {
			[[NSStatusBar systemStatusBar] removeStatusItem:statusItem];
			statusItem = nil;
		}
	});
}
