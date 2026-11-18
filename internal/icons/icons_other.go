//go:build !(linux || freebsd || openbsd || netbsd || dragonfly || windows || (darwin && cgo))

package icons

import "image"

func find(process, app string, size int) image.Image { return nil }
