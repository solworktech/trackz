//go:build windows

package instance

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// runtimeDir is %TEMP%, which is per user.
func runtimeDir(string) (string, error) { return os.TempDir(), nil }

var procAllowSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("AllowSetForegroundWindow")

// allowForeground lets the running copy bring its window to the front:
// Windows only lets the process the user last interacted with (this one,
// just started) take the foreground, or one it allows.
func allowForeground() {
	const asfwAny = ^uintptr(0) // ASFW_ANY: (DWORD)-1
	if r, _, err := procAllowSetForegroundWindow.Call(asfwAny); r == 0 {
		// The window then only flashes in the taskbar.
		log.Printf("instance: AllowSetForegroundWindow: %v", err)
	}
}
