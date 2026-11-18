//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include "darwin.h"
#include "_cgo_export.h"

// Accessibility (AX) is needed for window titles and title-change
// notifications; without it only application switches are seen and the
// application name is used as the title.

static int axTrusted = 0;
static AXObserverRef observer = NULL;
static AXUIElementRef observedApp = NULL;
static AXUIElementRef observedWindow = NULL;
static NSMutableArray *tokens = nil;

static void reportFrontmost(void) {
	NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
	if (app == nil)
		return;
	pid_t pid = app.processIdentifier;
	NSString *name = app.localizedName ?: @"";
	NSString *title = nil;
	if (axTrusted) {
		AXUIElementRef appEl = AXUIElementCreateApplication(pid);
		CFTypeRef win = NULL;
		if (AXUIElementCopyAttributeValue(appEl, kAXFocusedWindowAttribute, &win) == kAXErrorSuccess && win != NULL) {
			CFTypeRef t = NULL;
			if (AXUIElementCopyAttributeValue((AXUIElementRef)win, kAXTitleAttribute, &t) == kAXErrorSuccess && t != NULL) {
				if (CFGetTypeID(t) == CFStringGetTypeID())
					title = [(__bridge NSString *)t copy];
				CFRelease(t);
			}
			CFRelease(win);
		}
		CFRelease(appEl);
	}
	if (title == nil || title.length == 0)
		title = name;
	goDarwinWindow((int)pid, (char *)[title UTF8String], (char *)[name UTF8String]);
}

static void watchFocusedWindow(void) {
	if (observer == NULL || observedApp == NULL)
		return;
	if (observedWindow != NULL) {
		AXObserverRemoveNotification(observer, observedWindow, kAXTitleChangedNotification);
		CFRelease(observedWindow);
		observedWindow = NULL;
	}
	CFTypeRef win = NULL;
	if (AXUIElementCopyAttributeValue(observedApp, kAXFocusedWindowAttribute, &win) == kAXErrorSuccess && win != NULL) {
		observedWindow = (AXUIElementRef)win; // owns the reference
		AXObserverAddNotification(observer, observedWindow, kAXTitleChangedNotification, NULL);
	}
}

static void axCallback(AXObserverRef obs, AXUIElementRef element, CFStringRef notification, void *refcon) {
	(void)obs;
	(void)element;
	(void)refcon;
	if (CFEqual(notification, kAXFocusedWindowChangedNotification) ||
	    CFEqual(notification, kAXMainWindowChangedNotification))
		watchFocusedWindow();
	reportFrontmost();
}

static void unwatchApp(void) {
	if (observer != NULL) {
		CFRunLoopRemoveSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), kCFRunLoopDefaultMode);
		CFRelease(observer);
		observer = NULL;
	}
	if (observedWindow != NULL) {
		CFRelease(observedWindow);
		observedWindow = NULL;
	}
	if (observedApp != NULL) {
		CFRelease(observedApp);
		observedApp = NULL;
	}
}

static void watchApp(pid_t pid) {
	unwatchApp();
	if (!axTrusted)
		axTrusted = AXIsProcessTrusted() ? 1 : 0; // permission may be granted later
	if (!axTrusted || pid <= 0)
		return;
	if (AXObserverCreate(pid, axCallback, &observer) != kAXErrorSuccess) {
		observer = NULL;
		return;
	}
	observedApp = AXUIElementCreateApplication(pid);
	AXObserverAddNotification(observer, observedApp, kAXFocusedWindowChangedNotification, NULL);
	AXObserverAddNotification(observer, observedApp, kAXMainWindowChangedNotification, NULL);
	CFRunLoopAddSource(CFRunLoopGetMain(), AXObserverGetRunLoopSource(observer), kCFRunLoopDefaultMode);
	watchFocusedWindow();
}

static void observe(NSNotificationCenter *nc, NSNotificationName name, void (^block)(NSNotification *)) {
	id token = [nc addObserverForName:name object:nil queue:[NSOperationQueue mainQueue] usingBlock:block];
	[tokens addObject:token];
}

int trackz_darwin_init(int promptAX) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
		tokens = [NSMutableArray array];

		NSDictionary *opts = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt : promptAX ? @YES : @NO};
		axTrusted = AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts) ? 1 : 0;

		NSNotificationCenter *ws = [[NSWorkspace sharedWorkspace] notificationCenter];
		observe(ws, NSWorkspaceDidActivateApplicationNotification, ^(NSNotification *n) {
			NSRunningApplication *app = n.userInfo[NSWorkspaceApplicationKey];
			watchApp(app.processIdentifier);
			reportFrontmost();
		});
		observe(ws, NSWorkspaceWillSleepNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("sleep", 1);
		});
		observe(ws, NSWorkspaceDidWakeNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("sleep", 0);
		});
		observe(ws, NSWorkspaceScreensDidSleepNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("display", 1);
		});
		observe(ws, NSWorkspaceScreensDidWakeNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("display", 0);
		});
		observe(ws, NSWorkspaceSessionDidResignActiveNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("session", 1);
		});
		observe(ws, NSWorkspaceSessionDidBecomeActiveNotification, ^(NSNotification *n) {
			(void)n;
			goDarwinPower("session", 0);
		});

		NSDistributedNotificationCenter *dnc = [NSDistributedNotificationCenter defaultCenter];
		observe(dnc, @"com.apple.screenIsLocked", ^(NSNotification *n) {
			(void)n;
			goDarwinPower("lock", 1);
		});
		observe(dnc, @"com.apple.screenIsUnlocked", ^(NSNotification *n) {
			(void)n;
			goDarwinPower("lock", 0);
		});
		return axTrusted;
	}
}

void trackz_darwin_run(void) {
	@autoreleasepool {
		NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
		if (app != nil)
			watchApp(app.processIdentifier);
		reportFrontmost();
	}
	CFRunLoopRun();
	@autoreleasepool {
		unwatchApp();
	}
}

void trackz_darwin_quit(void) {
	CFRunLoopStop(CFRunLoopGetMain());
}
