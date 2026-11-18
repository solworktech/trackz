package tracker

// BackendOptions configure platform backends.
type BackendOptions struct {
	// NoPower disables sleep/lock detection.
	NoPower bool
	// DBusMonitor makes the Linux/BSD backend eavesdrop on the session bus
	// (org.freedesktop.DBus.Monitoring.BecomeMonitor), as the original Rust
	// viewer did, instead of subscribing to broadcast signals. Use it if
	// your screen locker does not broadcast ActiveChanged.
	DBusMonitor bool
	// Debugf, if set, receives backend diagnostics.
	Debugf func(format string, args ...any)
}

func (o BackendOptions) debugf(format string, args ...any) {
	if o.Debugf != nil {
		o.Debugf(format, args...)
	}
}

// NewBackend returns the backend for the current platform.
func NewBackend(opts BackendOptions) (Backend, error) {
	return newPlatformBackend(opts)
}
