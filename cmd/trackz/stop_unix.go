//go:build !windows

package main

import (
	"context"
	"errors"
	"syscall"
)

// listenForStop: on Unix, "trackz stop" sends SIGTERM, which run already
// handles.
func listenForStop(context.Context, string, func()) (func(), error) {
	return func() {}, nil
}

// requestStop asks the trackz recording to dbPath (process pid) to stop.
func requestStop(_ string, pid int) error {
	if pid <= 0 {
		return errors.New("its process ID is unknown")
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// attachConsole is only needed on Windows.
func attachConsole() {}
