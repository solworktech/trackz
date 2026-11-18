//go:build !((linux || freebsd || openbsd || netbsd || dragonfly) && cgo && !nownck) && !(darwin && cgo) && !windows

package tracker

import (
	"fmt"
	"runtime"
)

func newPlatformBackend(BackendOptions) (Backend, error) {
	return nil, fmt.Errorf("no window tracking backend for %s/%s in this build (Unix builds need cgo and libwnck-3; macOS needs cgo)",
		runtime.GOOS, runtime.GOARCH)
}
