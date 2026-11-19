// Command trackz records which window has focus, and for how long, in an
// SQLite database. It replaces the devilspie2 + Lua hooks setup.
//
//	trackz [run] [flags]          record focus events (default)
//	trackz autostart [flags]      run, unless the systemd user unit does
//	trackz init-db [flags]        create the database and table
//	trackz fix-timestamps [flags] correct rows written by the Lua hooks
//	trackz stop [flags]           stop the trackz recording to the database
//	trackz version
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/solworktech/trackz/internal/config"
	"github.com/solworktech/trackz/internal/lockfile"
	"github.com/solworktech/trackz/internal/store"
	"github.com/solworktech/trackz/internal/tracker"
	"github.com/solworktech/trackz/internal/version"
)

// GUI toolkits (GTK for libwnck, AppKit on macOS) must run on the main
// thread; keep the main goroutine there.
func init() { runtime.LockOSThread() }

func main() {
	attachConsole()
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = cmdRun(args)
	case "autostart":
		err = cmdAutostart(args)
	case "init-db":
		err = cmdInitDB(args)
	case "fix-timestamps":
		err = cmdFixTimestamps(args)
	case "stop":
		err = cmdStop(args)
	case "version":
		fmt.Println("trackz", version.Version)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		usage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "trackz:", err)
		if errors.Is(err, lockfile.ErrLocked) {
			os.Exit(exitLocked)
		}
		os.Exit(1)
	}
}

// exitLocked is the exit status when another trackz holds the database
// (the systemd unit does not restart trackz for it: RestartPreventExitStatus).
const exitLocked = 3

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: trackz <command> [flags]

Commands:
  run             record window focus events (default)
  autostart       run, unless the systemd user unit starts trackz in this
                  session (for the XDG autostart entry; takes run's flags)
  init-db         create the database (and the trackz table) if missing
  fix-timestamps  correct timestamps written by the old devilspie2 Lua hooks
  stop            stop the trackz recording to the database, and wait for it
  version         print the version

Run "trackz <command> -h" for the flags of a command.
`)
}

func loadConfig() config.Config {
	path, err := config.Path()
	if err != nil {
		return config.Default()
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("warning: %v (using defaults)", err)
	}
	return cfg
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dbFlag := fs.String("db", "", "database path (default: $TRACKZ_DB, config file, or per-user data dir)")
	debug := fs.Bool("debug", false, "log every recorded event")
	logFile := fs.String("log", "", "append log output to this file instead of stderr")
	idFile := fs.String("id-file", "", `file mirroring the open record's id (default $XDG_RUNTIME_DIR/trackz_last_event_id; "-" to disable)`)
	noPower := fs.Bool("no-power", false, "do not record sleep/lock periods")
	dbusMonitor := fs.Bool("dbus-monitor", false, "Linux/BSD: eavesdrop on the session bus for screen-lock signals (BecomeMonitor)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }() // nothing left to log a failure to
		logger.SetOutput(f)
	}

	cfg := loadConfig()
	dbPath, err := config.ResolveDBPath(*dbFlag, cfg)
	if err != nil {
		return err
	}
	// SIGHUP (terminal closed) and SIGQUIT also end the run cleanly, so the
	// open record is closed.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer cancel()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	// One tracker per database: two would record every event twice.
	// trackz-viz briefly takes a shared lock to check that trackz runs, so
	// retry for a moment before giving up.
	var lock *lockfile.Lock
	for attempt := 0; ; attempt++ {
		lock, err = lockfile.Acquire(lockfile.ForDatabase(dbPath))
		if err == nil || !errors.Is(err, lockfile.ErrLocked) || attempt == 10 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		if errors.Is(err, lockfile.ErrLocked) {
			return fmt.Errorf("another trackz is already recording to %s: %w", dbPath, err)
		}
		return fmt.Errorf("lock %s: %w", dbPath, err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Printf("trackz: release lock: %v", err)
		}
	}()
	// "trackz stop" (on Unix it sends SIGTERM, handled above).
	stopListening, err := listenForStop(ctx, dbPath, func() {
		logger.Printf("trackz: stop requested")
		cancel()
	})
	if err != nil {
		return err
	}
	defer stopListening()

	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Printf("trackz: close database: %v", err)
		}
	}()

	tr := tracker.New(st, tracker.Options{
		LastEventIDFile: *idFile,
		HeartbeatFile:   heartbeatFile(dbPath),
		Logger:          logger,
		Debug:           *debug,
	})
	defer tr.Close()
	go tr.Heartbeat(ctx, 30*time.Second)

	opts := tracker.BackendOptions{NoPower: *noPower, DBusMonitor: *dbusMonitor}
	if *debug {
		opts.Debugf = func(format string, a ...any) { logger.Printf(format, a...) }
	}
	backend, err := tracker.NewBackend(opts)
	if err != nil {
		return err
	}
	logger.Printf("trackz %s: recording to %s using %s", version.Version, dbPath, backend.Name())
	if err := backend.Run(ctx, tr); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Printf("trackz: stopped")
	return nil
}

