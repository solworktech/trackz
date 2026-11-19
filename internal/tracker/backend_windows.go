//go:build windows

package tracker

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsBackend uses SetWinEventHook for foreground and title changes, and a
// hidden window for suspend/resume (WM_POWERBROADCAST) and session
// lock/unlock (WM_WTSSESSION_CHANGE). No cgo is needed.
type windowsBackend struct {
	opts BackendOptions
}

func newPlatformBackend(opts BackendOptions) (Backend, error) {
	return &windowsBackend{opts: opts}, nil
}

func (b *windowsBackend) Name() string { return "Windows (WinEvents)" }

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	wtsapi  = windows.NewLazySystemDLL("wtsapi32.dll")
	kernel  = windows.NewLazySystemDLL("kernel32.dll")
	version = windows.NewLazySystemDLL("version.dll")

	procSetWinEventHook      = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent       = user32.NewProc("UnhookWinEvent")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW   = user32.NewProc("PostThreadMessageW")
	procGetForegroundWindow  = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procEnumChildWindows     = user32.NewProc("EnumChildWindows")
	procGetClassNameW        = user32.NewProc("GetClassNameW")
	procGetModuleHandleW     = kernel.NewProc("GetModuleHandleW")
	procWTSRegister          = wtsapi.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegister        = wtsapi.NewProc("WTSUnRegisterSessionNotification")
	procGetFileVersionInfoSz = version.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfo   = version.NewProc("GetFileVersionInfoW")
	procVerQueryValue        = version.NewProc("VerQueryValueW")
)

const (
	eventSystemForeground  = 0x0003
	eventSystemSwitchEnd   = 0x0015
	eventSystemMinimizeEnd = 0x0017
	eventObjectNameChange  = 0x800C
	winEventOutOfContext   = 0x0000
	winEventSkipOwnProcess = 0x0002
	objidWindow            = 0
	childidSelf            = 0

	wmQuit              = 0x0012
	wmCheckForeground   = 0x8000 + 1 // WM_APP+1, posted to the hook thread
	wmPowerBroadcast    = 0x0218
	wmWTSSessionChange  = 0x02B1
	pbtAPMSuspend       = 0x0004
	pbtAPMResumeSuspend = 0x0007
	pbtAPMResumeAuto    = 0x0012
	wtsSessionLock      = 0x7
	wtsSessionUnlock    = 0x8
	notifyForThisSess   = 0
)

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

var (
	winMu    sync.Mutex
	winSink  Sink
	winOpts  BackendOptions
	winProcs = newProcCache()
	// The hook thread, for wmCheckForeground, and a channel closed when
	// Run returns.
	winThread  uint32
	winStopped chan struct{}

	// switcherUp is set while the foreground window is the shell's task
	// switcher (see isTaskSwitcher), and the foreground is polled.
	switcherUp atomic.Bool

	// Callbacks are created once: Windows limits the number of callbacks a
	// process can create with syscall.NewCallback.
	winEventCallback = syscall.NewCallback(winEventProc)
	wndProcCallback  = syscall.NewCallback(wndProc)
)

