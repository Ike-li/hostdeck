//go:build windows

package manualview

import (
	"context"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const unixReader = false

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	afInet                   = 2
	afInet6                  = 23
	tcpTableOwnerPIDListener = 3
	errorInsufficientBuffer  = 122
)

func readPlatform(ctx context.Context, _ commandRunner) (map[int]Listener, map[int]Proc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	procs, err := processTree()
	if err != nil {
		return nil, nil, err
	}
	if len(procs) == 0 {
		return nil, nil, &readFailure{en: "Could not list processes.", zh: "读不到进程列表。"}
	}
	rows, err := listeningSockets()
	if err != nil {
		return nil, nil, err
	}
	listeners := map[int]Listener{}
	for _, row := range rows {
		proc, ok := procs[row.PID]
		if !ok || row.Port <= 0 {
			continue
		}
		up, ok := processUptime(row.PID)
		if !ok {
			continue
		}
		proc.Uptime = up
		procs[row.PID] = proc
		l := listeners[row.PID]
		if l.Command == "" {
			l.Command = proc.Name
		}
		l.Ports = append(l.Ports, row.Port)
		listeners[row.PID] = l
	}
	return listeners, procs, nil
}

func listeningSockets() ([]tableRow, error) {
	v4, err := tcpTable(afInet)
	if err != nil {
		return nil, &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	v6, err := tcpTable(afInet6)
	if err != nil {
		return nil, &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	a, err := ipv4Listeners(v4)
	if err != nil {
		return nil, &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	b, err := ipv6Listeners(v6)
	if err != nil {
		return nil, &readFailure{en: "Could not list listening ports.", zh: "读不到正在监听的端口。"}
	}
	return append(a, b...), nil
}

func tcpTable(family uintptr) ([]byte, error) {
	var size uint32
	code, _, _ := getExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPIDListener, 0)
	if code != 0 && code != errorInsufficientBuffer {
		return nil, fmt.Errorf("GetExtendedTcpTable: %d", code)
	}
	if size < 4 {
		size = 4
	}
	buf := make([]byte, size)
	code, _, _ = getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPIDListener, 0)
	if code == errorInsufficientBuffer {
		buf = make([]byte, size)
		code, _, _ = getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPIDListener, 0)
	}
	if code != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable: %d", code)
	}
	if int(size) < len(buf) {
		buf = buf[:size]
	}
	return buf, nil
}

func processTree() (map[int]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, &readFailure{en: "Could not list processes.", zh: "读不到进程列表。"}
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err = windows.Process32First(snap, &entry); err != nil {
		return nil, &readFailure{en: "Could not list processes.", zh: "读不到进程列表。"}
	}
	procs := map[int]Proc{}
	for {
		procs[int(entry.ProcessID)] = Proc{PPID: int(entry.ParentProcessID), Name: windows.UTF16ToString(entry.ExeFile[:])}
		if err = windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return procs, nil
}

// processUptime reads the listener's start time. A process that cannot be
// opened is treated as already gone, so the card does not invent a duration.
func processUptime(pid int) (time.Duration, bool) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(handle)
	var created, exit, kernel, user windows.Filetime
	if err = windows.GetProcessTimes(handle, &created, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	up := time.Since(time.Unix(0, created.Nanoseconds()))
	if up < 0 {
		return 0, true
	}
	return up, true
}
