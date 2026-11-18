// Package instance keeps a program to one running copy per user. A second
// copy started while the first runs asks the first to activate itself
// (e.g. show its window) and is told to exit.
//
// Send passes other requests, such as Quit, to the running copy.
//
// The first copy holds an OS lock on <dir>/<name>.lock, released by the OS
// even if the process is killed, and listens on the Unix-domain socket
// <dir>/<name>.sock (supported on Windows 10 1803 and later). <dir> is
// private to the user: $XDG_RUNTIME_DIR on Linux/BSD (else a 0700
// directory in /tmp), $TMPDIR on macOS, %TEMP% on Windows.
package instance

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/solworktech/trackz/internal/lockfile"
)

// ErrRunning is returned by Start when another copy is running; it has
// been asked to activate.
var ErrRunning = errors.New("already running")

// ErrNotRunning is returned by Send when no copy is running.
var ErrNotRunning = errors.New("not running")

// Request is what a later copy (or Send) asks of the running one.
type Request string

const (
	// Activate asks the running copy to show itself.
	Activate Request = "activate"
	// Quit asks the running copy to exit.
	Quit Request = "quit"
)

// Instance is the running copy.
type Instance struct {
	lock     *lockfile.Lock
	ln       net.Listener
	sockPath string
}

const (
	reply = "ok\n"
	// How long a second copy keeps trying to reach the first, which may
	// still be starting (it holds the lock before it listens).
	dialTimeout = 3 * time.Second
	ioTimeout   = 2 * time.Second
)

// Start makes this process the running copy of name, calling onRequest
// (from another goroutine) with Activate whenever a later copy starts, and
// with whatever Send asks. If another copy is already running, it asks it
// to activate and returns ErrRunning.
func Start(name string, onRequest func(Request)) (*Instance, error) {
	dir, err := runtimeDir(name)
	if err != nil {
		return nil, err
	}
	base := filepath.Join(dir, name)
	lock, err := lockfile.Acquire(base + ".lock")
	if errors.Is(err, lockfile.ErrLocked) {
		if err := send(base+".sock", Activate); err != nil {
			return nil, fmt.Errorf("%s is running but did not respond: %w", name, err)
		}
		return nil, ErrRunning
	}
	if err != nil {
		return nil, err
	}
	in := &Instance{lock: lock, sockPath: base + ".sock"}
	// A socket file left by a copy that was killed; we hold the lock, so
	// nobody listens on it.
	if err := os.Remove(in.sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errors.Join(fmt.Errorf("remove stale socket: %w", err), lock.Release())
	}
	ln, err := net.Listen("unix", in.sockPath)
	if err != nil {
		return nil, errors.Join(err, lock.Release())
	}
	in.ln = ln
	go in.serve(onRequest)
	return in, nil
}

// Close stops answering later copies and releases the lock.
func (in *Instance) Close() error {
	err := in.ln.Close() // the listener removes its socket file
	return errors.Join(err, in.lock.Release())
}

// Send asks the running copy of name to carry out req. It returns
// ErrNotRunning if there is none.
func Send(name string, req Request) error {
	dir, err := runtimeDir(name)
	if err != nil {
		return err
	}
	held, err := Running(name)
	if err != nil {
		return err
	}
	if !held {
		return ErrNotRunning
	}
	return send(filepath.Join(dir, name)+".sock", req)
}

// Running reports whether a copy of name is running.
func Running(name string) (bool, error) {
	dir, err := runtimeDir(name)
	if err != nil {
		return false, err
	}
	held, _, err := lockfile.Held(filepath.Join(dir, name) + ".lock")
	return held, err
}

func (in *Instance) serve(onRequest func(Request)) {
	for {
		conn, err := in.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			log.Printf("instance: accept: %v", err)
			time.Sleep(100 * time.Millisecond) // don't spin on a persistent error
			continue
		}
		req, err := handle(conn)
		if err != nil {
			log.Printf("instance: %v", err)
			continue
		}
		if onRequest != nil {
			onRequest(req)
		}
	}
}

// handle reads one request and confirms it.
func handle(conn net.Conn) (req Request, err error) {
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read request: %w", err)
	}
	req = Request(strings.TrimSuffix(line, "\n"))
	if req != Activate && req != Quit {
		return "", fmt.Errorf("unexpected request %q", req)
	}
	if _, err := conn.Write([]byte(reply)); err != nil {
		return "", fmt.Errorf("reply: %w", err)
	}
	return req, nil
}

// send asks the running copy listening on sock to carry out req.
func send(sock string, req Request) error {
	if req == Activate {
		allowForeground()
	}
	deadline := time.Now().Add(dialTimeout)
	for {
		err := ask(sock, req)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func ask(sock string, req Request) (err error) {
	conn, err := net.DialTimeout("unix", sock, ioTimeout)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
		return err
	}
	if _, err := conn.Write([]byte(string(req) + "\n")); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if line != reply {
		return fmt.Errorf("unexpected reply %q", strings.TrimSpace(line))
	}
	return nil
}
