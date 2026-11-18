//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

// Windows has no SIGTERM for a process without a console: trackz waits on
// a named event per database, which "trackz stop" sets.
func stopEventName(dbPath string) (*uint16, error) {
	sum := sha256.Sum256([]byte(dbPath))
	return windows.UTF16PtrFromString(fmt.Sprintf(`Local\trackz-stop-%x`, sum[:6]))
}

// listenForStop calls stop when "trackz stop" is run for dbPath. The
// returned function stops listening.
func listenForStop(ctx context.Context, dbPath string, stop func()) (func(), error) {
	name, err := stopEventName(dbPath)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateEvent(nil, 1, 0, name) // manual reset, initially unset
	if err != nil {
		return nil, fmt.Errorf("create stop event: %w", err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			ev, err := windows.WaitForSingleObject(h, 500)
			switch {
			case err != nil:
				log.Printf("trackz: waiting for stop requests: %v", err)
				return
			case ev == windows.WAIT_OBJECT_0:
				stop()
				return
			}
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
		_ = windows.CloseHandle(h) // exiting; the handle goes with the process anyway
	}, nil
}

// requestStop asks the trackz recording to dbPath to stop.
func requestStop(dbPath string, _ int) error {
	name, err := stopEventName(dbPath)
	if err != nil {
		return err
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("it does not accept stop requests (started by an older trackz?): %w", err)
	}
	defer func() { _ = windows.CloseHandle(h) }() // only signalled
	return windows.SetEvent(h)
}

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// attachConsole makes output visible when trackz (built without a console
// window, so it starts silently at login) is run from a terminal.
func attachConsole() {
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS: (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return // not started from a console: nowhere to write to
	}
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	os.Stdout, os.Stderr = out, out
	log.SetOutput(out)
}
