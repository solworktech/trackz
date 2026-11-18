//go:build darwin && cgo

package tracker

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=10.13
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include "darwin.h"
*/
import "C"

import (
	"context"
	"sync"
)

// darwinBackend uses NSWorkspace (application activation, sleep/wake,
// screen lock) and the Accessibility API (focused window and its title).
//
// Window titles require the binary (or the terminal running it) to be
// allowed under System Settings > Privacy & Security > Accessibility. Without
// it, the application name is recorded as the window title.
type darwinBackend struct {
	opts BackendOptions
}

func newPlatformBackend(opts BackendOptions) (Backend, error) {
	return &darwinBackend{opts: opts}, nil
}

func (b *darwinBackend) Name() string { return "macOS (NSWorkspace + Accessibility)" }

var (
	darwinMu    sync.Mutex
	darwinSink  Sink
	darwinOpts  BackendOptions
	darwinProcs = newProcCache()
)

// Run must be called on the main OS thread (see runtime.LockOSThread in
// cmd/trackz).
func (b *darwinBackend) Run(ctx context.Context, sink Sink) error {
	darwinMu.Lock()
	darwinSink, darwinOpts = sink, b.opts
	darwinMu.Unlock()

	if C.trackz_darwin_init(1) == 0 {
		b.opts.debugf("Accessibility access not granted: window titles unavailable; " +
			"allow trackz in System Settings > Privacy & Security > Accessibility")
	}
	go func() {
		<-ctx.Done()
		C.trackz_darwin_quit()
	}()
	C.trackz_darwin_run()

	darwinMu.Lock()
	darwinSink = nil
	darwinMu.Unlock()
	return nil
}

//export goDarwinWindow
func goDarwinWindow(pid C.int, title, appName *C.char) {
	darwinMu.Lock()
	sink := darwinSink
	darwinMu.Unlock()
	if sink == nil {
		return
	}
	w := Window{PID: int(pid), Title: C.GoString(title), AppName: C.GoString(appName)}
	info := darwinProcs.lookup(w.PID)
	w.ProcessName, w.Owner = info.Name, info.Owner
	if w.ProcessName == "" {
		w.ProcessName = w.AppName
	}
	sink.WindowChanged(w)
}

//export goDarwinPower
func goDarwinPower(source *C.char, away C.int) {
	darwinMu.Lock()
	sink, opts := darwinSink, darwinOpts
	darwinMu.Unlock()
	if sink == nil || opts.NoPower {
		return
	}
	sink.Power(PowerEvent{Source: C.GoString(source), Away: away != 0})
}
