// Command trackz-viz shows charts and reports of the data recorded by trackz.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/unit"

	"github.com/solworktech/trackz/internal/instance"
	"github.com/solworktech/trackz/internal/report"
	"github.com/solworktech/trackz/internal/version"
	giou "github.com/solworktech/trackz/ui/gio"
)

func main() {
	dbFlag := flag.String("db", "", "database path (default: $TRACKZ_DB, config file, or per-user data dir)")
	cfgFlag := flag.String("config", "", "config file (default: $TRACKZ_CONFIG or per-user config dir)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	quit := flag.Bool("quit", false, "ask the running trackz-viz to quit, and wait for it to exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("trackz-viz", version.Version)
		return
	}
	if *quit {
		if err := quitRunning(10 * time.Second); err != nil {
			log.Fatalf("trackz-viz: %v", err)
		}
		return
	}

	// One viewer per user: starting it again brings the running one to the
	// front (reopening its window if it was closed to the tray).
	requests := make(chan instance.Request, 2)
	inst, err := instance.Start("trackz-viz", func(req instance.Request) {
		select {
		case requests <- req:
		default: // the same request is already pending
		}
	})
	if errors.Is(err, instance.ErrRunning) {
		fmt.Fprintln(os.Stderr, "trackz-viz is already running; showing its window")
		return
	}
	if err != nil {
		log.Printf("trackz-viz: warning: cannot check for a running viewer: %v", err)
	}

	svc, err := report.Open(context.Background(), report.Options{DBPath: *dbFlag, ConfigPath: *cfgFlag})
	if svc == nil {
		log.Fatalf("trackz-viz: %v", err)
	}
	if err != nil {
		log.Printf("trackz-viz: warning: %v", err)
	}

	go func() {
		// The database in the title makes it obvious when the viewer reads a
		// different file than the tracker writes.
		ui := giou.New(svc,
			app.Title("Trackz Visualisation"),
			app.Size(unit.Dp(1200), unit.Dp(820)),
			app.MinSize(unit.Dp(760), unit.Dp(520)))
		go func() {
			for req := range requests {
				switch req {
				case instance.Activate:
					ui.Raise()
				case instance.Quit:
					ui.Quit()
				}
			}
		}()
		err := ui.Run()
		_ = svc.Close()
		if inst != nil {
			if cerr := inst.Close(); cerr != nil {
				log.Printf("trackz-viz: %v", cerr)
			}
		}
		if err != nil {
			log.Printf("trackz-viz: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

// quitRunning asks the running trackz-viz to quit and waits until it has
// (the installer runs this before replacing its files).
func quitRunning(timeout time.Duration) error {
	switch err := instance.Send("trackz-viz", instance.Quit); {
	case errors.Is(err, instance.ErrNotRunning):
		fmt.Println("trackz-viz is not running")
		return nil
	case err != nil:
		return err
	}
	for deadline := time.Now().Add(timeout); ; {
		running, err := instance.Running("trackz-viz")
		if err != nil || !running {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the running trackz-viz did not quit within %s", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
