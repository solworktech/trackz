//go:build darwin

package tracker

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// processInfo uses sysctl(kern.proc.pid). p_comm is the executable name
// truncated to 16 characters, the macOS equivalent of Linux's comm.
func processInfo(pid int) (procInfo, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return procInfo{}, err
	}
	return procInfo{
		Name:  unix.ByteSliceToString(kp.Proc.P_comm[:]),
		Owner: userName(uidString(kp.Eproc.Pcred.P_ruid)),
	}, nil
}

func uidString(uid uint32) string { return strconv.FormatUint(uint64(uid), 10) }