// Run pumps Windows messages on a locked OS thread until ctx is cancelled.
func (b *windowsBackend) Run(ctx context.Context, sink Sink) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	threadID := windows.GetCurrentThreadId()
	stopped := make(chan struct{})
	defer close(stopped)

	winMu.Lock()
	winSink, winOpts = sink, b.opts
	winThread, winStopped = threadID, stopped
	winMu.Unlock()
	defer func() {
		winMu.Lock()
		winSink = nil
		winMu.Unlock()
		switcherUp.Store(false)
	}()

	var hooks []uintptr
	for _, ev := range []uintptr{eventSystemForeground, eventSystemSwitchEnd, eventSystemMinimizeEnd, eventObjectNameChange} {
		h, _, err := procSetWinEventHook.Call(ev, ev, 0, winEventCallback, 0, 0,
			winEventOutOfContext|winEventSkipOwnProcess)
		if h == 0 {
			return fmt.Errorf("SetWinEventHook(%#x): %v", ev, err)
		}
		hooks = append(hooks, h)
	}
	defer func() {
		for _, h := range hooks {
			_, _, _ = procUnhookWinEvent.Call(h) // also undone when the thread exits
		}
	}()

	if !b.opts.NoPower {
		if hwnd, err := createPowerWindow(); err != nil {
			b.opts.debugf("sleep/lock tracking disabled: %v", err)
		} else {
			if r, _, err := procWTSRegister.Call(hwnd, notifyForThisSess); r == 0 {
				b.opts.debugf("screen lock tracking disabled: WTSRegisterSessionNotification: %v", err)
			}
			defer func() {
				// Shutting down: the window goes with the thread anyway.
				_, _, _ = procWTSUnRegister.Call(hwnd)
				_, _, _ = procDestroyWindow.Call(hwnd)
			}()
		}
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-stopped:
			return
		}
		// PostThreadMessage fails until the thread has a message queue
		// (created by its first GetMessage); retry rather than leave the
		// loop running forever.
		for {
			r, _, err := procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
			if r != 0 {
				return
			}
			b.opts.debugf("PostThreadMessage(WM_QUIT): %v; retrying", err)
			select {
			case <-stopped:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	checkForeground()
	var m msg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1:
			return fmt.Errorf("GetMessage: %v", err)
		case 0: // WM_QUIT
			return nil
		}
		if m.hwnd == 0 && m.message == wmCheckForeground {
			checkForeground()
			continue
		}
		// Their results only say whether a translation happened and what
		// the window procedure returned.
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func winEventProc(hook, event, hwnd, idObject, idChild, thread, evTime uintptr) uintptr {
	if hwnd == 0 || int32(idObject) != objidWindow || int32(idChild) != childidSelf {
		return 0
	}
	switch event {
	case eventSystemForeground, eventSystemMinimizeEnd:
		reportWindow(hwnd)
	case eventSystemSwitchEnd:
		// Alt+Tab ended: hwnd is the switcher; report what it activated.
		checkForeground()
	case eventObjectNameChange:
		// Title changes of background windows are ignored.
		if fg, _, _ := procGetForegroundWindow.Call(); fg == hwnd {
			reportWindow(hwnd)
		}
	}
	return 0
}

func reportWindow(hwnd uintptr) {
	winMu.Lock()
	sink := winSink
	winMu.Unlock()
	if sink == nil {
		return
	}
	if isTaskSwitcher(hwnd) {
		// Not an application: keep the previous window, and find out
		// which window the switcher activates.
		watchSwitcher()
		return
	}
	switcherUp.Store(false)
	w := Window{Title: windowText(hwnd)}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil || pid == 0 {
		return // the window is already gone
	}
	info, path := processDetails(pid)
	// Store (UWP) apps are hosted by ApplicationFrameHost.exe; the real
	// process owns a child window.
	if strings.EqualFold(info.Name, "ApplicationFrameHost") {
		if child := uwpChildPID(hwnd, pid); child != 0 {
			pid = child
			info, path = processDetails(pid)
		}
	}
	w.PID = int(pid)
	w.ProcessName, w.Owner = info.Name, info.Owner
	w.AppName = fileDescription(path)
	if w.AppName == "" {
		w.AppName = w.ProcessName
	}
	sink.WindowChanged(w)
}

// switcherClasses are the window classes of explorer's task switching UI,
// which takes the foreground while it is up: Alt+Tab (TaskSwitcherWnd up to
// Windows 8, MultitaskingViewFrame on 10, XamlExplorerHostIslandWindow on
// 11, which also hosts Task View) and ForegroundStaging, a window explorer
// briefly activates while switching. Their titles ("Task Switching") are
// localised; the classes are not.
var switcherClasses = map[string]bool{
	"TaskSwitcherWnd":              true,
	"MultitaskingViewFrame":        true,
	"XamlExplorerHostIslandWindow": true,
	"ForegroundStaging":            true,
}

// isTaskSwitcher reports whether hwnd is explorer's task switching UI.
func isTaskSwitcher(hwnd uintptr) bool {
	var buf [64]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || !switcherClasses[windows.UTF16ToString(buf[:n])] {
		return false
	}
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil || pid == 0 {
		return false
	}
	info, _ := processDetails(pid)
	return strings.EqualFold(info.Name, "explorer")
}

// switcherPoll is how often the foreground is checked while the task
// switcher has it.
const switcherPoll = 250 * time.Millisecond

// watchSwitcher polls the foreground window while the task switcher has it:
// Windows does not always send EVENT_SYSTEM_FOREGROUND for the window
// picked with Alt+Tab, notably when it is the one that had the foreground
// before. The checks run on the hook thread (wmCheckForeground) and stop
// once a window other than the switcher has the foreground, or Run returns.
func watchSwitcher() {
	if switcherUp.Swap(true) {
		return // already polling
	}
	winMu.Lock()
	thread, stopped, opts := winThread, winStopped, winOpts
	winMu.Unlock()
	opts.debugf("task switcher has the foreground; waiting for the window it activates")
	go func() {
		t := time.NewTicker(switcherPoll)
		defer t.Stop()
		for switcherUp.Load() {
			select {
			case <-stopped:
				return
			case <-t.C:
			}
			if r, _, err := procPostThreadMessageW.Call(uintptr(thread), wmCheckForeground, 0, 0); r == 0 {
				opts.debugf("PostThreadMessage(check foreground): %v", err)
			}
		}
	}()
}

// checkForeground reports the foreground window, if there is one (there
// is none for a moment while the foreground changes).
func checkForeground() {
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		reportWindow(fg)
	}
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	n, _, _ = procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n]) // n is 0 if the window went away
}

