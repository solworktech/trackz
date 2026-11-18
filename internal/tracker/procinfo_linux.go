//go:build linux

package tracker

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
)

// processInfo reads /proc. The process name is /proc/<pid>/comm, which is
// what devilspie2's get_process_name() returned (at most 15 characters), so
// names stay consistent with existing data. The owner is the real UID.
func processInfo(pid int) (procInfo, error) {
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return procInfo{}, err
	}
	info := procInfo{Name: string(bytes.TrimSpace(comm))}
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return info, nil
	}
	defer func() { _ = f.Close() }() // read-only
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Uid:") {
			if fields := strings.Fields(line); len(fields) > 1 {
				info.Owner = userName(fields[1])
			}
			break
		}
	}
	return info, nil
}
