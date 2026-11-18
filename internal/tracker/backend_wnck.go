//go:build (linux || freebsd || openbsd || netbsd || dragonfly) && cgo && !nownck

package tracker

/*
#cgo pkg-config: libwnck-3.0 gdk-3.0
#include <stdlib.h>
#include "wnck.h"
*/
import "C"

import (
	"context"
	"errors"
	"os"
	"sync"
)

// wnckBackend watches X11 windows with libwnck. It replaces devilspie2.
type wnckBackend struct {
	opts BackendOptions
}

func newPlatformBackend(opts BackendOptions) (Backend, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, errors.New("DISPLAY is not set: libwnck needs an X11 (or XWayland) display")
	}
	return &wnckBackend{opts: opts}, nil
}

func (b *wnckBackend) Name() string { return "libwnck" }

var (
	wnckMu    sync.Mutex
	wnckSink  Sink
	wnckProcs = newProcCache()
)

// Run must be called on the main OS thread.
func (b *wnckBackend) Run(ctx context.Context, sink Sink) error {
	if rc := C.trackz_wnck_init(); rc != 0 {
		return errors.New("cannot open X11 display for libwnck")
	}
	wnckMu.Lock()
	wnckSink = sink
	wnckMu.Unlock()

	powerErr := make(chan error, 1)
	if !b.opts.NoPower {
		go func() { powerErr <- watchDBusPower(ctx, sink, b.opts) }()
	}
	go func() {
		<-ctx.Done()
		C.trackz_wnck_quit()
	}()
	C.trackz_wnck_run()

	wnckMu.Lock()
	wnckSink = nil
	wnckMu.Unlock()
	return nil
}

//export goWnckWindow
func goWnckWindow(pid C.int, title, appName, instance, class *C.char) {
	wnckMu.Lock()
	sink := wnckSink
	wnckMu.Unlock()
	if sink == nil {
		return
	}
	w := Window{
		PID:     int(pid),
		Title:   C.GoString(title),
		AppName: C.GoString(appName),
	}
	if w.PID > 0 {
		info := wnckProcs.lookup(w.PID)
		w.ProcessName, w.Owner = info.Name, info.Owner
	}
	if w.ProcessName == "" {
		// No _NET_WM_PID (e.g. remote X clients): fall back to WM_CLASS.
		w.ProcessName = C.GoString(instance)
	}
	// app_name comes from WM_CLASS (class group name, e.g. "XTerm",
	// "firefox-esr"). libwnck's application name is a guess that, for
	// applications that do not name their window group, is the title of the
	// first window seen, and does not follow later title changes; that made
	// per-application statistics unreliable.
	if c := C.GoString(class); c != "" {
		w.AppName = c
	}
	if w.AppName == "" {
		w.AppName = w.ProcessName
	}
	sink.WindowChanged(w)
}
