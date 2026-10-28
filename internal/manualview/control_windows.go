//go:build windows

package manualview

import (
	"errors"

	"golang.org/x/sys/windows"
)

func (osOperator) Term(pid int) error { return terminate(pid) }

func (osOperator) Kill(pid int) error { return terminate(pid) }

func terminate(pid int) error {
	if pid <= 1 {
		return errors.New("refused")
	}
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, 1)
}

func (osOperator) Alive(pid int) bool {
	if pid <= 1 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == 259
}
