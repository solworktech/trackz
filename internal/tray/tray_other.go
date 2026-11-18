//go:build !(linux || freebsd || openbsd || netbsd || dragonfly || windows || (darwin && cgo))

package tray

func newImpl(Options) (impl, error) { return nil, ErrUnsupported }
