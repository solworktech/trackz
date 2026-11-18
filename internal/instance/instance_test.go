package instance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const helperEnv = "TRACKZ_INSTANCE_HELPER"

// TestHelperRunning runs as a separate process: the first copy.
func TestHelperRunning(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("helper")
	}
	if _, err := Start("trackz-test", func(req Request) {
		if _, err := os.Stdout.WriteString(string(req) + "\n"); err != nil {
			os.Exit(4)
		}
	}); err != nil {
		// The test shows this as what the helper said.
		if _, werr := os.Stdout.WriteString("error: " + err.Error() + "\n"); werr != nil {
			os.Exit(4)
		}
		os.Exit(3)
	}
	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		os.Exit(4)
	}
	select {} // until killed
}

func TestSecondCopyActivatesFirst(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir) // Linux/BSD
	t.Setenv("TMPDIR", dir)          // macOS
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperRunning$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	kill := func() {
		t.Helper()
		if killed {
			return
		}
		killed = true
		if err := cmd.Process.Kill(); err != nil {
			t.Error(err)
		}
		_ = cmd.Wait() // "signal: killed", as intended
	}
	defer kill()
	lines := make(chan string, 4)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := out.Read(buf)
			if n > 0 {
				lines <- string(buf[:n])
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("helper said %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("helper did not say %q", want)
		}
	}
	expect("ready\n")

	if _, err := Start("trackz-test", nil); !errors.Is(err, ErrRunning) {
		t.Fatalf("second copy: got %v, want ErrRunning", err)
	}
	expect("activate\n")
	if err := Send("trackz-test", Quit); err != nil {
		t.Fatalf("send quit: %v", err)
	}
	expect("quit\n")

	// Once the first copy is gone (killed: no clean-up), a new one starts.
	kill()
	if err := Send("trackz-test", Quit); !errors.Is(err, ErrNotRunning) {
		t.Errorf("send with none running: %v", err)
	}
	in, err := Start("trackz-test", nil)
	if err != nil {
		t.Fatalf("after the first copy died: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// shortTempDir is a temporary directory with a short path: t.TempDir is
// under $TMPDIR, which on macOS (/var/folders/…/T/) with the test's name
// makes the socket path exceed the 103 characters a Unix socket allows.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "ti")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func TestSocketPathTooLong(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the runtime directory on Windows is the TEMP directory, not set by the test")
	}
	dir := filepath.Join(shortTempDir(t), strings.Repeat("d", 100))
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("TMPDIR", dir)
	_, err := Start("trackz-test", nil)
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("want a too-long error, got %v", err)
	}
}
