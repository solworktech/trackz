// Command trackz-viz shows charts and reports of the data recorded by trackz.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

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
	flag.Parse()
	if *showVersion {
		fmt.Println("trackz-viz", version.Version)
		return
	}

	// One viewer per user: starting it again brings the running one to the
	// front (reopening its window if it was closed to the tray).
	activate := make(chan struct{}, 1)
	inst, err := instance.Start("trackz-viz", func() {
		select {
		case activate <- struct{}{}:
		default: // one pending request is enough
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
			for range activate {
				ui.Raise()
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
