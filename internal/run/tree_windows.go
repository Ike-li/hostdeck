package run

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree puts the command in a job object, which every process it starts joins
// too. Closing the job kills whatever is still in it.
//
// The command joins the job just after it starts, so a process it starts in
// that moment would escape; WaitDelay still bounds the wait for it.
type tree struct {
	cmd *exec.Cmd
	job windows.Handle
}

func newTree(cmd *exec.Cmd) (*tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	cmd.Cancel = func() error {
		windows.TerminateJobObject(job, 1)
		return cmd.Process.Kill()
	}
	return &tree{cmd: cmd, job: job}, nil
}

func (t *tree) started() {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.AssignProcessToJobObject(t.job, h)
}

func (t *tree) close() {
	windows.CloseHandle(t.job)
}