// cmdStop stops the trackz recording to the database and waits until it
// has closed its open record and released the database.
func cmdStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	dbFlag := fs.String("db", "", "database path (default: as for run)")
	timeout := fs.Duration("timeout", 10*time.Second, "how long to wait for trackz to stop")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dbPath, err := config.ResolveDBPath(*dbFlag, loadConfig())
	if err != nil {
		return err
	}
	lockPath := lockfile.ForDatabase(dbPath)
	held, pid, err := lockfile.Held(lockPath)
	if err != nil {
		return err
	}
	if !held {
		fmt.Println("trackz is not recording to", dbPath)
		return nil
	}
	if err := requestStop(dbPath, pid); err != nil {
		return fmt.Errorf("cannot stop trackz (PID %d): %w", pid, err)
	}
	deadline := time.Now().Add(*timeout)
	for {
		time.Sleep(100 * time.Millisecond)
		if held, _, err = lockfile.Held(lockPath); err != nil {
			return err
		}
		if !held {
			fmt.Printf("trackz (PID %d) stopped\n", pid)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("trackz (PID %d) did not stop within %s", pid, *timeout)
		}
	}
}

func cmdInitDB(args []string) error {
	fs := flag.NewFlagSet("init-db", flag.ExitOnError)
	dbFlag := fs.String("db", "", "database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := config.ResolveDBPath(*dbFlag, loadConfig())
	if err != nil {
		return err
	}
	st, err := store.Open(context.Background(), path)
	if err != nil {
		return err
	}
	fmt.Println("database ready:", path)
	return st.Close()
}

func cmdFixTimestamps(args []string) (err error) {
	fs := flag.NewFlagSet("fix-timestamps", flag.ExitOnError)
	dbFlag := fs.String("db", "", "database path")
	maxID := fs.Int64("max-id", 0, "only fix records with id <= this (default: all current records)")
	tz := fs.String("tz", "Local", "time zone the Lua hooks ran in")
	apply := fs.Bool("apply", false, "write the changes (default is a dry run)")
	fs.Usage = func() {
		_, _ = fmt.Fprint(fs.Output(), `Usage: trackz fix-timestamps [flags]

The devilspie2 Lua hooks stored times shifted by the local UTC offset
(e.g. one hour early during summer time). This adds the offset back so the
data is plain Unix time, like everything the Go tracker writes.

Run it ONCE, before (or right after) switching to the Go tracker, ideally
with -max-id set to the last record written by the Lua hooks. Running it
twice shifts the data twice. Back up the database first.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	loc, err := config.LoadLocation(*tz)
	if err != nil {
		return err
	}
	ctx := context.Background()
	path, err := config.ResolveDBPath(*dbFlag, loadConfig())
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := st.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if *maxID == 0 {
		if *maxID, err = st.MaxID(ctx); err != nil {
			return err
		}
	}
	fixes, err := st.FixLegacyTimestamps(ctx, *maxID, loc, !*apply)
	if err != nil {
		return err
	}
	for i, f := range fixes {
		if i == 10 {
			fmt.Printf("  ... and %d more\n", len(fixes)-10)
			break
		}
		fmt.Printf("  id %d: start %s -> %s\n", f.ID, ts(f.OldStart), ts(f.NewStart))
	}
	if *apply {
		fmt.Printf("updated %d records (ids <= %d) in %s\n", len(fixes), *maxID, path)
	} else {
		fmt.Printf("dry run: %d records (ids <= %d) would change; re-run with -apply to write\n", len(fixes), *maxID)
	}
	return nil
}

// heartbeatFile is per database, in the same runtime directory as the
// last-event-id file.
func heartbeatFile(dbPath string) string {
	sum := sha256.Sum256([]byte(dbPath))
	return filepath.Join(filepath.Dir(tracker.DefaultLastEventIDFile()),
		fmt.Sprintf("trackz-%x.heartbeat", sum[:6]))
}

func ts(sec int64) string { return time.Unix(sec, 0).UTC().Format("2006-01-02 15:04:05Z") }
