//go:build darwin && cgo

package tray

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -mmacosx-version-min=10.13
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "tray_darwin.h"
*/
import "C"

import (
	"bytes"
	"fmt"
	"image/png"
	"sync"
	"unsafe"
)

type darwinTray struct {
	opts  Options
	mu    sync.Mutex
	items []Item
}

var darwinCurrent *darwinTray

func newImpl(opts Options) (impl, error) {
	t := &darwinTray{opts: opts}
	darwinCurrent = t
	var buf bytes.Buffer
	if opts.Icon != nil {
		if img := opts.Icon(64); img != nil {
			if err := png.Encode(&buf, img); err != nil {
				return nil, fmt.Errorf("tray: encode icon: %w", err)
			}
		}
	}
	data := buf.Bytes()
	if len(data) == 0 {
		data = []byte{0}
	}
	tip := opts.Tooltip
	if tip == "" {
		tip = opts.Title
	}
	cdata, ctip := C.CBytes(data), C.CString(tip)
	defer C.free(cdata)
	defer C.free(unsafe.Pointer(ctip))
	C.trackz_tray_create(cdata, C.int(len(data)), ctip)
	if err := t.setItems(opts.Items); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *darwinTray) setItems(items []Item) error {
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
	C.trackz_tray_menu_reset()
	for i, it := range items {
		if it.Label == "" {
			C.trackz_tray_menu_add(0, nil)
			continue
		}
		cl := C.CString(it.Label)
		C.trackz_tray_menu_add(C.int(i+1), cl)
		C.free(unsafe.Pointer(cl))
	}
	return nil
}

func (t *darwinTray) close() { C.trackz_tray_remove() }

// The menu bar is always there.
func (t *darwinTray) active() bool { return true }

//export goTrayActivate
func goTrayActivate() {
	if t := darwinCurrent; t != nil {
		run(t.opts.OnActivate)
	}
}

//export goTrayMenuItem
func goTrayMenuItem(tag C.int) {
	t := darwinCurrent
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if i := int(tag) - 1; i >= 0 && i < len(t.items) {
		run(t.items[i].OnClick)
	}
}
