//go:build windows

package manualview

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

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

func (osOperator) Spawn(exe string, _ []string, cmdline, dir string, env []string) error {
	if !filepath.IsAbs(exe) || cmdline == "" {
		return errors.New("no command")
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	if env == nil {
		env = []string{}
	}
	cmd.Env = env
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       cmdline,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
