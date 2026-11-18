//go:build windows

package tray

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	kernel  = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW         = kernel.NewProc("GetModuleHandleW")
)

const (
	wmNull         = 0x0000
	wmClose        = 0x0010
	wmDestroy      = 0x0002
	wmCommand      = 0x0111
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmContextMenu  = 0x007B
	wmApp          = 0x8000
	wmTrayCallback = wmApp + 1
	wmUpdateMenu   = wmApp + 2

	nimAdd     = 0x0
	nimModify  = 0x1
	nimDelete  = 0x2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString    = 0x0000
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             windows.HWND
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            windows.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     windows.Handle
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

type winTray struct {
	opts           Options
	hwnd           uintptr
	icon           windows.Handle
	taskbarCreated uintptr
	mu             sync.Mutex
	items          []Item
	ready          chan error
	closeOnce      sync.Once
}

// Only one tray icon per process is supported, which is all Trackz needs;
// the window procedure finds it here.
var (
	current   *winTray
	wndProcCB = syscall.NewCallback(wndProc)
	classOnce sync.Once
	classErr  error
	className = windows.StringToUTF16Ptr("TrackzTrayWindow")
)

func newImpl(opts Options) (impl, error) {
	t := &winTray{opts: opts, items: opts.Items, ready: make(chan error, 1)}
	current = t
	go t.loop()
	if err := <-t.ready; err != nil {
		return nil, err
	}
	return t, nil
}

// loop owns the hidden window, the icon and the message loop; Windows
// requires them to stay on one thread.
func (t *winTray) loop() {
	runtime.LockOSThread()
	inst, _, _ := procGetModuleHandleW.Call(0)
	classOnce.Do(func() {
		wc := wndClassEx{WndProc: wndProcCB, Instance: windows.Handle(inst), ClassName: className}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			classErr = fmt.Errorf("tray: RegisterClassEx: %v", err)
		}
	})
	if classErr != nil {
		t.ready <- classErr
		return
	}
	// A hidden top-level window (not message-only) so that the
	// "TaskbarCreated" broadcast arrives when Explorer restarts.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		t.ready <- fmt.Errorf("tray: CreateWindowEx: %v", err)
		return
	}
	t.hwnd = hwnd
	t.taskbarCreated, _, _ = procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	t.icon = makeIcon(t.opts.Icon)
	if err := t.notify(nimAdd); err != nil {
		_, _, _ = procDestroyWindow.Call(hwnd) // the notify error is the one to report
		t.ready <- err
		return
	}
	t.ready <- nil

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		// Their results only say whether a translation happened and what
		// the window procedure returned.
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if t.icon != 0 {
		_, _, _ = procDestroyIcon.Call(uintptr(t.icon)) // exiting; freed with the process anyway
	}
}

func (t *winTray) notify(op uintptr) error {
	nid := notifyIconData{
		HWnd:             windows.HWND(t.hwnd),
		UID:              1,
		UFlags:           nifMessage | nifTip,
		UCallbackMessage: wmTrayCallback,
		HIcon:            t.icon,
	}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	if t.icon != 0 {
		nid.UFlags |= nifIcon
	}
	tip := t.opts.Tooltip
	if tip == "" {
		tip = t.opts.Title
	}
	copy(nid.SzTip[:len(nid.SzTip)-1], windows.StringToUTF16(tip))
	if r, _, err := procShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return fmt.Errorf("tray: Shell_NotifyIcon: %v", err)
	}
	return nil
}

// makeIcon creates an HICON from a PNG of the logo (Windows Vista and later
// accept PNG data here).
func makeIcon(icon func(int) image.Image) windows.Handle {
	if icon == nil {
		return 0
	}
	img := icon(32)
	if img == nil {
		return 0
	}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return 0
	}
	b := buf.Bytes()
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 1, 0x00030000, 32, 32, 0)
	return windows.Handle(h)
}

func (t *winTray) showMenu() {
	t.mu.Lock()
	items := append([]Item(nil), t.items...)
	t.mu.Unlock()
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer func() { _, _, _ = procDestroyMenu.Call(menu) }() // nothing to do if it fails
	for i, it := range items {
		if it.Label == "" {
			if r, _, err := procAppendMenuW.Call(menu, mfSeparator, 0, 0); r == 0 {
				log.Printf("tray: AppendMenu: %v", err)
				return
			}
			continue
		}
		label, err := windows.UTF16PtrFromString(it.Label)
		if err != nil {
			log.Printf("tray: menu label %q: %v", it.Label, err)
			return
		}
		if r, _, err := procAppendMenuW.Call(menu, mfString, uintptr(i+1), uintptr(unsafe.Pointer(label))); r == 0 {
			log.Printf("tray: AppendMenu: %v", err)
			return
		}
	}
	var pt struct{ X, Y int32 }
	if r, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		log.Printf("tray: GetCursorPos: %v", err)
		return
	}
	// Required so the menu closes when the user clicks elsewhere. If
	// refused, the menu still works but may stay open after clicking away.
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	id, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	// The documented companion of the call above; harmless if it fails.
	_, _, _ = procPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	if id >= 1 && int(id) <= len(items) {
		run(items[id-1].OnClick)
	}
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	t := current
	switch {
	case t == nil:
	case message == wmTrayCallback:
		switch lParam & 0xffff {
		case wmLButtonUp:
			run(t.opts.OnActivate)
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case message == uintptr(t.taskbarCreated) && t.taskbarCreated != 0:
		if err := t.notify(nimAdd); err != nil { // Explorer restarted
			log.Printf("tray: cannot show the icon again: %v", err)
		}
		return 0
	case message == wmClose:
		// Exiting: the icon also goes once the window is destroyed.
		_ = t.notify(nimDelete)
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case message == wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0) // no return value
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

// The icon exists once NIM_ADD succeeded (newImpl fails otherwise).
func (t *winTray) active() bool { return true }

// setItems takes effect the next time the menu is opened (it is built then).
func (t *winTray) setItems(items []Item) error {
	t.mu.Lock()
	t.items = items
	t.mu.Unlock()
	return nil
}

func (t *winTray) close() {
	t.closeOnce.Do(func() {
		if r, _, err := procPostMessageW.Call(t.hwnd, wmClose, 0, 0); r == 0 {
			log.Printf("tray: cannot remove the icon: %v", err)
		}
	})
}
