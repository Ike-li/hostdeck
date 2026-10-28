package observation

import (
	"golang.org/x/sys/unix"
	"time"
)

func ProcessIdentity(pid int) Identity {
	if pid <= 1 {
		return Identity{}
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info.Proc.P_stat == 5 {
		return Identity{}
	}
	return Identity{pid, time.Unix(info.Proc.P_starttime.Sec, int64(info.Proc.P_starttime.Usec)*1000)}
}
