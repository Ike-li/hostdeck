//go:build windows

package listenerview

import (
	"context"
	"fmt"
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

func readPlatform(ctx context.Context, _ commandRunner) ([]Socket, map[int]Proc, error) {
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
	seen := map[string]bool{}
	var sockets []Socket
	for _, row := range rows {
		if _, ok := procs[row.PID]; !ok || row.Port <= 0 {
			continue
		}
		key := fmt.Sprintf("%d|%s|%d", row.PID, row.Addr, row.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		sockets = append(sockets, Socket{PID: row.PID, Port: row.Port, Addr: row.Addr, Command: procs[row.PID].Name})
	}
	return sockets, procs, nil
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
