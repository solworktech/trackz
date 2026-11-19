package main

import (
	"errors"
	"log"
	"os/exec"
	"strings"
)

// systemdUnit is the systemd user unit that runs trackz
// (packaging/systemd/trackz.service).
const systemdUnit = "trackz.service"

// cmdAutostart is `trackz run` for the XDG autostart entry, which the
// desktop starts at login whether or not systemd starts trackz too. It
// leaves the session to the unit where the unit runs: on desktops that
// start graphical-session.target (GNOME, KDE Plasma), the target the unit
// is wanted by. Elsewhere (Xfce, MATE, no systemd) it records itself.
func cmdAutostart(args []string) error {
	if reason := unitHandles(); reason != "" {
		log.Printf("trackz: not starting: %s", reason)
		return nil
	}
	return cmdRun(args)
}

// unitHandles says why the systemd unit, not the autostart entry, decides
// whether trackz runs in this session, or returns "" if it does not.
func unitHandles() string {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return ""
	}
	// is-enabled prints the state and exits non-zero for most of them.
	out, err := exec.Command("systemctl", "--user", "is-enabled", systemdUnit).Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return ""
	}
	switch state := strings.TrimSpace(string(out)); state {
	case "masked", "masked-runtime":
		// The user's way to opt out of the unit; it covers this too.
		return systemdUnit + " is masked"
	case "enabled", "enabled-runtime":
		if exec.Command("systemctl", "--user", "--quiet", "is-active", "graphical-session.target").Run() == nil {
			return "systemd starts " + systemdUnit + " in this session"
		}
	}
	return ""
}
