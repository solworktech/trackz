// Package icons finds application icons for process/application names, so
// frontends can decorate charts. Lookups are cached; a nil image means no
// icon was found.
//
//	Linux/BSD: freedesktop .desktop entries and icon themes (PNG and SVG)
//	macOS:     the application bundle's icon (NSWorkspace)
//	Windows:   the icon embedded in the running executable
package icons

import (
	"image"
	"image/png"
	"sync"
)

// Resolver caches icon lookups.
type Resolver struct {
	size  int
	mu    sync.Mutex
	cache map[string]image.Image
}

// New returns a resolver preferring icons of about size pixels.
func New(size int) *Resolver {
	return &Resolver{size: size, cache: map[string]image.Image{}}
}

// Icon returns the icon for a process (executable name) and application
// name, or nil.
func (r *Resolver) Icon(process, app string) image.Image {
	if process == "" && app == "" {
		return nil
	}
	key := process + "\x00" + app
	r.mu.Lock()
	img, ok := r.cache[key]
	r.mu.Unlock()
	if ok {
		return img
	}
	img = find(process, app, r.size)
	r.mu.Lock()
	r.cache[key] = img
	r.mu.Unlock()
	return img
}

var _ = png.Decode // register PNG decoder
