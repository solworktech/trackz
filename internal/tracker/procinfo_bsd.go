//go:build freebsd || openbsd || netbsd || dragonfly

package tracker

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// processInfo uses ps(1), whose output format is stable across the BSDs,
// unlike the kinfo_proc structures. The window's process is looked up once
// per focus change (results are cached), so the cost is negligible.
func processInfo(pid int) (procInfo, error) {
	out, err := exec.Command("ps", "-o", "ruser=", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return procInfo{}, err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 2 {
		return procInfo{}, fmt.Errorf("unexpected ps output %q", out)
	}
	return procInfo{Owner: fields[0], Name: strings.Join(fields[1:], " ")}, nil
}
