//go:build windows

package listenerview

import (
	"encoding/binary"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const maxInspectArgs = 1 << 20

func platformInspect(pid int) inspection {
	var out inspection
	access := uint32(windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_VM_READ | windows.PROCESS_QUERY_LIMITED_INFORMATION)
	handle, err := windows.OpenProcess(access, false, uint32(pid))
	if err != nil {
		return out
	}
	defer windows.CloseHandle(handle)
	if started, ok := processStarted(handle); ok {
		out.Started = started
	}
	params, ok := processParams(handle)
	if !ok {
		return out
	}
	if cmd, ok := readString(handle, params.CommandLine); ok && len(cmd) <= maxInspectArgs {
		out.CommandLine, out.KnowsArgs = cmd, true
	}
	if dir, ok := readString(handle, params.CurrentDirectory.DosPath); ok {
		out.Dir, out.KnowsDir = dir, true
	}
	return out
}

func processParams(handle windows.Handle) (windows.RTL_USER_PROCESS_PARAMETERS, bool) {
	var info windows.PROCESS_BASIC_INFORMATION
	var got uint32
	err := windows.NtQueryInformationProcess(handle, windows.ProcessBasicInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), &got)
	if err != nil || info.PebBaseAddress == nil {
		return windows.RTL_USER_PROCESS_PARAMETERS{}, false
	}
	var peb windows.PEB
	if !readAt(handle, uintptr(unsafe.Pointer(info.PebBaseAddress)), unsafe.Pointer(&peb), unsafe.Sizeof(peb)) || peb.ProcessParameters == nil {
		return windows.RTL_USER_PROCESS_PARAMETERS{}, false
	}
	var params windows.RTL_USER_PROCESS_PARAMETERS
	if !readAt(handle, uintptr(unsafe.Pointer(peb.ProcessParameters)), unsafe.Pointer(&params), unsafe.Sizeof(params)) {
		return windows.RTL_USER_PROCESS_PARAMETERS{}, false
	}
	return params, true
}

func readString(handle windows.Handle, s windows.NTUnicodeString) (string, bool) {
	if s.Buffer == nil || s.Length == 0 || s.Length%2 != 0 || int(s.Length) > maxInspectArgs {
		return "", false
	}
	buf := make([]byte, s.Length)
	if !readAt(handle, uintptr(unsafe.Pointer(s.Buffer)), unsafe.Pointer(&buf[0]), uintptr(len(buf))) {
		return "", false
	}
	return utf16Bytes(buf), true
}

func utf16Bytes(buf []byte) string {
	units := make([]uint16, len(buf)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(buf[i*2:])
	}
	return string(utf16.Decode(units))
}

func readAt(handle windows.Handle, addr uintptr, dst unsafe.Pointer, n uintptr) bool {
	if addr == 0 || n == 0 {
		return false
	}
	var got uintptr
	return windows.ReadProcessMemory(handle, addr, (*byte)(dst), n, &got) == nil && got == n
}

func processStarted(handle windows.Handle) (time.Time, bool) {
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exit, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}