var winPaths sync.Map // pid -> path, refreshed together with winProcs

// processDetails returns the process name (executable base name without
// ".exe", so names match other platforms), owner and executable path.
func processDetails(pid uint32) (procInfo, string) {
	info := winProcs.lookup(int(pid))
	p, _ := winPaths.Load(pid)
	path, _ := p.(string)
	return info, path
}

// processInfo implements the per-OS hook used by procCache.
func processInfo(pid int) (procInfo, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return procInfo{}, err
	}
	defer func() { _ = windows.CloseHandle(h) }() // query only

	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	var info procInfo
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err == nil {
		path := windows.UTF16ToString(buf[:size])
		winPaths.Store(uint32(pid), path)
		base := filepath.Base(path)
		info.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	var token windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err == nil {
		defer func() { _ = token.Close() }() // query only
		if tu, err := token.GetTokenUser(); err == nil {
			if account, _, _, err := tu.User.Sid.LookupAccount(""); err == nil {
				info.Owner = account
			} else {
				info.Owner = tu.User.Sid.String()
			}
		}
	}
	return info, nil
}

var (
	uwpMu           sync.Mutex
	uwpHost, uwpHit uint32
	uwpEnumCallback = syscall.NewCallback(uwpEnumProc)
)

// uwpChildPID finds the process owning a child window of an
// ApplicationFrameHost window, i.e. the actual Store app.
func uwpChildPID(hwnd uintptr, hostPID uint32) uint32 {
	uwpMu.Lock()
	defer uwpMu.Unlock()
	uwpHost, uwpHit = hostPID, 0
	// Its result is not meaningful (the callback stops it early on a hit).
	_, _, _ = procEnumChildWindows.Call(hwnd, uwpEnumCallback, 0)
	return uwpHit
}

func uwpEnumProc(child, _ uintptr) uintptr {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(windows.HWND(child), &pid); err != nil {
		return 1 // keep looking
	}
	if pid != 0 && pid != uwpHost {
		uwpHit = pid
		return 0 // stop enumerating
	}
	return 1
}

// fileDescription reads FileDescription from the executable's version
// resource (e.g. "Firefox", "Microsoft Edge"); the Windows equivalent of an
// application name.
func fileDescription(path string) string {
	if path == "" {
		return ""
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	size, _, _ := procGetFileVersionInfoSz.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return ""
	}
	data := make([]byte, size)
	if r, _, _ := procGetFileVersionInfo.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		return ""
	}
	var trans *[2]uint16
	var n uint32
	sub, _ := windows.UTF16PtrFromString(`\VarFileInfo\Translation`)
	if r, _, _ := procVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(&trans)), uintptr(unsafe.Pointer(&n))); r == 0 || n < 4 {
		return ""
	}
	key := fmt.Sprintf(`\StringFileInfo\%04x%04x\FileDescription`, trans[0], trans[1])
	sub, _ = windows.UTF16PtrFromString(key)
	var val *uint16
	if r, _, _ := procVerQueryValue.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(&val)), uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 {
		return ""
	}
	return strings.TrimSpace(windows.UTF16PtrToString(val))
}

func createPowerWindow() (uintptr, error) {
	inst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("TrackzPowerWatcher")
	wc := wndClassEx{
		wndProc:   wndProcCallback,
		instance:  windows.Handle(inst),
		className: className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, fmt.Errorf("RegisterClassEx: %v", err)
	}
	// A hidden top-level window (not a message-only window, which would not
	// receive WM_POWERBROADCAST).
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %v", err)
	}
	return hwnd, nil
}

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmPowerBroadcast:
		switch wParam {
		case pbtAPMSuspend:
			power("sleep", true)
		case pbtAPMResumeSuspend, pbtAPMResumeAuto:
			power("sleep", false)
		}
		return 1
	case wmWTSSessionChange:
		switch wParam {
		case wtsSessionLock:
			power("lock", true)
		case wtsSessionUnlock:
			power("lock", false)
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

func power(source string, away bool) {
	winMu.Lock()
	sink := winSink
	winMu.Unlock()
	if sink != nil {
		sink.Power(PowerEvent{Source: source, Away: away})
	}
}
