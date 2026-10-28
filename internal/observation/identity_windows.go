package observation

import (
	"golang.org/x/sys/windows"
	"time"
)

func ProcessIdentity(pid int) Identity {
	if pid <= 1 {
		return Identity{}
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return Identity{}
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 {
		return Identity{}
	}
	var created, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exit, &kernel, &user) != nil {
		return Identity{}
	}
	return Identity{pid, time.Unix(0, created.Nanoseconds())}
}
