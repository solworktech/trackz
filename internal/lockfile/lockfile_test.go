package lockfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHelperHold is run as a separate process by TestLockedByOtherProcess.
func TestHelperHold(t *testing.T) {
	path := os.Getenv("LOCKFILE_HELPER")
	if path == "" {
		t.Skip("helper")
	}
	if _, err := Acquire(path); err != nil {
		os.Exit(3)
	}
	if _, err := os.Stdout.WriteString("locked\n"); err != nil {
		os.Exit(4)
	}
	select {} // hold until killed
}

func TestLockedByOtherProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trackz.db.lock")
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperHold")
	cmd.Env = append(os.Environ(), "LOCKFILE_HELPER="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 7)
	if _, err := out.Read(buf); err != nil || string(buf) != "locked\n" {
		t.Fatalf("helper did not lock: %q %v", buf, err)
	}
	_, err = Acquire(path)
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "PID") {
		t.Errorf("want ErrLocked naming the PID, got %v", err)
	}
	if held, pid, err := Held(path); !held || pid != cmd.Process.Pid || err != nil {
		t.Errorf("Held = %v, %d, %v; want true, %d", held, pid, err, cmd.Process.Pid)
	}
	// Killing the holder (no clean-up) releases the lock.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // "signal: killed", as intended
	if held, _, err := Held(path); held || err != nil {
		t.Errorf("Held after the holder died = %v, %v", held, err)
	}
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("lock should be free after the holder died: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if held, _, err := Held(path); held || err != nil {
		t.Errorf("Held after release (file removed) = %v, %v", held, err)
	}
}
